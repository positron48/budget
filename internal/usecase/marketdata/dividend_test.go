package marketdata

import (
	"context"
	"testing"
	"time"
)

func dividendFixture() DividendFacts {
	f := bondFixture()
	f.Instrument.Kind = "share"
	return DividendFacts{Instrument: f.Instrument, From: f.From, To: f.To, AsOf: f.AsOf, Dividends: []Dividend{
		{RecordDate: f.From.AddDate(0, 0, 20), PaymentDate: f.To.AddDate(0, 0, 10), DividendType: "Cancelled", DividendNet: &BondAmount{Amount: "0", Currency: "USD"}},
		{RecordDate: f.From.AddDate(0, 0, 2), DividendType: "Return of Capital"},
	}}
}

func TestDividendFactsDoNotInferPaymentOrIncome(t *testing.T) {
	f := dividendFixture()
	n, err := NormalizeDividendFacts(f)
	if err != nil || n.Dividends[0].DividendNet != nil || !n.Dividends[0].PaymentDate.IsZero() || n.Dividends[1].DividendNet.Amount != "0" || !n.Dividends[1].PaymentDate.After(n.To) || f.Dividends[0].DividendType != "Cancelled" {
		t.Fatal(n, err)
	}
	for _, change := range []func(*DividendFacts){
		func(f *DividendFacts) { f.Dividends[0].RecordDate = f.To.Add(time.Second) },
		func(f *DividendFacts) { f.Dividends[0].DividendNet = &BondAmount{Amount: "-1", Currency: "RUB"} },
		func(f *DividendFacts) { f.Dividends = append(f.Dividends, f.Dividends[0]) },
		func(f *DividendFacts) { f.Instrument.Kind = "bond" },
		func(f *DividendFacts) { f.Dividends[0].DividendType = "bad\x00type" },
	} {
		f := dividendFixture()
		change(&f)
		if _, err := NormalizeDividendFacts(f); err == nil {
			t.Fatal("invalid declaration accepted")
		}
	}
}

type dividendQueueStub struct {
	fakeQueue
	facts DividendFacts
}

func (q *dividendQueueStub) CompleteDividendFacts(_ context.Context, _ Job, f DividendFacts) error {
	q.completed = true
	q.facts = f
	return nil
}

type dividendProviderStub struct {
	fakeProvider
	facts DividendFacts
}

func (p dividendProviderStub) LoadDividendFacts(context.Context, JobRequest) (DividendFacts, error) {
	return p.facts, p.err
}
func TestDividendWorkerDispatch(t *testing.T) {
	q := &dividendQueueStub{fakeQueue: fakeQueue{job: Job{JobRequest: JobRequest{Kind: "dividend_facts"}}}}
	w := Worker{Queue: q, Provider: dividendProviderStub{facts: dividendFixture()}, Scope: Scope{TenantID: "11111111-1111-4111-8111-111111111111", Provider: "tinvest", AccessScope: "personal"}, Lease: time.Minute}
	if err := w.RunOne(context.Background()); err != nil || !q.completed || len(q.facts.Dividends) != 2 {
		t.Fatal(q, err)
	}
}
