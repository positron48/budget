package officialfx

import (
	"context"
	"errors"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"strings"
	"testing"
	"time"
)

type fixture struct {
	policy            md.SavedFXPolicy
	from, to          time.Time
	calls             int
	existing          []md.SavedFXPoint
	fetchErr, saveErr error
}

func (f *fixture) ReadFXSnapshot(context.Context, string, string, time.Time, time.Time) (md.SavedFXPolicy, []md.SavedFXPoint, error) {
	return f.policy, f.existing, nil
}
func (f *fixture) SaveOfficialFX(_ context.Context, _, _ string, v int64, _ []md.FXPoint) error {
	f.calls++
	if v != f.policy.Version {
		return md.ErrConflict
	}
	return f.saveErr
}
func (f *fixture) FetchOfficialFX(_ context.Context, from, to time.Time) ([]md.FXPoint, error) {
	f.from, f.to = from, to
	if f.fetchErr != nil {
		return nil, f.fetchErr
	}
	var result []md.FXPoint
	for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
		for _, code := range []string{"USD", "EUR"} {
			at := day.Add(-3 * time.Hour)
			result = append(result, md.FXPoint{From: code, To: "RUB", Rate: "100", At: at, AvailableAt: at, ValidUntil: at.Add(24 * time.Hour), EvidenceSHA256: strings.Repeat("a", 64), RecordReference: "Synthetic"})
		}
	}
	return result, nil
}
func TestOfficialFXImportCoverageAndPolicyFence(t *testing.T) {
	day := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, mode := range []string{"full", "recent", "gap", "incomplete", "new-version", "fetch-failure", "version-failure", "market"} {
		t.Run(mode, func(t *testing.T) {
			f := &fixture{policy: md.SavedFXPolicy{Version: 3, FXPolicy: md.FXPolicy{Key: "cbr", Mode: "cbr", Source: "cbr", AccessScope: "public", SourceReference: "CBR", TimeConvention: "Effective Moscow date", ValueConvention: "Per unit", ValidityConvention: "Confirmed daily snapshot", Official: &md.OfficialFXPolicy{From: day, Convention: "effective_moscow_date_v1"}}}}
			if mode == "fetch-failure" {
				f.fetchErr = errors.New("source unavailable")
			}
			if mode == "version-failure" {
				f.saveErr = md.ErrConflict
			}
			if mode == "market" {
				f.policy.Mode = "market"
			}
			if mode == "recent" || mode == "gap" || mode == "incomplete" {
				last := day.AddDate(0, 0, 29)
				if mode == "gap" {
					last = day.AddDate(0, 0, 4)
				}
				past, _ := f.FetchOfficialFX(context.Background(), day, last)
				for _, point := range past {
					f.existing = append(f.existing, md.SavedFXPoint{FXPoint: point})
				}
				if mode == "incomplete" {
					f.existing = f.existing[2:]
				}
			}
			svc := Service{Source: f, Store: f, Tenant: "tenant", Key: "cbr"}
			n, e := svc.Import(context.Background(), day.AddDate(0, 0, 30), mode == "recent" || mode == "gap" || mode == "incomplete" || mode == "new-version")
			if mode == "full" || mode == "recent" || mode == "gap" || mode == "incomplete" || mode == "new-version" {
				want := 62
				if mode == "recent" {
					want = 16
				}
				if mode == "gap" {
					want = 52
				}
				if e != nil || n != want || f.calls != 1 {
					t.Fatal(n, e, f.calls)
				}
			} else if e == nil {
				t.Fatal("failure ignored")
			}
			if (mode == "fetch-failure" || mode == "market") && f.calls != 0 {
				t.Fatal("failed import published")
			}
		})
	}
}
