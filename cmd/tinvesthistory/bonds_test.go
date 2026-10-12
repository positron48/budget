package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	md "github.com/positron48/budget/internal/usecase/marketdata"
)

type fakeBonds struct {
	calls       int
	fail, wrong bool
}

func (f *fakeBonds) LoadBondFacts(_ context.Context, q md.JobRequest) (md.BondFacts, error) {
	f.calls++
	if f.fail && f.calls == 2 {
		return md.BondFacts{}, errors.New("interrupted")
	}
	instrument := md.Instrument{ID: uid, Kind: "bond", Detailed: true, Name: "Test bond", Ticker: "TEST", Currency: "RUB"}
	if f.wrong {
		instrument.ID = "02cfdf61-6298-4c0f-a9ca-9cabc82afaf3"
	}
	amount, nominal := "0", "1000"
	return md.BondFacts{Instrument: instrument, From: q.From, To: q.To, AsOf: q.To, Accrued: []md.BondAccrued{{Date: q.From, Value: &amount, Nominal: &nominal}}}, nil
}
func bondBase(t *testing.T) string {
	t.Helper()
	base := privateDir(t)
	c := coverageIndex{Instruments: []coverage{{State: "observed", Instrument: md.Instrument{ID: uid, Kind: "bond"}}}}
	if err := publish(filepath.Join(base, "coverage.json"), c); err != nil {
		t.Fatal(err)
	}
	return base
}
func TestBondResumeOfflineAndTampering(t *testing.T) {
	base, dir := bondBase(t), privateDir(t)
	p := testPlan()
	f := &fakeBonds{fail: true}
	run := func(offline bool) error {
		return collectBonds(context.Background(), base, dir, p, offline, func() (bondProvider, error) { return f, nil }, io.Discard)
	}
	if run(false) == nil || f.calls != 2 {
		t.Fatal("second window failure not preserved", f.calls)
	}
	f = &fakeBonds{}
	if err := run(false); err != nil {
		t.Fatal(err)
	}
	if f.calls != 1 {
		t.Fatal("completed window fetched twice", f.calls)
	}
	if err := collectBonds(context.Background(), base, dir, p, true, nil, io.Discard); err != nil {
		t.Fatal(err)
	}
	q := windows(p.Entries[0])[0]
	q.Kind = "bond_facts"
	h, _ := digest(q)
	path := filepath.Join(dir, h+".json")
	var facts md.BondFacts
	if err := load(path, &facts); err != nil {
		t.Fatal(err)
	}
	changed := "1"
	facts.Accrued[0].Value = &changed
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := publish(path, facts); err != nil {
		t.Fatal(err)
	}
	if run(true) == nil {
		t.Fatal("valid but changed financial evidence accepted")
	}
}
func TestBondRejectWrongIdentityBeforePublish(t *testing.T) {
	base, dir := bondBase(t), privateDir(t)
	f := &fakeBonds{wrong: true}
	err := collectBonds(context.Background(), base, dir, testPlan(), false, func() (bondProvider, error) { return f, nil }, io.Discard)
	if err == nil {
		t.Fatal("wrong listing accepted")
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 || files[0].Name() != "manifest.json" {
		t.Fatal("invalid facts published", files, err)
	}
}
