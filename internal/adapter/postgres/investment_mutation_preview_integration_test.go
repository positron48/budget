package postgres

import (
	"context"
	"errors"
	"fmt"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	"reflect"
	"testing"
	"time"
)

func TestInvestmentMutationPreviewMatchesWriterAndFences(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx := context.Background()
	var tenant, user, other string
	for _, v := range []struct {
		q string
		p *string
	}{{`INSERT INTO tenants(name,default_currency_code) VALUES('Preview','USD') RETURNING id`, &tenant}, {fmt.Sprintf(`INSERT INTO users(email,password_hash) VALUES('preview-%d@example.test','x') RETURNING id`, time.Now().UnixNano()), &user}, {`INSERT INTO tenants(name,default_currency_code) VALUES('Other','USD') RETURNING id`, &other}} {
		if e := pool.DB.QueryRow(ctx, v.q).Scan(v.p); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner'),($3,$2,'owner')`, tenant, user, other); e != nil {
		t.Fatal(e)
	}
	repo := NewInvestmentRepo(pool)
	svc := inv.NewService(repo, NewTenantRepo(pool))
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	l, e := svc.CreateLedger(ctx, tenant, user, "Broker", at, "ledger")
	if e != nil {
		t.Fatal(e)
	}
	version := int64(1)
	write := func(o d.InvestmentOperation, key string) d.InvestmentOperation {
		t.Helper()
		o.LedgerID = l.ID
		v, e := svc.Mutate(ctx, tenant, user, inv.Mutation{Action: "create", Operation: o, ExpectedLedgerVersion: version, RequestKey: key})
		if e != nil {
			t.Fatal(e)
		}
		version++
		return v
	}
	deposit := write(d.InvestmentOperation{Kind: "deposit", Currency: "USD", Amount: "100", OccurredAt: at}, "fund")
	buy := write(d.InvestmentOperation{Kind: "buy", InstrumentKey: "stock", Currency: "USD", Quantity: "3", Amount: "30", OccurredAt: at.Add(time.Hour)}, "buy")
	sell := write(d.InvestmentOperation{Kind: "sell", InstrumentKey: "stock", Currency: "USD", Quantity: "2", Amount: "25", OccurredAt: at.AddDate(0, 0, 1)}, "sell")
	state := func() string {
		var s string
		e := pool.DB.QueryRow(ctx, `SELECT jsonb_build_object('ledger',to_jsonb(l),'operations',(SELECT count(*) FROM investment_operations WHERE tenant_id=$1),'revisions',(SELECT count(*) FROM investment_operation_revisions WHERE tenant_id=$1),'requests',(SELECT count(*) FROM financial_write_requests WHERE tenant_id=$1),'jobs',(SELECT count(*) FROM investment_recalculation_jobs WHERE tenant_id=$1),'cash',(SELECT count(*) FROM investment_cash_legs WHERE tenant_id=$1),'securities',(SELECT count(*) FROM investment_security_legs WHERE tenant_id=$1))::text FROM investment_ledgers l WHERE tenant_id=$1 AND id=$2`, tenant, l.ID).Scan(&s)
		if e != nil {
			t.Fatal(e)
		}
		return s
	}
	original := state()
	bad := buy
	bad.Quantity = "1"
	amend := inv.Mutation{Action: "amend", Operation: bad, ExpectedLedgerVersion: version, ExpectedOperationVersion: buy.Version, Reason: "Statement correction", RequestKey: "reduce-buy"}
	preview, e := svc.PreviewMutation(ctx, tenant, user, amend)
	if e != nil || preview.Valid || len(preview.Issues) != 1 || preview.Issues[0].OperationID != sell.ID {
		t.Fatal("later sale must fail", e, preview)
	}
	void := inv.Mutation{Action: "void", Operation: deposit, ExpectedLedgerVersion: version, ExpectedOperationVersion: deposit.Version, Reason: "Missing funding", RequestKey: "void-deposit"}
	preview, e = svc.PreviewMutation(ctx, tenant, user, void)
	if e != nil || preview.Valid || preview.Issues[0].Code != "negative_cash" {
		t.Fatal("cash failure", e, preview)
	}
	if state() != original {
		t.Fatal("preview wrote facts/counters/jobs")
	}
	m := inv.Mutation{Action: "create", Operation: d.InvestmentOperation{LedgerID: l.ID, Kind: "buy", InstrumentKey: "stock", Currency: "USD", Quantity: "1", Amount: "10", OccurredAt: at.Add(2 * time.Hour), SettlementAt: at.AddDate(0, 0, 2)}, ExpectedLedgerVersion: version, RequestKey: "preview-buy"}
	preview, e = svc.PreviewMutation(ctx, tenant, user, m)
	if e != nil || !preview.Valid || len(preview.Hash) != 64 || len(preview.Comparisons) != 3 {
		t.Fatal("valid preview", e, preview)
	}
	if state() != original {
		t.Fatal("valid preview wrote")
	}
	m.PreviewHash = preview.Hash
	changed := m
	changed.Operation.Amount = "11"
	if _, e = svc.Mutate(ctx, tenant, user, changed); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("edited payload accepted", e)
	}
	if state() != original {
		t.Fatal("failed preview fence wrote")
	}
	posted, e := svc.Mutate(ctx, tenant, user, m)
	if e != nil {
		t.Fatal("reviewed commit", e)
	}
	version++
	snap, e := svc.Snapshot(ctx, tenant, user, l.ID, time.Now().UTC())
	after := preview.Comparisons[2].After
	for i := range after.Positions {
		for j := range after.Positions[i].Lots {
			lot := &after.Positions[i].Lots[j]
			if lot.OriginID == preview.Proposed.ID {
				lot.OriginID = posted.ID
			}
			if lot.AcquisitionOperationID == preview.Proposed.ID {
				lot.AcquisitionOperationID = posted.ID
			}
		}
	}
	if e != nil || !reflect.DeepEqual(snap.Cash, after.Cash) || !reflect.DeepEqual(snap.Positions, after.Positions) {
		t.Fatal("preview differs from writer", e, snap, after)
	}
	// IDs are assigned on commit; the deterministic preview ID is only hypothetical.
	if posted.ID == preview.Proposed.ID {
		t.Fatal("preview reserved operation identity")
	}
	fee := inv.Mutation{Action: "create", Operation: d.InvestmentOperation{LedgerID: l.ID, Kind: "fee", Currency: "USD", Amount: "0.123456789012", OccurredAt: at.AddDate(0, 0, 3)}, ExpectedLedgerVersion: version, RequestKey: "fee"}
	reviewed, e := svc.PreviewMutation(ctx, tenant, user, fee)
	if e != nil || !reviewed.Valid {
		t.Fatal(e)
	}
	// A metadata edit changes the reviewed account context without changing financial version.
	_, e = svc.UpdateLedger(ctx, tenant, user, inv.LedgerUpdate{ID: l.ID, Action: "edit", Name: "Renamed", Broker: "Broker", AccountType: "regular", DefaultCurrency: "USD", ExpectedMetadataVersion: 1, RequestKey: "rename", Reason: "Details"})
	if e != nil {
		t.Fatal(e)
	}
	fee.PreviewHash = reviewed.Hash
	if _, e = svc.Mutate(ctx, tenant, user, fee); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("stale context accepted", e)
	}
	fee.PreviewHash = ""
	reviewed, e = svc.PreviewMutation(ctx, tenant, user, fee)
	if e != nil {
		t.Fatal(e)
	}
	fee.PreviewHash = reviewed.Hash
	if _, e = svc.Mutate(ctx, tenant, user, fee); e != nil {
		t.Fatal(e)
	}
	beforeRetry := state()
	if _, e = svc.Mutate(ctx, tenant, user, fee); e != nil {
		t.Fatal("idempotent reviewed retry", e)
	}
	if state() != beforeRetry {
		t.Fatal("retry created another fact")
	}
	version++ // fee commit
	second, e := svc.CreateLedger(ctx, tenant, user, "Second", at, "second")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.Transfer(ctx, tenant, user, inv.TransferMutation{Action: "create", Transfer: d.InvestmentTransfer{FromLedgerID: l.ID, ToLedgerID: second.ID, InstrumentKey: "stock", Quantity: "1", Currency: "USD", Amount: "0", OccurredAt: at.AddDate(0, 0, 4)}, ExpectedFromVersion: version, ExpectedToVersion: 1, RequestKey: "move-security"}); e != nil {
		t.Fatal(e)
	}
	version++
	connectedFee := fee
	connectedFee.ExpectedLedgerVersion = version
	connectedFee.RequestKey = "connected-fee"
	connectedFee.PreviewHash = ""
	connectedPreview, e := svc.PreviewMutation(ctx, tenant, user, connectedFee)
	if e != nil || !connectedPreview.Valid || len(connectedPreview.AffectedLedgerIDs) != 2 || len(connectedPreview.Comparisons) != 6 {
		t.Fatal("linked preview", e, connectedPreview)
	}
	if _, e = svc.Mutate(ctx, tenant, user, inv.Mutation{Action: "create", Operation: d.InvestmentOperation{LedgerID: second.ID, Kind: "deposit", Currency: "USD", Amount: "1", OccurredAt: at.AddDate(0, 0, 5)}, ExpectedLedgerVersion: 2, RequestKey: "connected-deposit"}); e != nil {
		t.Fatal(e)
	}
	connectedFee.PreviewHash = connectedPreview.Hash
	if _, e = svc.Mutate(ctx, tenant, user, connectedFee); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("linked ledger change accepted", e)
	}
	fee.Operation.LedgerID = l.ID
	if _, e = svc.PreviewMutation(ctx, other, user, fee); !errors.Is(e, d.ErrInvestmentNotFound) {
		t.Fatal("foreign preview", e)
	}
}
