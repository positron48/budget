package postgres

import (
	"context"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

// WaitMarketPermit shares a conservative request gate across all workers using
// the same credential. A cancelled waiter consumes no future reservation.
func (r *MarketDataRepo) WaitMarketPermit(ctx context.Context, key string, interval time.Duration) error {
	for {
		delay, err := r.marketPermit(ctx, key, interval)
		if err != nil {
			return err
		}
		if delay == 0 {
			return nil
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
func (r *MarketDataRepo) marketPermit(ctx context.Context, key string, interval time.Duration) (time.Duration, error) {
	decoded, err := hex.DecodeString(key)
	if err != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != key || interval < 100*time.Millisecond || interval > time.Minute {
		return 0, md.ErrInvalid
	}
	var admitted int
	err = r.pool.DB.QueryRow(ctx, `INSERT INTO market_provider_limits(credential_key,next_at) VALUES($1,clock_timestamp()+$2*interval '1 second') ON CONFLICT(credential_key) DO UPDATE SET next_at=clock_timestamp()+$2*interval '1 second' WHERE market_provider_limits.next_at<=clock_timestamp() RETURNING 1`, key, interval.Seconds()).Scan(&admitted)
	if err == nil {
		return 0, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	var seconds float64
	if err = r.pool.DB.QueryRow(ctx, `SELECT GREATEST(0.001,EXTRACT(EPOCH FROM next_at-clock_timestamp()))::float8 FROM market_provider_limits WHERE credential_key=$1`, key).Scan(&seconds); err != nil {
		return 0, err
	}
	delay := time.Duration(seconds * float64(time.Second))
	if delay > time.Minute {
		delay = time.Minute
	}
	return delay, nil
}

// DeferMarketPermit propagates Retry-After to all scopes sharing a credential.
func (r *MarketDataRepo) DeferMarketPermit(ctx context.Context, key string, delay time.Duration) error {
	decoded, err := hex.DecodeString(key)
	if err != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != key || delay < 0 || delay > 24*time.Hour {
		return md.ErrInvalid
	}
	_, err = r.pool.DB.Exec(ctx, `INSERT INTO market_provider_limits(credential_key,next_at) VALUES($1,clock_timestamp()+$2*interval '1 second') ON CONFLICT(credential_key) DO UPDATE SET next_at=GREATEST(market_provider_limits.next_at,EXCLUDED.next_at)`, key, delay.Seconds())
	return err
}
