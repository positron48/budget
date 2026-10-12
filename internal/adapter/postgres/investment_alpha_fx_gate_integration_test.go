package postgres

import (
	"context"
	"strings"
	"sync"
	"testing"
)

func TestInvestmentAlphaFXPersistentQuota(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	repo := NewMarketDataRepo(pool)
	key := strings.Repeat("b", 64)
	var wg sync.WaitGroup
	results := make(chan bool, 20)
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Go(func() { ok, err := repo.ReserveAlphaFXBatch(ctx, key); results <- ok; errs <- err })
	}
	wg.Wait()
	close(results)
	close(errs)
	count := 0
	for ok := range results {
		if ok {
			count++
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if count != 1 {
		t.Fatal("replica burst admitted more than one batch", count)
	}
	if ok, err := NewMarketDataRepo(pool).ReserveAlphaFXBatch(ctx, key); err != nil || ok {
		t.Fatal("process restart bypassed quota", err)
	}
	if ok, err := repo.ReserveAlphaFXBatch(ctx, strings.Repeat("c", 64)); err != nil || !ok {
		t.Fatal("independent credential blocked", err)
	}
	if _, err := pool.DB.Exec(ctx, `UPDATE market_provider_limits SET next_at=clock_timestamp()-interval '1 second' WHERE credential_key=$1`, key); err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.ReserveAlphaFXBatch(ctx, key); err != nil || !ok {
		t.Fatal("elapsed quota slot did not reopen", err)
	}
	if _, err := repo.ReserveAlphaFXBatch(ctx, "not-a-fingerprint"); err == nil {
		t.Fatal("invalid quota key accepted")
	}
}
