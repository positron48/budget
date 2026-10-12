// tinvestmarketcheck probes public market coverage, never account positions.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/user"
	"strings"
	"time"

	"github.com/positron48/budget/internal/adapter/tinvest"
)

type probe struct {
	Instrument   tinvest.Instrument  `json:"instrument"`
	Prices       []tinvest.LastPrice `json:"prices,omitempty"`
	Candles      []tinvest.Candle    `json:"candles,omitempty"`
	PriceError   string              `json:"price_error,omitempty"`
	HistoryError string              `json:"history_error,omitempty"`
}

func run() error {
	query := flag.String("query", "SBER", "exact ticker to probe; all matching listings remain separate")
	start := flag.String("from", "2019-10-01", "first UTC day in seven-day historical probe")
	flag.Parse()
	from, err := time.Parse("2006-01-02", *start)
	if err != nil {
		return fmt.Errorf("invalid date")
	}
	u, err := user.Current()
	if err != nil {
		return fmt.Errorf("cannot determine macOS account")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client, err := tinvest.NewFromKeychain(ctx, u.Username)
	if err != nil {
		return err
	}
	instruments, err := client.FindInstrument(ctx, *query)
	if err != nil {
		return err
	}
	report := struct {
		Query     string    `json:"query"`
		CheckedAt time.Time `json:"checked_at"`
		From      time.Time `json:"from"`
		To        time.Time `json:"to"`
		Listings  []probe   `json:"listings"`
	}{Query: *query, CheckedAt: time.Now().UTC(), From: from, To: from.AddDate(0, 0, 7), Listings: []probe{}}
	for _, instrument := range instruments {
		if !strings.EqualFold(instrument.Ticker, *query) {
			continue
		}
		if len(report.Listings) >= 10 {
			return fmt.Errorf("too many matching listings; refine query")
		}
		p := probe{Instrument: instrument}
		p.Prices, err = client.ExchangeLastPrices(ctx, []string{instrument.UID})
		if err != nil {
			p.PriceError = err.Error()
		}
		p.Candles, err = client.ExchangeDailyCandles(ctx, instrument.UID, report.From, report.To)
		if err != nil {
			p.HistoryError = err.Error()
		}
		report.Listings = append(report.Listings, p)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
