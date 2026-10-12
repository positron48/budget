package postgres

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
)

func TestInvestmentJournalPersistence(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, foreign, user string
	for _, f := range []struct {
		q   string
		out *string
	}{
		{`INSERT INTO tenants(name,default_currency_code) VALUES('Journal','RUB') RETURNING id`, &tenant},
		{`INSERT INTO tenants(name,default_currency_code) VALUES('Foreign','RUB') RETURNING id`, &foreign},
		{fmt.Sprintf(`INSERT INTO users(email,password_hash) VALUES('journal-%d@example.test','test') RETURNING id`, time.Now().UnixNano()), &user},
	} {
		if err := pool.DB.QueryRow(ctx, f.q).Scan(f.out); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, user); err != nil {
		t.Fatal(err)
	}
	repo := NewInvestmentRepo(pool)
	svc := inv.NewService(repo, NewTenantRepo(pool))
	day := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ledger, err := svc.CreateLedger(ctx, tenant, user, "Broker", day, "ledger-key")
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.CreateLedger(ctx, tenant, user, "Broker", day, "ledger-key")
	if err != nil || again.ID != ledger.ID {
		t.Fatal("ledger retry", err)
	}
	if _, err = svc.CreateLedger(ctx, tenant, user, "Different", day, "ledger-key"); !errors.Is(err, d.ErrInvestmentConflict) {
		t.Fatal("ledger changed key", err)
	}
	makeMutation := func(key, kind, amount string, version int64, at time.Time) inv.Mutation {
		return inv.Mutation{Action: "create", RequestKey: key, ExpectedLedgerVersion: version, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: kind, Amount: amount, Currency: "USD", OccurredAt: at}}
	}
	deposit := makeMutation("deposit", "deposit", "1000", 1, day)
	dep, err := svc.Mutate(ctx, tenant, user, deposit)
	if err != nil {
		t.Fatal(err)
	}
	if repeated, err := svc.Mutate(ctx, tenant, user, deposit); err != nil || repeated.ID != dep.ID {
		t.Fatal("operation retry", err)
	}
	buy := makeMutation("buy", "buy", "200", 2, day.Add(time.Hour))
	buy.Operation.InstrumentKey = "TEST"
	buy.Operation.Quantity = "2"
	buy.Operation.Fee = "2"
	buy.Operation.SettlementAt = day.AddDate(0, 0, 2)
	purchased, err := svc.Mutate(ctx, tenant, user, buy)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := svc.Snapshot(ctx, tenant, user, ledger.ID, day.AddDate(0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Cash[0].Settled != "1000" || snapshot.Cash[0].Payable != "202" || snapshot.Cash[0].Economic != "798" || snapshot.Positions[0].CleanCost != "202" {
		t.Fatal(snapshot)
	}
	var cashLegs, securityLegs int
	if err = pool.DB.QueryRow(ctx, `SELECT (SELECT count(*) FROM investment_cash_legs WHERE ledger_id=$1),(SELECT count(*) FROM investment_security_legs WHERE ledger_id=$1)`, ledger.ID).Scan(&cashLegs, &securityLegs); err != nil || cashLegs != 7 || securityLegs != 1 {
		t.Fatal("missing legs", cashLegs, securityLegs, err)
	}
	if _, err = repo.Snapshot(ctx, foreign, ledger.ID, day); !errors.Is(err, d.ErrInvestmentNotFound) {
		t.Fatal("tenant leak", err)
	}
	// Concurrent sale intents see one account version: only one can succeed.
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sell := makeMutation(fmt.Sprintf("sell-%d", i), "sell", "120", 3, day.AddDate(0, 0, 3))
			sell.Operation.Quantity = "1"
			sell.Operation.InstrumentKey = "TEST"
			sell.Operation.Fee = "1"
			_, err := svc.Mutate(ctx, tenant, user, sell)
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for e := range results {
		if e == nil {
			success++
		} else if errors.Is(e, d.ErrInvestmentConflict) {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal(success, conflict)
	}
	// Removing the historical funding or purchase must roll back every write.
	void := inv.Mutation{Action: "void", RequestKey: "void-buy", Reason: "mistake", ExpectedLedgerVersion: 4, ExpectedOperationVersion: 1, Operation: d.InvestmentOperation{ID: purchased.ID, LedgerID: ledger.ID}}
	if _, err = svc.Mutate(ctx, tenant, user, void); !errors.Is(err, d.ErrInvestmentPrecondition) {
		t.Fatal("void broke later sale", err)
	}
	var revisions, jobs, requests int
	if err = pool.DB.QueryRow(ctx, `SELECT (SELECT count(*) FROM investment_operation_revisions WHERE ledger_id=$1),(SELECT count(*) FROM investment_recalculation_jobs WHERE ledger_id=$1),(SELECT count(*) FROM financial_write_requests WHERE tenant_id=$2 AND request_key='void-buy')`, ledger.ID, tenant).Scan(&revisions, &jobs, &requests); err != nil || revisions != 3 || jobs != 3 || requests != 0 {
		t.Fatal("partial failed write", revisions, jobs, requests, err)
	}
	snapshot, err = svc.Snapshot(ctx, tenant, user, ledger.ID, day.AddDate(0, 0, 4))
	if err != nil || snapshot.Cash[0].Economic != "917" || snapshot.Positions[0].CleanCost != "101" || snapshot.Positions[0].RealizedClean != "18" {
		t.Fatal(snapshot, err)
	}
	amended := buy
	amended.Action = "amend"
	amended.RequestKey = "amend-buy"
	amended.Reason = "fee correction"
	amended.ExpectedLedgerVersion = 4
	amended.ExpectedOperationVersion = 1
	amended.Operation.ID = purchased.ID
	amended.Operation.Fee = "4"
	updated, err := svc.Mutate(ctx, tenant, user, amended)
	if err != nil || updated.Version != 2 {
		t.Fatal(updated, err)
	}
	snapshot, err = svc.Snapshot(ctx, tenant, user, ledger.ID, day.AddDate(0, 0, 4))
	if err != nil || snapshot.Cash[0].Economic != "915" || snapshot.Positions[0].CleanCost != "102" || snapshot.Positions[0].RealizedClean != "17" {
		t.Fatal(snapshot, err)
	}
	var earliest time.Time
	if err = pool.DB.QueryRow(ctx, `SELECT dirty_from FROM investment_recalculation_jobs WHERE ledger_id=$1 AND journal_revision=5`, ledger.ID).Scan(&earliest); err != nil || !earliest.Equal(buy.Operation.OccurredAt) {
		t.Fatal(earliest, err)
	}
	operations, err := svc.Operations(ctx, tenant, user, ledger.ID, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	var sale d.InvestmentOperation
	for _, operation := range operations {
		if operation.Kind == "sell" {
			sale = operation
		}
	}
	cancel := inv.Mutation{Action: "void", RequestKey: "void-sale", Reason: "sale cancelled", ExpectedLedgerVersion: 5, ExpectedOperationVersion: sale.Version, Operation: d.InvestmentOperation{ID: sale.ID, LedgerID: ledger.ID}}
	voided, err := svc.Mutate(ctx, tenant, user, cancel)
	if err != nil || voided.Status != "voided" {
		t.Fatal(voided, err)
	}
	if retry, err := svc.Mutate(ctx, tenant, user, cancel); err != nil || retry.ID != sale.ID {
		t.Fatal("void retry", retry, err)
	}
	snapshot, err = svc.Snapshot(ctx, tenant, user, ledger.ID, day.AddDate(0, 0, 4))
	if err != nil || snapshot.Cash[0].Economic != "796" || snapshot.Positions[0].Quantity != "2" || snapshot.Positions[0].CleanCost != "204" || snapshot.Positions[0].RealizedClean != "0" {
		t.Fatal("void replay", snapshot, err)
	}

	// Source posting is transactional with replay and permanently deduplicated.
	importRepo := NewInvestmentImportRepo(pool)
	prepared, err := imp.PrepareUpload(imp.Upload{Filename: "review.csv", RequestKey: "review-upload", CSV: []byte("Event,Date,Symbol,Price,Quantity,Currency,FeeTax,Exchange,NKD,FeeCurrency,DoNotAdjustCash,Note\nBUY,2024-01-02 12:00:00,TEST,10,1,USD,0,X,0,,False,\nBUY,2024-01-02 13:00:00,TEST,10,1,USD,0,X,0,,False,\n"), Accounts: []imp.ImportAccount{{SourceKey: "a", Name: "Broker"}, {SourceKey: "b", Name: "Other"}}, Assignments: []imp.Assignment{{Record: 1, AccountKey: "a"}, {Record: 2, AccountKey: "a"}}})
	if err != nil {
		t.Fatal(err)
	}
	imported, err := importRepo.SaveImport(ctx, tenant, user, prepared)
	if err != nil {
		t.Fatal(err)
	}
	sourced := makeMutation("source-buy", "buy", "10000", 6, day.Add(time.Hour*2))
	sourced.Operation.Quantity = "1"
	sourced.Operation.InstrumentKey = "TEST"
	sourced.Source = &d.InvestmentSource{ImportID: imported.ID, Record: 1, ExpectedVersion: 1, ReviewReason: "Confirmed clean amount, identity, timezone and settlement from broker report"}
	if _, err = svc.Mutate(ctx, tenant, user, sourced); !errors.Is(err, d.ErrInvestmentPrecondition) {
		t.Fatal("unfunded import", err)
	}
	var links int
	if err = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_source_links WHERE tenant_id=$1`, tenant).Scan(&links); err != nil || links != 0 {
		t.Fatal("partial source link", links, err)
	}
	sourced.Operation.Amount = "10"
	sourced.Source.ExpectedVersion = 2
	if _, err = svc.Mutate(ctx, tenant, user, sourced); !errors.Is(err, d.ErrInvestmentConflict) {
		t.Fatal("stale source", err)
	}
	sourced.Source.ExpectedVersion = 1
	posted, err := svc.Mutate(ctx, tenant, user, sourced)
	if err != nil || posted.Source == nil || posted.Source.ImportID != imported.ID {
		t.Fatal(posted, err)
	}
	if retry, err := svc.Mutate(ctx, tenant, user, sourced); err != nil || retry.ID != posted.ID {
		t.Fatal("source retry", retry, err)
	}
	sourced.RequestKey = "duplicate-source"
	sourced.ExpectedLedgerVersion = 7
	if _, err = svc.Mutate(ctx, tenant, user, sourced); !errors.Is(err, d.ErrInvestmentConflict) {
		t.Fatal("duplicate source accepted", err)
	}
	var otherAccount string
	for _, a := range imported.Accounts {
		if a.SourceKey == "b" {
			otherAccount = a.ID
		}
	}
	if _, err = importRepo.ReassignImportRow(ctx, tenant, user, imported.ID, 1, otherAccount, 1, "move"); !errors.Is(err, imp.ErrConflict) {
		t.Fatal("posted row remapped", err)
	}
	second, err := svc.CreateLedger(ctx, tenant, user, "Second", day, "second-ledger")
	if err != nil {
		t.Fatal(err)
	}
	sourced.Operation.LedgerID = second.ID
	sourced.Source.Record = 2
	sourced.ExpectedLedgerVersion = 1
	if _, err = svc.Mutate(ctx, tenant, user, sourced); !errors.Is(err, d.ErrInvestmentConflict) {
		t.Fatal("source account split across ledgers", err)
	}
	sourced.Operation.LedgerID = ledger.ID
	sourced.Source.Record = 1
	sourced.ExpectedLedgerVersion = 7
	// Financial corrections preserve source provenance, even if omitted by client.
	amendedSource := sourced
	amendedSource.Action = "amend"
	amendedSource.RequestKey = "amend-source"
	amendedSource.Source = nil
	amendedSource.Operation.ID = posted.ID
	amendedSource.ExpectedOperationVersion = 1
	amendedSource.Reason = "correct note"
	corrected, err := svc.Mutate(ctx, tenant, user, amendedSource)
	if err != nil || corrected.Source == nil || corrected.Source.Record != 1 {
		t.Fatal("lost source", corrected, err)
	}
	if _, err = svc.Mutate(ctx, tenant, user, inv.Mutation{Action: "void", RequestKey: "void-source", Reason: "cancel", ExpectedLedgerVersion: 8, ExpectedOperationVersion: 2, Operation: d.InvestmentOperation{ID: posted.ID, LedgerID: ledger.ID}}); err != nil {
		t.Fatal(err)
	}
	sourced.ExpectedLedgerVersion = 9
	sourced.RequestKey = "repost-voided-source"
	if _, err = svc.Mutate(ctx, tenant, user, sourced); !errors.Is(err, d.ErrInvestmentConflict) {
		t.Fatal("void released source", err)
	}
	linkedRows, err := importRepo.ListImportRows(ctx, tenant, imported.ID, 50, 0)
	if err != nil || len(linkedRows) != 2 || linkedRows[0].OperationID != posted.ID || linkedRows[0].OperationStatus != "voided" || linkedRows[0].LedgerID != ledger.ID || linkedRows[1].OperationID != "" || linkedRows[0].Record != 1 || linkedRows[0].SourceRowID == "" {
		t.Fatal("source posting status", linkedRows, err)
	}
	// A source from another tenant is not accessible even with a valid target ledger.
	if _, err = pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, foreign, user); err != nil {
		t.Fatal(err)
	}
	foreignLedger, err := svc.CreateLedger(ctx, foreign, user, "Foreign", day, "foreign-ledger")
	if err != nil {
		t.Fatal(err)
	}
	sourced.Operation.LedgerID = foreignLedger.ID
	sourced.ExpectedLedgerVersion = 1
	if _, err = svc.Mutate(ctx, foreign, user, sourced); !errors.Is(err, d.ErrInvestmentNotFound) {
		t.Fatal("cross tenant source", err)
	}
	for _, migration := range []string{"0014_investment_source_links", "0013_investment_journal", "0012_investment_imports"} {
		assertInvestmentRollbackRefused(t, ctx, pool, migration)
	}
	afterRollback, err := svc.Snapshot(ctx, tenant, user, ledger.ID, day.AddDate(0, 0, 4))
	if err != nil || afterRollback.Ledger.Version != 9 || afterRollback.Cash[0].Economic != "796" {
		t.Fatal("rollback changed journal", afterRollback, err)
	}
	preservedRows, err := importRepo.ListImportRows(ctx, tenant, imported.ID, 50, 0)
	if err != nil || len(preservedRows) != 2 || preservedRows[0].OperationID != posted.ID {
		t.Fatal("rollback changed source links", preservedRows, err)
	}
	bondLedger, err := svc.CreateLedger(ctx, tenant, user, "Bonds", day, "bond-ledger")
	if err != nil {
		t.Fatal(err)
	}
	for i, kind := range []string{"deposit", "buy", "amortization", "redemption"} {
		m := makeMutation("bond-"+kind, kind, []string{"900", "900", "200", "800"}[i], int64(i+1), day.Add(time.Duration(i)*time.Hour))
		m.Operation.LedgerID = bondLedger.ID
		if i > 0 {
			m.Operation.InstrumentKey = "BOND"
		}
		if kind == "buy" || kind == "redemption" {
			m.Operation.Quantity = "1"
		}
		if kind == "amortization" {
			m.Operation.NominalBefore = "1000"
			m.Operation.NominalAfter = "800"
		}
		if kind == "redemption" {
			m.Operation.NominalBefore = "800"
			m.Operation.NominalAfter = "0"
		}
		if _, err = svc.Mutate(ctx, tenant, user, m); err != nil {
			t.Fatal(kind, err)
		}
	}
	bondSnapshot, err := svc.Snapshot(ctx, tenant, user, bondLedger.ID, day.Add(5*time.Hour))
	if err != nil || bondSnapshot.Cash[0].Economic != "1000" || bondSnapshot.Positions[0].RealizedClean != "100" || bondSnapshot.Positions[0].Quantity != "0" {
		t.Fatal(bondSnapshot, err)
	}
	assertInvestmentRollbackRefused(t, ctx, pool, "0015_investment_principal_events")
	couponLedger, err := svc.CreateLedger(ctx, tenant, user, "Coupons", day, "coupon-ledger")
	if err != nil {
		t.Fatal(err)
	}
	var couponLot string
	for i, kind := range []string{"deposit", "buy", "coupon"} {
		m := makeMutation("coupon-"+kind, kind, []string{"100", "10", "3"}[i], int64(i+1), day.Add(time.Duration(i)*time.Hour))
		m.Operation.LedgerID = couponLedger.ID
		if i > 0 {
			m.Operation.InstrumentKey = "COUPON-BOND"
		}
		if kind == "buy" {
			m.Operation.Quantity = "1"
			m.Operation.Accrued = "2"
		}
		if kind == "coupon" {
			m.Operation.CouponAllocationConfirmed = true
			m.Operation.AccruedRecoveries = []d.InvestmentAccruedRecovery{{OriginID: couponLot, AllRemaining: true}}
		}
		saved, err := svc.Mutate(ctx, tenant, user, m)
		if err != nil {
			t.Fatal(kind, err)
		}
		if kind == "buy" {
			couponLot = saved.ID
		}
	}
	couponSnapshot, err := svc.Snapshot(ctx, tenant, user, couponLedger.ID, day.Add(5*time.Hour))
	if err != nil || couponSnapshot.Cash[0].Economic != "91" || couponSnapshot.Positions[0].CouponEarned != "1" || couponSnapshot.Positions[0].AcquiredAccrued != "0" {
		t.Fatal(couponSnapshot, err)
	}
	double := makeMutation("coupon-double", "coupon", "3", 4, day.Add(3*time.Hour))
	double.Operation.LedgerID = couponLedger.ID
	double.Operation.InstrumentKey = "COUPON-BOND"
	double.Operation.CouponAllocationConfirmed = true
	double.Operation.AccruedRecoveries = []d.InvestmentAccruedRecovery{{OriginID: couponLot, Amount: "1"}}
	if _, err = svc.Mutate(ctx, tenant, user, double); !errors.Is(err, d.ErrInvestmentPrecondition) {
		t.Fatal("double recovery", err)
	}
	couponSnapshot, err = svc.Snapshot(ctx, tenant, user, couponLedger.ID, day.Add(5*time.Hour))
	if err != nil || couponSnapshot.Ledger.Version != 4 || couponSnapshot.Cash[0].Economic != "91" {
		t.Fatal("partial coupon mutation", couponSnapshot, err)
	}
	fxLedger, err := svc.CreateLedger(ctx, tenant, user, "FX", day, "fx-ledger")
	if err != nil {
		t.Fatal(err)
	}
	fxFunding := makeMutation("fx-fund", "deposit", "200", 1, day)
	fxFunding.Operation.LedgerID = fxLedger.ID
	if _, err = svc.Mutate(ctx, tenant, user, fxFunding); err != nil {
		t.Fatal(err)
	}
	conversion := makeMutation("fx-convert", "fx", "100", 2, day.Add(time.Hour))
	conversion.Operation.LedgerID = fxLedger.ID
	conversion.Operation.ToCurrency = "RUB"
	conversion.Operation.ToAmount = "9000"
	conversion.Operation.Fee = "1"
	converted, err := svc.Mutate(ctx, tenant, user, conversion)
	if err != nil {
		t.Fatal(err)
	}
	if retry, err := svc.Mutate(ctx, tenant, user, conversion); err != nil || retry.ID != converted.ID {
		t.Fatal("FX retry", retry, err)
	}
	fxSnapshot, err := svc.Snapshot(ctx, tenant, user, fxLedger.ID, day.Add(5*time.Hour))
	if err != nil || len(fxSnapshot.Cash) != 2 || fxSnapshot.Cash[0].Currency != "RUB" || fxSnapshot.Cash[0].Economic != "9000" || fxSnapshot.Cash[1].Economic != "99" {
		t.Fatal(fxSnapshot, err)
	}
	conversion.Action = "amend"
	conversion.RequestKey = "fx-overdraw"
	conversion.ExpectedLedgerVersion = 3
	conversion.ExpectedOperationVersion = 1
	conversion.Operation.ID = converted.ID
	conversion.Operation.Amount = "300"
	conversion.Reason = "test rejection"
	if _, err = svc.Mutate(ctx, tenant, user, conversion); !errors.Is(err, d.ErrInvestmentPrecondition) {
		t.Fatal("FX overdraw", err)
	}
	fxSnapshot, err = svc.Snapshot(ctx, tenant, user, fxLedger.ID, day.Add(5*time.Hour))
	if err != nil || fxSnapshot.Ledger.Version != 3 || fxSnapshot.Cash[0].Economic != "9000" || fxSnapshot.Cash[1].Economic != "99" {
		t.Fatal("partial FX write", fxSnapshot, err)
	}
	assertInvestmentRollbackRefused(t, ctx, pool, "0016_investment_fx")
	openingLedger, err := svc.CreateLedger(ctx, tenant, user, "Opening", day, "opening-ledger")
	if err != nil {
		t.Fatal(err)
	}
	openingInput := makeMutation("opening-security", "opening_position", "0", 1, day)
	openingInput.Operation.LedgerID = openingLedger.ID
	openingInput.Operation.InstrumentKey = "OPENING"
	openingInput.Operation.Quantity = "1"
	openingInput.Operation.Note = "Statement without acquisition data"
	openingSaved, err := svc.Mutate(ctx, tenant, user, openingInput)
	if err != nil {
		t.Fatal(err)
	}
	openingSale := makeMutation("opening-sale", "sell", "100", 2, day.Add(time.Hour))
	openingSale.Operation.LedgerID = openingLedger.ID
	openingSale.Operation.InstrumentKey = "OPENING"
	openingSale.Operation.Quantity = "1"
	if _, err = svc.Mutate(ctx, tenant, user, openingSale); err != nil {
		t.Fatal(err)
	}
	openingSnapshot, err := svc.Snapshot(ctx, tenant, user, openingLedger.ID, day.Add(5*time.Hour))
	if err != nil || openingSnapshot.Positions[0].RealizedClean != "" || openingSnapshot.Cash[0].Economic != "100" {
		t.Fatal("unknown opening became zero basis", openingSnapshot, err)
	}
	openingInput.Action = "amend"
	openingInput.RequestKey = "opening-confirm-cost"
	openingInput.ExpectedLedgerVersion = 3
	openingInput.ExpectedOperationVersion = 1
	openingInput.Operation.ID = openingSaved.ID
	openingInput.Operation.OpeningCostKnown = true
	openingInput.Operation.Amount = "80"
	openingInput.Operation.OpeningAccruedKnown = true
	openingInput.Operation.AcquiredAt = day.AddDate(-1, 0, 0)
	openingInput.Reason = "Original purchase evidence recovered"
	if _, err = svc.Mutate(ctx, tenant, user, openingInput); err != nil {
		t.Fatal(err)
	}
	openingSnapshot, err = svc.Snapshot(ctx, tenant, user, openingLedger.ID, day.Add(5*time.Hour))
	if err != nil || openingSnapshot.Positions[0].RealizedClean != "20" || openingSnapshot.Cash[0].Economic != "100" {
		t.Fatal("opening correction replay", openingSnapshot, err)
	}
	assertInvestmentRollbackRefused(t, ctx, pool, "0018_investment_openings")
	applyMigrations(t, ctx, pool.DB)
}
