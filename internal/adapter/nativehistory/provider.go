// Package nativehistory binds reviewed native listing identities to the
// ordinary durable market queue. It cannot search or infer a ticker mapping.
package nativehistory

import (
	"context"
	"strings"
	"time"

	"github.com/positron48/budget/internal/adapter/moex"
	td "github.com/positron48/budget/internal/adapter/twelvedata"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

type MOEX interface {
	History(context.Context, moex.Listing, string, string) (moex.History, error)
	BondHistory(context.Context, moex.Listing, string, string) (moex.History, error)
}
type Equities interface {
	LoadEquityHistory(context.Context, td.EquityListing, string, string) (td.EquityAudit, []md.Observation, error)
}
type Provider struct {
	Scope    md.Scope
	Lookup   func(context.Context, string) (md.Instrument, error)
	MOEX     MOEX
	Equities Equities
	Now      func() time.Time
}

// Identity verifies that canonical query terms reproduce the provider UUID.
// Broker UIDs and same-ticker listings cannot enter a native source by mistake.
func Identity(provider string, i md.Instrument) error {
	if md.ValidateInstrument(i) != nil || !i.Detailed {
		return md.ErrInvalid
	}
	switch provider {
	case "moex":
		l := moex.Listing{SecurityID: i.Ticker, ISIN: i.ISIN, Board: i.ClassCode}
		id := l.ID()
		if i.Kind == "bond" {
			id = l.BondID()
		} else if i.Kind != "share" && i.Kind != "etf" {
			return md.ErrInvalid
		}
		if id != i.ID {
			return md.ErrInvalid
		}
	case "twelvedata":
		parts := strings.Split(i.ClassCode, ":")
		if len(parts) != 2 || i.Kind != "share" || i.Currency != "USD" {
			return md.ErrInvalid
		}
		l := td.EquityListing{Symbol: i.Ticker, Exchange: parts[0], MIC: parts[1]}
		validVenue := l.Exchange == "NASDAQ" && l.MIC == "XNGS" || l.Exchange == "NYSE" && l.MIC == "XNYS"
		if l.ID() != i.ID || !validVenue {
			return md.ErrInvalid
		}
	default:
		return md.ErrInvalid
	}
	return nil
}

func (p *Provider) SearchMarketCatalog(context.Context, md.JobRequest) (md.CatalogResult, error) {
	return md.CatalogResult{}, md.ErrInvalid
}
func (p *Provider) LoadMarketInstrument(ctx context.Context, id string) (md.Instrument, error) {
	if md.ValidateScope(p.Scope) != nil || p.Lookup == nil {
		return md.Instrument{}, md.ErrInvalid
	}
	i, e := p.Lookup(ctx, id)
	if e != nil {
		return i, e
	}
	if i.ID != id || Identity(p.Scope.Provider, i) != nil {
		return md.Instrument{}, md.ErrInvalid
	}
	return i, nil
}
func (p *Provider) LoadMarketObservations(ctx context.Context, q md.JobRequest) ([]md.Observation, error) {
	if md.ValidateRequest(q) != nil || q.Kind != "daily_candle" {
		return nil, md.ErrInvalid
	}
	i, e := p.LoadMarketInstrument(ctx, q.InstrumentID)
	if e != nil {
		return nil, e
	}
	now := time.Now().UTC()
	if p.Now != nil {
		now = p.Now().UTC()
	}
	last := now.Truncate(24*time.Hour).AddDate(0, 0, -1)
	if p.Scope.Provider == "twelvedata" {
		loc, e := time.LoadLocation("America/New_York")
		if e != nil {
			return nil, e
		}
		local := now.In(loc)
		last = time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
	}
	first, end := q.From.UTC().Truncate(24*time.Hour), q.To.UTC().Truncate(24*time.Hour)
	if first.Before(q.From) {
		first = first.AddDate(0, 0, 1)
	}
	if end.After(last) {
		end = last
	}
	if first.After(end) {
		return []md.Observation{}, nil
	}
	var observations []md.Observation
	if p.Scope.Provider == "moex" {
		if p.MOEX == nil {
			return nil, md.ErrInvalid
		}
		l := moex.Listing{SecurityID: i.Ticker, ISIN: i.ISIN, Board: i.ClassCode}
		var h moex.History
		if i.Kind == "bond" {
			h, e = p.MOEX.BondHistory(ctx, l, first.Format(time.DateOnly), end.Format(time.DateOnly))
		} else {
			h, e = p.MOEX.History(ctx, l, first.Format(time.DateOnly), end.Format(time.DateOnly))
		}
		observations = h.Observations
	} else {
		if p.Equities == nil {
			return nil, md.ErrInvalid
		}
		parts := strings.Split(i.ClassCode, ":")
		_, observations, e = p.Equities.LoadEquityHistory(ctx, td.EquityListing{Symbol: i.Ticker, Exchange: parts[0], MIC: parts[1]}, first.Format(time.DateOnly), end.Format(time.DateOnly))
	}
	if e != nil {
		return nil, e
	}
	for _, o := range observations {
		if o.InstrumentID != i.ID || o.Currency != i.Currency || o.At.Before(q.From) || o.At.After(q.To) || o.Adjustment != "unadjusted" || o.SourceKind != "exchange" || o.Kind != "daily_candle" || !o.Complete {
			return nil, md.ErrInvalid
		}
		if i.Kind == "bond" && (o.Nominal == "" || o.NominalCurrency != i.Currency) {
			return nil, md.ErrInvalid
		}
		if _, e = md.Normalize(o); e != nil {
			return nil, e
		}
	}
	return observations, nil
}
