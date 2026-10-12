// Package alphafx refreshes explicitly approved personal retrospective FX.
package alphafx

import (
	"context"
	"errors"
	"time"

	av "github.com/positron48/budget/internal/adapter/alphavantage"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

type Source interface {
	BeginBatch(context.Context) (bool, error)
	History(context.Context, string, string, string) (av.Audit, error)
}
type Store interface {
	ReadFXSnapshot(context.Context, string, string, time.Time, time.Time) (md.SavedFXPolicy, []md.SavedFXPoint, error)
	SaveAlphaFX(context.Context, string, string, int64, []av.Audit) (int, error)
}
type Service struct {
	Source      Source
	Store       Store
	Tenant, Key string
	pause       func(context.Context) error
}
type Result struct {
	Deferred bool
	Points   int
}

func (s Service) Sync(ctx context.Context, now time.Time) (Result, error) {
	var result Result
	p, _, err := s.Store.ReadFXSnapshot(ctx, s.Tenant, s.Key, now, time.Time{})
	if err != nil {
		return result, err
	}
	if p.Alpha == nil || md.ValidateFXPolicy(p.FXPolicy) != nil {
		return result, md.ErrInvalid
	}
	from := p.Alpha.From.Format(time.DateOnly)
	to := now.UTC().Truncate(24 * time.Hour).Add(-24 * time.Hour).Format(time.DateOnly)
	if to < from {
		return result, md.ErrInvalid
	}
	admitted, err := s.Source.BeginBatch(ctx)
	if err != nil {
		return result, err
	}
	if !admitted {
		result.Deferred = true
		return result, nil
	}
	var audits []av.Audit
	for i, pair := range []string{"USD/RUB", "EUR/RUB"} {
		if i > 0 {
			pause := s.pause
			if pause == nil {
				pause = waitBetweenRequests
			}
			if err = pause(ctx); err != nil {
				return result, err
			}
		}
		audit, e := s.Source.History(ctx, pair, from, to)
		if e != nil {
			return result, e
		}
		if audit.Pair != pair || audit.From != from || audit.To != to {
			return result, errors.New("alpha history identity/window mismatch")
		}
		if _, e = av.RetrospectivePoints(p.FXPolicy, audit); e != nil {
			return result, e
		}
		audits = append(audits, audit)
	}
	result.Points, err = s.Store.SaveAlphaFX(ctx, s.Tenant, s.Key, p.Version, audits)
	return result, err
}
func waitBetweenRequests(ctx context.Context) error {
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Run polls hourly; the database gate permits a batch only once per six hours.
// Every admitted batch fetches full history, recovering outages and corrections.
func (s Service) Run(ctx context.Context, report func(Result, error)) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		attempt, cancel := context.WithTimeout(ctx, 5*time.Minute)
		result, err := s.Sync(attempt, time.Now().UTC())
		cancel()
		if ctx.Err() != nil {
			return
		}
		report(result, err)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
