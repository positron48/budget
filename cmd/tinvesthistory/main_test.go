package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/positron48/budget/internal/adapter/tinvest"
)

const uid = "001c39ac-ebaa-4fe7-a774-f7a2ba518ef3"

func at(s string) time.Time { v, _ := time.Parse(time.DateOnly, s); return v }

type fake struct {
	calls    int
	fail     bool
	conflict bool
	missing  bool
}

func (f *fake) InstrumentByUID(context.Context, string) (json.RawMessage, error) {
	f.calls++
	if f.missing {
		return nil, &tinvest.HTTPError{StatusCode: 404}
	}
	return json.RawMessage(`{"instrument":{"uid":"` + uid + `","ticker":"TEST","name":"Test","instrumentType":"share","currency":"rub"}}`), nil
}

func TestUnavailableListingIsRecordedAndResumedOffline(t *testing.T) {
	d := privateDir(t)
	f := &fake{missing: true}
	if err := collect(context.Background(), d, testPlan(), false, func() (provider, error) { return f, nil }, io.Discard); err != nil {
		t.Fatal(err)
	}
	if f.calls != 1 {
		t.Fatal("requested candles for unavailable instrument")
	}
	if err := collect(context.Background(), d, testPlan(), true, nil, io.Discard); err != nil {
		t.Fatal(err)
	}
	var index coverageIndex
	if err := load(filepath.Join(d, "coverage.json"), &index); err != nil {
		t.Fatal(err)
	}
	if len(index.Instruments) != 1 || index.Instruments[0].State != "instrument_unavailable" || index.Instruments[0].Bars != 0 || index.Instruments[0].UnavailableSHA256 == "" {
		t.Fatal("unavailable evidence lost")
	}
}
func (f *fake) ExchangeDailyHistory(_ context.Context, _ string, from, to time.Time) (tinvest.DailyHistory, error) {
	f.calls++
	if f.fail && f.calls == 3 {
		return tinvest.DailyHistory{}, errors.New("interrupted")
	}
	var out []tinvest.Candle
	for _, v := range []time.Time{from, to} {
		price := tinvest.Quotation{Units: "1"}
		if f.conflict && f.calls == 3 {
			price.Units = "2"
		}
		out = append(out, tinvest.Candle{Time: v, Source: "CANDLE_SOURCE_EXCHANGE", Open: price, High: price, Low: price, Close: price, Volume: "0", Complete: true})
	}
	return tinvest.DailyHistory{PriceCurrency: "pt.", Candles: out}, nil
}

func testPlan() plan {
	return plan{Version: 1, SourceSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Entries: []entry{{uid, at("2024-01-01"), at("2025-02-01")}}}
}
func privateDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	if err := os.Chmod(d, 0700); err != nil {
		t.Fatal(err)
	}
	return d
}
func TestResumeAndOffline(t *testing.T) {
	d := privateDir(t)
	p := testPlan()
	f := &fake{fail: true}
	if err := collect(context.Background(), d, p, false, func() (provider, error) { return f, nil }, io.Discard); err == nil {
		t.Fatal("expected interrupted second window")
	}
	if f.calls != 3 {
		t.Fatal(f.calls)
	}
	f = &fake{}
	if err := collect(context.Background(), d, p, false, func() (provider, error) { return f, nil }, io.Discard); err != nil {
		t.Fatal(err)
	}
	if f.calls != 1 {
		t.Fatalf("resume made %d calls", f.calls)
	}
	if err := collect(context.Background(), d, p, true, func() (provider, error) { t.Fatal("offline read credential"); return nil, nil }, io.Discard); err != nil {
		t.Fatal(err)
	}
	h, _ := digest(windows(p.Entries[0])[0])
	path := filepath.Join(d, h+".json")
	var c checkpoint
	if err := load(path, &c); err != nil {
		t.Fatal(err)
	}
	c.Request.InstrumentID = "02cfdf61-6298-4c0f-a9ca-9cabc82afaf3"
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := publish(path, c); err != nil {
		t.Fatal(err)
	}
	if err := collect(context.Background(), d, p, true, nil, io.Discard); err == nil {
		t.Fatal("wrong listing accepted")
	}
}
func TestOverlappingCorrectionRequiresNewEvidence(t *testing.T) {
	f := &fake{conflict: true}
	if err := collect(context.Background(), privateDir(t), testPlan(), false, func() (provider, error) { return f, nil }, io.Discard); err == nil {
		t.Fatal("different boundary prices accepted")
	}
}

func TestOfflineRejectsValidButChangedCheckpoint(t *testing.T) {
	d := privateDir(t)
	f := &fake{}
	if err := collect(context.Background(), d, testPlan(), false, func() (provider, error) { return f, nil }, io.Discard); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(d, uid+"-metadata.json")
	var m metadata
	if err := load(path, &m); err != nil {
		t.Fatal(err)
	}
	m.Response = json.RawMessage(`{"instrument":{"uid":"` + uid + `","ticker":"TEST","name":"Changed valid name","instrumentType":"share","currency":"rub"}}`)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := publish(path, m); err != nil {
		t.Fatal(err)
	}
	if err := collect(context.Background(), d, testPlan(), true, nil, io.Discard); err == nil {
		t.Fatal("changed evidence digest accepted")
	}
}
func TestPlanAndImmutableFiles(t *testing.T) {
	p := testPlan()
	if err := validatePlan(p, at("2026-01-01")); err != nil {
		t.Fatal(err)
	}
	p.Entries = append(p.Entries, p.Entries[0])
	if validatePlan(p, at("2026-01-01")) == nil {
		t.Fatal("duplicate entry")
	}
	p = testPlan()
	p.Entries[0].To = at("2027-01-01")
	if validatePlan(p, at("2026-01-01")) == nil {
		t.Fatal("future range")
	}
	d := privateDir(t)
	path := filepath.Join(d, "checkpoint.json")
	if err := publish(path, testPlan()); err != nil {
		t.Fatal(err)
	}
	if publish(path, testPlan()) == nil {
		t.Fatal("overwrote evidence")
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	var loaded plan
	if load(path, &loaded) == nil {
		t.Fatal("public checkpoint accepted")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{} {}`), 0600); err != nil {
		t.Fatal(err)
	}
	if load(path, &loaded) == nil {
		t.Fatal("trailing input accepted")
	}
}
