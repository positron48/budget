// tinvestfxcheck audits public currency metadata and full requested candle
// windows. It does not confirm an FX policy or import financial operations.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"time"

	ti "github.com/positron48/budget/internal/adapter/tinvest"
)

type chunk struct {
	From, To time.Time
	History  ti.DailyHistory
}
type listing struct {
	Metadata                ti.CurrencyDetails
	MetadataValidationError string `json:"metadata_validation_error,omitempty"`
	Chunks                  []chunk
}
type report struct {
	CatalogMethod       string                    `json:"catalog_method,omitempty"`
	CatalogItems        int                       `json:"catalog_items,omitempty"`
	CurrencyCatalog     []ti.CurrencyCatalogEntry `json:"currency_catalog,omitempty"`
	Missing             []string
	CatalogChecked      bool
	From, To, CheckedAt time.Time
	Listings            []listing
}

func save(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".fx-check-*")
	if e != nil {
		return e
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, e = f.Write(b); e != nil {
		_ = f.Close() // The failed write is primary; successful close is checked below.
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(f.Name(), path)
}
func summarize(v report) {
	method := v.CatalogMethod
	if method == "" {
		method = "search"
	}
	fmt.Printf("catalog_method=%s catalog_items=%d\n", method, v.CatalogItems)
	if len(v.Missing) > 0 {
		fmt.Printf("missing_currency_candidates=%v\n", v.Missing)
	}
	for _, l := range v.Listings {
		candles := map[time.Time]ti.Candle{}
		currencies := map[string]bool{}
		for _, c := range l.Chunks {
			currencies[c.History.PriceCurrency] = true
			for _, p := range c.History.Candles {
				candles[p.Time] = p
			}
		}
		dates := make([]time.Time, 0, len(candles))
		zeros, incomplete, post2024, postZeros := 0, 0, 0, 0
		boundary := time.Date(2024, 6, 13, 0, 0, 0, 0, time.UTC)
		for at, c := range candles {
			dates = append(dates, at)
			z := c.Volume == "" || c.Volume == "0"
			if z {
				zeros++
			}
			if !c.Complete {
				incomplete++
			}
			if !at.Before(boundary) {
				post2024++
				if z {
					postZeros++
				}
			}
		}
		sort.Slice(dates, func(i, j int) bool { return dates[i].Before(dates[j]) })
		gaps := 0
		for i := 1; i < len(dates); i++ {
			if dates[i].Sub(dates[i-1]) > 7*24*time.Hour {
				gaps++
			}
		}
		first, last := "none", "none"
		if len(dates) > 0 {
			first = dates[0].Format(time.RFC3339)
			last = dates[len(dates)-1].Format(time.RFC3339)
		}
		nominal, _ := l.Metadata.Nominal.Decimal()
		fmt.Printf("%s/%s metadata_valid=%t nominal=%s %s settlement=%s currencies=%v windows=%d candles=%d first=%s last=%s gaps_over_7_days=%d zero_volume=%d incomplete=%d since_2024_06_13=%d zero_volume_since=%d\n", l.Metadata.Ticker, l.Metadata.ClassCode, l.MetadataValidationError == "", nominal, l.Metadata.Nominal.Currency, l.Metadata.Currency, currencies, len(l.Chunks), len(candles), first, last, gaps, zeros, incomplete, post2024, postZeros)
	}
}
func run() error {
	start := flag.String("from", "2019-10-01", "first UTC day")
	end := flag.String("to", time.Now().UTC().Format("2006-01-02"), "last UTC boundary")
	out := flag.String("out", "", "private checkpoint path (required)")
	summary := flag.Bool("summary", false, "read existing report without credentials or network")
	fullCatalog := flag.Bool("full-catalog", false, "include archived currencies using Currencies INSTRUMENT_STATUS_ALL")
	flag.Parse()
	if *out == "" {
		return fmt.Errorf("out path required")
	}
	from, e := time.Parse("2006-01-02", *start)
	if e != nil {
		return fmt.Errorf("invalid from")
	}
	to, e := time.Parse("2006-01-02", *end)
	if e != nil || !to.After(from) || to.After(time.Now().UTC()) || from.Year() < 2000 {
		return fmt.Errorf("invalid to/range")
	}
	v := report{From: from, To: to}
	raw, e := os.ReadFile(*out)
	if e == nil {
		if e = json.Unmarshal(raw, &v); e != nil {
			return fmt.Errorf("invalid checkpoint")
		}
		if !*summary && (!v.From.Equal(from) || !v.To.Equal(to)) {
			return fmt.Errorf("checkpoint range mismatch")
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	if *summary {
		if e != nil {
			return e
		}
		summarize(v)
		return nil
	}
	method := "search"
	if *fullCatalog {
		method = "all"
	}
	if v.CatalogChecked {
		previous := v.CatalogMethod
		if previous == "" {
			previous = "search"
		}
		if previous != method {
			return fmt.Errorf("checkpoint catalog method mismatch; use a new output path")
		}
	}
	u, e := user.Current()
	if e != nil {
		return fmt.Errorf("macOS user unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	c, e := ti.NewFromKeychain(ctx, u.Username)
	if e != nil {
		return e
	}
	c.SetRequestGate(func(ctx context.Context, _ string) error {
		timer := time.NewTimer(650 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		}
	})
	if !v.CatalogChecked {
		v.Listings = nil
		v.Missing = nil
		v.CatalogMethod = method
		var currencies []ti.CurrencyCatalogEntry
		if *fullCatalog {
			currencies, e = c.AllCurrencies(ctx)
			if e != nil {
				return e
			}
			v.CatalogItems = len(currencies)
			v.CurrencyCatalog = currencies
		}
		for _, base := range []string{"USD", "EUR"} {
			candidates := currencies
			if !*fullCatalog {
				found, e := c.FindInstrumentOfKind(ctx, base, "INSTRUMENT_TYPE_CURRENCY")
				if e != nil {
					return e
				}
				if len(found) > 50 {
					return fmt.Errorf("too many currency search results")
				}
				for _, i := range found {
					metadata, e := c.CurrencyByUID(ctx, i.UID)
					if e != nil {
						return e
					}
					candidates = append(candidates, ti.CurrencyCatalogEntry{Metadata: metadata})
				}
			}
			matches := 0
			for _, entry := range candidates {
				metadata := entry.Metadata
				if metadata.ISOCurrency != base || metadata.Currency != "RUB" {
					continue
				}
				matches++
				v.Listings = append(v.Listings, listing{Metadata: metadata, MetadataValidationError: entry.ValidationError})
			}
			if matches == 0 {
				v.Missing = append(v.Missing, base)
			}
		}
		v.CatalogChecked = true
		v.CheckedAt = time.Now().UTC()
		if e = save(*out, v); e != nil {
			return e
		}
	}
	for i := range v.Listings {
		cursor := v.From
		for _, old := range v.Listings[i].Chunks {
			if !old.From.Equal(cursor) || !old.To.After(cursor) || old.To.After(v.To) {
				return fmt.Errorf("noncontiguous checkpoint")
			}
			cursor = old.To
		}
		for cursor.Before(v.To) {
			next := cursor.Add(365 * 24 * time.Hour)
			if next.After(v.To) {
				next = v.To
			}
			history, e := c.ExchangeDailyHistory(ctx, v.Listings[i].Metadata.UID, cursor, next)
			if e != nil {
				return e
			}
			v.Listings[i].Chunks = append(v.Listings[i].Chunks, chunk{cursor, next, history})
			v.CheckedAt = time.Now().UTC()
			if e = save(*out, v); e != nil {
				return e
			}
			cursor = next
		}
	}
	summarize(v)
	return nil
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
