// investmentcoverage builds a private, resumable coverage audit. It does not
// confirm instrument mappings or write prices into the financial journal.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/positron48/budget/internal/adapter/tinvest"
	"github.com/positron48/budget/internal/usecase/investmentimport"
)

type sourceInstrument struct {
	CashOnly bool   `json:"cash_only"`
	Symbol   string `json:"symbol"`
	Exchange string `json:"exchange"`
	FirstDay string `json:"first_day"`
	LastDay  string `json:"last_day"`
}
type candidate struct {
	Instrument   tinvest.Instrument  `json:"instrument"`
	CheckedAt    time.Time           `json:"checked_at"`
	Prices       []tinvest.LastPrice `json:"prices"`
	From         time.Time           `json:"from"`
	To           time.Time           `json:"to"`
	Candles      []tinvest.Candle    `json:"candles"`
	PriceError   string              `json:"price_error,omitempty"`
	HistoryError string              `json:"history_error,omitempty"`
}
type entry struct {
	Source        sourceInstrument `json:"source"`
	CheckedAt     time.Time        `json:"checked_at"`
	SearchKinds   []string         `json:"search_kinds,omitempty"`
	SearchResults int              `json:"search_results"`
	Candidates    []candidate      `json:"candidates"`
	SearchError   string           `json:"search_error,omitempty"`
	Complete      bool             `json:"probe_complete"`
}
type report struct {
	Version    int     `json:"version"`
	SourceHash string  `json:"source_sha256"`
	Scope      string  `json:"scope"`
	Entries    []entry `json:"entries"`
}

func sourceInstruments(stage investmentimport.StagedSource) ([]sourceInstrument, error) {
	index := map[string]sourceInstrument{}
	for _, row := range stage.Rows {
		symbol, exchange := strings.TrimSpace(row.Raw["Symbol"]), strings.TrimSpace(row.Raw["Exchange"])
		if symbol == "" {
			continue
		}
		date, err := time.Parse("2006-01-02 15:04:05", row.Raw["Date"])
		if err != nil {
			return nil, errors.New("invalid source date")
		}
		day := date.Format("2006-01-02")
		key := symbol + "\x00" + exchange
		cash := exchange == "" && symbol == row.Raw["Currency"] && (symbol == "RUB" || symbol == "USD" || symbol == "EUR") && (row.Raw["Event"] == "CASH_IN" || row.Raw["Event"] == "CASH_OUT" || row.Raw["Event"] == "CASH_GAIN")
		current, exists := index[key]
		if !exists {
			current = sourceInstrument{Symbol: symbol, Exchange: exchange, FirstDay: day, LastDay: day, CashOnly: cash}
		}
		current.CashOnly = current.CashOnly && cash
		if day < current.FirstDay {
			current.FirstDay = day
		}
		if day > current.LastDay {
			current.LastDay = day
		}
		index[key] = current
	}
	result := make([]sourceInstrument, 0, len(index))
	for _, item := range index {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Symbol == result[j].Symbol {
			return result[i].Exchange < result[j].Exchange
		}
		return result[i].Symbol < result[j].Symbol
	})
	return result, nil
}
func exactCandidate(symbol string, instrument tinvest.Instrument) bool {
	return strings.EqualFold(symbol, instrument.Ticker) || strings.EqualFold(symbol, instrument.ISIN) || strings.EqualFold(symbol, instrument.FIGI) || strings.EqualFold(symbol, instrument.UID)
}

