// tinvesthistory checkpoints exact-UID market evidence without financial writes.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"reflect"
	"sort"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/positron48/budget/internal/adapter/tinvest"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

type entry struct {
	InstrumentID string
	From, To     time.Time // requested coverage, half-open
}
type plan struct {
	Version      int
	SourceSHA256 string // identity of reviewed input used to derive this universe
	Entries      []entry
}
type metadata struct {
	InstrumentID string
	RetrievedAt  time.Time
	Response     json.RawMessage
}
type checkpoint struct {
	Request     md.JobRequest
	RetrievedAt time.Time
	History     tinvest.DailyHistory // retains original currency marker, inclusive boundary and incomplete bars
}
type coverage struct {
	State                     string
	UnavailableSHA256         string
	Instrument                md.Instrument
	MetadataSHA256            string
	CheckpointSHA256          []string
	PriceCurrency             []string
	Windows, Bars, Incomplete int
	First, Last               string
	LongestGapDays            int // diagnostic only; not missing-session classification
}
type unavailable struct {
	InstrumentID, Stage string
	HTTPStatus          int
	RetrievedAt         time.Time
}

func recordUnavailable(e entry, u unavailable) (coverage, error) {
	if u.InstrumentID != e.InstrumentID || u.Stage != "metadata" || u.HTTPStatus != 404 || u.RetrievedAt.IsZero() {
		return coverage{}, errors.New("invalid unavailable checkpoint")
	}
	h, err := digest(u)
	return coverage{State: "instrument_unavailable", UnavailableSHA256: h, Instrument: md.Instrument{ID: e.InstrumentID}}, err
}

type coverageIndex struct {
	PlanSHA256  string
	Instruments []coverage
}
type provider interface {
	InstrumentByUID(context.Context, string) (json.RawMessage, error)
	ExchangeDailyHistory(context.Context, string, time.Time, time.Time) (tinvest.DailyHistory, error)
}

