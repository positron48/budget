package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
)

type dailySeriesDefinition struct {
	Ledgers               []string
	Currency, FXPolicyKey string
	Series                map[string]nav.SeriesPolicy
	ConventionKey         string `json:",omitempty"`
}

func navSeries(q nav.Request) (string, []byte, error) {
	ids := append([]string{}, q.LedgerIDs...)
	sort.Strings(ids)
	def := dailySeriesDefinition{Ledgers: ids, Currency: q.Currency, FXPolicyKey: q.FXPolicyKey, Series: q.Series, ConventionKey: q.ConventionKey}
	raw, e := json.Marshal(def)
	return investmentHash(def), raw, e
}

type DailyNAV = nav.DailyNAV

// BuildDailyNAV computes outside the publication lock, then fences publication
// against the generation read in the same MVCC snapshot as the source data.
func (r *MarketDataRepo) BuildDailyNAV(ctx context.Context, q nav.Request, date string) (DailyNAV, error) {
	at, e := nav.DayCutoff(date)
	if e != nil || at.After(time.Now().UTC()) {
		return DailyNAV{}, d.ErrInvestmentInvalid
	}
	q.At = at
	result, e := r.ReadInvestmentValuation(ctx, q)
	if e != nil {
		return DailyNAV{}, e
	}
	return r.publishDailyNAV(ctx, q, date, result)
}
func (r *MarketDataRepo) publishDailyNAV(ctx context.Context, q nav.Request, date string, result nav.Result) (DailyNAV, error) {
	var out DailyNAV
	at, e := nav.DayCutoff(date)
	if e != nil || result.Provisional || !result.At.Equal(at) || result.TenantID != q.TenantID || result.Currency != q.Currency {
		return out, d.ErrInvestmentInvalid
	}
	key, definition, e := navSeries(q)
	if e != nil {
		return out, e
	}
	tx, e := r.pool.DB.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// A tenant without any journal cannot get here through the trusted reader.
	var generation int64
	e = tx.QueryRow(ctx, `SELECT generation FROM investment_valuation_generations WHERE tenant_id=$1 FOR UPDATE`, q.TenantID).Scan(&generation)
	if errors.Is(e, pgx.ErrNoRows) || e == nil && (result.DataGeneration < 1 || generation < result.DataGeneration) {
		return out, d.ErrInvestmentConflict
	}
	if e != nil {
		return out, e
	}
	var changed bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM investment_nav_invalidations WHERE tenant_id=$1 AND generation>$2 AND (affected_from IS NULL OR affected_from<=$3))`, q.TenantID, result.DataGeneration, at).Scan(&changed); e != nil {
		return out, e
	}
	if changed {
		return out, d.ErrInvestmentConflict
	}
	// Keep the actual source snapshot generation; a later unaffected update is
	// not evidence that the input snapshot was read at that later generation.
	generation = result.DataGeneration
	var currentPoint int64
	e = tx.QueryRow(ctx, `SELECT generation FROM investment_daily_nav WHERE tenant_id=$1 AND series_key=$2 AND report_date=$3::date`, q.TenantID, key, date).Scan(&currentPoint)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return out, e
	}
	if currentPoint > generation {
		return out, d.ErrInvestmentConflict
	}
	var role string
	e = tx.QueryRow(ctx, `SELECT role FROM user_tenants WHERE tenant_id=$1 AND user_id=$2`, q.TenantID, q.UserID).Scan(&role)
	if errors.Is(e, pgx.ErrNoRows) || e == nil && role != "owner" && role != "admin" && role != "member" {
		return out, d.ErrInvestmentForbidden
	}
	if e != nil {
		return out, e
	}
	raw, e := json.Marshal(result)
	if e != nil {
		return out, e
	}
	hash := investmentHash(result)
	var previousHash string
	var previousAt time.Time
	e = tx.QueryRow(ctx, `SELECT payload_hash,computed_at FROM investment_daily_nav_revisions WHERE tenant_id=$1 AND series_key=$2 AND report_date=$3::date AND generation=$4`, q.TenantID, key, date, generation).Scan(&previousHash, &previousAt)
	if e == nil {
		if previousHash != hash {
			return out, d.ErrInvestmentConflict
		}
		return dailyPoint(date, generation, previousAt, result), nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO investment_nav_series(tenant_id,series_key,definition) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, q.TenantID, key, definition); e != nil {
		return out, e
	}
	var computed time.Time
	e = tx.QueryRow(ctx, `INSERT INTO investment_daily_nav(tenant_id,series_key,report_date,generation,data,payload_hash) VALUES($1,$2,$3::date,$4,$5,$6) ON CONFLICT(tenant_id,series_key,report_date) DO UPDATE SET generation=EXCLUDED.generation,data=EXCLUDED.data,payload_hash=EXCLUDED.payload_hash,computed_at=clock_timestamp() RETURNING computed_at`, q.TenantID, key, date, generation, raw, hash).Scan(&computed)
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO investment_daily_nav_revisions(tenant_id,series_key,report_date,generation,data,payload_hash,computed_at) VALUES($1,$2,$3::date,$4,$5,$6,$7)`, q.TenantID, key, date, generation, raw, hash, computed); e != nil {
		return out, e
	}
	return dailyPoint(date, generation, computed, result), tx.Commit(ctx)
}
func dailyPoint(date string, generation int64, computed time.Time, result nav.Result) DailyNAV {
	state := "incomplete"
	if result.Complete {
		state = "ready"
	}
	return DailyNAV{Date: date, Generation: generation, ComputedAt: computed, State: state, Result: &result}
}

