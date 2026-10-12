package postgres

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMarketProviderGate(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	repo := NewMarketDataRepo(pool)
	ctx := context.Background()
	key := strings.Repeat("a", 64)
	var wg sync.WaitGroup
	out := make(chan time.Duration, 8)
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); d, e := repo.marketPermit(ctx, key, time.Minute); out <- d; errs <- e }()
	}
	wg.Wait()
	close(out)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	admitted := 0
	for delay := range out {
		if delay == 0 {
			admitted++
		}
	}
	if admitted != 1 {
		t.Fatal("credential-wide burst", admitted)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()
	if err := repo.WaitMarketPermit(waitCtx, key, time.Minute); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("wait ignored cancellation", err)
	}
	if d, e := repo.marketPermit(ctx, strings.Repeat("b", 64), time.Minute); e != nil || d != 0 {
		t.Fatal("independent credentials blocked", d, e)
	}
	if _, err := pool.DB.Exec(ctx, `UPDATE market_provider_limits SET next_at=clock_timestamp()-interval '1 second' WHERE credential_key=$1`, key); err != nil {
		t.Fatal(err)
	}
	if d, e := repo.marketPermit(ctx, key, time.Minute); e != nil || d != 0 {
		t.Fatal("gate did not reopen", d, e)
	}
	if err := repo.DeferMarketPermit(ctx, key, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeferMarketPermit(ctx, key, time.Second); err != nil {
		t.Fatal(err)
	}
	var seconds float64
	if err := pool.DB.QueryRow(ctx, `SELECT EXTRACT(EPOCH FROM next_at-clock_timestamp())::float8 FROM market_provider_limits WHERE credential_key=$1`, key).Scan(&seconds); err != nil || seconds < 3590 {
		t.Fatal("backoff was shortened", seconds, err)
	}

}
