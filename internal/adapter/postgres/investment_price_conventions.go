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

// RegisterPriceConventions is an operator workflow, never a worker inference.
// A revision, its audit and NAV invalidation commit atomically.
func (r *MarketDataRepo) RegisterPriceConventions(ctx context.Context, tenant, actor string, p nav.ConventionProfile, expected int64) (nav.SavedConventionProfile, error) {
	out := nav.SavedConventionProfile{ConventionProfile: p, ActorID: actor}
	for _, v := range []string{tenant, actor} {
		if id, e := uuid.Parse(v); e != nil || id.String() != v {
			return out, d.ErrInvestmentInvalid
		}
	}
	if nav.ValidateConventionProfile(tenant, p) != nil || expected < 0 {
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
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "investment-conventions:"+tenant+":"+p.Key); e != nil {
		return out, e
	}
	hash := investmentHash(struct {
		Profile nav.ConventionProfile
		Actor   string
	}{p, actor})
	var version int64
	e = tx.QueryRow(ctx, `SELECT version FROM investment_price_conventions WHERE tenant_id=$1 AND profile_key=$2 FOR UPDATE`, tenant, p.Key).Scan(&version)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return out, e
	}
	if version != expected {
		var old []byte
		var oldHash string
		e = tx.QueryRow(ctx, `SELECT data,payload_hash,actor_id,recorded_at FROM investment_price_convention_revisions WHERE tenant_id=$1 AND profile_key=$2 AND version=$3`, tenant, p.Key, expected+1).Scan(&old, &oldHash, &out.ActorID, &out.RecordedAt)
		if e == nil && oldHash == hash {
			out.Version = expected + 1
			e = json.Unmarshal(old, &out.ConventionProfile)
			return out, e
		}
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return out, e
		}
		return out, d.ErrInvestmentConflict
	}
	// Confirming a convention cannot grant an entitlement or invent a listing.
	// Revocation remains possible even after the source has been disabled.
	if p.Enabled {
		for _, periods := range p.Periods {
			for _, v := range periods {
				var exists bool
				e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM market_data_sources s JOIN market_provider_instruments i ON i.tenant_id=s.tenant_id AND i.provider=s.provider AND i.access_scope=s.access_scope WHERE s.tenant_id=$1 AND s.provider=$2 AND s.access_scope=$3 AND s.enabled AND i.instrument_id=$4)`, tenant, v.Provider, v.AccessScope, v.InstrumentID).Scan(&exists)
				if e != nil {
					return out, e
				}
				if !exists {
					return out, d.ErrInvestmentPrecondition
				}
			}
		}
	}
	if p.Enabled {
		if e = verifyReviewedAdjustments(ctx, tx, tenant, p); e != nil {
			return out, e
		}
		if e = verifyReviewedAccrued(ctx, tx, tenant, p); e != nil {
			return out, e
		}
	}
	out.Version = version + 1
	if _, e = tx.Exec(ctx, `INSERT INTO investment_price_conventions(tenant_id,profile_key,version) VALUES($1,$2,$3) ON CONFLICT(tenant_id,profile_key) DO UPDATE SET version=EXCLUDED.version`, tenant, p.Key, out.Version); e != nil {
		return out, e
	}
	e = tx.QueryRow(ctx, `INSERT INTO investment_price_convention_revisions(tenant_id,profile_key,version,data,payload_hash,actor_id) VALUES($1,$2,$3,$4,$5,$6) RETURNING recorded_at`, tenant, p.Key, out.Version, raw, hash, actor).Scan(&out.RecordedAt)
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}

func readPriceConventions(ctx context.Context, tx pgx.Tx, tenant, key string) (nav.SavedConventionProfile, error) {
	var out nav.SavedConventionProfile
	var raw []byte
	if !nav.ValidConventionKey(key) {
		return out, d.ErrInvestmentInvalid
	}
	e := tx.QueryRow(ctx, `SELECT r.data,r.version,r.actor_id,r.recorded_at FROM investment_price_conventions p JOIN investment_price_convention_revisions r ON r.tenant_id=p.tenant_id AND r.profile_key=p.profile_key AND r.version=p.version WHERE p.tenant_id=$1 AND p.profile_key=$2`, tenant, key).Scan(&raw, &out.Version, &out.ActorID, &out.RecordedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, d.ErrInvestmentNotFound
	}
	if e != nil {
		return out, e
	}
	e = json.Unmarshal(raw, &out.ConventionProfile)
	return out, e
}
