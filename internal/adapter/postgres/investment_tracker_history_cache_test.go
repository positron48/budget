package postgres

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
)

func TestTrackerHistoryCacheOwnsExactFactsAndExpires(t *testing.T) {
	c := newTrackerHistoryCache()
	now := time.Now()
	p := nav.TrackerHistoryPoint{Date: "2020-01-01", KnownValue: big.NewRat(1, 3), StrategyFlow: big.NewRat(-1, 7), State: "ready"}
	c.put("scope/day", p, now)
	p.KnownValue.SetInt64(99)
	got, ok := c.get("scope/day", now)
	if !ok || got.KnownValue.RatString() != "1/3" || got.StrategyFlow.RatString() != "-1/7" {
		t.Fatal(got, ok)
	}
	got.KnownValue.SetInt64(88)
	again, ok := c.get("scope/day", now)
	if !ok || again.KnownValue.RatString() != "1/3" {
		t.Fatal("mutable cache escaped")
	}
	if _, ok = c.get("other-tenant/day", now); ok {
		t.Fatal("scope crossed")
	}
	if _, ok = c.get("scope/day", now.Add(c.ttl)); ok {
		t.Fatal("expired entry reused")
	}
	if c.bytes != 0 || len(c.entries) != 0 {
		t.Fatal("expired memory retained")
	}
}

func TestTrackerHistoryCacheBoundsMemoryAndRetainsRecentAccess(t *testing.T) {
	c := newTrackerHistoryCache()
	c.limit = 1500
	now := time.Now()
	p := nav.TrackerHistoryPoint{Date: "2020-01-01", KnownValue: big.NewRat(1, 3), State: "ready"}
	c.put("one", p, now)
	c.put("two", p, now)
	c.limit = c.bytes + 1
	c.get("one", now)
	c.put("three", p, now)
	if c.bytes > c.limit {
		t.Fatal("cache exceeded bound")
	}
	if _, ok := c.get("two", now); ok {
		t.Fatal("oldest access was not evicted")
	}
	if _, ok := c.get("three", now); !ok {
		t.Fatal("newest entry absent")
	}
}

func TestTrackerHistoryCacheRetainsSevenYearsWithinBound(t *testing.T) {
	c := newTrackerHistoryCache()
	now := time.Now()
	first := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 2557; i++ {
		date := first.AddDate(0, 0, i).Format(time.DateOnly)
		c.put("tenant/actor/fenced-basis/RUB/"+date, nav.TrackerHistoryPoint{Date: date, State: "ready", KnownValue: big.NewRat(int64(i), 3), StrategyFlow: big.NewRat(-1, 7)}, now)
	}
	for i := 0; i < 2557; i++ {
		date := first.AddDate(0, 0, i).Format(time.DateOnly)
		got, ok := c.get("tenant/actor/fenced-basis/RUB/"+date, now)
		if !ok || got.KnownValue.Cmp(big.NewRat(int64(i), 3)) != 0 {
			t.Fatal("seven-year exact cache incomplete", date)
		}
	}
	if c.bytes > c.limit {
		t.Fatal("seven-year cache exceeded bound")
	}
}

func TestTrackerHistoryAdmissionIsBoundedAndCancellable(t *testing.T) {
	repo := NewMarketDataRepo(nil)
	first, e := repo.acquireTrackerHistorySlot(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	second, e := repo.acquireTrackerHistorySlot(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = repo.acquireTrackerHistorySlot(ctx); !errors.Is(e, context.Canceled) {
		t.Fatal("cancelled history admitted", e)
	}
	first()
	third, e := repo.acquireTrackerHistorySlot(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	third()
	second()
	if len(repo.trackerHistorySlots) != 0 {
		t.Fatal("history admission leaked")
	}
}
