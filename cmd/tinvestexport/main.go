// tinvestexport reads brokerage history and snapshots without exposing credentials.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/positron48/budget/internal/adapter/tinvest"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
	"os"
	"os/user"
	"path/filepath"
	"time"
)

type Snapshot struct {
	HistoryEvidence HistoryEvidence   `json:"history_evidence"`
	FetchedAt       time.Time         `json:"fetched_at"`
	From            string            `json:"from"`
	To              string            `json:"to"`
	Account         tinvest.Account   `json:"account"`
	Operations      []json.RawMessage `json:"operations"`
	Portfolio       json.RawMessage   `json:"portfolio"`
	Positions       json.RawMessage   `json:"positions"`
}

func save(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	_, e = f.Write(b)
	ce := f.Close()
	if e != nil {
		return e
	}
	return ce
}
func run() error {
	out := flag.String("out", "", "new private output directory")
	verify := flag.String("verify", "", "verify an existing account snapshot without network or credentials")
	tradeOutput := flag.String("trade-review", "", "private candidate JSON, requires -verify, -after and -ledger")
	afterText := flag.String("after", "", "exclusive RFC3339 cutoff for local trade review")
	ledger := flag.String("ledger", "", "ledger UUID for local candidates; not written to a database")
	flag.Parse()
	if *verify == "" && (*tradeOutput != "" || *afterText != "" || *ledger != "") {
		return fmt.Errorf("trade review requires -verify")
	}
	if *tradeOutput == "" && (*afterText != "" || *ledger != "") {
		return fmt.Errorf("-after and -ledger require -trade-review")
	}

	if *verify != "" {
		if *out != "" {
			return fmt.Errorf("choose export or verification")
		}
		raw, err := os.ReadFile(*verify)
		if err != nil {
			return err
		}
		var snapshot Snapshot
		if err = json.Unmarshal(raw, &snapshot); err != nil {
			return err
		}
		if _, err = imp.PrepareBrokerSnapshot(raw); err != nil {
			return err
		}
		if err = verifyHistory(snapshot); err != nil {
			return err
		}
		fmt.Printf("Verified pagination: pages=%d operations=%d; provider completeness not independently verified\n", len(snapshot.HistoryEvidence.Pages), len(snapshot.Operations))
		if *tradeOutput != "" {
			after, err := time.Parse(time.RFC3339, *afterText)
			if err != nil {
				return fmt.Errorf("invalid -after timestamp")
			}
			prepared, err := imp.PrepareBrokerSnapshot(raw)
			if err != nil {
				return err
			}
			review, err := reviewTrades(prepared, after, *ledger)
			if err != nil {
				return err
			}
			if err = save(*tradeOutput, review); err != nil {
				return err
			}
			candidates, components := 0, 0
			for _, row := range review.Rows {
				if row.State == "candidate" {
					candidates++
				}
				if row.State == "included_component" {
					components++
				}
			}
			fmt.Printf("Trade review: %d candidates, %d included components, %d unresolved; no operations posted\n", candidates, components, len(review.Rows)-candidates-components)
		}
		return nil
	}
	if *out == "" {
		return fmt.Errorf("-out required")
	}
	if err := os.Mkdir(*out, 0700); err != nil {
		return err
	}
	u, err := user.Current()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	c, err := tinvest.NewFromKeychain(ctx, u.Username)
	if err != nil {
		return err
	}
	accounts, err := c.Accounts(ctx)
	if err != nil {
		return err
	}
	if err = save(filepath.Join(*out, "accounts.json"), accounts); err != nil {
		return err
	}
	fmt.Printf("Accounts retrieved: %d\n", len(accounts))
	for i, a := range accounts {
		s := Snapshot{FetchedAt: time.Now().UTC(), From: a.OpenedDate, To: time.Now().UTC().Format(time.RFC3339), Account: a, Operations: []json.RawMessage{}}
		if s.From == "" {
			s.From = "2000-01-01T00:00:00Z"
		}
		cursor := ""
		seen := map[string]bool{}
		for page := 0; ; page++ {
			if page >= 10000 {
				return fmt.Errorf("pagination limit exceeded")
			}
			r, e := c.Operations(ctx, a.ID, s.From, s.To, cursor)
			if e != nil {
				return e
			}
			if e = s.HistoryEvidence.append(cursor, r); e != nil {
				return e
			}
			s.Operations = append(s.Operations, r.Items...)
			if !r.HasNext {
				break
			}
			if r.NextCursor == "" || seen[r.NextCursor] {
				return fmt.Errorf("invalid repeated pagination cursor")
			}
			seen[r.NextCursor] = true
			cursor = r.NextCursor
			time.Sleep(350 * time.Millisecond)
		}
		s.Portfolio, err = c.Portfolio(ctx, a.ID)
		if err != nil {
			return err
		}
		s.Positions, err = c.Positions(ctx, a.ID)
		if err != nil {
			return err
		}
		if err = save(filepath.Join(*out, fmt.Sprintf("account-%d.json", i+1)), s); err != nil {
			return err
		}
		fmt.Printf("Account %d: type=%s status=%s operations=%d; snapshots saved\n", i+1, a.Type, a.Status, len(s.Operations))
	}
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
