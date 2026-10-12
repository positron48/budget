package postgres

import (
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"testing"
	"time"
)

func TestOverviewFXHonorsSourceAvailabilityExpiryAndInverse(t *testing.T) {
	day := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	policy := md.SavedFXPolicy{FXPolicy: md.FXPolicy{Key: "market", Mode: "market", Source: "verified"}, Version: 2}
	point := func(from, to, rate string, days int) md.SavedFXPoint {
		at := day.AddDate(0, 0, days)
		return md.SavedFXPoint{FXPoint: md.FXPoint{From: from, To: to, Rate: rate, At: at, AvailableAt: at, ValidUntil: at.Add(24 * time.Hour)}, Revision: 3}
	}
	points := []md.SavedFXPoint{point("USD", "RUB", "10", 0), point("RUB", "USD", "0.05", 1), point("EUR", "RUB", "40", 0), point("USD", "RUB", "999", 3)}
	unpublished := point("USD", "RUB", "100", 1)
	unpublished.At = unpublished.At.Add(time.Hour)
	unpublished.AvailableAt = day.AddDate(0, 0, 3)
	points = append(points, unpublished)
	got := overviewFX(policy, points, "market", day.Add(36*time.Hour))
	if len(got) != 2 || got[0].Rate.RatString() != "20" || got[0].Change.RatString() != "1" || got[0].State != "fresh" || got[0].Revision != 3 || got[0].PolicyVersion != 2 || got[0].Source != "verified" || got[1].State != "expired" || got[1].Change != nil {
		t.Fatal(got)
	}
	points = append(points, point("USD", "RUB", "30", 1))
	got = overviewFX(policy, points, "market", day.Add(36*time.Hour))
	if got[0].Rate != nil || got[0].Change != nil || got[0].State != "unavailable" {
		t.Fatal("conflicting direct/inverse evidence", got)
	}
	got = overviewFX(md.SavedFXPolicy{}, nil, "missing", day)
	if got[0].Rate != nil || got[1].Rate != nil || got[0].State != "unavailable" {
		t.Fatal(got)
	}
}
