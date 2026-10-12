package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
)

// RegisterBenchmark publishes operator-reviewed benchmark identity and its audit.
// Portfolio NAV is unaffected; comparisons resolve this registry at read time.
func (r *MarketDataRepo) RegisterBenchmark(ctx context.Context, tenant, actor string, p nav.BenchmarkProfile, expected int64) (nav.SavedBenchmarkProfile, error) {
	out := nav.SavedBenchmarkProfile{BenchmarkProfile: p, ActorID: actor}
	for _, v := range []string{tenant, actor} {
		if id, e := uuid.Parse(v); e != nil || id.String() != v {
			return out, d.ErrInvestmentInvalid
		}
	}
	if !nav.ValidateBenchmarkProfile(p) || expected < 0 {
		return out, d.ErrInvestmentInvalid
	}
	raw, e := json.Marshal(p)
	if e != nil {
		return out, e
	}
	if len(raw) > 4*1024*1024 {
		return out, d.ErrInvestmentLimit
	}
	tx, e := r.pool.DB.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var role string
	e = tx.QueryRow(ctx, `SELECT role FROM user_tenants WHERE tenant_id=$1 AND user_id=$2 FOR SHARE`, tenant, actor).Scan(&role)
	if errors.Is(e, pgx.ErrNoRows) || e == nil && role != "owner" && role != "admin" {
		return out, d.ErrInvestmentForbidden
	}
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "investment-benchmarks:"+tenant+":"+p.Key); e != nil {
		return out, e
	}
	hash := investmentHash(struct {
		Profile nav.BenchmarkProfile
		Actor   string
	}{p, actor})
	var version int64
	e = tx.QueryRow(ctx, `SELECT version FROM investment_benchmarks WHERE tenant_id=$1 AND benchmark_key=$2 FOR UPDATE`, tenant, p.Key).Scan(&version)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return out, e
	}
	if version != expected {
		var old []byte
		var oldHash string
		e = tx.QueryRow(ctx, `SELECT data,payload_hash,actor_id,recorded_at FROM investment_benchmark_revisions WHERE tenant_id=$1 AND benchmark_key=$2 AND version=$3`, tenant, p.Key, expected+1).Scan(&old, &oldHash, &out.ActorID, &out.RecordedAt)
		if e == nil && oldHash == hash {
			out.Version = expected + 1
			e = json.Unmarshal(old, &out.BenchmarkProfile)
			return out, e
		}
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return out, e
		}
		return out, d.ErrInvestmentConflict
	}
	// Source and listing access are rechecked when publishing an enabled row.
	profile, e := readPriceConventions(ctx, tx, tenant, p.PriceProfileKey)
	if e != nil {
		return out, e
	}
	if p.Enabled {
		if !profile.Enabled || len(profile.Periods[p.InstrumentKey]) == 0 {
			return out, d.ErrInvestmentPrecondition
		}
		expectedKind := "index"
		if p.Kind == "etf_price_proxy" {
			expectedKind = "etf"
		}
		for _, period := range profile.Periods[p.InstrumentKey] {
			if period.Policy.Kind != "daily_candle" || period.Policy.Adjustment != "unadjusted" || period.Policy.SourceKind != "exchange" {
				return out, d.ErrInvestmentPrecondition
			}
			var valid bool
			e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM market_data_sources s JOIN market_provider_instruments i ON i.tenant_id=s.tenant_id AND i.provider=s.provider AND i.access_scope=s.access_scope WHERE s.tenant_id=$1 AND s.provider=$2 AND s.access_scope=$3 AND s.enabled AND i.instrument_id=$4 AND i.data->>'kind'=$5 AND (i.data->>'currency'='' OR i.data->>'currency'=$6))`, tenant, period.Provider, period.AccessScope, period.InstrumentID, expectedKind, p.Currency).Scan(&valid)
			if e != nil {
				return out, e
			}
			if !valid {
				return out, d.ErrInvestmentPrecondition
			}
		}
	}
	out.Version = version + 1
	if _, e = tx.Exec(ctx, `INSERT INTO investment_benchmarks(tenant_id,benchmark_key,version,price_profile_key) VALUES($1,$2,$3,$4) ON CONFLICT(tenant_id,benchmark_key) DO UPDATE SET version=EXCLUDED.version,price_profile_key=EXCLUDED.price_profile_key`, tenant, p.Key, out.Version, p.PriceProfileKey); e != nil {
		return out, e
	}
	e = tx.QueryRow(ctx, `INSERT INTO investment_benchmark_revisions(tenant_id,benchmark_key,version,data,payload_hash,actor_id) VALUES($1,$2,$3,$4,$5,$6) RETURNING recorded_at`, tenant, p.Key, out.Version, raw, hash, actor).Scan(&out.RecordedAt)
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}

func readBenchmarkProfile(ctx context.Context, tx pgx.Tx, tenant, key string) (nav.SavedBenchmarkProfile, error) {
	var out nav.SavedBenchmarkProfile
	if !nav.ValidConventionKey(key) {
		return out, d.ErrInvestmentInvalid
	}
	var raw []byte
	e := tx.QueryRow(ctx, `SELECT r.data,r.version,r.actor_id,r.recorded_at FROM investment_benchmarks b JOIN investment_benchmark_revisions r ON r.tenant_id=b.tenant_id AND r.benchmark_key=b.benchmark_key AND r.version=b.version WHERE b.tenant_id=$1 AND b.benchmark_key=$2`, tenant, key).Scan(&raw, &out.Version, &out.ActorID, &out.RecordedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, d.ErrInvestmentNotFound
	}
	if e != nil {
		return out, e
	}
	e = json.Unmarshal(raw, &out.BenchmarkProfile)
	return out, e
}
func (r *MarketDataRepo) ListBenchmarks(ctx context.Context, tenant, actor, after string) ([]nav.SavedBenchmarkProfile, error) {
	if after != "" && !nav.ValidConventionKey(after) {
		return nil, d.ErrInvestmentInvalid
	}
	rows, e := r.pool.DB.Query(ctx, `SELECT r.data,r.version,r.actor_id,r.recorded_at FROM investment_benchmarks b JOIN investment_benchmark_revisions r ON r.tenant_id=b.tenant_id AND r.benchmark_key=b.benchmark_key AND r.version=b.version WHERE b.tenant_id=$1 AND b.benchmark_key>$3 AND r.data->>'Enabled'='true' AND EXISTS(SELECT 1 FROM user_tenants WHERE tenant_id=$1 AND user_id=$2 AND role IN ('owner','admin','member')) ORDER BY b.benchmark_key LIMIT 100`, tenant, actor, after)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	result := []nav.SavedBenchmarkProfile{}
	for rows.Next() {
		var p nav.SavedBenchmarkProfile
		var raw []byte
		if e = rows.Scan(&raw, &p.Version, &p.ActorID, &p.RecordedAt); e != nil {
			return nil, e
		}
		if e = json.Unmarshal(raw, &p.BenchmarkProfile); e != nil {
			return nil, e
		}
		result = append(result, p)
	}
	return result, rows.Err()
}
