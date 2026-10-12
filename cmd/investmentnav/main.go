// investmentnav processes previously scheduled daily projections. It never
// obtains provider credentials or posts financial journal operations.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/google/uuid"
	"github.com/positron48/budget/internal/adapter/postgres"
	"github.com/positron48/budget/internal/adapter/workermetrics"
	"github.com/positron48/budget/internal/usecase/workerhealth"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func run() error {
	tenant := flag.String("tenant", "", "tenant UUID")
	once := flag.Bool("once", false, "process at most one available day")
	flag.Parse()
	if id, e := uuid.Parse(*tenant); e != nil || id.String() != *tenant {
		return errors.New("valid tenant is required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return errors.New("DATABASE_URL is required")
	}
	db, e := postgres.NewPool(ctx, url)
	if e != nil {
		return errors.New("NAV database configuration failed")
	}
	defer db.Close()
	if e = db.Ping(ctx); e != nil {
		return errors.New("NAV database unavailable")
	}
	repo := postgres.NewMarketDataRepo(db)
	monitor, e := workermetrics.New(ctx, "investmentnav", func(ctx context.Context) (workerhealth.Snapshot, error) {
		return repo.ReadNAVWorkerHealth(ctx, *tenant)
	})
	if e != nil {
		return e
	}
	closeMetrics, e := monitor.Start(os.Getenv("INVESTMENT_METRICS_ADDR"))
	if e != nil {
		return e
	}
	defer closeMetrics()
	for {
		_, err := repo.ScheduleDailyNAV(ctx, *tenant)
		worked := false
		if err == nil {
			worked, err = repo.RunDailyNAVOnce(ctx, *tenant)
		}
		if ctx.Err() != nil {
			return nil
		}
		monitor.Observe(err)
		if worked {
			outcome := "ready"
			if err != nil {
				outcome = "error"
			}
			monitor.ObserveJob("daily_nav", outcome)
		}
		if err != nil {
			// Do not expose SQL, configuration or private financial payloads in logs.
			if *once {
				return errors.New("NAV job failed; inspect persisted job status")
			}
			fmt.Fprintln(os.Stderr, "NAV processing failed; retry state is persisted")
		}
		if *once {
			return nil
		}
		if !worked || err != nil {
			timer := time.NewTimer(5 * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil
			case <-timer.C:
			}
		}
	}
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
