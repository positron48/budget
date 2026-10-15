package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	archive "github.com/positron48/budget/internal/usecase/investmentarchive"
)

func TestInvestmentForecastPersistenceLinksAndRestore(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, foreign, actor, outsider string
	for _, v := range []struct {
		sql string
		out *string
	}{{`INSERT INTO tenants(name,default_currency_code) VALUES('Forecast','USD') RETURNING id`, &tenant}, {`INSERT INTO tenants(name,default_currency_code) VALUES('Foreign','USD') RETURNING id`, &foreign}, {fmt.Sprintf(`INSERT INTO users(email,password_hash) VALUES('forecast-%d@example.test','fixture') RETURNING id`, time.Now().UnixNano()), &actor}, {fmt.Sprintf(`INSERT INTO users(email,password_hash) VALUES('forecast-outsider-%d@example.test','fixture') RETURNING id`, time.Now().UnixNano()), &outsider}} {
		if e := pool.DB.QueryRow(ctx, v.sql).Scan(v.out); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner'),($3,$2,'owner')`, tenant, actor, foreign); e != nil {
		t.Fatal(e)
	}
	repo := NewInvestmentRepo(pool)
	svc := inv.NewService(repo, NewTenantRepo(pool))
	day := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ledger, e := svc.CreateLedger(ctx, tenant, actor, "Forecast", day, "forecast-ledger")
	if e != nil {
		t.Fatal(e)
	}
	version := int64(1)
	create := func(kind, amount, quantity, key string) d.InvestmentOperation {
		t.Helper()
		o := d.InvestmentOperation{LedgerID: ledger.ID, Kind: kind, Currency: "USD", Amount: amount, Quantity: quantity, OccurredAt: day.Add(time.Duration(version) * time.Hour), SettlementAt: day.Add(time.Duration(version) * time.Hour)}
		if kind != "deposit" {
			o.InstrumentKey = "STOCK"
		}
		saved, e := svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: key, ExpectedLedgerVersion: version, Operation: o})
		if e != nil {
			t.Fatal(e)
		}
		version++
		return saved
	}
	create("deposit", "1000", "0", "fund")
	create("buy", "100", "10", "buy")
	current := time.Now().UTC().Format("2006-01")
	event := inv.ForecastEvent{ID: uuid.NewString(), InstrumentKey: "STOCK", Kind: "dividend", Nature: "announced", Currency: "USD", PerShare: "2", RecordDate: "2024-01-10", PaymentDate: current + "-28", Timezone: "Europe/Moscow", EntitlementRule: "settled_by_cutoff", EntitlementAt: day.AddDate(0, 0, 9), SourceReference: "Synthetic issuer", Evidence: "Synthetic explicit rights rule", Reason: "Reviewed fixture"}
	saved, e := svc.SaveForecastEvent(ctx, tenant, actor, event, 0)
	if e != nil || saved.Version != 1 {
		t.Fatal(e, saved)
	}
	if retry, e := svc.SaveForecastEvent(ctx, tenant, actor, event, 0); e != nil || retry.Version != 1 {
		t.Fatal("event retry", e)
	}
	wrong := event
	wrong.PerShare = "3"
	if _, e = svc.SaveForecastEvent(ctx, tenant, actor, wrong, 0); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("event lost update", e)
	}
	q := inv.ForecastQuery{FromMonth: current, Currency: "USD", TaxRate: "10"}
	report, e := svc.Forecast(ctx, tenant, actor, q)
	if e != nil || len(report.Rows) != 1 || report.Gross.RatString() != "20" || report.EstimatedNet.RatString() != "18" {
		t.Fatal(e, report)
	}
	// A forecast read or elapsed payment date must not post cash.
	var operationCount int
	if e = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_operations WHERE tenant_id=$1`, tenant).Scan(&operationCount); e != nil || operationCount != 2 {
		t.Fatal("forecast posted cash", operationCount, e)
	}
	actual := create("dividend", "18", "0", "actual")
	link := inv.ForecastLink{EventID: event.ID, EventVersion: saved.Version, LedgerID: ledger.ID, OperationID: actual.ID, OperationVersion: actual.Version, Reason: "Synthetic statement partial"}
	linked, e := svc.SaveForecastLink(ctx, tenant, actor, link, 0)
	if e != nil || linked.Version != 1 {
		t.Fatal(e, linked)
	}
	report, e = svc.Forecast(ctx, tenant, actor, q)
	if e != nil || report.Gross != nil || !slices.Contains(report.Rows[0].Issues, "partial_actual_payment") {
		t.Fatal("partial credit certified", e, report)
	}
	link.Complete = true
	link.Reason = "Statement reconciliation complete"
	linked, e = svc.SaveForecastLink(ctx, tenant, actor, link, 1)
	if e != nil || linked.Version != 2 {
		t.Fatal(e, linked)
	}
	if _, e = svc.SaveForecastLink(ctx, tenant, actor, link, 1); e != nil {
		t.Fatal("link retry", e)
	}
	report, e = svc.Forecast(ctx, tenant, actor, q)
	if e != nil || report.Paid != 1 || report.Gross.Sign() != 0 || report.Rows[0].ActualNet.RatString() != "18" {
		t.Fatal(e, report)
	}
	// Definition amendments retain the actual link but require renewed review.
	event.PerShare = "2.1"
	event.Reason = "Corrected announcement"
	if _, e = svc.SaveForecastEvent(ctx, tenant, actor, event, 1); e != nil {
		t.Fatal(e)
	}
	report, e = svc.Forecast(ctx, tenant, actor, q)
	if e != nil || report.Gross != nil || report.Paid != 0 || !slices.Contains(report.Rows[0].Issues, "actual_link_requires_review") {
		t.Fatal(e, report)
	}
	// Concurrent competing corrections of one version: exactly one wins.
	results := make(chan error, 2)
	for _, amount := range []string{"2.2", "2.3"} {
		candidate := event
		candidate.PerShare = amount
		candidate.Reason = "Concurrent fixture " + amount
		go func() { _, err := svc.SaveForecastEvent(ctx, tenant, actor, candidate, 2); results <- err }()
	}
	successes, conflicts := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			successes++
		} else if errors.Is(err, d.ErrInvestmentConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatal("competing correction lost", successes, conflicts)
	}
	if _, e = repo.Forecast(ctx, tenant, outsider, q); !errors.Is(e, d.ErrInvestmentForbidden) {
		t.Fatal("read access bypass", e)
	}
	if _, e = repo.SaveForecastEvent(ctx, tenant, outsider, event, 2); !errors.Is(e, d.ErrInvestmentForbidden) {
		t.Fatal("write access bypass", e)
	}
	if _, e = svc.SaveForecastLink(ctx, foreign, actor, link, 0); !errors.Is(e, d.ErrInvestmentNotFound) {
		t.Fatal("foreign link accepted", e)
	}
	if _, e = pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'member')`, tenant, outsider); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.SaveForecastEvent(ctx, tenant, outsider, event, 2); !errors.Is(e, d.ErrInvestmentForbidden) {
		t.Fatal("member changed shared schedule", e)
	}
	if _, e = repo.Forecast(ctx, tenant, outsider, q); e != nil {
		t.Fatal("member cannot read", e)
	}
	// The independent portable archive validates definitions, versions and links.
	bundle, e := repo.ExportInvestmentArchive(ctx, tenant, actor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = archive.VerifyJournal(bundle.Data); e != nil {
		t.Fatal("forecast archive", e)
	}
	if archiveRows(bundle, "investment_payout_events") != 1 || archiveRows(bundle, "investment_payout_event_revisions") != 3 || archiveRows(bundle, "investment_payout_links") != 1 || archiveRows(bundle, "investment_payout_link_revisions") != 2 {
		t.Fatal("missing forecast evidence")
	}
	for _, name := range []string{"investment_payout_event_revisions", "investment_payout_link_revisions"} {
		raw, e := json.Marshal(bundle.Data)
		if e != nil {
			t.Fatal(e)
		}
		var changed archive.Dataset
		if e = json.Unmarshal(raw, &changed); e != nil {
			t.Fatal(e)
		}
		for i := range changed.Tables {
			if changed.Tables[i].Name == name {
				changed.Tables[i].Rows = changed.Tables[i].Rows[1:]
			}
		}
		if e = archive.VerifyForecastEvidence(changed); e == nil {
			t.Fatal("missing evidence revision accepted", name)
		}
	}
	// Re-sealing a changed historical event must not legitimize a link to a
	// different instrument, even though the current event head stays untouched.
	encoded, err := json.Marshal(bundle.Data)
	if err != nil {
		t.Fatal(err)
	}
	var corrupt archive.Dataset
	if err = json.Unmarshal(encoded, &corrupt); err != nil {
		t.Fatal(err)
	}
	for i := range corrupt.Tables {
		if corrupt.Tables[i].Name == "investment_payout_event_revisions" {
			for j, raw := range corrupt.Tables[i].Rows {
				var row struct {
					Version int64 `json:"version"`
				}
				if err = json.Unmarshal(raw, &row); err != nil {
					t.Fatal(err)
				}
				if row.Version == 1 {
					var fields map[string]json.RawMessage
					if err = json.Unmarshal(raw, &fields); err != nil {
						t.Fatal(err)
					}
					var definition inv.ForecastEvent
					if err = json.Unmarshal(fields["data"], &definition); err != nil {
						t.Fatal(err)
					}
					definition.InstrumentKey = "WRONG"
					fields["data"], err = json.Marshal(definition)
					if err != nil {
						t.Fatal(err)
					}
					corrupt.Tables[i].Rows[j], err = json.Marshal(fields)
					if err != nil {
						t.Fatal(err)
					}
				}
			}
		}
	}
	if err = archive.VerifyForecastEvidence(corrupt); err == nil {
		t.Fatal("historical link meaning changed")
	}
	target, cleanTarget := withPg(t)
	defer cleanTarget()
	if _, e = target.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Restored forecast','USD')`, tenant); e != nil {
		t.Fatal(e)
	}
	if _, e = target.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,'restored-forecast@example.test','fixture')`, actor); e != nil {
		t.Fatal(e)
	}
	if _, e = target.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); e != nil {
		t.Fatal(e)
	}
	targetRepo := NewInvestmentRepo(target)
	if _, e = targetRepo.RestoreInvestmentArchive(ctx, actor, bundle, bundle.SHA256); e != nil {
		t.Fatal("forecast restore", e)
	}
	restored, e := targetRepo.Forecast(ctx, tenant, actor, q)
	if e != nil || len(restored.Rows) != 1 || restored.Gross != nil || !slices.Contains(restored.Rows[0].Issues, "actual_link_requires_review") {
		t.Fatal("restore changed rights", e, restored)
	}
	// Cancelling a stale association preserves both its audit and actual cash.
	link.Cancelled = true
	link.Reason = "Remove incorrect association"
	cancelled, e := targetRepo.SaveForecastLink(ctx, tenant, actor, link, 2)
	if e != nil || cancelled.Version != 3 {
		t.Fatal("cancel link", e, cancelled)
	}
	afterCancel, e := targetRepo.Forecast(ctx, tenant, actor, q)
	if e != nil || afterCancel.Paid != 0 || len(afterCancel.Rows) != 1 || afterCancel.Rows[0].ActualNet != nil || afterCancel.Gross == nil {
		t.Fatal("cancel changed facts or remained active", e, afterCancel)
	}
	afterArchive, e := targetRepo.ExportInvestmentArchive(ctx, tenant, actor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = archive.VerifyJournal(afterArchive.Data); e != nil {
		t.Fatal("cancel audit", e)
	}
	// Exact new query shapes, representative synthetic tenant/account filters.
	for _, query := range []string{forecastOperationsSQL, forecastLegsSQL} {
		rows, e := pool.DB.Query(ctx, "EXPLAIN "+query, tenant, []string{ledger.ID}, inv.MaxPayoutOperations+1)
		if e != nil {
			t.Fatal(e)
		}
		for rows.Next() {
			var line string
			if e = rows.Scan(&line); e != nil {
				rows.Close()
				t.Fatal(e)
			}
			t.Log(line)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			t.Fatal(e)
		}
	}
	assertInvestmentRollbackRefused(t, ctx, pool, "0055_investment_forecast")
}

func TestInvestmentTrackerForecastAndPayoutGrowthUseImportedHistory(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	repo := NewInvestmentRepo(pool)
	svc := inv.NewService(repo, NewTenantRepo(pool))
	day := time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)
	ledger, err := svc.CreateLedgerWithDetails(ctx, tenant, actor, d.InvestmentLedger{Name: "Tracker", StartedAt: time.Now().UTC(), PortfolioTracking: true}, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	var receipt d.InvestmentOperation
	for i, operation := range []d.InvestmentOperation{
		{Kind: "buy", Quantity: "10", Amount: "100", OccurredAt: day, SettlementAt: day},
		{Kind: "split", SplitNew: "2", SplitOld: "1", OccurredAt: day.AddDate(0, 0, 1)},
		{Kind: "sell", Quantity: "20", Amount: "300", OccurredAt: day.AddDate(0, 0, 3), SettlementAt: day.AddDate(0, 0, 3)},
		{Kind: "dividend", Amount: "100", Tax: "13", OccurredAt: day.AddDate(0, 0, 4)},
		{Kind: "dividend", Amount: "200", Tax: "7", OccurredAt: day.AddDate(1, 0, 4)},
	} {
		operation.LedgerID, operation.InstrumentKey, operation.Currency = ledger.ID, "STOCK", "USD"
		if operation.Kind == "split" {
			operation.Currency = ""
		}
		receipt, err = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: uuid.NewString(), ExpectedLedgerVersion: int64(i + 1), Operation: operation})
		if err != nil {
			t.Fatal(i, err)
		}
	}
	event := inv.ForecastEvent{ID: uuid.NewString(), InstrumentKey: "STOCK", Kind: "dividend", Nature: "announced", Currency: "USD", PerShare: "2", PaymentDate: time.Now().UTC().Format("2006-01") + "-28", Timezone: "Europe/Moscow", EntitlementRule: "settled_by_cutoff", EntitlementAt: day.AddDate(0, 0, 2), SourceReference: "Synthetic issuer", Evidence: "Synthetic historical record cutoff", Reason: "Synthetic event"}
	event, err = svc.SaveForecastEvent(ctx, tenant, actor, event, 0)
	if err != nil {
		t.Fatal(err)
	}
	q := inv.ForecastQuery{FromMonth: time.Now().UTC().Format("2006-01"), Currency: "USD"}
	report, err := svc.Forecast(ctx, tenant, actor, q)
	if err != nil || len(report.Rows) != 1 || report.Rows[0].Quantity == nil || report.Rows[0].Quantity.RatString() != "20" || report.Gross.RatString() != "40" {
		t.Fatal(err, report)
	}
	before, err := svc.Snapshot(ctx, tenant, actor, ledger.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.SaveForecastLink(ctx, tenant, actor, inv.ForecastLink{EventID: event.ID, EventVersion: event.Version, LedgerID: ledger.ID, OperationID: receipt.ID, OperationVersion: receipt.Version, Complete: true, Reason: "Synthetic statement reconciliation"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	report, err = svc.Forecast(ctx, tenant, actor, q)
	if err != nil || report.Paid != 1 || report.Rows[0].ActualNet.RatString() != "200" {
		t.Fatal(err, report)
	}
	after, err := svc.Snapshot(ctx, tenant, actor, ledger.ID, time.Now())
	if err != nil || before.Ledger.Version != after.Ledger.Version || len(after.Positions) != len(before.Positions) {
		t.Fatal("forecast changed book", err)
	}
	beforePositions, _ := json.Marshal(before.Positions)
	afterPositions, _ := json.Marshal(after.Positions)
	if string(beforePositions) != string(afterPositions) {
		t.Fatal("forecast changed holdings")
	}
	growth, err := svc.Payouts(ctx, tenant, actor, inv.PayoutQuery{LedgerIDs: []string{ledger.ID}, FromDate: "2021-01-01", ToDate: "2021-01-31", Kinds: []string{"dividend", "coupon"}})
	if err != nil || len(growth.Growth) != 1 || growth.Growth[0].PercentChange == nil || growth.Growth[0].PercentChange.RatString() != "100" {
		t.Fatal(err, growth)
	}
}
