package investmentcalc

import (
	"fmt"
	"math/big"
	"math/rand"
	"reflect"
	"testing"
	"time"
)

func TestFXSeriesMatchesResolverAcrossSevenYears(t *testing.T) {
	start := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
	fx := []FXFixing{}
	for i := 0; i < 2557; i++ {
		for j, c := range []string{"USD", "EUR"} {
			at := start.AddDate(0, 0, i)
			f := FXFixing{ID: fmt.Sprintf("%s-%d", c, i), Revision: 1, Policy: "market", From: c, To: "RUB", Rate: fmt.Sprintf("%d.%04d", 70+j*10, i), At: at, AvailableAt: at.Add(time.Duration(i%3) * time.Hour), ValidUntil: at.Add(40 * time.Hour)}
			if i%97 == 0 {
				f.Withdrawn = true
				f.Rate = ""
			}
			fx = append(fx, f)
		}
	}
	// Include out-of-order availability, ID/revision ties and differing validity.
	late := fx[7]
	late.AvailableAt = late.At.Add(30 * time.Hour)
	late.ValidUntil = late.At.Add(35 * time.Hour)
	fx = append([]FXFixing{late}, fx...)
	rand.New(rand.NewSource(42)).Shuffle(len(fx), func(i, j int) { fx[i], fx[j] = fx[j], fx[i] })
	series, e := NewFXSeries(fx, "market")
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2557; i++ {
		at := start.AddDate(0, 0, i).Add(time.Duration(i%48) * time.Hour)
		want, e := NewFXResolver(fx, "market", at)
		if e != nil {
			t.Fatal(e)
		}
		got, e := series.At(at)
		if e != nil {
			t.Fatal(e)
		}
		if !reflect.DeepEqual(got.latest, want.latest) {
			t.Fatalf("different eligibility at %s", at)
		}
		for _, pair := range [][2]string{{"USD", "EUR"}, {"EUR", "USD"}, {"RUB", "USD"}} {
			a, ok := got.Resolve(pair[0], pair[1])
			b, wok := want.Resolve(pair[0], pair[1])
			if ok != wok || ok && (a.Rate.Cmp(b.Rate) != 0 || !reflect.DeepEqual(a.Legs, b.Legs)) {
				t.Fatalf("different exact FX/path at %s", at)
			}
		}
	}
	// Resolvers and returned rates must not mutate the reusable series.
	r, _ := series.At(start.AddDate(0, 0, 4))
	v, ok := r.Resolve("USD", "RUB")
	if ok {
		v.Rate.SetInt64(1)
		again, _ := series.At(start.AddDate(0, 0, 4))
		v2, _ := again.Resolve("USD", "RUB")
		if v2.Rate.Cmp(big.NewRat(1, 1)) == 0 {
			t.Fatal("mutable shared rate")
		}
	}
}
func TestFXSeriesConflictBecomesVisibleAtAvailability(t *testing.T) {
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	a := FXFixing{ID: "a", Revision: 1, Policy: "market", From: "USD", To: "RUB", Rate: "90", At: at, AvailableAt: at, ValidUntil: at.Add(48 * time.Hour)}
	b := a
	b.ID = "b"
	b.Rate = "91"
	b.AvailableAt = at.Add(24 * time.Hour)
	s, e := NewFXSeries([]FXFixing{b, a}, "market")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.At(at); e != nil {
		t.Fatal("future conflict leaked", e)
	}
	for _, when := range []time.Time{b.AvailableAt, b.ValidUntil.Add(time.Hour)} {
		if _, e = s.At(when); e == nil {
			t.Fatal("conflict accepted", when)
		}
	}
	b.Policy = "other"
	b.Rate = "invalid"
	if _, e = NewFXSeries([]FXFixing{a, b}, "market"); e != nil {
		t.Fatal("other policy validated", e)
	}
	a.AvailableAt = a.At.Add(-time.Second)
	if _, e = NewFXSeries([]FXFixing{a}, "market"); e == nil {
		t.Fatal("invalid future fixing accepted")
	}
}
