// alphafx refreshes registered personal retrospective Alpha FX. It never creates
// a policy, performs journal writes, or accepts a token in arguments/environment.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"os/user"
	"syscall"
	"time"

	"github.com/google/uuid"
	av "github.com/positron48/budget/internal/adapter/alphavantage"
	"github.com/positron48/budget/internal/adapter/postgres"
	"github.com/positron48/budget/internal/adapter/workermetrics"
	"github.com/positron48/budget/internal/usecase/alphafx"
	"github.com/positron48/budget/internal/usecase/workerhealth"
)

func run() error {
	tenant := flag.String("tenant", "", "tenant UUID")
	key := flag.String("policy", "alpha-retrospective", "already registered personal retrospective policy")
	account := flag.String("keychain-account", "", "macOS account; ignored when ALPHA_FX_TOKEN_FILE is set")
	once := flag.Bool("once", false, "attempt one batch; quota deferral is reported without provider requests")
	flag.Parse()
	if id, e := uuid.Parse(*tenant); e != nil || id.String() != *tenant || flag.NArg() != 0 {
		return errors.New("valid tenant UUID required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL required")
	}
	pool, err := postgres.NewPool(ctx, dsn)
	if err != nil {
		return errors.New("database configuration failed")
	}
	defer pool.Close()
	if err = pool.Ping(ctx); err != nil {
		return errors.New("database unavailable")
	}
	repo := postgres.NewMarketDataRepo(pool)
	// Validate policy before reading a credential or consuming any quota.
	health, err := repo.ReadAlphaFXWorkerHealth(ctx, *tenant, *key, time.Now().UTC())
	if err != nil || health.SourceEnabled == nil || !*health.SourceEnabled {
		return errors.New("enabled reviewed Alpha policy required")
	}
	var client *av.Client
	if path := os.Getenv("ALPHA_FX_TOKEN_FILE"); path != "" {
		client, err = av.NewFromSecretFile(path)
	} else {
		if *account == "" {
			u, e := user.Current()
			if e != nil {
				return errors.New("keychain account unavailable")
			}
			*account = u.Username
		}
		client, err = av.NewFromKeychain(ctx, *account)
	}
	if err != nil {
		return err
	}
	client.SetBatchGate(repo.ReserveAlphaFXBatch)
	monitor, err := workermetrics.New(ctx, "alphafx", func(ctx context.Context) (workerhealth.Snapshot, error) {
		return repo.ReadAlphaFXWorkerHealth(ctx, *tenant, *key, time.Now().UTC())
	})
	if err != nil {
		return err
	}
	closeMetrics, err := monitor.Start(os.Getenv("INVESTMENT_METRICS_ADDR"))
	if err != nil {
		return err
	}
	defer closeMetrics()
	service := alphafx.Service{Source: client, Store: repo, Tenant: *tenant, Key: *key}
	report := func(result alphafx.Result, err error) {
		if result.Deferred {
			fmt.Println("Alpha FX batch deferred by persistent credential quota; cache retained")
			return
		}
		monitor.Observe(err)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Alpha FX update failed; cache retained; retry governed by persistent quota")
		} else {
			fmt.Printf("Alpha retrospective FX checked: points=%d; freshness is reported separately\n", result.Points)
		}
	}
	if *once {
		attempt, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		result, e := service.Sync(attempt, time.Now().UTC())
		report(result, e)
		if e != nil {
			return errors.New("alpha FX batch failed")
		}
		return nil
	}
	service.Run(ctx, report)
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
