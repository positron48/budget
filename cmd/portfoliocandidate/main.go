// portfoliocandidate creates a separate database and prepares a complete,
// source-bound portfolio replacement. Existing Budget tables are never written.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	pg "github.com/positron48/budget/internal/adapter/postgres"
	archive "github.com/positron48/budget/internal/usecase/investmentarchive"
)

func privateRead(path string, v any) error {
	f, e := os.Open(path)
	if e != nil {
		return errors.New("cannot read candidate input")
	}
	defer func() { _ = f.Close() }()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > archive.MaxBytes {
		return errors.New("candidate input must be a private regular file")
	}
	decoder := json.NewDecoder(io.LimitReader(f, archive.MaxBytes+1))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(v); e != nil {
		return errors.New("invalid candidate input")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("trailing candidate data")
	}
	return nil
}
func privateWrite(path string, v any) error {
	raw, e := json.Marshal(v)
	if e != nil {
		return e
	}
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return errors.New("candidate output must be a new private file")
	}
	if _, e = f.Write(raw); e != nil {
		_ = f.Close()
		return errors.New("candidate write failed")
	}
	if e = f.Sync(); e != nil {
		_ = f.Close()
		return errors.New("candidate flush failed")
	}
	return f.Close()
}
func candidateURL(admin, name string) (string, error) {
	if len(name) > 63 || !regexp.MustCompile(`^budget_portfolio_candidate_[a-z0-9_]{1,64}$`).MatchString(name) {
		return "", errors.New("isolated candidate database name required")
	}
	parsed, e := url.Parse(admin)
	if e != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Hostname() == "" || parsed.Path == "/"+name {
		return "", errors.New("valid separate PostgreSQL source/admin URL required")
	}
	parsed.Path = "/" + name
	parsed.RawPath = ""
	return parsed.String(), nil
}
func run(args []string) error {
	flags := flag.NewFlagSet("portfoliocandidate", flag.ContinueOnError)
	beforeFile := flags.String("before", "", "private current source archive")
	planFile := flags.String("plan", "", "private original/derived CSV and verified source controls")
	marketFile := flags.String("market", "", "private verified FX and exchange evidence")
	actor := flags.String("actor", "", "actual source owner/admin UUID")
	database := flags.String("database-name", "", "new isolated database beginning budget_portfolio_candidate_")
	output := flags.String("output", "", "new private canonical candidate archive")
	reportFile := flags.String("report", "", "new private report")
	if flags.Parse(args) != nil {
		return errors.New("invalid candidate flags")
	}
	id, e := uuid.Parse(*actor)
	if e != nil || id.String() != *actor || *beforeFile == "" || *planFile == "" || *marketFile == "" || *output == "" || *reportFile == "" || *output == *reportFile || flags.NArg() != 0 {
		return errors.New("before, plan, market, actor, database-name, output and report are required")
	}
	for _, p := range []string{*output, *reportFile} {
		if _, e = os.Lstat(p); !os.IsNotExist(e) {
			return errors.New("candidate outputs already exist or unavailable")
		}
	}
	admin := os.Getenv("PORTFOLIO_SOURCE_ADMIN_URL")
	if admin == "" {
		return errors.New("PORTFOLIO_SOURCE_ADMIN_URL required")
	}
	target, e := candidateURL(admin, *database)
	if e != nil {
		return e
	}
	var before archive.Bundle
	if e = privateRead(*beforeFile, &before); e != nil {
		return e
	}
	raw, e := json.Marshal(before)
	if e != nil {
		return e
	}
	before, e = archive.Decode(bytes.NewReader(raw))
	if e != nil {
		return errors.New("source archive integrity failed")
	}
	var plan pg.PortfolioCandidateInput
	if e = privateRead(*planFile, &plan); e != nil {
		return e
	}
	var market pg.PortfolioMarketInput
	if e = privateRead(*marketFile, &market); e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	adminPool, e := pg.NewPool(ctx, admin)
	if e != nil {
		return errors.New("source/admin connection failed")
	}
	defer adminPool.Close()
	if _, e = adminPool.DB.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{*database}.Sanitize()); e != nil {
		return errors.New("cannot create fresh isolated candidate database")
	}
	probe, e := pg.NewPool(ctx, target)
	if e != nil {
		return errors.New("candidate scope connection failed")
	}
	var actualDatabase string
	e = probe.DB.QueryRow(ctx, `SELECT current_database()`).Scan(&actualDatabase)
	probe.Close()
	if e != nil || actualDatabase != *database {
		return errors.New("candidate database scope mismatch; migrations refused")
	}
	if e = pg.RunMigrations(target); e != nil {
		return errors.New("isolated candidate migrations failed")
	}
	pool, e := pg.NewPool(ctx, target)
	if e != nil {
		return errors.New("candidate connection failed")
	}
	defer pool.Close()
	var schemaVersion int64
	var schemaDirty bool
	if e = pool.DB.QueryRow(ctx, `SELECT version,dirty FROM schema_migrations`).Scan(&schemaVersion, &schemaDirty); e != nil || schemaDirty || schemaVersion != 61 {
		return errors.New("candidate schema61 required")
	}
	built, e := pg.NewInvestmentRepo(pool).BuildPortfolioCandidate(ctx, *actor, before, plan)
	if e != nil {
		return errors.New("isolated source candidate build failed; database retained for diagnosis")
	}
	marketRepo := pg.NewMarketDataRepo(pool)
	if e = marketRepo.ConfigurePortfolioMarket(ctx, before.Data.TenantID, *actor, market); e != nil {
		return errors.New("isolated market evidence publication failed; database retained")
	}
	coverage, e := pg.NewInvestmentRepo(pool).VerifyPortfolioMarketCoverage(ctx, before.Data.TenantID, *actor, market)
	if e != nil {
		return errors.New("candidate market coverage incomplete for actual source currencies/dates")
	}

	built.Archive, e = pg.NewInvestmentRepo(pool).ExportInvestmentArchive(ctx, before.Data.TenantID, *actor)
	if e != nil {
		return errors.New("candidate export failed")
	}
	if _, e = archive.VerifyJournal(built.Archive.Data); e != nil {
		return errors.New("candidate archive replay failed")
	}
	stateSHA, e := pg.InvestmentArchiveStateSHA256(before)
	if e != nil {
		return e
	}
	if e = privateWrite(*output, built.Archive); e != nil {
		return e
	}
	summary := map[string]any{"Status": "isolated_portfolio_candidate_prepared", "SourceWrites": false, "CandidateDatabase": *database, "BeforeStateSHA256": stateSHA, "ArchiveSHA256": built.Archive.SHA256, "MarketSHA256": pg.PortfolioMarketSHA256(market), "Accounts": built.Ledgers, "Operations": built.Operations, "Payouts": built.Payouts, "PendingSourceRows": built.PendingRows, "FreshQuotes": coverage.FreshQuotes, "RequiredCurrencyDates": coverage.RequiredCurrencyDates, "FXChecks": coverage.FXChecks, "SchemaVersion": schemaVersion}
	if e = privateWrite(*reportFile, summary); e != nil {
		return e
	}
	return json.NewEncoder(os.Stdout).Encode(summary)
}
func main() {
	if e := run(os.Args[1:]); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
