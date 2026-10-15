package postgres

import (
	"context"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

type nativeDailyQuota struct{ until time.Time }

func (nativeDailyQuota) Error() string               { return "native provider daily quota reserved" }
func (nativeDailyQuota) Temporary() bool             { return true }
func (e nativeDailyQuota) RetryDelay() time.Duration { return max(time.Second, time.Until(e.until)) }

// WaitNativeEquityPermit leaves headroom below Basic's 8/minute, 800/day.
// Coordination covers workers sharing this DB and credential fingerprint;
// other clients can still exhaust the provider quota. No paid plan is enabled.
func (r *MarketDataRepo) WaitNativeEquityPermit(ctx context.Context, key string) error {
	if e := r.WaitMarketPermit(ctx, key, 8*time.Second); e != nil {
		return e
	}
	return r.reserveNativeDaily(ctx, key, 750)
}
func (r *MarketDataRepo) reserveNativeDaily(ctx context.Context, key string, limit int) error {
	b, e := hex.DecodeString(key)
	if e != nil || len(b) != 32 || hex.EncodeToString(b) != key || limit < 1 || limit > 750 {
		return md.ErrInvalid
	}
	var used int
	e = r.pool.DB.QueryRow(ctx, `INSERT INTO market_provider_daily_limits(credential_key,day,used) VALUES($1,(clock_timestamp() AT TIME ZONE 'UTC')::date,1)
ON CONFLICT(credential_key,day) DO UPDATE SET used=market_provider_daily_limits.used+1 WHERE market_provider_daily_limits.used<$2 RETURNING used`, key, limit).Scan(&used)
	if errors.Is(e, pgx.ErrNoRows) {
		return nativeDailyQuota{time.Now().UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)}
	}
	return e
}
