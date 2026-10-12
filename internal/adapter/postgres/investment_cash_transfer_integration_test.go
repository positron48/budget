package postgres

import (
	"context"
	"errors"
	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	"github.com/positron48/budget/internal/usecase/asset"
	inv "github.com/positron48/budget/internal/usecase/investment"
	archive "github.com/positron48/budget/internal/usecase/investmentarchive"
	"sync"
	"testing"
	"time"
)

func TestInvestmentCashBridgeAtomicCorrectionsAndArchiveBoundary(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, user string
	if e := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Cash bridge','RUB') RETURNING id`).Scan(&tenant); e != nil {
		t.Fatal(e)
	}
	if e := pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES($1,'test') RETURNING id`, uuid.NewString()+"@example.test").Scan(&user); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, user); e != nil {
		t.Fatal(e)
	}
	ctx = ctxutil.WithUserID(ctxutil.WithTenantID(ctx, tenant), user)
	assets := NewAssetRepo(pool)
	aSvc := asset.NewService(assets, NewFxRepo(pool), NewTenantRepo(pool))
	repo := NewInvestmentRepo(pool)
	svc := inv.NewService(repo, NewTenantRepo(pool))
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	cut := at.Add(time.Hour)
	makeAccount := func(kind string, minor int64) d.AssetAccount {
		t.Helper()
		fixed := ""
		if kind == "investment" {
			fixed = "RUB"
		}
		a, e := aSvc.CreateAccount(ctx, d.AssetAccount{TenantID: tenant, Name: kind, Kind: kind, FixedCurrencyCode: fixed}, []d.AssetSnapshot{{UserID: user, Amount: d.Money{CurrencyCode: "RUB", MinorUnits: minor}, AsOf: at}}, uuid.NewString())
		if e != nil {
			t.Fatal(e)
		}
		return a
	}
	bank, account := makeAccount("bank", 100000), makeAccount("investment", 0)
	ledger, e := svc.CreateLedger(ctx, tenant, user, "Broker", at, "ledger")
	if e != nil {
		t.Fatal(e)
	}
	activation := d.InvestmentAssetActivation{AccountID: account.ID, LedgerID: ledger.ID, ValuationFrom: cut}
	p, _, e := svc.AssetActivation(ctx, tenant, user, activation, false)
	if e != nil {
		t.Fatal(e)
	}
	activation.PreviewHash = p.PreviewHash
	activation.ExpectedAccountVersion = p.Account.Version
	activation.ExpectedLedgerVersion = p.Ledger.Version
	activation.Evidence = "Zero cash reconciled"
	activation.RequestKey = "activate"
	_, account, e = svc.AssetActivation(ctx, tenant, user, activation, true)
	if e != nil {
		t.Fatal(e)
	}
	m := inv.CashTransferMutation{Action: "create", RequestKey: "fund", ExpectedLedgerVersion: 1, ExpectedFromAccountVersion: bank.Version, ExpectedToAccountVersion: account.Version, Transfer: d.AssetTransfer{FromAccountID: bank.ID, ToAccountID: account.ID, FromAmount: d.Money{CurrencyCode: "RUB", MinorUnits: 12345}, ToAmount: d.Money{CurrencyCode: "RUB", MinorUnits: 12345}, OccurredAt: cut.Add(time.Hour), Note: "Actual funding"}}
	var wg sync.WaitGroup
	results := make(chan inv.CashTransfer, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); v, e := svc.CashTransfer(ctx, tenant, user, m); results <- v; errs <- e }()
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var funded inv.CashTransfer
	for v := range results {
		if funded.OperationID != "" && funded.OperationID != v.OperationID {
			t.Fatal("duplicate operation")
		}
		funded = v
	}
	assertBalances := func(bankMinor, investmentMinor int64, version int64) {
		t.Helper()
		b, e := assets.GetAccount(ctx, tenant, bank.ID, time.Now())
		if e != nil {
			t.Fatal(e)
		}
		a, e := assets.GetAccount(ctx, tenant, account.ID, time.Now())
		if e != nil {
			t.Fatal(e)
		}
		if b.Balances[0].Amount.MinorUnits != bankMinor || a.Balances[0].Amount.MinorUnits != investmentMinor {
			t.Fatal("balances", b.Balances, a.Balances)
		}
		s, e := svc.Snapshot(ctx, tenant, user, ledger.ID, time.Now())
		if e != nil || s.Ledger.Version != version {
			t.Fatal("version", s, e)
		}
	}
	assertBalances(87655, 12345, 2)
	var count int
	if e = pool.DB.QueryRow(ctx, `SELECT count(*) FROM asset_movements WHERE transfer_id=$1`, funded.Transfer.ID).Scan(&count); e != nil || count != 1 {
		t.Fatal("must be one ordinary movement", count, e)
	}
	changed := m
	changed.Transfer.Note = "changed key"
	if _, e = svc.CashTransfer(ctx, tenant, user, changed); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("key payload", e)
	}
	bad := m
	bad.RequestKey = "before-cut"
	bad.ExpectedLedgerVersion = 2
	bad.Transfer.OccurredAt = at
	if _, e = svc.CashTransfer(ctx, tenant, user, bad); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal("pre-cut", e)
	}
	bad = m
	bad.RequestKey = "stale-account"
	bad.ExpectedFromAccountVersion++
	if _, e = svc.CashTransfer(ctx, tenant, user, bad); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("stale account", e)
	}
	if _, e = aSvc.UpdateTransfer(ctx, funded.Transfer); !errors.Is(e, d.ErrAssetPrecondition) {
		t.Fatal("legacy edit bypass", e)
	}
	if e = aSvc.DeleteTransfer(ctx, tenant, funded.Transfer.ID, 1); !errors.Is(e, d.ErrAssetPrecondition) {
		t.Fatal("legacy delete bypass", e)
	}
	if _, e = svc.Mutate(ctx, tenant, user, inv.Mutation{Action: "void", RequestKey: "single-void", Reason: "Not allowed", ExpectedLedgerVersion: 2, ExpectedOperationVersion: 1, Operation: d.InvestmentOperation{ID: funded.OperationID, LedgerID: ledger.ID}}); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal("single-side void", e)
	}
	amend := m
	amend.Action = "amend"
	amend.Transfer = funded.Transfer
	amend.Transfer.FromAmount.MinorUnits = 15000
	amend.Transfer.ToAmount.MinorUnits = 15000
	amend.ExpectedVersion = 1
	amend.ExpectedLedgerVersion = 2
	amend.Reason = "Correct statement"
	amend.RequestKey = "amend"
	funded, e = svc.CashTransfer(ctx, tenant, user, amend)
	if e != nil || funded.Transfer.Version != 2 {
		t.Fatal("amend", funded, e)
	}
	assertBalances(85000, 15000, 3)
	// Failure after the journal write must roll back revisions, jobs and the document.
	if _, e = pool.DB.Exec(ctx, `CREATE FUNCTION reject_bridge_movement() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test movement failure'; END $$; CREATE TRIGGER reject_bridge_movement BEFORE INSERT ON asset_movements FOR EACH ROW EXECUTE FUNCTION reject_bridge_movement()`); e != nil {
		t.Fatal(e)
	}
	fail := m
	fail.ExpectedLedgerVersion = 3
	fail.RequestKey = "retry-after-failure"
	if _, e = svc.CashTransfer(ctx, tenant, user, fail); e == nil {
		t.Fatal("injected failure accepted")
	}
	assertBalances(85000, 15000, 3)
	if _, e = pool.DB.Exec(ctx, `DROP TRIGGER reject_bridge_movement ON asset_movements; DROP FUNCTION reject_bridge_movement()`); e != nil {
		t.Fatal(e)
	}
	retry, e := svc.CashTransfer(ctx, tenant, user, fail)
	if e != nil {
		t.Fatal("rollback request retry", e)
	}
	assertBalances(72655, 27345, 4)
	withdrawal := m
	withdrawal.RequestKey = "withdraw"
	withdrawal.ExpectedLedgerVersion = 4
	withdrawal.ExpectedFromAccountVersion = account.Version
	withdrawal.ExpectedToAccountVersion = bank.Version
	withdrawal.Transfer.FromAccountID = account.ID
	withdrawal.Transfer.ToAccountID = bank.ID
	withdrawal.Transfer.FromAmount.MinorUnits = 30000
	withdrawal.Transfer.ToAmount.MinorUnits = 30000
	if _, e = svc.CashTransfer(ctx, tenant, user, withdrawal); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal("overdraw", e)
	}
	assertBalances(72655, 27345, 4)
	withdrawal.Transfer.FromAmount.MinorUnits = 5000
	withdrawal.Transfer.ToAmount.MinorUnits = 5000
	withdrawal.RequestKey = "withdraw-good"
	out, e := svc.CashTransfer(ctx, tenant, user, withdrawal)
	if e != nil {
		t.Fatal(e)
	}
	assertBalances(77655, 22345, 5)
	void := amend
	void.Action = "void"
	void.Transfer = funded.Transfer
	void.ExpectedVersion = 2
	void.ExpectedLedgerVersion = 5
	void.RequestKey = "void"
	void.Reason = "Cancel incorrect funding"
	funded, e = svc.CashTransfer(ctx, tenant, user, void)
	if e != nil || funded.Status != "voided" || funded.Transfer.Version != 3 {
		t.Fatal("void", funded, e)
	}
	assertBalances(92655, 7345, 6)
	got, e := svc.ReadCashTransfer(ctx, tenant, user, funded.Transfer.ID)
	if e != nil || got.Status != "voided" {
		t.Fatal("read", e)
	}
	// The actual ordinary and journal values both survive each correction.
	var revisions int
	if e = pool.DB.QueryRow(ctx, `SELECT jsonb_array_length(revisions) FROM investment_cash_links WHERE tenant_id=$1 AND transfer_id=$2`, tenant, funded.Transfer.ID).Scan(&revisions); e != nil || revisions != 3 {
		t.Fatal("joint revision history", revisions, e)
	}
	// Removing funding used by a later withdrawal is refused atomically.
	invalidVoid := m
	invalidVoid.Action = "void"
	invalidVoid.Transfer = retry.Transfer
	invalidVoid.ExpectedVersion = 1
	invalidVoid.ExpectedLedgerVersion = 6
	invalidVoid.Reason = "Would remove spent funds"
	invalidVoid.RequestKey = "invalid-void"
	if _, e = svc.CashTransfer(ctx, tenant, user, invalidVoid); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal("future history must remain valid", e)
	}
	assertBalances(92655, 7345, 6)
	// Different currencies are explicit actual settlements, never an analytical FX guess.
	usd, e := aSvc.CreateAccount(ctx, d.AssetAccount{TenantID: tenant, Name: "Multi-currency broker", Kind: "investment", FixedCurrencyCode: "RUB"}, []d.AssetSnapshot{{UserID: user, Amount: d.Money{CurrencyCode: "RUB", MinorUnits: 0}, AsOf: at}}, "usd-account")
	if e != nil {
		t.Fatal(e)
	}
	usdLedger, e := svc.CreateLedger(ctx, tenant, user, "USD journal", at, "usd-ledger")
	if e != nil {
		t.Fatal(e)
	}
	am := d.InvestmentAssetActivation{AccountID: usd.ID, LedgerID: usdLedger.ID, ValuationFrom: cut}
	up, _, e := svc.AssetActivation(ctx, tenant, user, am, false)
	if e != nil {
		t.Fatal(e)
	}
	am.PreviewHash = up.PreviewHash
	am.ExpectedAccountVersion = up.Account.Version
	am.ExpectedLedgerVersion = up.Ledger.Version
	am.Evidence = "Reconciled USD cash"
	am.RequestKey = "usd-activate"
	_, usd, e = svc.AssetActivation(ctx, tenant, user, am, true)
	if e != nil {
		t.Fatal(e)
	}
	fx := m
	fx.RequestKey = "actual-fx-funding"
	fx.ExpectedLedgerVersion = 1
	fx.ExpectedToAccountVersion = usd.Version
	fx.Transfer.ToAccountID = usd.ID
	fx.Transfer.FromAmount.MinorUnits = 100000
	fx.Transfer.ToAmount = d.Money{CurrencyCode: "USD", MinorUnits: 1234}
	fxSaved, e := svc.CashTransfer(ctx, tenant, user, fx)
	if e != nil {
		t.Fatal(e)
	}
	cash, e := svc.Snapshot(ctx, tenant, user, usdLedger.ID, time.Now())
	if e != nil || len(cash.Cash) != 1 || cash.Cash[0].Economic != "12.34" {
		t.Fatal("actual USD amount", cash, e)
	}
	var originalBank string
	if e = pool.DB.QueryRow(ctx, `SELECT revisions->0->'Document'->'Transfer'->'FromAmount'->>'MinorUnits' FROM investment_cash_links WHERE tenant_id=$1 AND transfer_id=$2`, tenant, fxSaved.Transfer.ID).Scan(&originalBank); e != nil || originalBank != "100000" {
		t.Fatal("actual bank amount audit", originalBank, e)
	}
	// A voided joint document remains audited and protects schema rollback.
	assertInvestmentRollbackRefused(t, ctx, pool, "0049_investment_cash_links")
	data, e := repo.ExportInvestmentArchive(ctx, tenant, user)
	if e != nil {
		t.Fatal(e)
	}
	if data.Data.Excluded["investment_cash_links"] != "budget_integration_requires_full_backup" {
		t.Fatal("archive boundary")
	}
	if _, e = archive.VerifyJournal(data.Data); e != nil {
		t.Fatal("journal provenance archive", e)
	}
	_ = out
}
