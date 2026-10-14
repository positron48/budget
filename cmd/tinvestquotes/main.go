// tinvestquotes exports current market evidence for explicit listing identifiers.
// It reads the token inside the client; no account/position API is called.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/user"
	"time"

	"github.com/positron48/budget/internal/adapter/tinvest"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

type quoteExportItem struct {
	Request     tinvest.QuoteIdentifier `json:"request"`
	Candidates  []tinvest.Instrument    `json:"candidates,omitempty"`
	Instrument  *md.Instrument          `json:"instrument,omitempty"`
	Price       *tinvest.LastPrice      `json:"price,omitempty"`
	Observation *md.Observation         `json:"observation,omitempty"`
	BondFacts   *md.BondFacts           `json:"bond_facts,omitempty"`
	Issue       string                  `json:"issue,omitempty"`
}
type quoteExport struct {
	Version   int               `json:"version"`
	FetchedAt time.Time         `json:"fetched_at"`
	Items     []quoteExportItem `json:"items"`
}

func run() error {
	input := flag.String("identifiers", "", "JSON public listing identifier manifest")
	output := flag.String("out", "", "new private output JSON (never overwrites)")
	flag.Parse()
	if *input == "" || *output == "" || flag.NArg() != 0 {
		return errors.New("identifiers and out are required")
	}
	f, e := os.Open(*input)
	if e != nil {
		return errors.New("cannot read identifiers")
	}
	defer func() { _ = f.Close() }()
	var identifiers []tinvest.QuoteIdentifier
	decoder := json.NewDecoder(io.LimitReader(f, 1<<20))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&identifiers); e != nil || len(identifiers) == 0 || len(identifiers) > 500 {
		return errors.New("invalid identifier manifest")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return errors.New("unexpected trailing identifier data")
	}
	seen := map[string]bool{}
	for _, q := range identifiers {
		if e = tinvest.ValidateQuoteIdentifier(q); e != nil || seen[q.Key] {
			return errors.New("invalid or duplicate quote identifier")
		}
		seen[q.Key] = true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	u, e := user.Current()
	if e != nil {
		return errors.New("local credential account unavailable")
	}
	client, e := tinvest.NewFromKeychain(ctx, u.Username)
	if e != nil {
		return e
	}
	// Bound traffic without printing credentials or writing them into the export.
	var next time.Time
	client.SetRequestGate(func(ctx context.Context, _ string) error {
		delay := time.Until(next)
		if delay > 0 {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
			}
		}
		next = time.Now().Add(700 * time.Millisecond)
		return nil
	})
	result := quoteExport{Version: 1, FetchedAt: time.Now().UTC().Truncate(time.Microsecond)}
	for i, q := range identifiers {
		item := quoteExportItem{Request: q}
		query := q.Ticker
		if q.ISIN != "" {
			query = q.ISIN
		}
		item.Candidates, e = client.FindInstrument(ctx, query)
		if e != nil {
			item.Issue = "catalog_unavailable"
			result.Items = append(result.Items, item)
			continue
		}
		selected, e := tinvest.SelectQuoteInstrument(q, item.Candidates)
		if e != nil {
			item.Issue = "identity_unresolved"
			result.Items = append(result.Items, item)
			continue
		}
		metadata, e := client.LoadMarketInstrument(ctx, selected.UID)
		if e != nil || metadata.ID != selected.UID || metadata.ISIN != selected.ISIN || metadata.Ticker != selected.Ticker || metadata.ClassCode != selected.ClassCode {
			item.Issue = "metadata_unavailable_or_conflicting"
			result.Items = append(result.Items, item)
			continue
		}
		item.Instrument = &metadata
		prices, e := client.ExchangeLastPrices(ctx, []string{selected.UID})
		if e != nil || len(prices) != 1 {
			item.Issue = "exchange_quote_unavailable"
			result.Items = append(result.Items, item)
			continue
		}
		item.Price = &prices[0]
		observation, e := tinvest.PriceObservation(prices[0])
		if e != nil {
			item.Issue = "quote_invalid"
		} else {
			item.Observation = &observation
		}
		if metadata.Kind == "bond" {
			from := prices[0].Time.UTC().Truncate(24 * time.Hour)
			today := time.Now().UTC().Truncate(24 * time.Hour)
			if from.Before(today.Add(-7 * 24 * time.Hour)) {
				item.Issue = "bond_quote_stale"
			} else {
				facts, err := client.LoadBondFacts(ctx, md.JobRequest{Kind: "bond_facts", InstrumentID: selected.UID, From: from, To: today.Add(24*time.Hour - time.Second)})
				if err != nil {
					item.Issue = "bond_terms_unavailable"
				} else {
					item.BondFacts = &facts
				}
			}
		}
		result.Items = append(result.Items, item)
		if (i+1)%10 == 0 {
			fmt.Fprintf(os.Stderr, "Market identifiers checked: %d/%d\n", i+1, len(identifiers))
		}
	}
	file, e := os.OpenFile(*output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return errors.New("cannot create private quote export")
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	e = encoder.Encode(result)
	closed := file.Close()
	if e != nil || closed != nil {
		return errors.New("quote export write failed")
	}
	issues := map[string]int{}
	quoted := 0
	for _, item := range result.Items {
		if item.Issue != "" {
			issues[item.Issue]++
		}
		if item.Observation != nil {
			quoted++
		}
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"instruments": len(result.Items), "quotes": quoted, "issues": issues})
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
