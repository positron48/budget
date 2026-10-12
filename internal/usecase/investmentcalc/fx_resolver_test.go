package investmentcalc

import (
	"math/big"
	"testing"
	"time"
)

func TestFXInverseAndCrossFeedPortfolioExactly(t *testing.T) {
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	fx := []FXFixing{testFX("USD", "RUB", "3", "market", at), testFX("EUR", "RUB", "10", "market", at)}
	// 3 RUB -> 1 USD exactly, avoiding any decimal truncation of 1/3.
	got, e := Value([]Component{{ID: "cash", Currency: "RUB", Amount: "3", Known: true}}, fx, "USD", "market", at)
	if e != nil || !got.Complete {
		t.Fatal(got, e)
	}
	equal(t, got.KnownValue, "1")
	if len(got.FXPaths["RUB"]) != 1 || !got.FXPaths["RUB"][0].Inverse {
		t.Fatal(got.FXPaths)
	}
	// 3 EUR -> 30 RUB -> 10 USD, every applied leg remains auditable.
	got, e = Value([]Component{{ID: "cash", Currency: "EUR", Amount: "3", Known: true}}, fx, "USD", "market", at)
	if e != nil || !got.Complete {
		t.Fatal(got, e)
	}
	equal(t, got.KnownValue, "10")
	path := got.FXPaths["EUR"]
	if len(path) != 2 || path[0].From != "EUR" || path[0].Inverse || !path[1].Inverse || path[1].Revision != 1 {
		t.Fatal(path)
	}
}
func TestFXAvailabilityExpiryAndPolicyIsolation(t *testing.T) {
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	usd := testFX("USD", "RUB", "90", "market", at)
	eur := testFX("EUR", "RUB", "100", "market", at)
	eur.AvailableAt = at.Add(time.Hour)
	cases := []struct {
		name   string
		fx     []FXFixing
		cutoff time.Time
		found  bool
	}{
		{"unpublished leg", []FXFixing{usd, eur}, at, false},
		{"publication boundary", []FXFixing{usd, eur}, eur.AvailableAt, true},
		{"expiry boundary", []FXFixing{usd, eur}, eur.ValidUntil, false},
		{"no CBR leg", []FXFixing{usd, testFX("EUR", "RUB", "100", "cbr", at)}, at, false},
		{"no future leg", []FXFixing{usd, testFX("EUR", "RUB", "100", "market", at.Add(time.Hour))}, at, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, e := NewFXResolver(c.fx, "market", c.cutoff)
			if e != nil {
				t.Fatal(e)
			}
			_, ok := r.Resolve("EUR", "USD")
			if ok != c.found {
				t.Fatal(ok)
			}
		})
	}
	// A newer expired quote must not resurrect a previous overlapping quote.
	newer := usd
	newer.ID = "new"
	newer.At = at.Add(time.Hour)
	newer.AvailableAt = newer.At
	newer.ValidUntil = at.Add(2 * time.Hour)
	r, e := NewFXResolver([]FXFixing{usd, newer}, "market", newer.ValidUntil)
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := r.Resolve("USD", "RUB"); ok {
		t.Fatal("resurrected older quote")
	}
}
func TestFXConflictsAndDeterministicPaths(t *testing.T) {
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	one := testFX("USD", "RUB", "90", "market", at)
	equivalent := one
	equivalent.ID = "equivalent"
	equivalent.Rate = "90.0000"
	for _, values := range [][]FXFixing{{one, equivalent}, {equivalent, one}} {
		r, e := NewFXResolver(values, "market", at)
		if e != nil {
			t.Fatal(e)
		}
		got, ok := r.Resolve("USD", "RUB")
		if !ok || got.Legs[0].ID != one.ID {
			t.Fatal(got, ok)
		}
		got.Rate.SetInt64(0)
		again, _ := r.Resolve("USD", "RUB")
		equal(t, again.Rate, "90")
	}
	different := one
	different.Rate = "91"
	if _, e := NewFXResolver([]FXFixing{one, different}, "market", at); e == nil {
		t.Fatal("conflicting fixing accepted")
	}
	for _, mutate := range []func(*FXFixing){func(f *FXFixing) { f.ID = "" }, func(f *FXFixing) { f.Revision = 0 }, func(f *FXFixing) { f.AvailableAt = time.Time{} }, func(f *FXFixing) { f.ValidUntil = f.At }, func(f *FXFixing) { f.Rate = "0" }, func(f *FXFixing) { f.To = f.From }} {
		bad := one
		mutate(&bad)
		if _, e := NewFXResolver([]FXFixing{bad}, "market", at); e == nil {
			t.Fatal("invalid evidence accepted", bad)
		}
	}
	// Explicit direct quote wins over a synthetic cross, irrespective of ordering.
	direct := testFX("EUR", "USD", "1.2", "market", at)
	fx := []FXFixing{one, testFX("EUR", "RUB", "100", "market", at), direct}
	r, e := NewFXResolver(fx, "market", at)
	if e != nil {
		t.Fatal(e)
	}
	got, ok := r.Resolve("EUR", "USD")
	if !ok || got.Rate.Cmp(big.NewRat(6, 5)) != 0 || len(got.Legs) != 1 {
		t.Fatal(got, ok)
	}
}
func TestExpiredFXProducesGapNotZeroOrCBR(t *testing.T) {
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	expired := testFX("USD", "RUB", "90", "market", at)
	got, e := Value([]Component{{"rub", "RUB", "5", true}, {"usd", "USD", "10", true}}, []FXFixing{expired, testFX("USD", "RUB", "91", "cbr", expired.ValidUntil)}, "RUB", "market", expired.ValidUntil)
	if e != nil || got.Complete || len(got.Issues) != 1 || got.Issues[0].Code != "fx_missing" {
		t.Fatal(got, e)
	}
	equal(t, got.KnownValue, "5")
}
