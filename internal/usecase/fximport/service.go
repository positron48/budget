// Package fximport imports official reference rates without changing transactions.
package fximport

import (
	"context"
	"fmt"
	"time"
)

type Quote struct {
	Currency string
	Rate     string // RUB per one currency unit, decimal
	Date     time.Time
}

type Source interface {
	Fetch(context.Context, time.Time, time.Time) ([]Quote, error)
}
type Store interface {
	SaveCBRRates(context.Context, []Quote) error
}
type Service struct {
	Source Source
	Store  Store
}

func (s Service) Import(ctx context.Context, from, to time.Time, dryRun bool) (int, error) {
	if from.IsZero() || to.Before(from) {
		return 0, fmt.Errorf("invalid import period")
	}
	quotes, err := s.Source.Fetch(ctx, from, to)
	if err != nil {
		return 0, err
	}
	if len(quotes) == 0 {
		return 0, fmt.Errorf("empty FX import")
	}
	if !dryRun {
		if err := s.Store.SaveCBRRates(ctx, quotes); err != nil {
			return 0, err
		}
	}
	return len(quotes), nil
}

// MoscowDate represents a CBR calendar day as midnight UTC (the SQL date boundary).
func MoscowDate(now time.Time) time.Time {
	y, m, d := now.In(time.FixedZone("MSK", 3*60*60)).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// Run retries failed imports hourly. Only a fully persisted import advances the window.
// A restart rechecks all history, repairing gaps without a separate checkpoint table.
func (s Service) Run(ctx context.Context, report func(int, error)) {
	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	var through time.Time
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		today := MoscowDate(time.Now())
		if today.After(through) {
			attempt, cancel := context.WithTimeout(ctx, 10*time.Minute)
			n, err := s.Import(attempt, from, today, false)
			cancel()
			report(n, err)
			if err == nil {
				through = today
				from = today.AddDate(0, 0, -7)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
