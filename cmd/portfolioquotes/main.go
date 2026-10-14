// portfolioquotes imports reviewed quote-only mappings without reloading a book.
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

func readQuotes(path string) ([]postgres.PortfolioQuoteSeed, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, errors.New("cannot open private quote input")
	}
	defer func() { _ = f.Close() }()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 16<<20 {
		return nil, errors.New("quote input must be a private regular file")
	}
	decoder := json.NewDecoder(io.LimitReader(f, 16<<20+1))
	decoder.DisallowUnknownFields()
	var quotes []postgres.PortfolioQuoteSeed
	if decoder.Decode(&quotes) != nil || decoder.Decode(new(any)) != io.EOF || len(quotes) < 1 || len(quotes) > 500 {
		return nil, errors.New("invalid quote input")
	}
	return quotes, nil
}

func run() error {
	input := flag.String("input", "", "private reviewed quote file")
	plan := flag.Bool("plan", false, "print typed checksum without database access")
	ledger := flag.String("scope-ledger", "", "existing standalone ledger identifying the authorized tenant")
	tenant := flag.String("expected-tenant", "", "tenant identity from the reviewed scope file")
	expected := flag.String("expected-sha256", "", "exact reviewed typed input checksum")
	apply := flag.Bool("apply", false, "commit after checking the complete quote batch; default rolls back")
	flag.Parse()
	if *input == "" || flag.NArg() != 0 || *plan && (*ledger != "" || *tenant != "" || *expected != "" || *apply) || !*plan && (*ledger == "" || *tenant == "" || *expected == "") {
		return errors.New("input and either plan or exact scope/checksum required")
	}
	quotes, e := readQuotes(*input)
	if e != nil {
		return e
	}
	sha := postgres.PortfolioQuotesSHA256(quotes)
	if *plan {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"Status": "quote_plan", "SHA256": sha, "Quotes": len(quotes), "SourceWrites": false, "DatabaseAccess": false})
	}
	if sha != *expected {
		return errors.New("reviewed quote checksum mismatch")
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
	result, e := postgres.NewMarketDataRepo(pool).ImportPortfolioQuotes(ctx, scope.TenantID, scope.ActorID, quotes, sha, *apply)
	if e != nil {
		return errors.New("quote import did not confirm completion; retry the same reviewed input; raw database diagnostics withheld")
	}
	status := "quote_import_rehearsed"
	if *apply {
		status = "quotes_imported"
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"Status": status, "SourceWrites": *apply, "MarketWrites": *apply, "BookWrites": false, "HouseholdWrites": false, "Quotes": len(result.Quotes), "InputSHA256": sha})
}

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
