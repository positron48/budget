package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	archive "github.com/positron48/budget/internal/usecase/investmentarchive"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

// Optional private fixtures are exercised only in disposable testcontainers.
// No production connection, identity or financial fixture is embedded in code.
func TestInvestmentPrivatePortfolioImportRehearsal(t *testing.T) {
	dir := os.Getenv("INVESTMENT_PORTFOLIO_REHEARSAL_DIR")
	if dir == "" {
		t.Skip("private portfolio fixtures not provided")
	}
	read := func(name string, target any) {
		t.Helper()
		raw, e := os.ReadFile(filepath.Join(dir, name))
		if e != nil {
			t.Fatal("read private fixture", e)
		}
		if e = json.Unmarshal(raw, target); e != nil {
			t.Fatal("decode private fixture", e)
		}
	}
	var manifest struct {
		EmptyAccounts []struct {
			Name string `json:"name"`
		} `json:"empty_accounts"`
		Accounts []struct {
			Name string `json:"name"`
			CSV  string `json:"csv"`
			Rows []struct {
				Record       int    `json:"record"`
				SourceRecord int    `json:"source_record"`
				Reason       string `json:"reason"`
			} `json:"rows"`
		} `json:"accounts"`
	}
	read("manifest.json", &manifest)
	var expected []struct {
		Name       string
		Positions  []d.InvestmentPosition
		Operations []d.InvestmentOperation
		Failed     []json.RawMessage
	}
	read("source-replay.json", &expected)
	if len(manifest.Accounts) == 0 || len(manifest.Accounts) != len(expected) {
		t.Fatal("invalid private account fixture")
	}
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	repo := NewInvestmentRepo(pool)
	svc := inv.NewService(repo, NewTenantRepo(pool))
	importer := imp.NewImportService(NewInvestmentImportRepo(pool), NewTenantRepo(pool))
	type reportAccount struct {
		Name                 string
		Imported, Unresolved int
		Snapshot             d.InvestmentSnapshot
	}
	report := []reportAccount{}
	total := 0
	for i, a := range manifest.Accounts {
		if a.Name != expected[i].Name || filepath.Base(a.CSV) != a.CSV {
			t.Fatal("private account fixture mismatch")
		}
		ledger, e := svc.CreateLedgerWithDetails(ctx, tenant, actor, d.InvestmentLedger{Name: "Rehearsal " + string(rune('A'+i)), StartedAt: time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC), PortfolioTracking: true}, "rehearsal-"+string(rune('a'+i)))
		if e != nil {
			t.Fatal(e)
		}
		csv, e := os.ReadFile(filepath.Join(dir, a.CSV))
		if e != nil {
			t.Fatal("read private source", e)
		}
		upload := imp.Upload{Filename: a.CSV, RequestKey: "source-" + string(rune('a'+i)), CSV: csv, Accounts: []imp.ImportAccount{{SourceKey: "source", Name: "Rehearsal source"}}}
		for _, r := range a.Rows {
			upload.Assignments = append(upload.Assignments, imp.Assignment{Record: r.Record, AccountKey: "source", Evidence: "Private checksum-bound source allocation, original row lineage retained externally"})
		}
		source, e := importer.Upload(ctx, tenant, actor, upload)
		if e != nil {
			t.Fatal("persist private source", e)
		}
		var mapping imp.FinancialMapping
		read(a.CSV+".mapping.json", &mapping)
		for record := range mapping.LedgerByRecord {
			mapping.LedgerByRecord[record] = ledger.ID
		}
		mapped, e := importer.PreviewFinancial(ctx, tenant, actor, imp.FinancialReviewRequest{ImportID: source.ID, ExpectedImportVersion: source.Version, RequestKey: "review-" + string(rune('a'+i)), Mapping: mapping, Limit: 5000})
		if e != nil || mapped.Batch == nil {
			t.Fatal("financial mapping", e)
		}
		accepted := map[int64]bool{}
		for _, o := range expected[i].Operations {
			accepted[o.Sequence] = true
		}
		batch := *mapped.Batch
		batch.Entries = nil
		for _, entry := range mapped.Batch.Entries {
			if accepted[int64(entry.Record)] {
				batch.Entries = append(batch.Entries, entry)
			}
		}
		if len(batch.Entries) != len(expected[i].Operations) {
			t.Fatal("accepted row count mismatch")
		}
		preview, e := svc.PreviewImportBatch(ctx, tenant, actor, batch)
		if e != nil {
			t.Fatal("portfolio import preview", e)
		}
		committed, e := svc.CommitImportBatch(ctx, tenant, actor, batch, preview.Hash)
		if e != nil || !committed.Committed {
			t.Fatal("portfolio import commit", e)
		}
		repeated, e := svc.CommitImportBatch(ctx, tenant, actor, batch, preview.Hash)
		if e != nil || repeated.Hash != committed.Hash {
			t.Fatal("portfolio import retry", e)
		}
		restarted := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
		state, e := restarted.Snapshot(ctx, tenant, actor, ledger.ID, time.Now().UTC())
		if e != nil {
			t.Fatal("restarted portfolio read", e)
		}
		if !reflect.DeepEqual(portfolioRehearsalValues(state.Positions), portfolioRehearsalValues(expected[i].Positions)) {
			t.Fatal("persisted portfolio quantities, native principal or acquisition dates changed")
		}
		if slices.Contains(state.Issues, "source_trades_pending") != (len(expected[i].Failed) > 0) {
			t.Fatal("pending source trades were not reported accurately")
		}
		var links int
		if e = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_source_links WHERE tenant_id=$1 AND ledger_id=$2`, tenant, ledger.ID).Scan(&links); e != nil || links != len(batch.Entries) {
			t.Fatal("source lineage count", e)
		}
		report = append(report, reportAccount{Name: a.Name, Imported: links, Unresolved: len(a.Rows) - links, Snapshot: state})
		total += links
	}
	emptyLedgerIDs := []string{}
	for i, a := range manifest.EmptyAccounts {
		ledger, e := svc.CreateLedgerWithDetails(ctx, tenant, actor, d.InvestmentLedger{Name: a.Name, StartedAt: time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC), PortfolioTracking: true}, "empty-"+string(rune('a'+i)))
		if e != nil {
			t.Fatal(e)
		}
		emptyLedgerIDs = append(emptyLedgerIDs, ledger.ID)
	}
	if _, e := pool.DB.Exec(ctx, `CREATE TABLE schema_migrations(version bigint PRIMARY KEY,dirty boolean NOT NULL); INSERT INTO schema_migrations VALUES(61,false)`); e != nil {
		t.Fatal(e)
	}
	scope, e := repo.InspectPortfolioScope(ctx, report[0].Snapshot.Ledger.ID)
	if e != nil || scope.TenantID != tenant || scope.ActorID != actor || len(scope.Accounts) != len(report)+len(emptyLedgerIDs) || scope.SchemaDirty {
		t.Fatal("operator scope inspection", e)
	}
	bundle, e := repo.ExportInvestmentArchive(ctx, tenant, actor)
	if e != nil {
		t.Fatal("portfolio export", e)
	}
	bundleJSON, e := json.Marshal(bundle)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, "database-archive.json"), bundleJSON, 0600); e != nil {
		t.Fatal(e)
	}
	verified, e := archive.VerifyJournal(bundle.Data)
	if e != nil || verified.Operations != total {
		t.Fatal("exported portfolio journal verification", e)
	}
	// Build again from raw CSV in an EMPTY isolated DB with the actual target
	// identities, not a fabricated tenant or a production journal reset.
	candidatePool, candidateDone := withPg(t)
	defer candidateDone()
	originalCSV, e := os.ReadFile(filepath.Join(dir, "original.csv"))
	if e != nil {
		t.Fatal(e)
	}
	input := PortfolioCandidateInput{OriginalCSV: originalCSV}
	for i, a := range manifest.Accounts {
		csv, e := os.ReadFile(filepath.Join(dir, a.CSV))
		if e != nil {
			t.Fatal(e)
		}
		var mapping imp.FinancialMapping
		read(a.CSV+".mapping.json", &mapping)
		ledger := report[i].Snapshot.Ledger.ID
		for record := range mapping.LedgerByRecord {
			mapping.LedgerByRecord[record] = ledger
		}
		source := PortfolioCandidateSource{Filename: a.CSV, LedgerID: ledger, AccountKey: "candidate-" + string(rune('a'+i)), CSV: csv, Mapping: mapping, ExpectedPositions: expected[i].Positions}
		for _, line := range a.Rows {
			source.Lineage = append(source.Lineage, PortfolioCandidateLineage{SourceRecord: line.SourceRecord, Reason: line.Reason})
		}
		for _, operation := range expected[i].Operations {
			source.AcceptedRecords = append(source.AcceptedRecords, int(operation.Sequence))
		}
		input.Sources = append(input.Sources, source)
	}
	if binary := os.Getenv("PORTFOLIO_CANDIDATE_BINARY"); binary != "" {
		marketPath := os.Getenv("INVESTMENT_PORTFOLIO_MARKET_INPUT")
		if marketPath == "" {
			t.Fatal("CLI rehearsal requires private market input")
		}
		cliDir := t.TempDir()
		beforePath, planPath := filepath.Join(cliDir, "before.json"), filepath.Join(cliDir, "plan.json")
		planJSON, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		for path, raw := range map[string][]byte{beforePath: bundleJSON, planPath: planJSON} {
			if err = os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
		}
		cliCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(cliCtx, binary, "-before", beforePath, "-plan", planPath,
			"-market", marketPath, "-actor", actor, "-database-name", "budget_portfolio_candidate_cli",
			"-output", filepath.Join(cliDir, "candidate.json"), "-report", filepath.Join(cliDir, "report.json"))
		cmd.Env = append(os.Environ(), "PORTFOLIO_SOURCE_ADMIN_URL="+pool.DB.Config().ConnString())
		stdout, err := cmd.Output()
		if err != nil {
			t.Fatal("candidate CLI creation/migration/import failed", err)
		}
		var summary struct {
			Status                                                                  string
			SourceWrites                                                            bool
			Accounts, Operations, FreshQuotes, RequiredCurrencyDates, SchemaVersion int
		}
		if err = json.Unmarshal(stdout, &summary); err != nil || summary.Status != "isolated_portfolio_candidate_prepared" || summary.SourceWrites || summary.Accounts != verified.Ledgers || summary.Operations != total || summary.FreshQuotes == 0 || summary.RequiredCurrencyDates == 0 || summary.SchemaVersion != 61 {
			t.Fatal("candidate CLI did not certify the complete source", err)
		}
		t.Log("Candidate CLI created and migrated a new database, imported the complete source and verified actual FX/quote coverage")
	}
	built, e := NewInvestmentRepo(candidatePool).BuildPortfolioCandidate(ctx, actor, bundle, input)
	if e != nil || built.Operations != total || built.Ledgers != verified.Ledgers {
		t.Fatal("isolated source candidate build", e)
	}
	var marketInput *PortfolioMarketInput
	if marketPath := os.Getenv("INVESTMENT_PORTFOLIO_MARKET_INPUT"); marketPath != "" {
		raw, e := os.ReadFile(marketPath)
		if e != nil {
			t.Fatal(e)
		}
		marketInput = &PortfolioMarketInput{}
		if e = json.Unmarshal(raw, marketInput); e != nil {
			t.Fatal(e)
		}
		marketRepo := NewMarketDataRepo(candidatePool)
		if e = marketRepo.ConfigurePortfolioMarket(ctx, tenant, actor, *marketInput); e != nil {
			t.Fatal("candidate market configuration", e)
		}
		keys := []string{}
		for _, quote := range marketInput.Quotes {
			keys = append(keys, quote.Mapping.InstrumentKey)
		}
		quotes, e := marketRepo.InvestmentQuotes(ctx, tenant, actor, keys)
		if e != nil {
			t.Fatal(e)
		}
		for _, quote := range quotes.Quotes {
			if quote.State != "fresh" || quote.CleanPrice == nil {
				t.Fatal("candidate quote unavailable", quote.InstrumentKey, quote.Issues)
			}
		}
		coverage, e := NewInvestmentRepo(candidatePool).VerifyPortfolioMarketCoverage(ctx, tenant, actor, *marketInput)
		if e != nil || coverage.RequiredCurrencyDates == 0 || coverage.FreshQuotes != len(marketInput.Quotes) {
			t.Fatal("actual source FX/quote coverage", e)
		}
		health, e := marketRepo.ReadMarketWorkerHealth(ctx, md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"})
		if e != nil || health.QuoteStates["fresh"] != int64(coverage.FreshQuotes) || health.QuoteStates["missing"] != 0 || health.QuoteStates["expired"] != 0 {
			t.Fatal("actual held quote demand differs from verified coverage", health, e)
		}
		coverageJSON, e := json.Marshal(coverage)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(dir, "candidate-market-coverage.json"), coverageJSON, 0600); e != nil {
			t.Fatal(e)
		}
		built.Archive, e = NewInvestmentRepo(candidatePool).ExportInvestmentArchive(ctx, tenant, actor)
		if e != nil {
			t.Fatal(e)
		}
	}
	if e = repo.SeedPortfolioCandidateScope(ctx, actor, bundle); e == nil {
		t.Fatal("candidate seeding accepted populated source DB")
	}
	afterBuild, e := repo.ExportInvestmentArchive(ctx, tenant, actor)
	if e != nil {
		t.Fatal(e)
	}
	sourceBeforeHash, e := InvestmentArchiveStateSHA256(bundle)
	if e != nil {
		t.Fatal(e)
	}
	sourceAfterHash, e := InvestmentArchiveStateSHA256(afterBuild)
	if e != nil || sourceAfterHash != sourceBeforeHash {
		t.Fatal("isolated build changed source", e)
	}
	candidateJSON, e := json.Marshal(built)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, "built-candidate.json"), candidateJSON, 0600); e != nil {
		t.Fatal(e)
	}
	// Restore the real imported archive in a different empty PostgreSQL instance.
	restoredPool, restoredDone := withPg(t)
	defer restoredDone()
	if _, e = restoredPool.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Restore rehearsal','RUB')`, tenant); e != nil {
		t.Fatal(e)
	}
	if _, e = restoredPool.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,'restore@example.test','synthetic')`, actor); e != nil {
		t.Fatal(e)
	}
	if _, e = restoredPool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); e != nil {
		t.Fatal(e)
	}
	restoredRepo := NewInvestmentRepo(restoredPool)
	rehearsal, e := restoredRepo.CheckInvestmentArchiveRestore(ctx, actor, bundle)
	if e != nil || rehearsal != verified {
		t.Fatal("private archive restore rehearsal", e)
	}
	restored, e := restoredRepo.RestoreInvestmentArchive(ctx, actor, bundle, bundle.SHA256)
	if e != nil || restored != verified {
		t.Fatal("private archive restore", e)
	}
	retry, e := restoredRepo.RestoreInvestmentArchive(ctx, actor, bundle, bundle.SHA256)
	if e != nil || retry != verified {
		t.Fatal("private archive restore retry", e)
	}
	restoredService := inv.NewService(NewInvestmentRepo(restoredPool), NewTenantRepo(restoredPool))
	for _, a := range report {
		state, e := restoredService.Snapshot(ctx, tenant, actor, a.Snapshot.Ledger.ID, time.Now().UTC())
		if e != nil || !slices.Equal(state.Issues, a.Snapshot.Issues) || !reflect.DeepEqual(portfolioRehearsalValues(state.Positions), portfolioRehearsalValues(a.Snapshot.Positions)) {
			t.Fatal("restored private positions", e)
		}
	}
	// Replace genuinely different existing history, preserving every empty account.
	current, e := restoredService.Snapshot(ctx, tenant, actor, report[0].Snapshot.Ledger.ID, time.Now().UTC())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = restoredService.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: "obsolete-rehearsal-row", ExpectedLedgerVersion: current.Ledger.Version, Operation: d.InvestmentOperation{LedgerID: current.Ledger.ID, Kind: "buy", InstrumentKey: "SYNTHETIC_OBSOLETE", Currency: "RUB", Quantity: "1", Amount: "1", OccurredAt: current.Ledger.StartedAt.Add(time.Hour)}}); e != nil {
		t.Fatal(e)
	}
	before, e := restoredRepo.ExportInvestmentArchive(ctx, tenant, actor)
	if e != nil {
		t.Fatal(e)
	}
	beforeHash, e := InvestmentArchiveStateSHA256(before)
	if e != nil {
		t.Fatal(e)
	}
	replacementArchive := bundle
	if marketInput != nil {
		replacementArchive = built.Archive
	}
	options := PortfolioArchiveReplacement{ExpectedStateSHA256: beforeHash, ExpectedArchiveSHA256: replacementArchive.SHA256, Market: marketInput}
	if marketInput != nil {
		options.ExpectedMarketSHA256 = PortfolioMarketSHA256(*marketInput)
	}
	if check, retry, e := restoredRepo.ReplacePortfolioArchive(ctx, actor, replacementArchive, options, false); e != nil || retry || check != verified {
		t.Fatal("private replacement rehearsal", e)
	}
	afterCheck, e := restoredRepo.ExportInvestmentArchive(ctx, tenant, actor)
	if e != nil {
		t.Fatal(e)
	}
	afterCheckHash, e := InvestmentArchiveStateSHA256(afterCheck)
	if e != nil || afterCheckHash != beforeHash {
		t.Fatal("private replacement rehearsal changed target", e)
	}
	if applied, retry, e := restoredRepo.ReplacePortfolioArchive(ctx, actor, replacementArchive, options, true); e != nil || retry || applied != verified {
		t.Fatal("private replacement apply", e)
	}
	if applied, retry, e := restoredRepo.ReplacePortfolioArchive(ctx, actor, replacementArchive, options, true); e != nil || !retry || applied != verified {
		t.Fatal("private replacement retry", e)
	}
	for _, a := range report {
		state, e := restoredService.Snapshot(ctx, tenant, actor, a.Snapshot.Ledger.ID, time.Now().UTC())
		if e != nil || !reflect.DeepEqual(portfolioRehearsalValues(state.Positions), portfolioRehearsalValues(a.Snapshot.Positions)) || !slices.Equal(state.Issues, a.Snapshot.Issues) {
			t.Fatal("replacement lost actual source positions or pending issues", e)
		}
	}
	if marketInput != nil {
		keys := []string{}
		for _, quote := range marketInput.Quotes {
			keys = append(keys, quote.Mapping.InstrumentKey)
		}
		quotes, e := NewMarketDataRepo(restoredPool).InvestmentQuotes(ctx, tenant, actor, keys)
		if e != nil {
			t.Fatal(e)
		}
		if len(quotes.Quotes) != len(keys) {
			t.Fatal("restored quote count")
		}
		for _, quote := range quotes.Quotes {
			if quote.State != "fresh" || quote.CleanPrice == nil {
				t.Fatal("replacement lost quote cache", quote.InstrumentKey, quote.Issues)
			}
		}
		rates, e := NewMarketDataRepo(restoredPool).InvestmentExchangeRates(ctx, tenant, actor, marketInput.AlphaPolicy.Key, "RUB", []string{"USD", "EUR"}, []string{"2019-12-09", "2026-10-12"})
		if e != nil {
			t.Fatal(e)
		}
		for _, rate := range rates.Rates {
			if rate.Rate == nil {
				t.Fatal("replacement lost FX cache")
			}
		}
	}
	for _, id := range emptyLedgerIDs {
		state, e := restoredService.Snapshot(ctx, tenant, actor, id, time.Now().UTC())
		if e != nil || len(state.Positions) != 0 {
			t.Fatal("empty account lost during replacement", e)
		}
	}
	raw, e := json.MarshalIndent(report, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, "database-rehearsal-"+strings.NewReplacer(":", "_", "/", "_").Replace(os.Getenv("PG_TEST_IMAGE"))+".json"), raw, 0600); e != nil {
		t.Fatal("write private evidence", e)
	}
	t.Logf("Imported and retried %d source-linked operations across %d native portfolios; restart, archive replay, separate-database restore and restore retry verified", total, len(report))
}

// Database-generated acquisition operation IDs differ from isolated source
// replay IDs. Compare economic values and acquisition dates, never those IDs.
func portfolioRehearsalValues(positions []d.InvestmentPosition) []string {
	out := []string{}
	for _, p := range positions {
		p.Lots = append([]d.InvestmentLot(nil), p.Lots...)
		for i := range p.Lots {
			p.Lots[i].OriginID = ""
			p.Lots[i].AcquisitionOperationID = ""
			p.Lots[i].OriginLedgerID = ""
			p.Lots[i].OriginSequence = 0
		}
		raw, _ := json.Marshal(p)
		out = append(out, string(bytes.TrimSpace(raw)))
	}
	sort.Strings(out)
	return out
}