// Temp-file + rename makes each checkpoint atomic; financial source metadata
// stays private even when the destination already existed with broader access.
func save(path string, value report) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".coverage-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err = file.Write(data); err != nil {
		_ = file.Close() // Preserve the write error while releasing its file.
		return err
	}
	if err = file.Sync(); err != nil {
		_ = file.Close() // Preserve the flush error while releasing its file.
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
func wait(ctx context.Context) error {
	timer := time.NewTimer(600 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func run() error {
	source := flag.String("source", "", "Snowball CSV source")
	output := flag.String("out", "", "private JSON checkpoint, resumed if present")
	summaryOnly := flag.Bool("summary", false, "summarize an existing checkpoint without network or credentials")
	flag.Parse()
	if *summaryOnly {
		data, err := os.ReadFile(*output)
		if err != nil {
			return errors.New("cannot read checkpoint")
		}
		var r report
		if err = json.Unmarshal(data, &r); err != nil || r.Version != 1 {
			return errors.New("invalid checkpoint")
		}
		return json.NewEncoder(os.Stdout).Encode(summarize(r))
	}
	if *source == "" || *output == "" {
		return errors.New("provide -source CSV and -out JSON")
	}
	input, err := os.Open(*source)
	if err != nil {
		return errors.New("cannot open source CSV")
	}
	defer func() { _ = input.Close() }()
	stage, err := investmentimport.StageSnowball(input, true)
	if err != nil {
		return err
	}
	sources, err := sourceInstruments(stage)
	if err != nil {
		return err
	}
	result := report{Version: 1, SourceHash: stage.Audit.SHA256, Scope: "candidate_probes_not_confirmed_mappings_or_continuous_history"}
	if data, err := os.ReadFile(*output); err == nil {
		if err = json.Unmarshal(data, &result); err != nil || result.Version != 1 || result.SourceHash != stage.Audit.SHA256 {
			return errors.New("checkpoint version or source mismatch")
		}
	} else if !os.IsNotExist(err) {
		return errors.New("cannot read checkpoint")
	}
	existing := map[string]entry{}
	for _, e := range result.Entries {
		existing[e.Source.Symbol+"\x00"+e.Source.Exchange] = e
	}
	result.Entries = nil
	for _, s := range sources {
		e, ok := existing[s.Symbol+"\x00"+s.Exchange]
		if !ok {
			e = entry{Source: s}
		}
		e.Source = s
		if s.CashOnly {
			e.Complete = true
			e.Candidates = nil
			e.SearchError = ""
			e.SearchKinds = nil
			e.SearchResults = 0
		}
		result.Entries = append(result.Entries, e)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	u, err := user.Current()
	if err != nil {
		return errors.New("cannot determine macOS account")
	}
	client, err := tinvest.NewFromKeychain(ctx, u.Username)
	if err != nil {
		return err
	}
	for i := range result.Entries {
		e := &result.Entries[i]
		if e.Complete {
			continue
		}
		if err = wait(ctx); err != nil {
			return err
		}

		var instruments []tinvest.Instrument
		var searchErr error
		e.SearchKinds = nil
		if len(e.Source.Symbol) == 1 {
			e.SearchKinds = []string{"INSTRUMENT_TYPE_SHARE", "INSTRUMENT_TYPE_BOND", "INSTRUMENT_TYPE_ETF", "INSTRUMENT_TYPE_CURRENCY"}
			for _, kind := range e.SearchKinds {
				if err = wait(ctx); err != nil {
					return err
				}
				var found []tinvest.Instrument
				searchErr = retryRead(ctx, func() error {
					var err error
					found, err = client.FindInstrumentOfKind(ctx, e.Source.Symbol, kind)
					return err
				})
				if searchErr != nil {
					break
				}
				instruments = append(instruments, found...)
			}
		} else {
			searchErr = retryRead(ctx, func() error {
				var err error
				instruments, err = client.FindInstrument(ctx, e.Source.Symbol)
				return err
			})
		}

		e.CheckedAt = time.Now().UTC()
		e.Candidates = []candidate{}
		e.SearchError = ""
		e.SearchResults = len(instruments)
		if searchErr != nil {
			e.SearchError = searchErr.Error()
		} else {
			seen := map[string]bool{}
			for _, instrument := range instruments {
				if !exactCandidate(e.Source.Symbol, instrument) || seen[instrument.UID] {
					continue
				}
				seen[instrument.UID] = true
				if err = wait(ctx); err != nil {
					return err
				}
				from, _ := time.Parse("2006-01-02", e.Source.FirstDay)
				p := candidate{Instrument: instrument, CheckedAt: time.Now().UTC(), From: from, To: from.AddDate(0, 0, 7)}
				err = retryRead(ctx, func() error {
					var err error
					p.Prices, err = client.ExchangeLastPrices(ctx, []string{instrument.UID})
					return err
				})
				if err != nil {
					p.PriceError = err.Error()
				}
				if err = wait(ctx); err != nil {
					return err
				}
				err = retryRead(ctx, func() error {
					var err error
					p.Candles, err = client.ExchangeDailyCandles(ctx, instrument.UID, p.From, p.To)
					return err
				})
				if err != nil {
					p.HistoryError = err.Error()
				}
				e.Candidates = append(e.Candidates, p)
			}
			e.Complete = true
			for _, p := range e.Candidates {
				if p.PriceError != "" || p.HistoryError != "" {
					e.Complete = false
				}
			}
		}
		if err = save(*output, result); err != nil {
			return errors.New("cannot save private checkpoint")
		}
		fmt.Printf("Probed %d/%d; candidates=%d; complete=%t\n", i+1, len(result.Entries), len(e.Candidates), e.Complete)
	}
	if err = save(*output, result); err != nil {
		return errors.New("cannot save private checkpoint")
	}
	incomplete := 0
	for _, e := range result.Entries {
		if !e.Complete {
			incomplete++
		}
	}
	fmt.Printf("Coverage checkpoint saved; source instruments=%d; incomplete probes=%d\n", len(result.Entries), incomplete)
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
