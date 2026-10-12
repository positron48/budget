package grpcadapter

import (
	d "github.com/positron48/budget/internal/domain"
	"reflect"
	"testing"
	"time"
)

func TestDistributionProtocolRoundTrip(t *testing.T) {
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	op := d.InvestmentOperation{Kind: "distribution", OccurredAt: at, SettlementAt: at, Distribution: &d.InvestmentDistribution{EventKey: "event", TargetInstrumentKey: "target", NewUnits: "7.0135", OldUnits: "1", CostNumerator: "1", CostDenominator: "2", ConsumeSource: false}}
	parsed, err := parseInvestmentOperation(protoInvestmentOperation(op))
	if err != nil || !reflect.DeepEqual(parsed.Distribution, op.Distribution) {
		t.Fatal(parsed, err)
	}
	parsed.Distribution.EventKey = "changed"
	if op.Distribution.EventKey != "event" {
		t.Fatal("protocol aliases domain terms")
	}
}
