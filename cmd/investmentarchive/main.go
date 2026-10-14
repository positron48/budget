// investmentarchive exports, verifies, rehearses and restores investment data.
// Output files are private and exclusive; restore requires an explicit checksum.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/positron48/budget/internal/adapter/postgres"
	archive "github.com/positron48/budget/internal/usecase/investmentarchive"
)

func writeArchive(path string, bundle archive.Bundle) (resultErr error) {
	data, e := json.Marshal(bundle)
	if e != nil {
		return errors.New("archive encoding failed")
	}
	file, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return errors.New("cannot create private archive; destination must not exist")
	}
	ok := false
	defer func() {
		_ = file.Close() // The successful write path checks its final close below.
		if !ok {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				resultErr = errors.Join(resultErr, errors.New("cannot remove incomplete archive"))
			}
		}
	}()
	if _, e = file.Write(data); e != nil {
		return errors.New("archive write failed")
	}
	if e = file.Sync(); e != nil {
		return errors.New("archive flush failed")
	}
	if e = file.Close(); e != nil {
		return errors.New("archive close failed")
	}
	ok = true
	return nil
}
func run() error {
	tenant := flag.String("tenant", "", "tenant UUID")
	actor := flag.String("actor", "", "owner/admin UUID")
	output := flag.String("output", "", "new private output file")
	verify := flag.String("verify", "", "verify an existing archive without database access")
	checkRestore := flag.String("check-restore", "", "rehearse restore into an empty matching tenant; always rolls back")
	restore := flag.String("restore", "", "restore into an empty matching tenant")
	expectedSHA := flag.String("expected-sha", "", "required reviewed checksum for restore")
	replace := flag.String("replace", "", "atomically replace a matching standalone portfolio archive")
	checkReplace := flag.String("check-replace", "", "rehearse portfolio replacement; always rolls back")
	beforeState := flag.String("before-state-sha256", "", "canonical investment state digest from reviewed current export")
	scopeLedger := flag.String("scope-ledger", "", "inspect portfolio account identities/counters only, using the existing ledger creator")
	marketFile := flag.String("market-file", "", "optional private market evidence included in the same replacement transaction")
	marketSHA := flag.String("expected-market-sha256", "", "reviewed market evidence digest")
	flag.Parse()
	if (*marketFile != "") != (*marketSHA != "") {
		return errors.New("market-file and expected-market-sha256 are required together")
	}
	if *marketFile != "" && *replace == "" && *checkReplace == "" {
		return errors.New("market evidence requires replacement")
	}
	if *scopeLedger != "" {
		if *replace != "" || *checkReplace != "" || *beforeState != "" || *restore != "" || *checkRestore != "" || *verify != "" || *output != "" || *tenant != "" || *actor != "" || *expectedSHA != "" || flag.NArg() != 0 {
			return errors.New("scope-ledger cannot be combined with archive modes")
		}
		return inspectScope(*scopeLedger)
	}
	if *replace != "" || *checkReplace != "" {
		if (*replace != "" && *checkReplace != "") || *restore != "" || *checkRestore != "" || *verify != "" || *output != "" || *tenant != "" || *actor == "" || *expectedSHA == "" || *beforeState == "" || flag.NArg() != 0 {
			return errors.New("replacement requires actor, expected-sha and before-state-sha256, with exactly one replacement mode")
		}
		path := *replace
		if path == "" {
			path = *checkReplace
		}
		return replaceArchive(path, *actor, *expectedSHA, *beforeState, *marketFile, *marketSHA, *replace != "")
	}
	if *beforeState != "" {
		return errors.New("before-state-sha256 requires replacement")
	}
	if *restore != "" {
		if *checkRestore != "" || *verify != "" || *output != "" || *tenant != "" || *actor == "" || *expectedSHA == "" {
			return errors.New("restore requires actor and expected-sha, and cannot be combined with another mode")
		}
		return restoreArchive(*restore, *actor, *expectedSHA, true)
	}
	if *expectedSHA != "" {
		return errors.New("expected-sha requires restore")
	}

	if *checkRestore != "" {
		if *verify != "" || *output != "" || *tenant != "" || *actor == "" {
			return errors.New("check-restore requires actor and cannot be combined with verify, output or tenant")
		}
		return restoreArchive(*checkRestore, *actor, "", false)
	}
	if *verify != "" {
		if *output != "" || *tenant != "" || *actor != "" {
			return errors.New("verify cannot be combined with export flags")
		}
		file, e := os.Open(*verify)
		if e != nil {
			return errors.New("cannot open archive")
		}
		defer func() { _ = file.Close() }()
		bundle, e := archive.Decode(file)
		if e != nil {
			return e
		}
		verification, e := archive.VerifyJournal(bundle.Data)
		if e != nil {
			return e
		}
		fmt.Printf("Archive integrity and journal replay verified: %d ledgers, %d operations; SHA256 %s\n", verification.Ledgers, verification.Operations, bundle.SHA256)
		return nil
	}
	if *output == "" || *tenant == "" || *actor == "" {
		return errors.New("tenant, actor and output are required")
	}
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return errors.New("DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db, e := postgres.NewPool(ctx, url)
	if e != nil {
		return errors.New("database connection failed")
	}
	defer db.Close()
	bundle, e := postgres.NewInvestmentRepo(db).ExportInvestmentArchive(ctx, *tenant, *actor)
	if e != nil {
		return errors.New("export failed; check membership, schema classification and archive limits")
	}
	if e = writeArchive(*output, bundle); e != nil {
		return e
	}
	state, e := postgres.InvestmentArchiveStateSHA256(bundle)
	if e != nil {
		return errors.New("cannot fingerprint investment archive")
	}
	fmt.Printf("Archive written: %d tables; SHA256 %s; state SHA256 %s\n", len(bundle.Data.Tables), bundle.SHA256, state)
	return nil
}
func restoreArchive(path, actor, expectedSHA string, apply bool) error {
	file, err := os.Open(path)
	if err != nil {
		return errors.New("cannot open archive")
	}
	defer func() { _ = file.Close() }()
	bundle, err := archive.Decode(file)
	if err != nil {
		return err
	}
	if apply && (expectedSHA == "" || expectedSHA != bundle.SHA256) {
		return errors.New("restore checksum mismatch")
	}
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return errors.New("DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db, err := postgres.NewPool(ctx, url)
	if err != nil {
		return errors.New("database connection failed")
	}
	defer db.Close()
	var result archive.JournalVerification
	status := "Restore rehearsal rolled back"
	if apply {
		result, err = postgres.NewInvestmentRepo(db).RestoreInvestmentArchive(ctx, actor, bundle, expectedSHA)
		status = "Restore committed (or previously committed)"
	} else {
		result, err = postgres.NewInvestmentRepo(db).CheckInvestmentArchiveRestore(ctx, actor, bundle)
	}
	if err != nil {
		return errors.New("restore failed; check checksum, authorization, empty tenant, actors, schema and constraints")
	}
	fmt.Printf("%s: %d ledgers, %d operations; SHA256 %s\n", status, result.Ledgers, result.Operations, bundle.SHA256)
	return nil
}
func replaceArchive(path, actor, expectedSHA, beforeState, marketFile, marketSHA string, apply bool) error {
	file, e := os.Open(path)
	if e != nil {
		return errors.New("cannot open replacement archive")
	}
	defer func() { _ = file.Close() }()
	bundle, e := archive.Decode(file)
	if e != nil {
		return e
	}
	if expectedSHA != bundle.SHA256 {
		return errors.New("replacement checksum mismatch")
	}
	market, e := readReplacementMarket(marketFile, marketSHA)
	if e != nil {
		return e
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	pool, e := postgres.NewPool(ctx, dsn)
	if e != nil {
		return errors.New("database connection failed")
	}
	defer pool.Close()
	result, retry, e := postgres.NewInvestmentRepo(pool).ReplacePortfolioArchive(ctx, actor, bundle, postgres.PortfolioArchiveReplacement{ExpectedStateSHA256: beforeState, ExpectedArchiveSHA256: expectedSHA, Market: market, ExpectedMarketSHA256: marketSHA}, apply)
	if e != nil {
		return errors.New("portfolio replacement failed; check current state digest, authorization, matching standalone accounts, archive and constraints")
	}
	status := "replacement_rehearsal_rolled_back"
	if apply {
		status = "portfolio_replaced"
	}
	if retry {
		status = "replacement_already_applied"
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"Status": status, "ArchiveSHA256": bundle.SHA256, "Verification": result, "HouseholdWrites": false, "MarketEvidenceIncluded": market != nil, "MarketSHA256": marketSHA})
}

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
