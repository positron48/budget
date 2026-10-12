package investmentcalc

import (
	"math/big"
	"testing"
)

func equal(t *testing.T, got *big.Rat, want string) {
	t.Helper()
	w, _ := new(big.Rat).SetString(want)
	if got.Cmp(w) != 0 {
		t.Fatalf("got %s want %s", got, want)
	}
}
func TestC01C02Fees(t *testing.T) {
	var b Book
	if err := b.Buy("first", "2", "100", "2"); err != nil {
		t.Fatal(err)
	}
	q, c := b.Totals()
	equal(t, q, "2")
	equal(t, c, "202")
	m, p, err := b.Sell("1", "120", "1")
	if err != nil {
		t.Fatal(err)
	}
	equal(t, p, "18")
	equal(t, m[0].Cost, "101")
	q, c = b.Totals()
	equal(t, q, "1")
	equal(t, c, "101")
}
func TestC09C10SplitFIFO(t *testing.T) {
	var b Book
	if err := b.Buy("a", "2", "100", "0"); err != nil {
		t.Fatal(err)
	}
	if err := b.Buy("b", "2", "120", "0"); err != nil {
		t.Fatal(err)
	}
	matches, p, err := b.Sell("3", "150", "0")
	if err != nil {
		t.Fatal(err)
	}
	equal(t, p, "130")
	if len(matches) != 2 || matches[0].OriginID != "a" {
		t.Fatal(matches)
	}
	q, c := b.Totals()
	equal(t, q, "1")
	equal(t, c, "120")
	if err := b.Split("2", "1"); err != nil {
		t.Fatal(err)
	}
	q, c = b.Totals()
	equal(t, q, "2")
	equal(t, c, "120")
}
func TestFractionalAllocationAndAtomicFailure(t *testing.T) {
	var b Book
	if err := b.Buy("a", "3", "0", "1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.Sell("4", "1", "0"); err == nil {
		t.Fatal("accepted short")
	}
	q, c := b.Totals()
	equal(t, q, "3")
	equal(t, c, "1")
	for range 3 {
		matches, _, err := b.Sell("1", "1", "0")
		if err != nil {
			t.Fatal(err)
		}
		equal(t, matches[0].Cost, "1/3")
	}
	q, c = b.Totals()
	equal(t, q, "0")
	equal(t, c, "0")
	if err := b.Buy("a", "1", "1", "0"); err == nil {
		t.Fatal("duplicate origin")
	}
}
func TestSplitRoundtripAndOwnedResults(t *testing.T) {
	for n := 1; n < 30; n++ {
		var b Book
		if err := b.Buy("a", "7.123456789", "9.00000000001", "0.1"); err != nil {
			t.Fatal(err)
		}
		beforeQ, beforeC := b.Totals()
		ratio := big.NewInt(int64(n)).String()
		if err := b.Split(ratio, "7"); err != nil {
			t.Fatal(err)
		}
		if err := b.Split("7", ratio); err != nil {
			t.Fatal(err)
		}
		lots := b.Lots()
		lots[0].Cost.SetInt64(0)
		lots[0].Quantity.SetInt64(0)
		q, c := b.Totals()
		equal(t, q, beforeQ.RatString())
		equal(t, c, beforeC.RatString())
	}
}

func TestAmortizationPreservesLotsAndExactCost(t *testing.T) {
	var b Book
	if err := b.BuyCost("a", "1", "900"); err != nil {
		t.Fatal(err)
	}
	if err := b.BuyCost("b", "2", "1900"); err != nil {
		t.Fatal(err)
	}
	gain, err := b.Amortize("1000", "800", "600")
	if err != nil || gain.RatString() != "40" {
		t.Fatal(gain, err)
	}
	lots := b.Lots()
	if lots[0].OriginID != "a" || lots[0].Cost.RatString() != "720" || lots[1].Cost.RatString() != "1520" {
		t.Fatal(lots)
	}
	if _, err = b.Amortize("800", "900", "1"); err == nil {
		t.Fatal("accepted growing nominal")
	}
	q, c := b.Totals()
	if q.RatString() != "3" || c.RatString() != "2240" {
		t.Fatal(q, c)
	}
	_, gain, err = b.SellProceeds("2", "1600")
	if err != nil || gain.RatString() != "120" {
		t.Fatal(gain, err)
	}
}

func TestRecoverCostsAtomicAndExact(t *testing.T) {
	var b Book
	_ = b.BuyCost("a", "3", "1")
	_ = b.BuyCost("b", "1", "2")
	_, _, _ = b.SellProceeds("1", "0") // a has recurring 2/3 cost remaining
	if _, err := b.RecoverCosts(map[string]string{"a": "0.5", "b": "3"}); err == nil {
		t.Fatal("excess recovery accepted")
	}
	_, c := b.Totals()
	if c.RatString() != "8/3" {
		t.Fatal("partial mutation", c)
	}
	r, err := b.RecoverCosts(map[string]string{"a": "0.5", "b": "2"})
	if err != nil || r.RatString() != "5/2" {
		t.Fatal(r, err)
	}
	q, c := b.Totals()
	if q.RatString() != "3" || c.RatString() != "1/6" {
		t.Fatal(q, c)
	}
	recovered, err := b.RecoverCosts(map[string]string{"a": "all"})
	if err != nil || recovered.RatString() != "1/6" {
		t.Fatal(recovered, err)
	}
	_, c = b.Totals()
	if c.Sign() != 0 {
		t.Fatal("rounded cost residue", c)
	}
}
