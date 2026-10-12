package grpcadapter

import (
	d "github.com/positron48/budget/internal/domain"
	"testing"
	"time"
)

func TestDistributionMarketEvidenceRPCRoundTrip(t *testing.T) {
	op := d.InvestmentOperation{OccurredAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Distribution: &d.InvestmentDistribution{MarketValue: "9007199254740993.123456789012", MarketValueEvidence: "Reviewed total value", EventKey: "event", TargetInstrumentKey: "target", NewUnits: "1", OldUnits: "2", CostNumerator: "1", CostDenominator: "3"}}
	wire := protoInvestmentOperation(op)
	back, err := parseInvestmentOperation(wire)
	if err != nil {
		t.Fatal(err)
	}
	if back.Distribution == nil || back.Distribution.MarketValue != op.Distribution.MarketValue || back.Distribution.MarketValueEvidence != op.Distribution.MarketValueEvidence {
		t.Fatal(back)
	}
}
