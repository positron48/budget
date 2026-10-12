// twelvefxcheck audits candidate forex history. It never writes financial data.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	td "github.com/positron48/budget/internal/adapter/twelvedata"
)

var pairs = []string{"USD/RUB", "EUR/RUB", "EUR/USD"}

type manifest struct{ Source, From, To string }

// publish makes complete checkpoints visible without replacing existing evidence.
func publish(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".twelve-fx-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err = f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Link(f.Name(), path)
}
func load(path string, value any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() > 8<<20 {
		return errors.New("oversized checkpoint")
	}
	decoder := json.NewDecoder(f)
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("trailing checkpoint data")
	}
	return nil
}
func prepare(dir string, m manifest) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	path := filepath.Join(dir, "manifest.json")
	var old manifest
	err := load(path, &old)
	if errors.Is(err, os.ErrNotExist) {
		return publish(path, m)
	}
	if err != nil {
		return err
	}
	if old != m {
		return errors.New("checkpoint range/source mismatch; use a new directory")
	}
	return nil
}
func run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("twelvefxcheck", flag.ContinueOnError)
	out := flags.String("out", "", "private audit directory (required; existing checkpoints are resumed)")
	from := flags.String("from", "2019-10-01", "first UTC bar date")
	to := flags.String("to", time.Now().UTC().AddDate(0, 0, -1).Format(time.DateOnly), "last closed UTC bar date")
	summary := flags.Bool("summary", false, "validate saved evidence offline; never read Keychain")
	account := flags.String("keychain-account", "", "macOS account (default current user)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *out == "" || flags.NArg() != 0 {
		return errors.New("-out is required; positional arguments are unsupported")
	}
	expected := manifest{Source: "twelvedata", From: *from, To: *to}
	// Validate before creating evidence or accessing credentials.
	a, e1 := time.Parse(time.DateOnly, *from)
	b, e2 := time.Parse(time.DateOnly, *to)
	if e1 != nil || e2 != nil || a.After(b) || !b.Before(time.Now().UTC().Truncate(24*time.Hour)) || b.Sub(a) > 3660*24*time.Hour {
		return errors.New("invalid closed UTC date range")
	}
	if *summary {
		var saved manifest
		if err := load(filepath.Join(*out, "manifest.json"), &saved); err != nil {
			return err
		}
		if saved != expected {
			return errors.New("checkpoint range/source mismatch")
		}
	} else if err := prepare(*out, expected); err != nil {
		return err
	}
	var client *td.Client
	requested := false
	for _, pair := range pairs {
		path := filepath.Join(*out, strings.ReplaceAll(pair, "/", "-")+".json")
		var audit td.Audit
		err := load(path, &audit)
		if errors.Is(err, os.ErrNotExist) {
			if *summary {
				fmt.Printf("pair=%s checkpoint=missing financially_approved=false\n", pair)
				continue
			}
			if client == nil {
				if *account == "" {
					u, e := user.Current()
					if e != nil {
						return errors.New("cannot determine Keychain account")
					}
					*account = u.Username
				}
				client, err = td.NewFromKeychain(ctx, *account)
				if err != nil {
					return err
				}
			}
			if requested {
				timer := time.NewTimer(8 * time.Second)
				select {
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				case <-timer.C:
				}
			}
			requested = true
			audit, err = client.History(ctx, pair, *from, *to)
			if err != nil {
				return err
			}
			if err = publish(path, audit); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if audit.Meta.Symbol != pair || audit.From != *from || audit.To != *to {
			return errors.New("checkpoint identity/range mismatch")
		}
		coverage, err := audit.Validate()
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(coverage)
		if err != nil {
			return err
		}
		fmt.Printf("pair=%s coverage=%s\n", pair, encoded)
	}
	return nil
}
func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	ctx, timeout := context.WithTimeout(ctx, 5*time.Minute)
	defer timeout()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
