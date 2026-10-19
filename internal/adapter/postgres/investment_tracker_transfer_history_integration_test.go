package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func TestTrackerHistoryUsesAtomicTransferLegs(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	market := NewMarketDataRepo(pool)
	at := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	source, err := svc.CreateLedgerWithDetails(ctx, tenant, actor, d.InvestmentLedger{Name: "Source", StartedAt: at.Add(-time.Hour), PortfolioTracking: true}, "source")
	if err != nil {
		t.Fatal(err)
	}
	dest, err := svc.CreateLedgerWithDetails(ctx, tenant, actor, d.InvestmentLedger{Name: "Destination", StartedAt: at.Add(-time.Hour), PortfolioTracking: true}, "dest")
	if err != nil {
		t.Fatal(err)
	}
	buy, err := svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: "buy", ExpectedLedgerVersion: 1, Operation: d.InvestmentOperation{LedgerID: source.ID, Kind: "buy", InstrumentKey: "S", Quantity: "2", Amount: "20", Currency: "RUB", OccurredAt: at}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Transfer(ctx, tenant, actor, inv.TransferMutation{Action: "create", RequestKey: "move", ExpectedFromVersion: 2, ExpectedToVersion: 1, Transfer: d.InvestmentTransfer{FromLedgerID: source.ID, ToLedgerID: dest.ID, InstrumentKey: "S", Quantity: "2", Currency: "RUB", OccurredAt: at.Add(24 * time.Hour), Note: "Confirmed transfer"}})
	if err != nil {
		t.Fatal(err)
	}
	estimates := []md.PriceEstimate{{Price: "10", Currency: "RUB", At: at, From: at, Reference: "Synthetic price"}}
	_, err = market.SaveMapping(ctx, tenant, actor, md.MappingChange{InstrumentKey: "S", RequestKey: "map", Reason: "Synthetic", PriceEstimates: &estimates})
	if err != nil {
		t.Fatal(err)
	}
	policy := md.FXPolicy{Key: "test-market", Mode: "market", Source: "synthetic", AccessScope: "personal", SourceReference: "Synthetic", TimeConvention: "Explicit", ValueConvention: "One unit", ValidityConvention: "Explicit"}
	_, err = market.RegisterFXPolicy(ctx, tenant, actor, policy, 0)
	if err != nil {
		t.Fatal(err)
	}
	q := nav.TrackerHistoryRequest{LedgerIDs: []string{dest.ID}, Currency: "RUB", FXPolicyKey: policy.Key, From: "2024-01-01", To: "2024-01-03", PageDays: 93}
	h, err := market.TrackerHistory(ctx, tenant, actor, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Points) != 3 || h.Points[0].KnownValue.Sign() != 0 || h.Points[1].KnownValue.RatString() != "20" || h.Points[2].KnownValue.RatString() != "20" {
		t.Fatal(h)
	}
	fenceQ := q
	fenceQ.BasisOnly = true
	fence, err := market.TrackerHistory(ctx, tenant, actor, fenceQ)
	if err != nil || fence.BasisKey != h.BasisKey || len(fence.Points) != 0 {
		t.Fatal("cheap fence changed basis", fence, err)
	}
	// A corrected acquisition quantity atomically rebuilds transfer dependents and
	// invalidates history. The receiving account still owns its transferred 2.
	buy.Quantity = "3"
	buy.Amount = "30"
	_, err = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "amend", Reason: "Correct source acquisition", RequestKey: uuid.NewString(), ExpectedLedgerVersion: 3, ExpectedOperationVersion: buy.Version, Operation: buy})
	if err != nil {
		t.Fatal(err)
	}
	next, err := market.TrackerHistory(ctx, tenant, actor, q)
	if err != nil {
		t.Fatal(err)
	}
	if next.Generation <= h.Generation || next.Points[1].KnownValue.RatString() != "20" {
		t.Fatal("corrected source did not preserve transfer history", next)
	}
}
