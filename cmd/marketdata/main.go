// marketdata runs a tenant-scoped read-only provider worker. Migrations must
// already be applied by budgetd; no token is accepted on the command line.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/positron48/budget/internal/adapter/postgres"
	"github.com/positron48/budget/internal/adapter/workermetrics"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"github.com/positron48/budget/internal/usecase/workerhealth"
)

func run() error {
	provider := flag.String("provider", "tinvest", "tinvest, moex or twelvedata")
	tenant := flag.String("tenant", "", "tenant UUID")
	access := flag.String("access-scope", "", "entitlement scope, never a token")
	register := flag.String("register-source", "", "operator provisioning: source title to enable for this tenant")
	once := flag.Bool("once", false, "process at most one available job")
	history := flag.Bool("enqueue-history", false, "atomically enqueue a long UTC daily-candle range; reuse existing jobs")
	instrument := flag.String("enqueue-instrument", "", "optional provider UID to enqueue before processing")
	kind := flag.String("kind", "last_price", "last_price, daily_candle, bond_facts, dividend_facts or catalog_search")
	searchQuery := flag.String("search-query", "", "provider catalog query")
	searchKind := flag.String("instrument-kind", "share", "catalog asset kind: share, bond, etf or currency")
	fromArg := flag.String("from", "", "first UTC date for daily_candle, bond_facts or dividend_facts")
	toArg := flag.String("to", "", "last UTC date for daily_candle, bond_facts or dividend_facts (inclusive for bond_facts/dividend_facts)")
	flag.Parse()
	if *history && (*instrument == "" || *kind != "daily_candle" || *searchQuery != "") {
		return errors.New("enqueue-history requires an instrument and daily_candle kind")
	}
	scope := md.Scope{TenantID: *tenant, Provider: *provider, AccessScope: *access}
	if err := md.ValidateScope(scope); err != nil {
		return errors.New("valid tenant and access-scope are required")
	}
	if scope.Provider != "tinvest" && (*searchQuery != "" || *instrument != "" && *kind != "daily_candle") {
		return errors.New("native history workers accept only mapped daily candles")
	}
	var request md.JobRequest
	if *instrument != "" || *searchQuery != "" {
		request = md.JobRequest{InstrumentID: *instrument, Kind: *kind}
		if *searchQuery != "" {
			request.SearchQuery = *searchQuery
			request.InstrumentKind = *searchKind
		}
		if *kind == "daily_candle" || *kind == "bond_facts" || *kind == "dividend_facts" {
			var err error
			request.From, err = time.Parse(time.DateOnly, *fromArg)
			if err != nil {
				return errors.New("invalid from date")
			}
			request.To, err = time.Parse(time.DateOnly, *toArg)
			if err != nil {
				return errors.New("invalid to date")
			}
		}
		if *kind != "daily_candle" && *kind != "bond_facts" && *kind != "dividend_facts" && (*fromArg != "" || *toArg != "") {
			return errors.New("this job kind does not take a date range")
		}
		if *kind == "bond_facts" || *kind == "dividend_facts" {
			request.To = request.To.Add(24*time.Hour - time.Second)
		}
		var validationErr error
		if *history {
			_, validationErr = md.PlanHistory(request.InstrumentID, request.From, request.To)
		} else {
			validationErr = md.ValidateRequest(request)
		}
		if validationErr != nil {
			return errors.New("invalid market job request")
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return errors.New("DATABASE_URL is required")
	}
	db, err := postgres.NewPool(ctx, url)
	if err != nil {
		return errors.New("market database configuration failed")
	}
	defer db.Close()
	if err = db.Ping(ctx); err != nil {
		return errors.New("market database unavailable")
	}
	repo := postgres.NewMarketDataRepo(db)
	client, err := workerProvider(ctx, repo, scope)
	if err != nil {
		return err
	}
	if *register != "" {
		if err = repo.ConfigureSource(ctx, scope, *register, true); err != nil {
			return errors.New("cannot provision market source")
		}
	}
	if *instrument != "" || *searchQuery != "" {
		if *history {
			var jobs []md.Job
			jobs, err = repo.EnqueueHistory(ctx, scope, request.InstrumentID, request.From, request.To)
			if err == nil {
				counts := map[string]int{}
				for _, j := range jobs {
					counts[j.Status]++
				}
				fmt.Printf("History jobs: %d pending, %d running, %d ready, %d error; coverage not inferred\n", counts["pending"], counts["running"], counts["ready"], counts["error"])
			}
		} else {
			_, err = repo.Enqueue(ctx, scope, request)
		}
		if err != nil {
			return errors.New("cannot enqueue market job; check tenant and migrations")
		}
		if !*history {
			fmt.Println("Market job queued")
		}
	}
	worker := md.Worker{Queue: repo, Provider: client, Scope: scope, Lease: 2 * time.Minute}
	workerName := "marketdata"
	switch scope.Provider {
	case "moex":
		workerName = "moexhistory"
	case "twelvedata":
		workerName = "equityhistory"
	}
	monitor, err := workermetrics.New(ctx, workerName, func(ctx context.Context) (workerhealth.Snapshot, error) {
		return repo.ReadMarketWorkerHealth(ctx, scope)
	})
	if err != nil {
		return err
	}
	closeMetrics, err := monitor.Start(os.Getenv("INVESTMENT_METRICS_ADDR"))
	if err != nil {
		return err
	}
	defer closeMetrics()
	worker.Observe = monitor.ObserveJob
	var nextSchedule time.Time
	for {
		if now := time.Now().UTC(); !now.Before(nextSchedule) {
			if _, err = repo.ScheduleMarket(ctx, scope, now, 50); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return errors.New("market scheduler failed; inspect database availability and migrations")
			}
			if _, err = repo.ScheduleMarketHistory(ctx, scope, now, 50); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return errors.New("market history hydration failed; inspect database availability and mappings")
			}
			nextSchedule = now.Add(time.Minute)
		}
		enabled, sourceErr := repo.SourceEnabled(ctx, scope)
		if sourceErr != nil {
			if ctx.Err() != nil {
				return nil
			}
			return errors.New("cannot check market source availability")
		}
		if enabled {
			err = worker.RunOne(ctx)
		} else {
			err = md.ErrNoJob
		}
		if ctx.Err() != nil {
			return nil
		}
		if errors.Is(err, md.ErrNoJob) {
			monitor.Observe(nil)
		} else {
			monitor.Observe(err)
		}
		if err != nil && !errors.Is(err, md.ErrNoJob) && !errors.Is(err, md.ErrLeaseLost) {
			return errors.New("market worker iteration failed; inspect database availability and migrations")
		}
		if *once {
			if errors.Is(err, md.ErrNoJob) {
				fmt.Println("No market job available")
			} else {
				fmt.Println("Market job attempt finished; persisted status records success or retry")
			}
			return nil
		}
		delay := time.Second
		if errors.Is(err, md.ErrNoJob) {
			delay = 5 * time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
