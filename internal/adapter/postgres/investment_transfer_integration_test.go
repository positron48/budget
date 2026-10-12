package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	archive "github.com/positron48/budget/internal/usecase/investmentarchive"
)

func TestInvestmentTransferAtomicity(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, foreign, user string
	for _, f := range []struct {
		q   string
		out *string
	}{
		{`INSERT INTO tenants(name,default_currency_code) VALUES('Transfers','RUB') RETURNING id`, &tenant},
		{`INSERT INTO tenants(name,default_currency_code) VALUES('Other','RUB') RETURNING id`, &foreign},
		{`INSERT INTO users(email,password_hash) VALUES('` + uuid.NewString() + `@example.test','test') RETURNING id`, &user},
	} {
		if err := pool.DB.QueryRow(ctx, f.q).Scan(f.out); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$3,'owner'),($2,$3,'owner')`, tenant, foreign, user); err != nil {
		t.Fatal(err)
	}
	defer func() {
		bundle, err := NewInvestmentRepo(pool).ExportInvestmentArchive(ctx, tenant, user)
		if err != nil {
			t.Error(err)
			return
		}
		if _, err = archive.VerifyJournal(bundle.Data); err != nil {
			t.Error("transfer archive verification", err)
		}
		for _, corruption := range []string{"missing_link", "wrong_amount", "wrong_counterparty"} {
			raw, _ := json.Marshal(bundle.Data)
			var changed archive.Dataset
			if err := json.Unmarshal(raw, &changed); err != nil {
				t.Error(err)
				return
			}
			for i := range changed.Tables {
				table := &changed.Tables[i]
				if table.Name != "investment_transfers" || len(table.Rows) == 0 {
					continue
				}
				if corruption == "missing_link" {
					table.Rows = nil
					continue
				}
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(table.Rows[0], &fields); err != nil {
					t.Error(err)
					return
				}
				var tr d.InvestmentTransfer
				if err := json.Unmarshal(fields["data"], &tr); err != nil {
					t.Error(err)
					return
				}
				if corruption == "wrong_amount" {
					tr.Amount = "999"
				} else {
					tr.ToLedgerID = tr.FromLedgerID
				}
				fields["data"], _ = json.Marshal(tr)
				table.Rows[0], _ = json.Marshal(fields)
			}
			if _, err := archive.VerifyJournal(changed); err == nil {
				t.Error("accepted transfer corruption", corruption)
			}
		}
	}()
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	day := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	a, err := svc.CreateLedger(ctx, tenant, user, "A", day, "a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.CreateLedger(ctx, tenant, user, "B", day, "b")
	if err != nil {
		t.Fatal(err)
	}
	other, err := svc.CreateLedger(ctx, foreign, user, "Other", day, "other")
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Mutate(ctx, tenant, user, inv.Mutation{Action: "create", RequestKey: "fund", ExpectedLedgerVersion: 1, Operation: d.InvestmentOperation{LedgerID: a.ID, Kind: "deposit", Currency: "USD", Amount: "1000", OccurredAt: day}})
	if err != nil {
		t.Fatal(err)
	}
	m := inv.TransferMutation{Action: "create", RequestKey: strings.Repeat("k", 128), ExpectedFromVersion: 2, ExpectedToVersion: 1, Transfer: d.InvestmentTransfer{FromLedgerID: a.ID, ToLedgerID: b.ID, Currency: "USD", Amount: "300", OccurredAt: day.Add(time.Hour)}}
	transfer, err := svc.Transfer(ctx, tenant, user, m)
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.Transfer(ctx, tenant, user, m)
	if err != nil || again.ID != transfer.ID {
		t.Fatal(again, err)
	}
	snapshot := func(id string) d.InvestmentSnapshot {
		v, e := svc.Snapshot(ctx, tenant, user, id, day.Add(10*time.Hour))
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	if snapshot(a.ID).Cash[0].Economic != "700" || snapshot(b.ID).Cash[0].Economic != "300" {
		t.Fatal("C06 transfer balances")
	}
	if _, err = svc.GetTransfer(ctx, foreign, user, transfer.ID); !errors.Is(err, d.ErrInvestmentNotFound) {
		t.Fatal("transfer tenant leak", err)
	}
	m.Action = "amend"
	m.Transfer.ID = transfer.ID
	m.ExpectedVersion = 1
	m.ExpectedFromVersion = 3
	m.ExpectedToVersion = 2
	m.Reason = "correction"
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i, amount := range []string{"200", "250"} {
		wg.Add(1)
		go func(i int, amount string) {
			defer wg.Done()
			edit := m
			edit.RequestKey = "race-" + strconv.Itoa(i)
			edit.Transfer.Amount = amount
			_, e := svc.Transfer(ctx, tenant, user, edit)
			results <- e
		}(i, amount)
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, d.ErrInvestmentConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal(success, conflict)
	}
	transfer, err = svc.GetTransfer(ctx, tenant, user, transfer.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Ordinary API must never cancel just one side.
	_, err = svc.Mutate(ctx, tenant, user, inv.Mutation{Action: "void", RequestKey: "one-side", Reason: "invalid", ExpectedLedgerVersion: 4, ExpectedOperationVersion: 2, Operation: d.InvestmentOperation{ID: transfer.FromOperationID, LedgerID: a.ID}})
	if !errors.Is(err, d.ErrInvestmentPrecondition) {
		t.Fatal("independent transfer edit", err)
	}
	cross := m
	cross.Action = "create"
	cross.Transfer.ID = ""
	cross.Transfer.ToLedgerID = other.ID
	cross.RequestKey = "cross-tenant"
	cross.ExpectedFromVersion = 4
	cross.ExpectedToVersion = 1
	if _, err = svc.Transfer(ctx, tenant, user, cross); !errors.Is(err, d.ErrInvestmentNotFound) {
		t.Fatal("foreign target", err)
	}
	if snapshot(a.ID).Ledger.Version != 4 {
		t.Fatal("partial foreign transfer")
	}
	amount, _ := strconv.Atoi(transfer.Amount)
	withdrawal, err := svc.Mutate(ctx, tenant, user, inv.Mutation{Action: "create", RequestKey: "spend", ExpectedLedgerVersion: 3, Operation: d.InvestmentOperation{LedgerID: b.ID, Kind: "withdrawal", Currency: "USD", Amount: strconv.Itoa(amount - 50), OccurredAt: day.Add(2 * time.Hour)}})
	if err != nil {
		t.Fatal(err)
	}
	cancel := inv.TransferMutation{Action: "void", RequestKey: "void-transfer", Reason: "cancel", ExpectedVersion: 2, ExpectedFromVersion: 4, ExpectedToVersion: 4, Transfer: transfer}
	if _, err = svc.Transfer(ctx, tenant, user, cancel); !errors.Is(err, d.ErrInvestmentPrecondition) {
		t.Fatal("dependent cancellation", err)
	}
	if snapshot(a.ID).Ledger.Version != 4 || snapshot(b.ID).Ledger.Version != 4 || snapshot(b.ID).Cash[0].Economic != "50" {
		t.Fatal("partial failed cancellation")
	}
	m.Transfer = transfer
	m.Transfer.Amount = strconv.Itoa(amount + 100)
	m.ExpectedVersion = 2
	m.ExpectedFromVersion = 4
	m.ExpectedToVersion = 4
	m.RequestKey = "increase"
	transfer, err = svc.Transfer(ctx, tenant, user, m)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot(b.ID).Cash[0].Economic != "150" {
		t.Fatal("amended both sides")
	}
	_, err = svc.Mutate(ctx, tenant, user, inv.Mutation{Action: "void", RequestKey: "undo-spend", Reason: "cancel", ExpectedLedgerVersion: 5, ExpectedOperationVersion: 1, Operation: d.InvestmentOperation{ID: withdrawal.ID, LedgerID: b.ID}})
	if err != nil {
		t.Fatal(err)
	}
	cancel.ExpectedVersion = 3
	cancel.ExpectedFromVersion = 5
	cancel.ExpectedToVersion = 6
	cancelled, err := svc.Transfer(ctx, tenant, user, cancel)
	if err != nil || cancelled.Status != "voided" {
		t.Fatal(cancelled, err)
	}
	if _, err = svc.Transfer(ctx, tenant, user, cancel); err != nil {
		t.Fatal("cancel retry", err)
	}
	if snapshot(a.ID).Cash[0].Economic != "1000" || len(snapshot(b.ID).Cash) != 0 {
		t.Fatal("cancel balances")
	}
	var requests int
	if err = pool.DB.QueryRow(ctx, `SELECT count(*) FROM financial_write_requests WHERE tenant_id=$1 AND request_key IN ('one-side','cross-tenant')`, tenant).Scan(&requests); err != nil || requests != 0 {
		t.Fatal("partial requests", requests, err)
	}
	assertInvestmentRollbackRefused(t, ctx, pool, "0017_investment_transfers")
	applyMigrations(t, ctx, pool.DB)
}
