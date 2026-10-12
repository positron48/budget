package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	"github.com/positron48/budget/internal/usecase/asset"
	inv "github.com/positron48/budget/internal/usecase/investment"
	archive "github.com/positron48/budget/internal/usecase/investmentarchive"
)

func TestInvestmentAssetActivationReplacesManualValueAndFencesWrites(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, user, member string
	if e := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Activation','RUB') RETURNING id`).Scan(&tenant); e != nil {
		t.Fatal(e)
	}
	for _, dst := range []*string{&user, &member} {
		if e := pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES($1,'test') RETURNING id`, uuid.NewString()+"@example.test").Scan(dst); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner'),($1,$3,'member')`, tenant, user, member); e != nil {
		t.Fatal(e)
	}
	ctx = ctxutil.WithUserID(ctxutil.WithTenantID(ctx, tenant), user)
	assets := NewAssetRepo(pool)
	svc := asset.NewService(assets, NewFxRepo(pool), NewTenantRepo(pool))
	journal := NewInvestmentRepo(pool)
	invest := inv.NewService(journal, NewTenantRepo(pool))
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	cut := at.Add(24 * time.Hour)
	create := func(kind string, minor int64) d.AssetAccount {
		t.Helper()
		currency := ""
		if kind == "investment" {
			currency = "RUB"
		}
		a, e := svc.CreateAccount(ctx, d.AssetAccount{TenantID: tenant, Name: kind, Kind: kind, FixedCurrencyCode: currency}, []d.AssetSnapshot{{UserID: user, Amount: d.Money{CurrencyCode: "RUB", MinorUnits: minor}, AsOf: at}}, uuid.NewString())
		if e != nil {
			t.Fatal(e)
		}
		return a
	}
	account, bank := create("investment", 100000), create("bank", 200000)
	oldTransfer, e := svc.CreateTransfer(ctx, d.AssetTransfer{TenantID: tenant, UserID: user, FromAccountID: bank.ID, ToAccountID: account.ID, FromAmount: d.Money{CurrencyCode: "RUB", MinorUnits: 10000}, ToAmount: d.Money{CurrencyCode: "RUB", MinorUnits: 10000}, OccurredAt: at.Add(time.Hour), RequestKey: "old-manual-transfer"})
	if e != nil {
		t.Fatal(e)
	}
	valuation, e := svc.CreateSnapshot(ctx, d.AssetSnapshot{TenantID: tenant, AccountID: account.ID, UserID: user, Kind: "valuation", Amount: d.Money{CurrencyCode: "RUB", MinorUnits: 110000}, AsOf: cut}, "cut-valuation")
	if e != nil {
		t.Fatal(e)
	}
	ledger, e := invest.CreateLedger(ctx, tenant, user, "Broker", at, "ledger")
	if e != nil {
		t.Fatal(e)
	}
	_, e = invest.Mutate(ctx, tenant, user, inv.Mutation{Action: "create", RequestKey: "deposit", ExpectedLedgerVersion: 1, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "deposit", Currency: "RUB", Amount: "1100", OccurredAt: at}})
	if e != nil {
		t.Fatal(e)
	}
	m := d.InvestmentAssetActivation{AccountID: account.ID, LedgerID: ledger.ID, ValuationFrom: cut}
	preview, _, e := invest.AssetActivation(ctx, tenant, user, m, false)
	if e != nil || !preview.Complete || !preview.Matches || preview.InventoryCount != 3 || preview.LedgerKnownValue.MinorUnits != 110000 {
		t.Fatal(preview, e)
	}
	if _, _, e = invest.AssetActivation(ctx, tenant, member, m, false); !errors.Is(e, d.ErrInvestmentForbidden) {
		t.Fatal("member activation preview", e)
	}
	m.PreviewHash = preview.PreviewHash
	m.ExpectedAccountVersion = preview.Account.Version
	m.ExpectedLedgerVersion = preview.Ledger.Version
	m.Evidence = "Reconciled bank transfer and broker cash"
	m.RequestKey = "activate"
	valuation.Note = "Changed inventory"
	valuation, e = svc.UpdateSnapshot(ctx, valuation)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = invest.AssetActivation(ctx, tenant, user, m, true); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("stale preview accepted", e)
	}
	preview, _, e = invest.AssetActivation(ctx, tenant, user, m, false)
	if e != nil {
		t.Fatal(e)
	}
	m.PreviewHash = preview.PreviewHash
	_, activated, e := invest.AssetActivation(ctx, tenant, user, m, true)
	if e != nil || activated.InvestmentMode != "ledger" || activated.Balances[0].Amount.MinorUnits != 110000 {
		t.Fatal(activated, e)
	}
	_, retry, e := invest.AssetActivation(ctx, tenant, user, m, true)
	if e != nil || retry.Version != activated.Version {
		t.Fatal("activation retry", retry, e)
	}
	before, e := assets.GetAccount(ctx, tenant, account.ID, cut.Add(-time.Microsecond))
	if e != nil || before.ValuationStatus != "manual" || before.Balances[0].Amount.MinorUnits != 110000 {
		t.Fatal(before, e)
	}
	if _, e = svc.CreateSnapshot(ctx, d.AssetSnapshot{TenantID: tenant, AccountID: account.ID, UserID: user, Kind: "valuation", Amount: d.Money{CurrencyCode: "RUB", MinorUnits: 999999}, AsOf: cut.Add(time.Hour)}, "bypass"); !errors.Is(e, d.ErrAssetPrecondition) {
		t.Fatal("manual cutover bypass", e)
	}
	if _, e = svc.UpdateSnapshot(ctx, valuation); !errors.Is(e, d.ErrAssetPrecondition) {
		t.Fatal("post-cutover snapshot edit", e)
	}
	if e = svc.DeleteSnapshot(ctx, tenant, valuation.ID, valuation.Version); !errors.Is(e, d.ErrAssetPrecondition) {
		t.Fatal("post-cutover snapshot delete", e)
	}
	if e = svc.DeleteTransfer(ctx, tenant, oldTransfer.ID, oldTransfer.Version); !errors.Is(e, d.ErrAssetPrecondition) {
		t.Fatal("historical linked transfer delete", e)
	}
	_, e = invest.Mutate(ctx, tenant, user, inv.Mutation{Action: "create", RequestKey: "later-deposit", ExpectedLedgerVersion: 2, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "deposit", Currency: "RUB", Amount: "25", OccurredAt: cut.Add(time.Hour)}})
	if e != nil {
		t.Fatal(e)
	}
	overview, e := svc.GetOverview(ctx, tenant, "RUB", cut.Add(2*time.Hour))
	if e != nil || overview.Incomplete || overview.Total.MinorUnits != 302500 {
		t.Fatal("double counted manual value or transfer", overview, e)
	}
	current, e := svc.GetAccount(ctx, tenant, account.ID)
	if e != nil || current.Balances[0].Amount.MinorUnits != 112500 {
		t.Fatal(current, e)
	}
	if _, e = svc.SetArchived(ctx, tenant, account.ID, activated.Version, true); !errors.Is(e, d.ErrAssetPrecondition) {
		t.Fatal("bound account archived", e)
	}
	assertInvestmentRollbackRefused(t, ctx, pool, "0048_investment_asset_activation")
	_, e = invest.Mutate(ctx, tenant, user, inv.Mutation{Action: "create", RequestKey: "unmapped-buy", ExpectedLedgerVersion: 3, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "buy", InstrumentKey: "UNMAPPED", Currency: "RUB", Quantity: "1", Amount: "100", OccurredAt: cut.Add(3 * time.Hour)}})
	if e != nil {
		t.Fatal(e)
	}
	incomplete, e := svc.GetOverview(ctx, tenant, "RUB", cut.Add(4*time.Hour))
	if e != nil || !incomplete.Incomplete || incomplete.Total.MinorUnits != 292500 {
		t.Fatal("missing quote used manual fallback", incomplete, e)
	}
	for _, a := range incomplete.Accounts {
		if a.ID == account.ID && (!a.ValuationIncomplete || a.ValuationStatus != "incomplete" || len(a.ValuationIssues) == 0) {
			t.Fatal("missing quality", a)
		}
	}
	// Savings FX follows its CBR/manual convention even without a registered
	// investment market-FX policy. A rate correction fences the prepared switch.
	foreignAsset := create("investment", 110000)
	usd, e := invest.CreateLedger(ctx, tenant, user, "USD cash", at, "usd-ledger")
	if e != nil {
		t.Fatal(e)
	}
	_, e = invest.Mutate(ctx, tenant, user, inv.Mutation{Action: "create", RequestKey: "usd-cash", ExpectedLedgerVersion: 1, Operation: d.InvestmentOperation{LedgerID: usd.ID, Kind: "deposit", Currency: "USD", Amount: "10", OccurredAt: at}})
	if e != nil {
		t.Fatal(e)
	}
	rates := NewFxRepo(pool)
	if _, e = rates.UpsertRate(ctx, "USD", "RUB", "110", at, "cbr"); e != nil {
		t.Fatal(e)
	}
	foreignRequest := d.InvestmentAssetActivation{AccountID: foreignAsset.ID, LedgerID: usd.ID, ValuationFrom: cut}
	pending, e := svc.CreateSnapshot(ctx, d.AssetSnapshot{TenantID: tenant, AccountID: foreignAsset.ID, UserID: user, Kind: "valuation", Amount: d.Money{CurrencyCode: "RUB", MinorUnits: 110000}, AsOf: cut.Add(time.Hour)}, "future-manual")
	if e != nil {
		t.Fatal(e)
	}
	blocked, _, e := invest.AssetActivation(ctx, tenant, user, foreignRequest, false)
	if e != nil || blocked.Complete || len(blocked.Issues) == 0 {
		t.Fatal("future manual valuation ignored", blocked, e)
	}
	if e = svc.DeleteSnapshot(ctx, tenant, pending.ID, pending.Version); e != nil {
		t.Fatal(e)
	}
	fxPreview, _, e := invest.AssetActivation(ctx, tenant, user, foreignRequest, false)
	if e != nil || !fxPreview.Matches {
		t.Fatal("CBR valuation", fxPreview, e)
	}
	foreignRequest.PreviewHash = fxPreview.PreviewHash
	foreignRequest.ExpectedAccountVersion = 1
	foreignRequest.ExpectedLedgerVersion = 2
	foreignRequest.Evidence = "USD statement and effective CBR rate"
	foreignRequest.RequestKey = "activate-usd"
	if _, e = rates.UpsertRate(ctx, "USD", "RUB", "100", at, "cbr"); e != nil {
		t.Fatal(e)
	}
	if _, _, e = invest.AssetActivation(ctx, tenant, user, foreignRequest, true); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("FX correction ignored", e)
	}
	mismatch, _, e := invest.AssetActivation(ctx, tenant, user, foreignRequest, false)
	if e != nil || !mismatch.Complete || mismatch.Matches {
		t.Fatal("mismatch preview", mismatch, e)
	}
	foreignRequest.PreviewHash = mismatch.PreviewHash
	if _, _, e = invest.AssetActivation(ctx, tenant, user, foreignRequest, true); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal("mismatched values activated", e)
	}
	if _, e = rates.UpsertRate(ctx, "USD", "RUB", "110", at, "manual"); e != nil {
		t.Fatal(e)
	}
	fxPreview, _, e = invest.AssetActivation(ctx, tenant, user, foreignRequest, false)
	if e != nil || !fxPreview.Matches {
		t.Fatal("manual same-date priority", fxPreview, e)
	}
	foreignRequest.PreviewHash = fxPreview.PreviewHash
	type answer struct {
		account d.AssetAccount
		err     error
	}
	answers := make(chan answer, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, a, e := invest.AssetActivation(ctx, tenant, user, foreignRequest, true)
			answers <- answer{a, e}
		}()
	}
	wg.Wait()
	close(answers)
	for result := range answers {
		if result.err != nil || result.account.Version != 2 || result.account.Balances[0].Amount.MinorUnits != 110000 {
			t.Fatal("concurrent activation retry", result)
		}
	}
	bundle, e := journal.ExportInvestmentArchive(ctx, tenant, user)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = archive.VerifyJournal(bundle.Data); e != nil {
		t.Fatal("activated investment journal archive", e)
	}
	if bundle.Data.Excluded["investment_asset_activations"] != "budget_integration_requires_full_backup" {
		t.Fatal("activation portability boundary hidden")
	}
	var hasEvidence bool
	if e = pool.DB.QueryRow(ctx, `SELECT comparison ? 'Inventory' AND comparison ? 'Result' FROM investment_asset_activations WHERE tenant_id=$1 AND account_id=$2`, tenant, foreignAsset.ID).Scan(&hasEvidence); e != nil || !hasEvidence {
		t.Fatal("comparison audit missing", e)
	}

	// One journal cannot contribute its value through two savings accounts.
	duplicate := create("investment", 110000)
	duplicateRequest := d.InvestmentAssetActivation{AccountID: duplicate.ID, LedgerID: usd.ID, ValuationFrom: cut}
	if _, _, e = invest.AssetActivation(ctx, tenant, user, duplicateRequest, false); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal("duplicate journal binding", e)
	}

	// A committed FX correction between native NAV and overview conversion must
	// not mix two rate snapshots or manufacture a loss in the conversion back.
	// Savings manual overrides are pair-specific; supply the inverse pair
	// explicitly rather than assuming the legacy CBR cross resolver inverts it.
	if _, e = rates.UpsertRate(ctx, "RUB", "USD", "0.00909091", at, "manual"); e != nil {
		t.Fatal(e)
	}
	coherent := asset.NewService(assets, activationRateHook{repo: rates, before: func() {
		if _, err := rates.UpsertRate(ctx, "RUB", "USD", "0.00454545", at, "manual"); err != nil {
			t.Fatal(err)
		}
	}}, NewTenantRepo(pool))
	fixed, e := coherent.GetOverview(ctx, tenant, "USD", cut.Add(4*time.Hour))
	if e != nil || len(fixed.Rates) != 1 || fixed.Rates[0].RateDecimal != "0.00909091" {
		t.Fatal("mixed NAV/overview FX snapshots", fixed, e)
	}
	currentRate, _, e := rates.GetRateAsOf(ctx, "RUB", "USD", cut.Add(4*time.Hour))
	if e != nil || currentRate != "0.00454545" {
		t.Fatal("rate correction did not commit", currentRate, e)
	}

}

type activationRateHook struct {
	repo   *FxRepo
	before func()
}

func (h activationRateHook) GetAssetRates(ctx context.Context, codes []string, target string, at time.Time) ([]d.AssetFxRate, error) {
	h.before()
	return h.repo.GetAssetRates(ctx, codes, target, at)
}