func digest(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}
func validatePlan(p plan, now time.Time) error {
	h, err := hex.DecodeString(p.SourceSHA256)
	if err != nil || len(h) != 32 || hex.EncodeToString(h) != p.SourceSHA256 || p.Version != 1 || len(p.Entries) == 0 || len(p.Entries) > 500 {
		return errors.New("invalid history plan")
	}
	previous := ""
	for _, e := range p.Entries {
		u, err := uuid.Parse(e.InstrumentID)
		if err != nil || u.String() != e.InstrumentID || e.InstrumentID <= previous || e.From.Year() < 1900 || !e.To.After(e.From) || e.To.After(now.UTC().Truncate(24*time.Hour)) || e.To.Sub(e.From) > 3660*24*time.Hour || !e.From.Equal(e.From.UTC().Truncate(24*time.Hour)) || !e.To.Equal(e.To.UTC().Truncate(24*time.Hour)) {
			return errors.New("invalid history entry identity/date range")
		}
		previous = e.InstrumentID
	}
	return nil
}
func windows(e entry) []md.JobRequest {
	var out []md.JobRequest
	for from := e.From; from.Before(e.To); {
		to := from.Add(365 * 24 * time.Hour)
		if to.After(e.To) {
			to = e.To
		}
		out = append(out, md.JobRequest{InstrumentID: e.InstrumentID, Kind: "daily_candle", From: from, To: to})
		from = to
	}
	return out
}
func load(path string, v any) error {
	i, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !i.Mode().IsRegular() || i.Mode().Perm()&0077 != 0 || i.Size() > 16<<20 {
		return errors.New("checkpoint must be a bounded private regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	d := json.NewDecoder(io.LimitReader(f, (16<<20)+1))
	d.DisallowUnknownFields()
	if err = d.Decode(v); err != nil {
		return errors.New("invalid checkpoint JSON")
	}
	var trailing any
	if d.Decode(&trailing) != io.EOF {
		return errors.New("trailing checkpoint data")
	}
	return nil
}
func publish(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".history-*")
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
	return os.Link(f.Name(), path) // immutable, never replace previous evidence
}
func validateCheckpoint(c checkpoint, q md.JobRequest) error {
	if c.Request != q || c.RetrievedAt.IsZero() || len(c.History.Candles) > 1000 {
		return errors.New("history checkpoint identity/range mismatch")
	}
	observations, err := tinvest.CandleObservations(q.InstrumentID, c.History)
	if err != nil {
		return err
	}
	seen := map[time.Time]bool{}
	for _, o := range observations {
		if o.InstrumentID != q.InstrumentID || o.Kind != q.Kind || o.SourceKind != "exchange" || o.At.Before(q.From) || o.At.After(q.To) || seen[o.At] {
			return errors.New("invalid history checkpoint observation")
		}
		if _, err := md.Normalize(o); err != nil {
			return err
		}
		seen[o.At] = true
	}
	return nil
}

// collect revalidates resumed evidence and rejects differing overlapping bars.
// Empty responses mean no observations, not a verified calendar or complete NAV.
func collect(ctx context.Context, dir string, p plan, offline bool, factory func() (provider, error), output io.Writer) error {
	planHash, err := digest(p)
	if err != nil {
		return err
	}
	index := coverageIndex{PlanSHA256: planHash}
	var client provider
	getClient := func() (provider, error) {
		if client != nil {
			return client, nil
		}
		var err error
		client, err = factory()
		return client, err
	}
	for _, e := range p.Entries {
		refusalPath := filepath.Join(dir, e.InstrumentID+"-unavailable.json")
		var refusal unavailable
		err := load(refusalPath, &refusal)
		if err == nil {
			item, er := recordUnavailable(e, refusal)
			if er != nil {
				return er
			}
			index.Instruments = append(index.Instruments, item)
			if _, er = fmt.Fprintf(output, "instrument=%s state=instrument_unavailable http_status=404 valuation_approved=false\n", e.InstrumentID); er != nil {
				return er
			}
			continue
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		var m metadata
		path := filepath.Join(dir, e.InstrumentID+"-metadata.json")
		err = load(path, &m)
		if errors.Is(err, os.ErrNotExist) && !offline {
			c, er := getClient()
			if er != nil {
				return er
			}
			raw, er := c.InstrumentByUID(ctx, e.InstrumentID)
			if er != nil {
				var httpErr *tinvest.HTTPError
				if errors.As(er, &httpErr) && httpErr.StatusCode == 404 {
					refusal = unavailable{e.InstrumentID, "metadata", 404, time.Now().UTC()}
					if er = publish(refusalPath, refusal); er != nil {
						return er
					}
					item, er := recordUnavailable(e, refusal)
					if er != nil {
						return er
					}
					index.Instruments = append(index.Instruments, item)
					if _, er = fmt.Fprintf(output, "instrument=%s state=instrument_unavailable http_status=404 valuation_approved=false\n", e.InstrumentID); er != nil {
						return er
					}
					continue
				}
				return er
			}
			m = metadata{e.InstrumentID, time.Now().UTC(), raw}
			if _, er = tinvest.DecodeMarketInstrument(e.InstrumentID, raw); er != nil {
				return er
			}
			if er = publish(path, m); er != nil {
				return er
			}
		} else if err != nil {
			return err
		}
		if m.InstrumentID != e.InstrumentID || m.RetrievedAt.IsZero() {
			return errors.New("metadata checkpoint mismatch")
		}
		instrument, err := tinvest.DecodeMarketInstrument(e.InstrumentID, m.Response)
		if err != nil {
			return err
		}
		metaHash, err := digest(m)
		if err != nil {
			return err
		}
		item := coverage{State: "observed", Instrument: instrument, MetadataSHA256: metaHash}
		currencies := map[string]bool{}
		all := map[time.Time]md.Observation{}
		requests := windows(e)
		for _, q := range requests {
			hash, er := digest(q)
			if er != nil {
				return er
			}
			path = filepath.Join(dir, hash+".json")
			var c checkpoint
			err = load(path, &c)
			if errors.Is(err, os.ErrNotExist) && !offline {
				api, er := getClient()
				if er != nil {
					return er
				}
				observations, er := api.ExchangeDailyHistory(ctx, q.InstrumentID, q.From, q.To)
				if er != nil {
					return er
				}
				c = checkpoint{q, time.Now().UTC(), observations}
				if er = validateCheckpoint(c, q); er != nil {
					return er
				}
				if er = publish(path, c); er != nil {
					return er
				}
			} else if err != nil {
				return err
			}
			if err = validateCheckpoint(c, q); err != nil {
				return err
			}
			checkpointHash, er := digest(c)
			if er != nil {
				return er
			}
			item.CheckpointSHA256 = append(item.CheckpointSHA256, checkpointHash)
			currencies[c.History.PriceCurrency] = true
			observations, er := tinvest.CandleObservations(q.InstrumentID, c.History)
			if er != nil {
				return er
			}
			for _, o := range observations {
				if old, ok := all[o.At]; ok && !reflect.DeepEqual(old, o) {
					return errors.New("conflicting overlap; use a new evidence directory for a correction")
				}
				all[o.At] = o
			}
		}
		var times []time.Time
		incomplete := 0
		for at, o := range all {
			if !at.Before(e.To) {
				continue
			}
			times = append(times, at)
			if !o.Complete {
				incomplete++
			}
		}
		sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
		first, last := "none", "none"
		if len(times) > 0 {
			first = times[0].Format(time.RFC3339)
			last = times[len(times)-1].Format(time.RFC3339)
		}
		for i := 1; i < len(times); i++ {
			gap := int(times[i].Sub(times[i-1]) / (24 * time.Hour))
			if gap > item.LongestGapDays {
				item.LongestGapDays = gap
			}
		}
		for currency := range currencies {
			item.PriceCurrency = append(item.PriceCurrency, currency)
		}
		sort.Strings(item.PriceCurrency)
		item.Windows, item.Bars, item.Incomplete, item.First, item.Last = len(requests), len(times), incomplete, first, last
		index.Instruments = append(index.Instruments, item)
		if _, err := fmt.Fprintf(output, "instrument=%s kind=%s windows=%d bars=%d incomplete=%d first=%s last=%s valuation_approved=false\n", e.InstrumentID, instrument.Kind, len(requests), len(times), incomplete, first, last); err != nil {
			return err
		}
	}
	var old coverageIndex
	path := filepath.Join(dir, "coverage.json")
	err = load(path, &old)
	if errors.Is(err, os.ErrNotExist) && !offline {
		return publish(path, index)
	}
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(old, index) {
		return errors.New("history evidence digest/coverage changed")
	}
	return nil
}
func run(ctx context.Context, args []string) error {
	f := flag.NewFlagSet("tinvesthistory", flag.ContinueOnError)
	input := f.String("plan", "", "private exact-UID history plan")
	out := f.String("out", "", "private immutable checkpoint directory")
	bondOut := f.String("bond-out", "", "optional separate directory for bond terms/coupons/events/historical accrued; base history must be complete")
	offline := f.Bool("offline", false, "validate complete checkpoints without credentials/network")
	account := f.String("keychain-account", "", "macOS account, default current user")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *input == "" || *out == "" || f.NArg() != 0 {
		return errors.New("-plan and -out are required")
	}
	var p plan
	if err := load(*input, &p); err != nil {
		return err
	}
	if err := validatePlan(p, time.Now()); err != nil {
		return err
	}
	if !*offline {
		if err := os.MkdirAll(*out, 0700); err != nil {
			return err
		}
	}
	i, err := os.Lstat(*out)
	if err != nil || !i.IsDir() || i.Mode().Perm()&0077 != 0 {
		return errors.New("audit directory must be private and not a symlink")
	}
	manifest := filepath.Join(*out, "manifest.json")
	var saved plan
	err = load(manifest, &saved)
	if errors.Is(err, os.ErrNotExist) && !*offline {
		if err = publish(manifest, p); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if !reflect.DeepEqual(saved, p) {
		return errors.New("history plan changed; use a new directory")
	}
	factory := func() (provider, error) {
		if *account == "" {
			u, er := user.Current()
			if er != nil {
				return nil, errors.New("cannot determine macOS account")
			}
			*account = u.Username
		}
		c, er := tinvest.NewFromKeychain(ctx, *account)
		if er != nil {
			return nil, er
		}
		next := time.Now()
		c.SetRequestGate(func(ctx context.Context, _ string) error {
			wait := time.Until(next)
			if wait > 0 {
				timer := time.NewTimer(wait)
				defer timer.Stop()
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-timer.C:
				}
			}
			next = time.Now().Add(time.Second)
			return nil
		})
		return c, nil
	}
	if *bondOut != "" {
		if err := collect(ctx, *out, p, true, nil, io.Discard); err != nil {
			return err
		}
		return collectBonds(ctx, *out, *bondOut, p, *offline, func() (bondProvider, error) {
			c, err := factory()
			if err != nil {
				return nil, err
			}
			b, ok := c.(bondProvider)
			if !ok {
				return nil, errors.New("bond provider unavailable")
			}
			return b, nil
		}, os.Stdout)
	}
	return collect(ctx, *out, p, *offline, factory, os.Stdout)
}
func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	ctx, timeout := context.WithTimeout(ctx, 15*time.Minute)
	defer timeout()
	if err := run(ctx, os.Args[1:]); err != nil && !errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