// DailyNAVHistory is an internal projection read. It checks membership and
// generation in one SQL statement; obsolete values are returned as gaps.
func (r *MarketDataRepo) DailyNAVHistory(ctx context.Context, q nav.Request, from, to string) ([]DailyNAV, error) {
	return readDailyNAVHistory(ctx, r.pool.DB, q, from, to)
}

// The reader can be a repeatable-read transaction shared with journal and FX.
type investmentNAVReader interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readDailyNAVHistory(ctx context.Context, db investmentNAVReader, q nav.Request, from, to string) ([]DailyNAV, error) {
	return readDailyNAVHistoryMode(ctx, db, q, from, to, false)
}

// Summary reads retain every field exposed by chart RPCs and used for returns,
// while full composition/provenance remain available to snapshot readers.
func readDailyNAVHistoryMode(ctx context.Context, db investmentNAVReader, q nav.Request, from, to string, summary bool) ([]DailyNAV, error) {
	start, e := time.Parse("2006-01-02", from)
	if e != nil {
		return nil, d.ErrInvestmentInvalid
	}
	end, e := time.Parse("2006-01-02", to)
	if e != nil || end.Before(start) || end.Sub(start) > 3660*24*time.Hour {
		return nil, d.ErrInvestmentInvalid
	}
	key, _, e := navSeries(q)
	if e != nil {
		return nil, e
	}
	rows, e := db.Query(ctx, `SELECT ($4::date+days.i)::text,COALESCE(n.generation,0),n.computed_at,CASE WHEN n.generation<=g.generation AND NOT EXISTS(
 SELECT 1 FROM investment_nav_invalidations i WHERE i.tenant_id=n.tenant_id AND i.generation>n.generation
 AND (i.affected_from IS NULL OR i.affected_from<=((n.report_date+1)::timestamp AT TIME ZONE 'Europe/Moscow')-interval '1 microsecond')
 ) THEN CASE WHEN $6::boolean THEN n.data - ARRAY['Positions','ComponentValues','Components','Evidence','InstrumentFacts','LedgerVersions','FXPaths']::text[] ELSE n.data END ELSE NULL END
 FROM user_tenants u CROSS JOIN generate_series(0,$5::date-$4::date) days(i)
 LEFT JOIN investment_daily_nav n ON n.tenant_id=u.tenant_id AND n.series_key=$3 AND n.report_date=$4::date+days.i
 LEFT JOIN investment_valuation_generations g ON g.tenant_id=u.tenant_id
 WHERE u.tenant_id=$1 AND u.user_id=$2 AND u.role IN ('owner','admin','member') ORDER BY days.i`, q.TenantID, q.UserID, key, from, to, summary)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []DailyNAV{}
	for rows.Next() {
		var p DailyNAV
		var raw []byte
		var computed *time.Time
		if e = rows.Scan(&p.Date, &p.Generation, &computed, &raw); e != nil {
			return nil, e
		}
		p.State = "stale"
		if computed != nil {
			p.ComputedAt = *computed
		} else {
			p.State = "unavailable"
		}
		if raw != nil {
			var result nav.Result
			if e = json.Unmarshal(raw, &result); e != nil {
				return nil, e
			}
			p = dailyPoint(p.Date, p.Generation, p.ComputedAt, result)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
