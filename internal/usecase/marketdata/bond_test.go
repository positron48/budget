package marketdata

import (
	"context"
	"testing"
	"time"
)

func bondFixture() BondFacts {
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	return BondFacts{Instrument: Instrument{ID: "11111111-1111-4111-8111-111111111111", Name: "Bond", Ticker: "TEST", Kind: "bond", Currency: "RUB", Detailed: true}, From: at, To: at.AddDate(0, 0, 30), AsOf: at, Terms: BondTerms{Nominal: &BondAmount{"0", "RUB"}}}
}
func TestBondFactsValidation(t *testing.T) {
	f := bondFixture()
	if _, err := NormalizeBondFacts(f); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*BondFacts){
		func(f *BondFacts) { f.Terms.Nominal.Amount = "-1" }, func(f *BondFacts) { f.Instrument.Kind = "share" }, func(f *BondFacts) { f.Terms.InitialNominal = &BondAmount{"0", "RUB"} },
		func(f *BondFacts) { f.Accrued = []BondAccrued{{Date: f.From.Add(-time.Second)}} },
		func(f *BondFacts) {
			f.Events = []BondEvent{{Kind: "EVENT_TYPE_CPN", Date: f.From, Note: "bad\x00text"}}
		},
		func(f *BondFacts) {
			f.Coupons = []BondCoupon{{Number: 1, Kind: "COUPON_TYPE_FIX", Date: f.From}, {Number: 1, Kind: "COUPON_TYPE_FIX", Date: f.To}}
		},
	} {
		f := bondFixture()
		mutate(&f)
		if _, err := NormalizeBondFacts(f); err == nil {
			t.Fatal("invalid evidence accepted", f)
		}
	}
}

type bondQueueStub struct {
	fakeQueue
	facts BondFacts
}

func (q *bondQueueStub) CompleteBondFacts(_ context.Context, _ Job, f BondFacts) error {
	q.completed = true
	q.facts = f
	return nil
}

type bondProviderStub struct {
	fakeProvider
	facts BondFacts
}

func (p bondProviderStub) LoadBondFacts(context.Context, JobRequest) (BondFacts, error) {
	return p.facts, p.err
}
func TestBondWorkerDispatch(t *testing.T) {
	for _, fail := range []bool{false, true} {
		q := &bondQueueStub{fakeQueue: fakeQueue{job: Job{JobRequest: JobRequest{Kind: "bond_facts"}}}}
		p := bondProviderStub{facts: bondFixture()}
		if fail {
			p.err = ErrInvalid
		}
		w := Worker{Queue: q, Provider: p, Scope: Scope{TenantID: "11111111-1111-4111-8111-111111111111", Provider: "tinvest", AccessScope: "personal"}, Lease: time.Minute}
		if err := w.RunOne(context.Background()); err != nil {
			t.Fatal(err)
		}
		if q.completed == fail || q.failed != fail || !fail && q.facts.Instrument.ID != p.facts.Instrument.ID {
			t.Fatal(q)
		}
	}
}
