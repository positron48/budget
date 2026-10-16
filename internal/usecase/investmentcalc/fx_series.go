package investmentcalc

import (
	"fmt"
	"sort"
	"time"
)

type fxSeriesEntry struct {
	at     time.Time
	fixing FXFixing
	order  int
}

// FXSeries validates an immutable snapshot once and indexes the latest eligible
// fixing per pair. A query keeps the same availability, conflict and expiry
// rules as NewFXResolver, including not reviving an older expired replacement.
type FXSeries struct {
	pairs        map[[2]string][]fxSeriesEntry
	conflictFrom time.Time
}

func NewFXSeries(fixings []FXFixing, policy string) (*FXSeries, error) {
	if policy == "" {
		return nil, fmt.Errorf("FX policy and cutoff required")
	}
	out := &FXSeries{pairs: map[[2]string][]fxSeriesEntry{}}
	type event struct {
		fixing FXFixing
		order  int
		rate   string
	}
	events := []event{}
	for order, f := range fixings {
		if f.Policy != policy {
			continue
		}
		rate, e := validateFXFixing(f)
		if e != nil {
			return nil, e
		}
		r := "withdrawn"
		if rate != nil {
			r = rate.RatString()
		}
		events = append(events, event{f, order, r})
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].fixing.AvailableAt.Before(events[j].fixing.AvailableAt) })
	type identity struct {
		pair [2]string
		at   time.Time
	}
	seen := map[identity]string{}
	for _, event := range events {
		f := event.fixing
		pair := [2]string{f.From, f.To}
		key := identity{pair, f.At.UTC()}
		if old, ok := seen[key]; ok && old != event.rate && out.conflictFrom.IsZero() {
			out.conflictFrom = f.AvailableAt
		}
		seen[key] = event.rate
		entries := out.pairs[pair]
		replace := len(entries) == 0
		if !replace {
			old := entries[len(entries)-1].fixing
			replace = f.At.After(old.At)
			if f.At.Equal(old.At) {
				replace = f.ID < old.ID
				if f.ID == old.ID {
					replace = f.Revision > old.Revision || f.Revision == old.Revision && event.order < entries[len(entries)-1].order
				}
			}
		}
		if replace {
			out.pairs[pair] = append(entries, fxSeriesEntry{f.AvailableAt, f, event.order})
		}
	}
	return out, nil
}
func (s *FXSeries) At(at time.Time) (*FXResolver, error) {
	if s == nil || at.IsZero() {
		return nil, fmt.Errorf("FX policy and cutoff required")
	}
	if !s.conflictFrom.IsZero() && !at.Before(s.conflictFrom) {
		return nil, fmt.Errorf("conflicting FX fixing")
	}
	out := &FXResolver{latest: map[[2]string]FXFixing{}}
	for pair, entries := range s.pairs {
		i := sort.Search(len(entries), func(i int) bool { return entries[i].at.After(at) }) - 1
		if i < 0 {
			continue
		}
		f := entries[i].fixing
		if !f.Withdrawn && at.Before(f.ValidUntil) {
			out.latest[pair] = f
		}
	}
	return out, nil
}
