// Package officialfx publishes official reference FX independently of market FX.
package officialfx

import (
	"context"
	"fmt"
	"github.com/positron48/budget/internal/usecase/fximport"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"time"
)

type Source interface {
	FetchOfficialFX(context.Context, time.Time, time.Time) ([]md.FXPoint, error)
}
type Store interface {
	ReadFXSnapshot(context.Context, string, string, time.Time, time.Time) (md.SavedFXPolicy, []md.SavedFXPoint, error)
	SaveOfficialFX(context.Context, string, string, int64, []md.FXPoint) error
}
type Service struct {
	Source      Source
	Store       Store
	Tenant, Key string
}

func (s Service) Import(ctx context.Context, now time.Time, recent bool) (int, error) {
	p, existing, err := s.Store.ReadFXSnapshot(ctx, s.Tenant, s.Key, now, time.Time{})
	if err != nil {
		return 0, err
	}
	if p.Official == nil || md.ValidateFXPolicy(p.FXPolicy) != nil {
		return 0, md.ErrInvalid
	}
	from, to := p.Official.From, fximport.MoscowDate(now)
	if recent && len(existing) > 0 {
		stored := make([]md.FXPoint, len(existing))
		last := from
		for i, point := range existing {
			stored[i] = point.FXPoint
			if day := point.At.Add(3 * time.Hour); day.After(last) {
				last = day
			}
		}
		// An edited policy or an interrupted history must recover the whole
		// range. A long outage must not be reduced to the last seven days.
		if md.ValidateOfficialFXPoints(p.FXPolicy, stored, from, last) == nil {
			candidate := to.AddDate(0, 0, -7)
			if last.Before(candidate) {
				candidate = last.AddDate(0, 0, 1)
			}
			if candidate.After(from) {
				from = candidate
			}
		}
	}
	if to.Before(from) {
		return 0, fmt.Errorf("official FX history has not started")
	}
	points, err := s.Source.FetchOfficialFX(ctx, from, to)
	if err != nil {
		return 0, err
	}
	// Require both currencies for every requested date; a provider failure must
	// never advance the update window or publish a partially fetched history.
	if md.ValidateOfficialFXPoints(p.FXPolicy, points, from, to) != nil {
		return 0, fmt.Errorf("incomplete official FX range")
	}
	if err = s.Store.SaveOfficialFX(ctx, s.Tenant, s.Key, p.Version, points); err != nil {
		return 0, err
	}
	return len(points), nil
}

// Run rechecks full history on startup, then seven overlapping calendar days.
// Only success advances the window; failed attempts retry hourly.
func (s Service) Run(ctx context.Context, report func(int, error)) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	var through time.Time
	var version int64
	for {
		now := time.Now()
		today := fximport.MoscowDate(now)
		attempt, cancel := context.WithTimeout(ctx, 10*time.Minute)
		policy, _, readErr := s.Store.ReadFXSnapshot(attempt, s.Tenant, s.Key, now, time.Time{})
		if readErr != nil {
			report(0, readErr)
		} else if today.After(through) || policy.Version != version {
			n, err := s.Import(attempt, now, !through.IsZero() && policy.Version == version)
			report(n, err)
			if err == nil {
				through, version = today, policy.Version
			}
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
