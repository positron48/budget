package postgres

import (
	"bytes"
	"encoding/json"
	"errors"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	archive "github.com/positron48/budget/internal/usecase/investmentarchive"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
	"os"
	"reflect"
	"sort"
	"testing"
	"time"
)

// Opt-in rehearsal uses only testcontainers PostgreSQL. No DATABASE_URL or
// production connection is accepted. Source/mapping and output remain private.
func TestInvestmentPrivateFinancialRehearsal(t *testing.T) {
	sourcePath, mappingPath, outPath := os.Getenv("INVESTMENT_REHEARSAL_SOURCE"), os.Getenv("INVESTMENT_REHEARSAL_MAPPING"), os.Getenv("INVESTMENT_REHEARSAL_REPORT")
	if sourcePath == "" || mappingPath == "" || outPath == "" {
		t.Skip("private financial rehearsal fixtures not provided")
	}
	var source imp.StagedSource
	data, e := os.ReadFile(sourcePath)
	if e != nil {
		t.Fatal("read source")
	}
	if json.Unmarshal(data, &source) != nil {
		t.Fatal("decode source")
	}
	data, e = os.ReadFile(mappingPath)
	if e != nil {
		t.Fatal("read mapping")
	}
	mapping, e := imp.DecodeFinancialMapping(data)
	if e != nil {
		t.Fatal("decode mapping", e)
	}
	original, e := imp.MapSnowballFinancial(bytes.NewReader(source.OriginalCSV), mapping)
	if e != nil {
		t.Fatal("verify original mapping", e)
	}
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	journal := NewInvestmentRepo(pool)
	svc := inv.NewService(journal, NewTenantRepo(pool))
	importer := imp.NewImportService(NewInvestmentImportRepo(pool), NewTenantRepo(pool))
	// Derive source account identity from supplied broker evidence, never from a
	// ticker or amount match. Rows with no confirmed account stay in one bucket.
	keys := map[int]string{}
	ledgerKeys := map[string]string{}
	starts := map[string]time.Time{}
	for _, row := range original.Rows {
		key := "other-brokers"
		if v, ok := mapping.TradeEvidence[row.Record]; ok {
			key = "tinvest:" + v.BrokerAccountID
		}
		if v, ok := mapping.IncomeEvidence[row.Record]; ok {
			key = "tinvest:" + v.Credit.AccountID
		}
		if v, ok := mapping.CashEvidence[row.Record]; ok {
			key = "tinvest:" + v.Operation.AccountID
		}
		if v, ok := mapping.SplitEvidence[row.Record]; ok && v.BrokerAccountID != "" {
			key = "tinvest:" + v.BrokerAccountID
		}
		keys[row.Record] = key
		if row.Operation != nil {
			old := row.Operation.LedgerID
			if prior := ledgerKeys[old]; prior != "" && prior != key {
				t.Fatal("conflicting account evidence")
			}
			ledgerKeys[old] = key
			at := row.Operation.OccurredAt
			if starts[old].IsZero() || at.Before(starts[old]) {
				starts[old] = at
			}
		}
	}
	remap := map[string]string{}
	oldIDs := []string{}
	for id := range starts {
		oldIDs = append(oldIDs, id)
	}
	sort.Strings(oldIDs)
	for i, id := range oldIDs {
		l, e := svc.CreateLedger(ctx, tenant, actor, "Private rehearsal", starts[id], "ledger-"+string(rune('a'+i)))
		if e != nil {
			t.Fatal(e)
		}
		remap[id] = l.ID
	}
	accounts := map[string]bool{}
	upload := imp.Upload{Filename: "private-source.csv", RequestKey: "private-rehearsal", MappingSourceSHA256: original.SourceSHA256, CSV: source.OriginalCSV}
	for _, row := range original.Rows {
		key := keys[row.Record]
		accounts[key] = true
		upload.Assignments = append(upload.Assignments, imp.Assignment{Record: row.Record, AccountKey: key, Evidence: "Source account from supplied checksum-bound mapping; remaining brokers grouped by user instruction"})
	}
	accountKeys := []string{}
	for key := range accounts {
		accountKeys = append(accountKeys, key)
	}
	sort.Strings(accountKeys)
	for _, key := range accountKeys {
		upload.Accounts = append(upload.Accounts, imp.ImportAccount{SourceKey: key, Name: "Private rehearsal account"})
	}
	saved, e := importer.Upload(ctx, tenant, actor, upload)
	if e != nil {
		t.Fatal("persist source", e)
	}
	retry, e := importer.Upload(ctx, tenant, actor, upload)
	if e != nil || retry.ID != saved.ID {
		t.Fatal("source retry", e)
	}
	for record, id := range mapping.LedgerByRecord {
		if target := remap[id]; target != "" {
			mapping.LedgerByRecord[record] = target
		}
	}
	// The user authorized one common account for every remaining broker.
	earliest := time.Now().UTC()
	for _, at := range starts {
		if at.Before(earliest) {
			earliest = at
		}
	}
	other, e := svc.CreateLedger(ctx, tenant, actor, "Other brokers", earliest, "other-brokers")
	if e != nil {
		t.Fatal(e)
	}
	sourceLedgers := map[string]string{"other-brokers": other.ID}
	for old, key := range ledgerKeys {
		sourceLedgers[key] = remap[old]
	}
	for record, key := range keys {
		if ledger := sourceLedgers[key]; ledger != "" {
			mapping.LedgerByRecord[record] = ledger
		}
	}
	review, e := importer.PreviewFinancial(ctx, tenant, actor, imp.FinancialReviewRequest{ImportID: saved.ID, ExpectedImportVersion: saved.Version, RequestKey: "financial-rehearsal", Mapping: mapping, Limit: inv.MaxBatchOperations})
	if e != nil {
		t.Fatal("server preparation", e)
	}
	type result struct {
		SourceAccount                                        string
		Prepared, Inserted, RetrySkipped, Failed, Unresolved int
		State, Failure                                       string
		Snapshot                                             *d.InvestmentSnapshot  `json:",omitempty"`
		Checkpoints                                          []d.InvestmentSnapshot `json:",omitempty"`
	}
	report := struct {
		Version                                                                   int
		State, SourceSHA256, OriginalFinancialHash, StagingHash, ServerReviewHash string
		Rows, Prepared, Unresolved                                                int
		Issues                                                                    map[string]int
		Accounts                                                                  []result
		AlphaFX                                                                   *privateAlphaReport `json:",omitempty"`
		ArchiveVerified                                                           bool
		Limitations                                                               []string
	}{Version: 1, State: "incomplete_history", SourceSHA256: original.SourceSHA256, OriginalFinancialHash: original.Hash, StagingHash: saved.PreviewHash, ServerReviewHash: review.Hash, Rows: saved.Rows, Prepared: review.Ready, Unresolved: review.Unresolved, Issues: map[string]int{}, Accounts: []result{}, Limitations: []string{"Isolated disposable PostgreSQL only; no production writes.", "Only explicitly prepared source rows are considered; unresolved rows remain stored.", "Closing broker reconciliation and complete historical coverage are not proved.", "A failed account batch is rolled back in full; no synthetic funding or opening position is inserted."}}
	unresolvedByAccount := map[string]int{}
	for _, row := range review.Rows {
		if row.Operation == nil {
			unresolvedByAccount[keys[row.Record]]++
		}
		for _, issue := range row.Issues {
			report.Issues[issue]++
		}
	}
	if review.Batch != nil {
		base := *review.Batch
		for i, old := range oldIDs {
			id := remap[old]
			b := base
			b.RequestKey = "account-" + string(rune('a'+i))
			b.ExpectedLedgerVersions = map[string]int64{id: 1}
			b.Entries = nil
			for _, entry := range base.Entries {
				if entry.Operation.LedgerID == id {
					b.Entries = append(b.Entries, entry)
				}
			}
			if len(b.Entries) == 0 {
				continue
			}
			item := result{SourceAccount: ledgerKeys[old], Prepared: len(b.Entries), Unresolved: unresolvedByAccount[ledgerKeys[old]]}
			preview, previewErr := svc.PreviewImportBatch(ctx, tenant, actor, b)
			if previewErr != nil {
				item.State = "blocked_replay"
				item.Failed = len(b.Entries)
				item.Failure = previewErr.Error()
				// Exercise the write path too: invalid replay must not leave any account facts.
				if _, e = svc.CommitImportBatch(ctx, tenant, actor, b, investmentBatchHash(tenant, actor, b)); !errors.Is(e, d.ErrInvestmentPrecondition) {
					t.Fatal("invalid replay committed")
				}
				var count int
				if e = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_operations WHERE tenant_id=$1 AND ledger_id=$2`, tenant, id).Scan(&count); e != nil || count != 0 {
					t.Fatal("failed account left partial operations", e)
				}
			} else {
				posted, e := svc.CommitImportBatch(ctx, tenant, actor, b, preview.Hash)
				if e != nil {
					t.Fatal("commit passing account", e)
				}
				if !posted.Committed || len(posted.Operations) != len(b.Entries) {
					t.Fatal("commit counts")
				}
				item.Inserted = len(posted.Operations)
				item.State = "posted_in_isolated_database"
				repeated, e := svc.CommitImportBatch(ctx, tenant, actor, b, preview.Hash)
				if e != nil || len(repeated.Operations) != item.Inserted {
					t.Fatal("batch retry", e)
				}
				item.RetrySkipped = item.Inserted
				var count int
				if e = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_operations WHERE tenant_id=$1 AND ledger_id=$2`, tenant, id).Scan(&count); e != nil || count != item.Inserted {
					t.Fatal("retry duplicated operations", e)
				}
				snap, e := svc.Snapshot(ctx, tenant, actor, id, time.Now().UTC())
				if e != nil {
					t.Fatal(e)
				}
				item.Snapshot = &snap
				// Check the first month, year ends, corporate event dates and last closed day.
				loc, e := time.LoadLocation(mapping.Timezone)
				if e != nil {
					t.Fatal(e)
				}
				now := time.Now().In(loc)
				cutoff := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).Add(-time.Microsecond)
				start := starts[old].In(loc)
				dates := map[time.Time]bool{}
				dates[time.Date(start.Year(), start.Month()+1, 1, 0, 0, 0, 0, loc).Add(-time.Microsecond)] = true
				for year := start.Year(); year < cutoff.Year(); year++ {
					dates[time.Date(year+1, 1, 1, 0, 0, 0, 0, loc).Add(-time.Microsecond)] = true
				}
				dates[cutoff] = true
				for _, entry := range b.Entries {
					switch entry.Operation.Kind {
					case "split", "amortization", "redemption", "distribution":
						dates[entry.Operation.OccurredAt] = true
					}
				}
				ordered := []time.Time{}
				for date := range dates {
					if !date.Before(start) && !date.After(cutoff) {
						ordered = append(ordered, date)
					}
				}
				sort.Slice(ordered, func(i, j int) bool { return ordered[i].Before(ordered[j]) })
				for _, date := range ordered {
					point, e := svc.Snapshot(ctx, tenant, actor, id, date)
					if e != nil {
						t.Fatal("control date replay", e)
					}
					item.Checkpoints = append(item.Checkpoints, point)
				}

			}
			report.Accounts = append(report.Accounts, item)
		}
	}
	if os.Getenv("INVESTMENT_REHEARSAL_ALPHA_DIR") != "" {
		var checkpoints []d.InvestmentSnapshot
		for _, account := range report.Accounts {
			checkpoints = append(checkpoints, account.Checkpoints...)
		}
		report.AlphaFX = privateAlphaRehearsal(t, pool, tenant, actor, checkpoints)
	}
	bundle, e := journal.ExportInvestmentArchive(ctx, tenant, actor)
	if e != nil {
		t.Fatal("export", e)
	}
	if _, e = archive.VerifyJournal(bundle.Data); e != nil {
		t.Fatal("verify financial archive", e)
	}
	target, cleanupTarget := withPg(t)
	defer cleanupTarget()
	if _, e = target.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Private restore','RUB')`, tenant); e != nil {
		t.Fatal(e)
	}
	if _, e = target.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,$2,'test')`, actor, actor+"@example.test"); e != nil {
		t.Fatal(e)
	}
	if _, e = target.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); e != nil {
		t.Fatal(e)
	}
	targetRepo := NewInvestmentRepo(target)
	if _, e = targetRepo.RestoreInvestmentArchive(ctx, actor, bundle, bundle.SHA256); e != nil {
		t.Fatal("restore private rehearsal", e)
	}
	restored, e := targetRepo.ExportInvestmentArchive(ctx, tenant, actor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = archive.VerifyJournal(restored.Data); e != nil {
		t.Fatal("verify restored replay", e)
	}
	report.Limitations = append(report.Limitations, "Canonical archive was restored and replay-verified in a second disposable PostgreSQL.")
	targetService := inv.NewService(targetRepo, NewTenantRepo(target))
	for _, account := range report.Accounts {
		for _, point := range account.Checkpoints {
			restored, e := targetService.Snapshot(ctx, tenant, actor, point.Ledger.ID, point.AsOf)
			if e != nil || !reflect.DeepEqual(restored.Cash, point.Cash) || !reflect.DeepEqual(restored.Positions, point.Positions) {
				t.Fatal("restored control date differs", e)
			}
		}
	}
	if report.AlphaFX != nil {
		verifyPrivateAlphaRestore(t, target, tenant, actor, report.AlphaFX)
	}
	report.Accounts = append(report.Accounts, result{SourceAccount: "other-brokers", State: "unresolved_financial_rules", Unresolved: unresolvedByAccount["other-brokers"]})
	inserted, failed, unresolved := 0, 0, 0
	for _, account := range report.Accounts {
		inserted += account.Inserted
		failed += account.Failed
		unresolved += account.Unresolved
	}
	if inserted+failed+unresolved != report.Rows {
		t.Fatal("report row accounting")
	}

	report.ArchiveVerified = true
	if report.Prepared+report.Unresolved != report.Rows {
		t.Fatal("source rows lost")
	}
	encoded, e := json.MarshalIndent(report, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	f, e := os.OpenFile(outPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if e != nil {
		t.Fatal("report output")
	}
	if e = f.Chmod(0600); e != nil {
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		t.Fatal(e)
	}
	_, e = f.Write(encoded)
	closeErr := f.Close()
	if e != nil || closeErr != nil {
		t.Fatal("write report")
	}
	t.Logf("Private rehearsal: %d rows, %d prepared, %d unresolved, %d account results; archive verified; no production changes", report.Rows, report.Prepared, report.Unresolved, len(report.Accounts))
}
