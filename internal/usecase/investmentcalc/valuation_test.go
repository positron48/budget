package investmentcalc

import (
	"testing"
	"time"
)

func TestC04C11HistoricalFX(t *testing.T) {
	day := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	components := []Component{{ID: "shares", Currency: "USD", Amount: "1000", Known: true}}
	fixings := []FXFixing{testFX("USD", "RUB", "80", "market", day), testFX("USD", "RUB", "90", "market", day.AddDate(0, 0, 1)), testFX("USD", "RUB", "70", "cbr", day.AddDate(0, 0, -1))}
	for i, want := range []string{"80000", "90000"} {
		v, err := Value(components, fixings, "RUB", "market", day.AddDate(0, 0, i))
		if err != nil || !v.Complete {
			t.Fatal(v, err)
		}
		equal(t, v.KnownValue, want)
	}
	v, err := Value(components, fixings, "RUB", "market", day.AddDate(0, 0, -1))
	if err != nil || v.Complete || len(v.Issues) != 1 {
		t.Fatal(v, err)
	}
	equal(t, v.KnownValue, "0")
	v, err = Value(components, fixings, "USD", "market", day.AddDate(0, 0, -1))
	if err != nil || !v.Complete {
		t.Fatal(v, err)
	}
	equal(t, v.KnownValue, "1000")
}
func TestC07Bond(t *testing.T) {
	v, err := BondValue("2", "1000", "95", "20")
	if err != nil {
		t.Fatal(err)
	}
	equal(t, v, "1940")
}
func TestC14SettlementPreservesEconomicCash(t *testing.T) {
	trade, err := (Cash{"1000", "0", "202"}).Economic()
	if err != nil {
		t.Fatal(err)
	}
	settled, err := (Cash{"798", "0", "0"}).Economic()
	if err != nil {
		t.Fatal(err)
	}
	equal(t, trade, "798")
	equal(t, settled, "798")
}
func TestMissingPriceAndDuplicateComponents(t *testing.T) {
	day := time.Now()
	c := []Component{{ID: "cash", Currency: "USD", Amount: "10", Known: true}, {ID: "suspended", Currency: "USD", Known: false}}
	v, err := Value(c, nil, "USD", "market", day)
	if err != nil || v.Complete {
		t.Fatal(v, err)
	}
	equal(t, v.KnownValue, "10")
	c = append(c, c[0])
	if _, err := Value(c, nil, "USD", "market", day); err == nil {
		t.Fatal("double counted component")
	}
}

func testFX(from, to, rate, policy string, at time.Time) FXFixing {
	return FXFixing{From: from, To: to, Rate: rate, Policy: policy, ID: from + to + at.String(), Revision: 1, At: at, AvailableAt: at, ValidUntil: at.Add(7 * 24 * time.Hour)}
}
