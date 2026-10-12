package investmentcalc

import "testing"

func TestDistributionConservesCostsAndFractionalRights(t *testing.T) {
	var source Book
	if err := source.BuyCost("first", "1", "1395.88"); err != nil {
		t.Fatal(err)
	}
	if err := source.BuyCost("second", "1", "1258.3"); err != nil {
		t.Fatal(err)
	}
	target, err := source.Distribute("7.0135", "1", "2", "3", false)
	if err != nil {
		t.Fatal(err)
	}
	sq, sc := source.Totals()
	tq, tc := target.Totals()
	if sq.RatString() != "2" || tq.RatString() != "14027/1000" {
		t.Fatal(sq, tq)
	}
	total := sc.Add(sc, tc)
	if total.RatString() != "132709/50" {
		t.Fatal(total)
	}
	lots := target.Lots()
	if len(lots) != 2 || lots[0].OriginID != "first" || lots[1].OriginID != "second" {
		t.Fatal(lots)
	}
	// Sale leaves the fractional right; it is not silently rounded away.
	if _, _, err = target.SellProceeds("14", "1622.51"); err != nil {
		t.Fatal(err)
	}
	remaining, _ := target.Totals()
	if remaining.RatString() != "27/1000" {
		t.Fatal(remaining)
	}
	q, c := source.Totals()
	if q.RatString() != "2" || c.RatString() != "132709/150" {
		t.Fatal(q, c)
	}
}

func TestReplacementTransfersOriginsWithoutRealizedProfit(t *testing.T) {
	var source Book
	_ = source.BuyCost("origin", "3", "10")
	target, err := source.Distribute("1", "2", "1", "1", true)
	if err != nil {
		t.Fatal(err)
	}
	q, c := source.Totals()
	tq, tc := target.Totals()
	if q.Sign() != 0 || c.Sign() != 0 || tq.RatString() != "3/2" || tc.RatString() != "10" {
		t.Fatal(q, c, tq, tc)
	}
	matches, profit, err := target.SellProceeds("1.5", "12")
	if err != nil || len(matches) != 1 || matches[0].OriginID != "origin" || profit.RatString() != "2" {
		t.Fatal(matches, profit, err)
	}
	if err = source.BuyCost("origin", "1", "1"); err == nil {
		t.Fatal("consumed origin reused")
	}
}

func TestDistributionInvalidTermsAreAtomic(t *testing.T) {
	for _, terms := range [][4]string{{"0", "1", "1", "1"}, {"1", "0", "1", "1"}, {"1", "1", "-1", "1"}, {"1", "1", "2", "1"}, {"1", "1", "1", "0"}, {"NaN", "1", "1", "1"}, {"1", "1", "1", "2"}} {
		var source Book
		_ = source.BuyCost("origin", "2", "13")
		if _, err := source.Distribute(terms[0], terms[1], terms[2], terms[3], true); err == nil {
			t.Fatal(terms)
		}
		q, c := source.Totals()
		if q.RatString() != "2" || c.RatString() != "13" {
			t.Fatal(q, c)
		}
	}
	var empty Book
	if _, err := empty.Distribute("1", "1", "0", "1", false); err == nil {
		t.Fatal("empty position accepted")
	}
}

func TestAbsorbRestoresAcquisitionOrderAndMovesExactCosts(t *testing.T) {
	var source, destination Book
	_ = source.BuyCost("old", "3", "10")
	_ = destination.BuyCost("recent", "1", "8")
	incoming, err := source.Distribute("1", "1", "1", "3", false)
	if err != nil {
		t.Fatal(err)
	}
	if err = destination.Absorb(incoming, []string{"old", "recent"}); err != nil {
		t.Fatal(err)
	}
	q, c := incoming.Totals()
	if q.Sign() != 0 || c.Sign() != 0 {
		t.Fatal(q, c)
	}
	matches, _, err := destination.SellProceeds("1", "2")
	if err != nil || len(matches) != 1 || matches[0].OriginID != "old" || matches[0].Cost.RatString() != "10/9" {
		t.Fatal(matches, err)
	}
	q, c = source.Totals()
	if q.RatString() != "3" || c.RatString() != "20/3" {
		t.Fatal(q, c)
	}
}

func TestAbsorbInvalidOrderAndDuplicateAreAtomic(t *testing.T) {
	for _, order := range [][]string{nil, {"a"}, {"a", "a"}, {"a", "missing"}, {"a", "b", "extra"}} {
		var a, b Book
		_ = a.BuyCost("a", "1", "2")
		_ = b.BuyCost("b", "3", "4")
		if err := a.Absorb(&b, order); err == nil {
			t.Fatal(order)
		}
		aq, ac := a.Totals()
		bq, bc := b.Totals()
		if aq.RatString() != "1" || ac.RatString() != "2" || bq.RatString() != "3" || bc.RatString() != "4" {
			t.Fatal(aq, ac, bq, bc)
		}
	}
	var a, b Book
	_ = a.BuyCost("same", "1", "2")
	_, _, _ = a.SellProceeds("1", "3")
	_ = b.BuyCost("same", "1", "2")
	if err := a.Absorb(&b, []string{"same"}); err == nil {
		t.Fatal("consumed origin accepted twice")
	}
	if err := b.Absorb(&b, []string{"same"}); err == nil {
		t.Fatal("self absorption accepted")
	}
}
