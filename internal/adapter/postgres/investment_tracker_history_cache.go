package postgres

import (
	"container/list"
	"context"
	"encoding/json"
	"sync"
	"time"

	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
)

// A bounded, disposable projection cache. Every read first rechecks membership,
// ledger identities and the current journal/market/FX/classification basis in a
// repeatable-read transaction. Entries never replace those authorization checks.
// Exact daily facts are reusable across periods; financial journals stay in PG.
type trackerHistoryCache struct {
	mu      sync.Mutex
	entries map[string]*list.Element
	order   list.List
	bytes   int
	limit   int
	ttl     time.Duration
}
type trackerHistoryCacheEntry struct {
	key     string
	data    []byte
	expires time.Time
}

func newTrackerHistoryCache() *trackerHistoryCache {
	return &trackerHistoryCache{entries: map[string]*list.Element{}, limit: 32 << 20, ttl: 24 * time.Hour}
}

func (c *trackerHistoryCache) get(key string, now time.Time) (nav.TrackerHistoryPoint, bool) {
	if c == nil {
		return nav.TrackerHistoryPoint{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return nav.TrackerHistoryPoint{}, false
	}
	value := entry.Value.(trackerHistoryCacheEntry)
	if !now.Before(value.expires) {
		c.remove(entry)
		return nav.TrackerHistoryPoint{}, false
	}
	var point nav.TrackerHistoryPoint
	if json.Unmarshal(value.data, &point) != nil {
		c.remove(entry)
		return nav.TrackerHistoryPoint{}, false
	}
	c.order.MoveToBack(entry)
	return point, true
}

func (c *trackerHistoryCache) put(key string, point nav.TrackerHistoryPoint, now time.Time) {
	if c == nil {
		return
	}
	data, e := json.Marshal(point)
	if e != nil || len(data) > 64<<10 {
		return
	}
	size := len(key) + len(data) + 128
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry := c.entries[key]; entry != nil {
		c.remove(entry)
	}
	if size > c.limit {
		return
	}
	for c.bytes+size > c.limit {
		c.remove(c.order.Front())
	}
	entry := c.order.PushBack(trackerHistoryCacheEntry{key: key, data: data, expires: now.Add(c.ttl)})
	c.entries[key] = entry
	c.bytes += size
}

func (c *trackerHistoryCache) remove(entry *list.Element) {
	if entry == nil {
		return
	}
	value := entry.Value.(trackerHistoryCacheEntry)
	c.bytes -= len(value.key) + len(value.data) + 128
	delete(c.entries, value.key)
	c.order.Remove(entry)
}

// Keep simultaneous cold replays within the app's memory/CPU budget. Waiting
// requests have not loaded journals or allocated daily position snapshots yet.
func (r *MarketDataRepo) acquireTrackerHistorySlot(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.trackerHistorySlots == nil {
		return func() {}, nil
	}
	select {
	case r.trackerHistorySlots <- struct{}{}:
		return func() { <-r.trackerHistorySlots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
