// investcbr imports official CBR FX under an explicitly registered tenant policy.
// It never reads a broker credential or publishes into a market-mode policy.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/google/uuid"
	"github.com/positron48/budget/internal/adapter/cbr"
	"github.com/positron48/budget/internal/adapter/postgres"
	"github.com/positron48/budget/internal/adapter/workermetrics"
	"github.com/positron48/budget/internal/usecase/fximport"
	"github.com/positron48/budget/internal/usecase/officialfx"
	"github.com/positron48/budget/internal/usecase/workerhealth"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func run() error {
	tenant := flag.String("tenant", "", "tenant UUID")
	key := flag.String("policy", "cbr", "registered official policy key")
	once := flag.Bool("once", false, "import full registered history once; default retries hourly and updates daily")
	dry := flag.Bool("dry-run", false, "fetch/validate public data without connecting to a database")
	output := flag.String("output", "", "optional public FX JSON output for dry-run only")
	fromArg := flag.String("from", "2019-10-01", "dry-run first date")
	toArg := flag.String("to", fximport.MoscowDate(time.Now()).Format(time.DateOnly), "dry-run last date")
	flag.Parse()
	if *output != "" && !*dry {
		return errors.New("output requires dry-run")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	source := cbr.New()
	if *dry {
		from, e := time.Parse(time.DateOnly, *fromArg)
		if e != nil {
			return errors.New("invalid from date")
		}
		to, e := time.Parse(time.DateOnly, *toArg)
		if e != nil || to.After(fximport.MoscowDate(time.Now())) {
			return errors.New("invalid or future to date")
		}
		attempt, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
		points, e := source.FetchOfficialFX(attempt, from, to)
		if e != nil {
			return e
		}
		if *output != "" {
			raw, err := json.MarshalIndent(points, "", "  ")
			if err != nil {
				return err
			}
			if err = os.WriteFile(*output, append(raw, '\n'), 0600); err != nil {
				return errors.New("cannot save public FX output")
			}
		}
		fmt.Printf("Official CBR FX validated: from=%s through=%s daily_points=%d; no writes\n", *fromArg, *toArg, len(points))
		return nil
	}
	if id, e := uuid.Parse(*tenant); e != nil || id.String() != *tenant {
		return errors.New("valid tenant UUID required")
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL required")
	}
	db, e := postgres.NewPool(ctx, dsn)
	if e != nil {
		return errors.New("database configuration failed")
	}
	defer db.Close()
	if e = db.Ping(ctx); e != nil {
		return errors.New("database unavailable")
	}
	svc := officialfx.Service{Source: source, Store: postgres.NewMarketDataRepo(db), Tenant: *tenant, Key: *key}
	monitor, e := workermetrics.New(ctx, "investcbr", func(ctx context.Context) (workerhealth.Snapshot, error) {
		return workerhealth.Snapshot{}, db.Ping(ctx)
	})
	if e != nil {
		return e
	}
	closeMetrics, e := monitor.Start(os.Getenv("INVESTMENT_METRICS_ADDR"))
	if e != nil {
		return e
	}
	defer closeMetrics()
	report := func(n int, e error) {
		if ctx.Err() != nil {
			return
		}
		monitor.Observe(e)
		if e != nil {
			fmt.Fprintln(os.Stderr, "Official CBR FX import failed; saved history retained; retry in one hour")
		} else {
			fmt.Printf("Official CBR FX import complete: daily_points=%d\n", n)
		}
	}
	if *once {
		attempt, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
		n, e := svc.Import(attempt, time.Now(), false)
		if e != nil {
			monitor.Observe(e)
			return errors.New("official FX import failed; check policy version, source and database")
		}
		report(n, nil)
		return nil
	}
	svc.Run(ctx, report)
	return nil
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
