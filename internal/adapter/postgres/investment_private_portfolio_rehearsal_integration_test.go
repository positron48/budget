package postgres

import (
	"bytes"
	"encoding/json"
	"os"
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
				Record int `json:"record"`
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
	options := PortfolioArchiveReplacement{ExpectedStateSHA256: beforeHash, ExpectedArchiveSHA256: bundle.SHA256}
	if check, retry, e := restoredRepo.ReplacePortfolioArchive(ctx, actor, bundle, options, false); e != nil || retry || check != verified {
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
	if applied, retry, e := restoredRepo.ReplacePortfolioArchive(ctx, actor, bundle, options, true); e != nil || retry || applied != verified {
		t.Fatal("private replacement apply", e)
	}
	if applied, retry, e := restoredRepo.ReplacePortfolioArchive(ctx, actor, bundle, options, true); e != nil || !retry || applied != verified {
		t.Fatal("private replacement retry", e)
	}
	for _, a := range report {
		state, e := restoredService.Snapshot(ctx, tenant, actor, a.Snapshot.Ledger.ID, time.Now().UTC())
		if e != nil || !reflect.DeepEqual(portfolioRehearsalValues(state.Positions), portfolioRehearsalValues(a.Snapshot.Positions)) || !slices.Equal(state.Issues, a.Snapshot.Issues) {
			t.Fatal("replacement lost actual source positions or pending issues", e)
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
