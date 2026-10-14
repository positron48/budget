package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/positron48/budget/internal/adapter/postgres"
)

func inspectScope(ledger string) error {
	id, e := uuid.Parse(ledger)
	if e != nil || id.String() != ledger {
		return errors.New("canonical scope ledger UUID required")
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool, e := postgres.NewPool(ctx, dsn)
	if e != nil {
		return errors.New("database connection failed")
	}
	defer pool.Close()
	scope, e := postgres.NewInvestmentRepo(pool).InspectPortfolioScope(ctx, ledger)
	if e != nil {
		return errors.New("scope unavailable; standalone ledger and active owner/admin creator required")
	}
	return json.NewEncoder(os.Stdout).Encode(scope)
}
