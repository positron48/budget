package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/positron48/budget/internal/adapter/cbr"
	"github.com/positron48/budget/internal/adapter/postgres"
	"github.com/positron48/budget/internal/usecase/fximport"
)

func run() error {
	fromArg := flag.String("from", "2025-01-01", "first calendar date, YYYY-MM-DD")
	toArg := flag.String("to", fximport.MoscowDate(time.Now()).Format(time.DateOnly), "last calendar date, YYYY-MM-DD")
	dryRun := flag.Bool("dry-run", false, "fetch and validate without connecting to the database")
	flag.Parse()
	from, err := time.Parse(time.DateOnly, *fromArg)
	if err != nil {
		return err
	}
	to, err := time.Parse(time.DateOnly, *toArg)
	if err != nil {
		return err
	}
	if to.After(fximport.MoscowDate(time.Now())) {
		return fmt.Errorf("future import date")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	service := fximport.Service{Source: cbr.New()}
	if !*dryRun {
		databaseURL := os.Getenv("DATABASE_URL")
		if databaseURL == "" {
			return fmt.Errorf("DATABASE_URL is required")
		}
		db, err := postgres.NewPool(ctx, databaseURL)
		if err != nil {
			return fmt.Errorf("connect to database (check DATABASE_URL)")
		}
		defer db.Close()
		service.Store = postgres.NewFxRepo(db)
	}
	n, err := service.Import(ctx, from, to, *dryRun)
	if err != nil {
		return err
	}
	fmt.Printf("FX import complete: provider=cbr from=%s through=%s quotes=%d dry_run=%t\n", *fromArg, *toArg, n, *dryRun)
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "FX import failed:", err)
		os.Exit(1)
	}
}
