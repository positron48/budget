package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"

	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
)

var navSeriesKey = regexp.MustCompile(`^[a-f0-9]{64}$`)

// Public reads expose only the caller's registered ranges. The client cannot
// inject quote conventions, calendars or tenant/actor identities in a request.
func (r *MarketDataRepo) ListNAVSeries(ctx context.Context, tenant, actor, after string) ([]nav.SeriesSummary, error) {
	if after != "" && !navSeriesKey.MatchString(after) {
		return nil, d.ErrInvestmentInvalid
	}
	rows, e := r.pool.DB.Query(ctx, `SELECT w.series_key,w.request,w.from_date::text,w.to_date::text,w.extension_limited FROM investment_nav_watches w JOIN user_tenants u ON u.tenant_id=w.tenant_id AND u.user_id=w.actor_id WHERE w.tenant_id=$1 AND w.actor_id=$2 AND w.enabled AND u.role IN ('owner','admin','member') AND w.series_key>$3 ORDER BY w.series_key LIMIT 100`, tenant, actor, after)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []nav.SeriesSummary{}
	for rows.Next() {
		var v nav.SeriesSummary
		var raw []byte
		if e = rows.Scan(&v.Key, &raw, &v.From, &v.To, &v.ExtensionLimited); e != nil {
			return nil, e
		}
		var q nav.Request
		if e = json.Unmarshal(raw, &q); e != nil {
			return nil, e
		}
		v.LedgerIDs = q.LedgerIDs
		v.Currency = q.Currency
		v.FXPolicyKey = q.FXPolicyKey
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *MarketDataRepo) navRegisteredRequest(ctx context.Context, tenant, actor, key string) (nav.Request, string, string, error) {
	return readNAVRegisteredRequest(ctx, r.pool.DB, tenant, actor, key)
}

func readNAVRegisteredRequest(ctx context.Context, db investmentNAVReader, tenant, actor, key string) (nav.Request, string, string, error) {
	var q nav.Request
	var from, to string
	if !navSeriesKey.MatchString(key) {
		return q, from, to, d.ErrInvestmentInvalid
	}
	var raw []byte
	e := db.QueryRow(ctx, `SELECT w.request,w.from_date::text,w.to_date::text FROM investment_nav_watches w JOIN user_tenants u ON u.tenant_id=w.tenant_id AND u.user_id=w.actor_id WHERE w.tenant_id=$1 AND w.actor_id=$2 AND w.series_key=$3 AND u.role IN ('owner','admin','member')`, tenant, actor, key).Scan(&raw, &from, &to)
	if errors.Is(e, pgx.ErrNoRows) {
		return q, from, to, d.ErrInvestmentNotFound
	}
	if e != nil {
		return q, from, to, e
	}
	if e = json.Unmarshal(raw, &q); e != nil {
		return q, from, to, e
	}
	// Actor and tenant come from authorization, even if stored JSON is malformed.
	q.TenantID = tenant
	q.UserID = actor
	return q, from, to, nil
}

func (r *MarketDataRepo) NAVHistoryForSeries(ctx context.Context, tenant, actor, key, from, to string) ([]nav.DailyNAV, error) {
	q, start, end, e := r.navRegisteredRequest(ctx, tenant, actor, key)
	if e != nil {
		return nil, e
	}
	if _, e = nav.DayCutoff(from); e != nil {
		return nil, d.ErrInvestmentInvalid
	}
	if _, e = nav.DayCutoff(to); e != nil || from < start || to > end || to < from {
		return nil, d.ErrInvestmentInvalid
	}
	return readDailyNAVHistoryMode(ctx, r.pool.DB, q, from, to, true)
}

// RequestNAVRefresh advances no cursor itself. The scheduler consumes the reset
// under the same watch lock as invalidations. A per-series cooldown bounds load.
func (r *MarketDataRepo) RequestNAVRefresh(ctx context.Context, tenant, actor, key string) error {
	if _, _, _, e := r.navRegisteredRequest(ctx, tenant, actor, key); e != nil {
		return e
	}
	tag, e := r.pool.DB.Exec(ctx, `UPDATE investment_nav_watches w SET cursor_generation=0,refresh_requested_at=clock_timestamp()
 WHERE tenant_id=$1 AND actor_id=$2 AND series_key=$3 AND enabled AND (refresh_requested_at IS NULL OR refresh_requested_at<=clock_timestamp()-interval '30 seconds')
 AND EXISTS(SELECT 1 FROM user_tenants u WHERE u.tenant_id=w.tenant_id AND u.user_id=w.actor_id AND u.role IN ('owner','admin','member'))`, tenant, actor, key)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		var enabled bool
		if e = r.pool.DB.QueryRow(ctx, `SELECT enabled FROM investment_nav_watches WHERE tenant_id=$1 AND actor_id=$2 AND series_key=$3`, tenant, actor, key).Scan(&enabled); e == nil && !enabled {
			return d.ErrInvestmentPrecondition
		}
		return d.ErrInvestmentLimit
	}
	return nil
}

func (r *MarketDataRepo) GetNAVSeries(ctx context.Context, tenant, actor, key string) (nav.SeriesSummary, error) {
	var out nav.SeriesSummary
	if !navSeriesKey.MatchString(key) {
		return out, d.ErrInvestmentInvalid
	}
	var raw []byte
	e := r.pool.DB.QueryRow(ctx, `SELECT w.series_key,w.request,w.from_date::text,w.to_date::text,w.extension_limited FROM investment_nav_watches w JOIN user_tenants u ON u.tenant_id=w.tenant_id AND u.user_id=w.actor_id WHERE w.tenant_id=$1 AND w.actor_id=$2 AND w.series_key=$3 AND u.role IN ('owner','admin','member')`, tenant, actor, key).Scan(&out.Key, &raw, &out.From, &out.To, &out.ExtensionLimited)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, d.ErrInvestmentNotFound
	}
	if e != nil {
		return out, e
	}
	var q nav.Request
	if e = json.Unmarshal(raw, &q); e != nil {
		return out, e
	}
	out.LedgerIDs = q.LedgerIDs
	out.Currency = q.Currency
	out.FXPolicyKey = q.FXPolicyKey
	return out, nil
}
