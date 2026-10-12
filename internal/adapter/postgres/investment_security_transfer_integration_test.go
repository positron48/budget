package postgres

import (
	"context"
	"errors"
	"math/big"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	archive "github.com/positron48/budget/internal/usecase/investmentarchive"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func TestInvestmentSecurityTransferAtomicReplayValuationAndRestore(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, actor string
	if e := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Security transfers','RUB') RETURNING id`).Scan(&tenant); e != nil {
		t.Fatal(e)
	}
	if e := pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('security-transfer@example.test','test') RETURNING id`).Scan(&actor); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); e != nil {
		t.Fatal(e)
	}
	journal := NewInvestmentRepo(pool)
	svc := inv.NewService(journal, NewTenantRepo(pool))
	market := NewMarketDataRepo(pool)
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	a, e := svc.CreateLedger(ctx, tenant, actor, "Source", at, "source")
	if e != nil {
		t.Fatal(e)
	}
	b, e := svc.CreateLedger(ctx, tenant, actor, "Destination", at, "destination")
	if e != nil {
		t.Fatal(e)
	}
	mutate := func(ledger string, version int64, kind, quantity, amount, fee string, when time.Time) d.InvestmentOperation {
		t.Helper()
		instrument := "S"
		if kind == "deposit" {
			instrument = ""
		}
		v, e := svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: uuid.NewString(), ExpectedLedgerVersion: version, Operation: d.InvestmentOperation{LedgerID: ledger, Kind: kind, InstrumentKey: instrument, Currency: "RUB", Quantity: quantity, Amount: amount, Fee: fee, OccurredAt: when}})
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	mutate(a.ID, 1, "deposit", "", "1000", "0", at)
	buy := mutate(a.ID, 2, "buy", "3", "300", "3", at.Add(time.Hour))
	mutate(b.ID, 1, "deposit", "", "500", "0", at)
	request := inv.TransferMutation{Action: "create", RequestKey: "move-securities", ExpectedFromVersion: 3, ExpectedToVersion: 2, Transfer: d.InvestmentTransfer{FromLedgerID: a.ID, ToLedgerID: b.ID, InstrumentKey: "S", Quantity: "2", Currency: "RUB", OccurredAt: at.Add(34 * time.Hour), Note: "Synthetic broker confirmation"}}
	type answer struct {
		transfer d.InvestmentTransfer
		err      error
	}
	answers := make(chan answer, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); v, e := svc.Transfer(ctx, tenant, actor, request); answers <- answer{v, e} }()
	}
	wg.Wait()
	close(answers)
	var transfer d.InvestmentTransfer
	for result := range answers {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if transfer.ID != "" && transfer.ID != result.transfer.ID {
			t.Fatal("duplicate pair")
		}
		transfer = result.transfer
	}
	source, e := svc.Snapshot(ctx, tenant, actor, a.ID, at.Add(48*time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	destination, e := svc.Snapshot(ctx, tenant, actor, b.ID, at.Add(48*time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	if source.Positions[0].Quantity != "1" || source.Positions[0].CleanCost != "101" || destination.Positions[0].Quantity != "2" || destination.Positions[0].CleanCost != "202" || destination.Cash[0].Economic != "500" || destination.Positions[0].Lots[0].AcquisitionOperationID != buy.ID {
		t.Fatal(source, destination)
	}

	// Failure while writing the second side must roll back the first side as well.
	missingTarget := request
	missingTarget.RequestKey = "missing-target"
	missingTarget.ExpectedFromVersion = 4
	missingTarget.Transfer.ToLedgerID = uuid.NewString()
	if _, e = svc.Transfer(ctx, tenant, actor, missingTarget); e == nil {
		t.Fatal("missing destination accepted")
	}
	unchanged, e := svc.Snapshot(ctx, tenant, actor, a.ID, at.Add(48*time.Hour))
	if e != nil || !reflect.DeepEqual(source, unchanged) {
		t.Fatal("second-side failure mutated source", unchanged, e)
	}
	assertInvestmentRollbackRefused(t, ctx, pool, "0047_security_transfers")
	if _, e = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "void", RequestKey: "detach", Reason: "Rejected", ExpectedLedgerVersion: 4, ExpectedOperationVersion: 1, Operation: d.InvestmentOperation{ID: transfer.FromOperationID, LedgerID: a.ID}}); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal("independent leg void", e)
	}
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	if e = market.ConfigureSource(ctx, scope, "Synthetic", true); e != nil {
		t.Fatal(e)
	}
	instrument := md.Instrument{ID: uuid.NewString(), Ticker: "S", ClassCode: "TEST", Name: "Synthetic security", Kind: "share", Currency: "RUB"}
	tx, e := pool.DB.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = saveMarketInstrument(ctx, tx, scope, instrument); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = market.SaveMapping(ctx, tenant, actor, md.MappingChange{InstrumentKey: "S", RequestKey: "listing", Reason: "Reviewed", Periods: []md.MappingPeriod{{Provider: scope.Provider, AccessScope: scope.AccessScope, InstrumentID: instrument.ID, Kind: "share", Currency: "RUB", Unit: "currency_per_unit", From: at, Evidence: "Synthetic"}}}); e != nil {
		t.Fatal(e)
	}
	profile := nav.ConventionProfile{Key: "transfer-prices", Title: "Reviewed", Evidence: "Synthetic", Enabled: true, Periods: map[string][]nav.PriceConventionPeriod{"S": {{Scope: scope, InstrumentID: instrument.ID, From: at, Policy: nav.SeriesPolicy{Kind: "last_price", SourceKind: "exchange", Adjustment: "unadjusted", MaxQuoteAge: 24 * time.Hour}}}}}
	if _, e = market.RegisterPriceConventions(ctx, tenant, actor, profile, 0); e != nil {
		t.Fatal(e)
	}
	fxPolicy := md.FXPolicy{Key: "market", Mode: "market", Source: "test", AccessScope: "personal", SourceReference: "Synthetic", TimeConvention: "Explicit", ValueConvention: "Per unit", ValidityConvention: "Explicit"}
	if _, e = market.RegisterFXPolicy(ctx, tenant, actor, fxPolicy, 0); e != nil {
		t.Fatal(e)
	}
	for i, rate := range []string{"3", "10"} {
		day := at.AddDate(0, 0, i)
		if _, e = market.SaveObservations(ctx, scope, []md.Observation{{InstrumentID: instrument.ID, Kind: "last_price", At: day.Add(4 * time.Hour), SourceKind: "exchange", PriceUnit: "currency_per_unit", Adjustment: "unadjusted", Currency: "RUB", Price: "120", Complete: true}}); e != nil {
			t.Fatal(e)
		}
		if _, e = market.SaveFXPoints(ctx, tenant, "market", 1, []md.FXPoint{{From: "USD", To: "RUB", Rate: rate, At: day, AvailableAt: day, ValidUntil: day.Add(24 * time.Hour), EvidenceSHA256: strings.Repeat("a", 64), RecordReference: uuid.NewString()}}); e != nil {
			t.Fatal(e)
		}
	}
	series := func(ids []string, currency string) string {
		t.Helper()
		q := nav.Request{TenantID: tenant, UserID: actor, LedgerIDs: ids, Currency: currency, FXPolicyKey: "market", ConventionKey: profile.Key}
		if _, e = market.EnqueueDailyNAV(ctx, q, "2024-01-01", "2024-01-02"); e != nil {
			t.Fatal(e)
		}
		for _, date := range []string{"2024-01-01", "2024-01-02"} {
			if _, e = market.BuildDailyNAV(ctx, q, date); e != nil {
				t.Fatal(e)
			}
		}
		key, _, e := navSeries(q)
		if e != nil {
			t.Fatal(e)
		}
		return key
	}
	sourceKey, destinationKey, combinedKey := series([]string{a.ID}, "RUB"), series([]string{b.ID}, "RUB"), series([]string{a.ID, b.ID}, "RUB")
	for _, key := range []string{sourceKey, destinationKey, combinedKey} {
		profit, e := market.ProfitForSeries(ctx, tenant, actor, key, "2024-01-02", "2024-01-02")
		if e != nil || profit.Performance.Profit == nil || profit.Performance.Profit.Sign() != 0 || profit.Performance.TWR.Return == nil || profit.Performance.TWR.Return.Sign() != 0 || !profit.Instruments.Complete || profit.Instruments.Rows[0].Known.Sign() != 0 || !profit.Attribution.Complete {
			t.Fatal("transfer manufactured profit", profit, e)
		}
	}
	usdKey := series([]string{b.ID}, "USD")
	lots, e := market.InvestmentLots(ctx, tenant, actor, usdKey, "2024-01-02", "S", "", nil)
	if e != nil || len(lots.Rows) != 1 || lots.Rows[0].Origin.ID != buy.ID || lots.Rows[0].Origin.LedgerID != a.ID || lots.Rows[0].HistoricalCost.Clean.Cmp(big.NewRat(202, 3)) != 0 || len(lots.Rows[0].Changes) != 3 {
		t.Fatal(lots, e)
	}
	beforeVersion := destination.Ledger.Version
	correction := buy
	correction.Amount = "600"
	if _, e = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "amend", RequestKey: "correct-source", Reason: "Correct broker acquisition", ExpectedLedgerVersion: 4, ExpectedOperationVersion: 1, Operation: correction}); e != nil {
		t.Fatal(e)
	}
	corrected, e := svc.Snapshot(ctx, tenant, actor, b.ID, at.Add(48*time.Hour))
	if e != nil || corrected.Positions[0].CleanCost != "402" || corrected.Ledger.Version != beforeVersion {
		t.Fatal(corrected, e)
	}
	holdings, e := market.InvestmentPositions(ctx, tenant, actor, usdKey, "2024-01-02", nil)
	if e != nil || holdings.ValuationState != "stale" || holdings.Rows[0].HistoricalCost.Clean.Cmp(big.NewRat(134, 1)) != 0 {
		t.Fatal(holdings, e)
	}
	// A later dependent sale makes reduction/void fail as one atomic pair.
	sale := mutate(b.ID, 3, "sell", "2", "500", "0", at.Add(58*time.Hour))
	amend := request
	amend.Action = "amend"
	amend.RequestKey = "invalid-reduction"
	amend.Reason = "Would detach sale"
	amend.ExpectedVersion = 1
	amend.ExpectedFromVersion = 5
	amend.ExpectedToVersion = 4
	amend.Transfer = transfer
	amend.Transfer.Quantity = "1"
	if _, e = svc.Transfer(ctx, tenant, actor, amend); e == nil {
		t.Fatal("future dependent sale ignored")
	}
	after, e := svc.GetTransfer(ctx, tenant, actor, transfer.ID)
	if e != nil || after.Version != 1 || after.Quantity != "2" {
		t.Fatal(after, e)
	}
	bundle, e := journal.ExportInvestmentArchive(ctx, tenant, actor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = archive.VerifyJournal(bundle.Data); e != nil {
		t.Fatal("archived transfer replay", e)
	}
	target, targetCleanup := withPg(t)
	defer targetCleanup()
	if _, e = target.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Restored','RUB')`, tenant); e != nil {
		t.Fatal(e)
	}
	if _, e = target.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,'restored-security@example.test','test')`, actor); e != nil {
		t.Fatal(e)
	}
	if _, e = target.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); e != nil {
		t.Fatal(e)
	}
	if _, e = NewInvestmentRepo(target).RestoreInvestmentArchive(ctx, actor, bundle, bundle.SHA256); e != nil {
		t.Fatal(e)
	}
	restored, e := NewInvestmentRepo(target).Snapshot(ctx, tenant, b.ID, at.Add(48*time.Hour))
	if e != nil || !reflect.DeepEqual(corrected.Positions, restored.Positions) {
		t.Fatal("restored lot lineage differs", restored, e)
	}
	// Both amendments and cancellations must replay the complete connected history.
	if _, e = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "void", RequestKey: "cancel-sale", Reason: "Broker cancellation", ExpectedLedgerVersion: 4, ExpectedOperationVersion: 1, Operation: d.InvestmentOperation{ID: sale.ID, LedgerID: b.ID}}); e != nil {
		t.Fatal(e)
	}
	amend.RequestKey = "valid-reduction"
	amend.ExpectedToVersion = 5
	reduced, e := svc.Transfer(ctx, tenant, actor, amend)
	if e != nil || reduced.Version != 2 || reduced.Quantity != "1" {
		t.Fatal(reduced, e)
	}
	receiving, e := svc.Snapshot(ctx, tenant, actor, b.ID, at.Add(72*time.Hour))
	if e != nil || receiving.Positions[0].Quantity != "1" || receiving.Positions[0].CleanCost != "201" {
		t.Fatal(receiving, e)
	}
	cancel := amend
	cancel.Action = "void"
	cancel.RequestKey = "cancel-securities"
	cancel.ExpectedVersion = 2
	cancel.ExpectedFromVersion = 6
	cancel.ExpectedToVersion = 6
	cancelled, e := svc.Transfer(ctx, tenant, actor, cancel)
	if e != nil || cancelled.Status != "voided" || cancelled.Version != 3 {
		t.Fatal(cancelled, e)
	}
	returned, e := svc.Snapshot(ctx, tenant, actor, a.ID, at.Add(72*time.Hour))
	if e != nil || returned.Positions[0].Quantity != "3" || returned.Positions[0].CleanCost != "603" {
		t.Fatal(returned, e)
	}
	receiving, e = svc.Snapshot(ctx, tenant, actor, b.ID, at.Add(72*time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	for _, p := range receiving.Positions {
		if p.Quantity != "0" {
			t.Fatal("void left securities", p)
		}
	}
	cancelledArchive, e := journal.ExportInvestmentArchive(ctx, tenant, actor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = archive.VerifyJournal(cancelledArchive.Data); e != nil {
		t.Fatal("voided archive replay", e)
	}

}
