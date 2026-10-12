package alphafx

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	av "github.com/positron48/budget/internal/adapter/alphavantage"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

type fixtureSource struct {
	admitted                bool
	gates, calls            int
	failSecond, wrongWindow bool
}

func (f *fixtureSource) BeginBatch(context.Context) (bool, error) { f.gates++; return f.admitted, nil }
func (f *fixtureSource) History(_ context.Context, pair, from, to string) (av.Audit, error) {
	f.calls++
	if f.failSecond && pair == "EUR/RUB" {
		return av.Audit{}, errors.New("provider failed")
	}
	if f.wrongWindow {
		from = "2024-01-06"
	}
	return av.Audit{Source: "alphavantage", Pair: pair, From: from, To: to, Timezone: "UTC", LastRefreshed: "2024-01-05", BodySHA256: strings.Repeat("a", 64), RetrievedAt: time.Date(2024, 1, 8, 12, 0, 0, 0, time.UTC), Bars: []av.Bar{{Date: "2024-01-05", Open: "90", High: "100", Low: "80", Close: "90"}}}, nil
}

type fixtureStore struct {
	policy   md.SavedFXPolicy
	saves    int
	failSave bool
}

func (f *fixtureStore) ReadFXSnapshot(context.Context, string, string, time.Time, time.Time) (md.SavedFXPolicy, []md.SavedFXPoint, error) {
	return f.policy, nil, nil
}
func (f *fixtureStore) SaveAlphaFX(_ context.Context, _, _ string, v int64, a []av.Audit) (int, error) {
	if v != f.policy.Version || len(a) != 2 {
		panic("invalid publication")
	}
	if f.failSave {
		return 0, md.ErrConflict
	}
	f.saves++
	return len(a), nil
}
func fixture() (Service, *fixtureSource, *fixtureStore, time.Time) {
	source := &fixtureSource{admitted: true}
	store := &fixtureStore{policy: md.SavedFXPolicy{FXPolicy: md.FXPolicy{Key: "alpha-retrospective", Mode: "market", Source: "alphavantage", AccessScope: "personal", SourceReference: "Synthetic", TimeConvention: md.AlphaTimeConvention, ValueConvention: md.AlphaValueConvention, ValidityConvention: md.AlphaValidityConvention, Alpha: &md.AlphaFXPolicy{From: time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC), Convention: md.AlphaRetrospectiveConvention}}, Version: 3}}
	return Service{Source: source, Store: store, Tenant: "fixture", Key: "alpha-retrospective", pause: func(context.Context) error { return nil }}, source, store, time.Date(2024, 1, 8, 12, 0, 0, 0, time.UTC)
}
func TestSyncWholeHistoryAndQuotaDeferral(t *testing.T) {
	s, source, store, now := fixture()
	result, err := s.Sync(context.Background(), now)
	if err != nil || result.Deferred || result.Points != 2 || store.saves != 1 || source.calls != 2 || source.gates != 1 {
		t.Fatal(result, err)
	}
	source.admitted = false
	// A replacement service instance still consults the shared gate.
	restarted := Service{Source: source, Store: store, Tenant: s.Tenant, Key: s.Key}
	result, err = restarted.Sync(context.Background(), now)
	if err != nil || !result.Deferred || store.saves != 1 || source.calls != 2 {
		t.Fatal(result, err)
	}
}
func TestSyncRefusesPartialWrongOrUnapprovedPublication(t *testing.T) {
	for _, kind := range []string{"second", "window", "policy", "cancel", "version"} {
		t.Run(kind, func(t *testing.T) {
			s, source, store, now := fixture()
			switch kind {
			case "second":
				source.failSecond = true
			case "window":
				source.wrongWindow = true
			case "policy":
				store.policy.Alpha = nil
			case "cancel":
				s.pause = func(context.Context) error { return context.Canceled }
			case "version":
				store.failSave = true
			}
			if _, err := s.Sync(context.Background(), now); err == nil || store.saves != 0 {
				t.Fatal("partial or unapproved batch published", err)
			}
			if kind == "policy" && source.gates != 0 {
				t.Fatal("unreviewed policy consumed credential quota")
			}
		})
	}
}
