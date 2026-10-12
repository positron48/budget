package investmentimport

import (
	"strings"
	"testing"
)

func TestInstrumentIdentityRequiresMatchingEvidence(t *testing.T) {
	uid := "11111111-1111-4111-8111-111111111111"
	position := "22222222-2222-4222-8222-222222222222"
	m := FinancialMapping{InstrumentIdentities: map[string]InstrumentIdentity{uid: {FIGI: "BBG000VFX6Y4", PositionUID: position, Reason: "Matching FIGI and broker position"}}}
	key, err := m.instrumentKey(uid, "BBG000VFX6Y4", position)
	if err != nil || key != "tinvest-position:"+position {
		t.Fatal(key, err)
	}
	for _, pair := range [][2]string{{"", position}, {"BBG000000000", position}, {"BBG000VFX6Y4", ""}} {
		if _, err := m.instrumentKey(uid, pair[0], pair[1]); err == nil {
			t.Fatal("mismatched proof accepted")
		}
	}
	key, err = m.instrumentKey("other", "", "")
	if err != nil || key != "tinvest:other" {
		t.Fatal(key, err)
	}
}
func TestInstrumentIdentityInvalidConfigurationRejected(t *testing.T) {
	csv := financialHeader + "CASH_IN,2024-01-01 12:00:00,,1,1,RUB,0,,0,,False,\n"
	m := financialConfig(t, csv)
	m.InstrumentIdentities = map[string]InstrumentIdentity{"bad": {FIGI: "BBG000VFX6Y4", PositionUID: "22222222-2222-4222-8222-222222222222", Reason: "reviewed"}}
	if _, err := MapSnowballFinancial(strings.NewReader(csv), m); err == nil {
		t.Fatal("invalid UID accepted")
	}
}

func TestTradeIdentityChangesKeyOnlyWithMatchingProof(t *testing.T) {
	csv, m := evidenceFixture(t)
	e := m.TradeEvidence[1]
	e.FIGI = "BBG000VFX6Y4"
	e.PositionUID = "22222222-2222-4222-8222-222222222222"
	m.TradeEvidence[1] = e
	original, err := MapSnowballFinancial(strings.NewReader(csv), m)
	if err != nil {
		t.Fatal(err)
	}
	m.InstrumentIdentities = map[string]InstrumentIdentity{e.InstrumentUID: {FIGI: e.FIGI, PositionUID: e.PositionUID, Reason: "Verified position"}}
	report, err := MapSnowballFinancial(strings.NewReader(csv), m)
	if err != nil || report.Ready != 1 || report.Rows[0].Operation.InstrumentKey != "tinvest-position:"+e.PositionUID || original.Hash == report.Hash {
		t.Fatal(report, err)
	}
	e.PositionUID = "33333333-3333-4333-8333-333333333333"
	m.TradeEvidence[1] = e
	report, err = MapSnowballFinancial(strings.NewReader(csv), m)
	if err != nil || report.Ready != 0 || report.Rows[0].Operation != nil {
		t.Fatal("conflicting broker position accepted", report, err)
	}
}
