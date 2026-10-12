package postgres

import (
	"context"
	"errors"
	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	"github.com/positron48/budget/internal/usecase/asset"
	inv "github.com/positron48/budget/internal/usecase/investment"
	"testing"
	"time"
)

func TestInvestmentCashTransferReadOnlyPreviewCivilDateAndFinancialHash(t *testing.T) {
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
	m := inv.CashTransferMutation{Action: "create", RequestKey: "fund-date", ExpectedLedgerVersion: 1, ExpectedFromAccountVersion: bank.Version, ExpectedToAccountVersion: account.Version, Timing: &d.InvestmentOperationTiming{Timezone: "Europe/Moscow", OccurredDate: "2024-01-02"}, Transfer: d.AssetTransfer{FromAccountID: bank.ID, ToAccountID: account.ID, FromAmount: d.Money{CurrencyCode: "RUB", MinorUnits: 12345}, ToAmount: d.Money{CurrencyCode: "RUB", MinorUnits: 12345}}}
	preview, e := svc.PreviewCashTransfer(ctx, tenant, user, m)
	if e != nil || !preview.Preview.Valid || preview.Transfer.Transfer.OccurredAt.Format(time.RFC3339Nano) != "2024-01-02T20:59:59.999999Z" {
		t.Fatal(preview, e)
	}
	if len(preview.Accounts) == 0 || !preview.Accounts[0].BeforeKnown || preview.Accounts[0].BeforeMinor != 100000 || preview.Accounts[0].AfterMinor != 87655 {
		t.Fatal(preview.Accounts)
	}
	var n int
	if e = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_cash_links WHERE tenant_id=$1`, tenant).Scan(&n); e != nil || n != 0 {
		t.Fatal(n, e)
	}
	// EXPLAIN the exact source-history query with its actual account binding.
	rows, e := pool.DB.Query(ctx, "EXPLAIN "+cashTransferHistorySQL, tenant, bank.ID)
	if e != nil {
		t.Fatal(e)
	}
	for rows.Next() {
		var line string
		if e = rows.Scan(&line); e != nil {
			t.Fatal(e)
		}
	}
	if e = rows.Err(); e != nil {
		t.Fatal(e)
	}
	rows.Close()
	// A real reconciliation changes financial history without changing account
	// metadata version; the old preview must not authorize writing afterward.
	if _, e = aSvc.CreateSnapshot(ctx, d.AssetSnapshot{TenantID: tenant, UserID: user, AccountID: bank.ID, Kind: "reconciliation", Amount: d.Money{CurrencyCode: "RUB", MinorUnits: 90000}, AsOf: startForBridgeTest(at)}, "reconcile"); e != nil {
		t.Fatal(e)
	}
	m.PreviewHash = preview.Preview.Hash
	if _, e = svc.CashTransfer(ctx, tenant, user, m); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("stale bank history", e)
	}
	preview, e = svc.PreviewCashTransfer(ctx, tenant, user, m)
	if e != nil || !preview.Preview.Valid {
		t.Fatal(preview, e)
	}
	m.PreviewHash = preview.Preview.Hash
	funded, e := svc.CashTransfer(ctx, tenant, user, m)
	if e != nil || funded.Transfer.ID != preview.Transfer.Transfer.ID || funded.OperationID != preview.Transfer.OperationID || funded.Timing.OccurredDate != "2024-01-02" {
		t.Fatal(funded, e)
	}
	if retry, e := svc.CashTransfer(ctx, tenant, user, m); e != nil || retry.Transfer.ID != funded.Transfer.ID {
		t.Fatal(retry, e)
	}
	bankState, e := assets.GetAccount(ctx, tenant, bank.ID, time.Now())
	if e != nil || bankState.Balances[0].Amount.MinorUnits != 77655 {
		t.Fatal(bankState, e)
	}
	// Move the document behind a later bank observation. Its bank effect becomes
	// absorbed by that observation, while the journal still moves on the new date.
	if _, e = aSvc.CreateSnapshot(ctx, d.AssetSnapshot{TenantID: tenant, UserID: user, AccountID: bank.ID, Kind: "reconciliation", Amount: d.Money{CurrencyCode: "RUB", MinorUnits: 80000}, AsOf: at.AddDate(0, 0, 3)}, "later-reconcile"); e != nil {
		t.Fatal(e)
	}
	amend := m
	amend.Action = "amend"
	amend.Transfer = funded.Transfer
	amend.ExpectedVersion = 1
	amend.ExpectedLedgerVersion = 2
	amend.RequestKey = "amend-date"
	amend.Reason = "Correct report date"
	amend.PreviewHash = ""
	amend.Transfer.OccurredAt = time.Time{}
	amend.Timing = &d.InvestmentOperationTiming{Timezone: "Europe/Moscow", OccurredDate: "2024-01-05"}
	preview, e = svc.PreviewCashTransfer(ctx, tenant, user, amend)
	if e != nil || !preview.Preview.Valid {
		t.Fatal(preview, e)
	}
	found := false
	for _, a := range preview.Accounts {
		if a.Stage == "current" {
			found = true
			if a.BeforeMinor != 80000 || a.AfterMinor != 67655 {
				t.Fatal(a)
			}
		}
	}
	if !found {
		t.Fatal(preview)
	}
	amend.PreviewHash = preview.Preview.Hash
	funded, e = svc.CashTransfer(ctx, tenant, user, amend)
	if e != nil {
		t.Fatal(e)
	}
	cancel := amend
	cancel.Action = "void"
	cancel.Transfer = funded.Transfer
	cancel.ExpectedVersion = 2
	cancel.ExpectedLedgerVersion = 3
	cancel.PreviewHash = ""
	cancel.RequestKey = "void"
	cancel.Reason = "Cancel both sides"
	preview, e = svc.PreviewCashTransfer(ctx, tenant, user, cancel)
	if e != nil || !preview.Preview.Valid {
		t.Fatal(preview, e)
	}
	cancel.PreviewHash = preview.Preview.Hash
	if _, e = svc.CashTransfer(ctx, tenant, user, cancel); e != nil {
		t.Fatal(e)
	}
	bankState, e = assets.GetAccount(ctx, tenant, bank.ID, time.Now())
	if e != nil || bankState.Balances[0].Amount.MinorUnits != 80000 {
		t.Fatal(bankState, e)
	}
	snapshot, e := svc.Snapshot(ctx, tenant, user, ledger.ID, time.Now())
	if e != nil || len(snapshot.Cash) != 0 {
		t.Fatal(snapshot, e)
	}
}
func startForBridgeTest(at time.Time) time.Time { return at.Add(12 * time.Hour) }
