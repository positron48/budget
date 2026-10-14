// portfolioassets links reviewed manual investment estimates to simple trackers.
// Credentials are accepted only through DATABASE_URL; no migrations are applied.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/positron48/budget/internal/adapter/postgres"
)

func readLinks(path string) ([]postgres.TrackerAssetLink, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, errors.New("cannot open private link input")
	}
	defer func() { _ = f.Close() }()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 16<<20 {
		return nil, errors.New("link input must be a private regular file")
	}
	decoder := json.NewDecoder(io.LimitReader(f, 16<<20+1))
	decoder.DisallowUnknownFields()
	var links []postgres.TrackerAssetLink
	if decoder.Decode(&links) != nil || decoder.Decode(new(any)) != io.EOF || len(links) < 1 || len(links) > 20 {
		return nil, errors.New("invalid link input")
	}
	return links, nil
}

func run() error {
	input := flag.String("input", "", "private reviewed link file")
	plan := flag.Bool("plan", false, "print typed checksum without database access")
	ledger := flag.String("scope-ledger", "", "existing standalone ledger identifying the authorized tenant")
	tenant := flag.String("expected-tenant", "", "tenant identity from the reviewed scope file")
	expected := flag.String("expected-sha256", "", "exact reviewed typed input checksum")
	apply := flag.Bool("apply", false, "commit after checking the complete link batch; default rolls back")
	flag.Parse()
	if *input == "" || flag.NArg() != 0 || *plan && (*ledger != "" || *tenant != "" || *expected != "" || *apply) || !*plan && (*ledger == "" || *tenant == "" || *expected == "") {
		return errors.New("input and either plan or exact scope/checksum required")
	}
	links, e := readLinks(*input)
	if e != nil {
		return e
	}
	sha := postgres.TrackerAssetLinksSHA256(links)
	if *plan {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"Status": "link_plan", "SHA256": sha, "Links": len(links), "SourceWrites": false, "DatabaseAccess": false})
	}
	if sha != *expected {
		return errors.New("reviewed link checksum mismatch")
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool, e := postgres.NewPool(ctx, dsn)
	if e != nil {
		return errors.New("database connection failed")
	}
	defer pool.Close()
	scope, e := postgres.NewInvestmentRepo(pool).InspectPortfolioScope(ctx, *ledger)
	if e != nil || scope.TenantID != *tenant || scope.SchemaVersion != 61 || scope.SchemaDirty {
		return errors.New("clean schema61 and active standalone portfolio scope required")
	}
	result, e := postgres.NewInvestmentRepo(pool).LinkTrackerAssets(ctx, scope.TenantID, scope.ActorID, links, sha, *apply)
	if e != nil {
		return errors.New("link import did not confirm completion; retry the same reviewed input; raw database diagnostics withheld")
	}
	status := "tracker_asset_links_rehearsed"
	if *apply {
		status = "tracker_assets_linked"
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"Status": status, "SourceWrites": *apply, "InvestmentAssetWrites": *apply, "BookWrites": false, "OrdinaryAssetWrites": false, "Links": len(result), "InputSHA256": sha, "Results": result})
}

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
