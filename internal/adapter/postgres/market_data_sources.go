package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

// ConfigureSource is operator-only; deliberately not exposed through public RPC.
func (r *MarketDataRepo) ConfigureSource(ctx context.Context, s md.Scope, title string, enabled bool) error {
	if err := md.ValidateScope(s); err != nil {
		return err
	}
	title = strings.TrimSpace(title)
	if len(title) == 0 || len(title) > 200 {
		return md.ErrInvalid
	}
	_, err := r.pool.DB.Exec(ctx, `INSERT INTO market_data_sources(tenant_id,provider,access_scope,title,enabled) VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,provider,access_scope) DO UPDATE SET title=EXCLUDED.title,enabled=EXCLUDED.enabled`, s.TenantID, s.Provider, s.AccessScope, title, enabled)
	return err
}
func (r *MarketDataRepo) Sources(ctx context.Context, tenant string) ([]md.Source, error) {
	rows, err := r.pool.DB.Query(ctx, `SELECT provider,access_scope,title FROM market_data_sources WHERE tenant_id=$1 AND enabled ORDER BY provider,access_scope LIMIT 101`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []md.Source{}
	for rows.Next() {
		s := md.Source{Scope: md.Scope{TenantID: tenant}}
		if err = rows.Scan(&s.Provider, &s.AccessScope, &s.Title); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	if len(result) > 100 {
		return nil, md.ErrLimit
	}
	return result, rows.Err()
}
func (r *MarketDataRepo) SourceEnabled(ctx context.Context, s md.Scope) (bool, error) {
	var enabled bool
	err := r.pool.DB.QueryRow(ctx, `SELECT enabled FROM market_data_sources WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3`, s.TenantID, s.Provider, s.AccessScope).Scan(&enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return enabled, err
}
func (r *MarketDataRepo) RequestRefresh(ctx context.Context, s md.Scope, q md.JobRequest) (md.Job, error) {
	q = md.NormalizeRequest(q)
	if err := md.ValidateScope(s); err != nil {
		return md.Job{}, err
	}
	if err := md.ValidateRequest(q); err != nil {
		return md.Job{}, err
	}
	tx, err := r.pool.DB.Begin(ctx)
	if err != nil {
		return md.Job{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "market-refresh:"+s.TenantID); err != nil {
		return md.Job{}, err
	}
	var enabled bool
	err = tx.QueryRow(ctx, `SELECT enabled FROM market_data_sources WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 FOR SHARE`, s.TenantID, s.Provider, s.AccessScope).Scan(&enabled)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !enabled {
		return md.Job{}, md.ErrNotFound
	}
	if err != nil {
		return md.Job{}, err
	}
	job, err := scanMarketJob(tx.QueryRow(ctx, `SELECT `+marketJobColumns+` FROM market_data_jobs WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 AND instrument_id IS NOT DISTINCT FROM NULLIF($4,'')::uuid AND kind=$5 AND from_at=$6 AND to_at=$7 AND search_query=$8 AND instrument_kind=$9 AND (status IN ('pending','running') OR created_at>clock_timestamp()-interval '1 minute') ORDER BY created_at DESC LIMIT 1`, s.TenantID, s.Provider, s.AccessScope, q.InstrumentID, q.Kind, q.From, q.To, q.SearchQuery, q.InstrumentKind))
	if err == nil {
		if err = touchMarketSubscription(ctx, tx, s, q); err != nil {
			return md.Job{}, err
		}
		return job, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return md.Job{}, err
	}
	var active, recent int
	if err = tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE status IN ('pending','running')),count(*) FILTER(WHERE created_at>clock_timestamp()-interval '1 minute') FROM market_data_jobs WHERE tenant_id=$1`, s.TenantID).Scan(&active, &recent); err != nil {
		return md.Job{}, err
	}
	if active >= 100 || recent >= 10 {
		return md.Job{}, md.ErrLimit
	}
	job, err = scanMarketJob(tx.QueryRow(ctx, `INSERT INTO market_data_jobs(tenant_id,provider,access_scope,instrument_id,kind,from_at,to_at,search_query,instrument_kind) VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,$7,$8,$9) ON CONFLICT(tenant_id,provider,access_scope,instrument_id,kind,from_at,to_at,search_query,instrument_kind) WHERE status IN ('pending','running') DO UPDATE SET id=market_data_jobs.id RETURNING `+marketJobColumns, s.TenantID, s.Provider, s.AccessScope, q.InstrumentID, q.Kind, q.From, q.To, q.SearchQuery, q.InstrumentKind))
	if err != nil {
		return md.Job{}, err
	}
	if err = touchMarketSubscription(ctx, tx, s, q); err != nil {
		return md.Job{}, err
	}
	return job, tx.Commit(ctx)
}
func (r *MarketDataRepo) JobStatus(ctx context.Context, tenant, id string) (md.JobStatus, error) {
	var j md.JobStatus
	err := r.pool.DB.QueryRow(ctx, `SELECT j.id,j.tenant_id,j.provider,j.access_scope,COALESCE(j.instrument_id::text,''),j.kind,j.from_at,j.to_at,j.status,j.attempts,j.error_code,j.available_at,COALESCE(c.state,''),COALESCE(c.observation_count,0),j.search_query,j.instrument_kind,COALESCE(c.instrument_count,0) FROM market_data_jobs j JOIN market_data_sources s ON s.tenant_id=j.tenant_id AND s.provider=j.provider AND s.access_scope=j.access_scope AND s.enabled LEFT JOIN market_data_coverage c ON c.tenant_id=j.tenant_id AND c.job_id=j.id WHERE j.tenant_id=$1 AND j.id=$2`, tenant, id).Scan(&j.ID, &j.Scope.TenantID, &j.Scope.Provider, &j.Scope.AccessScope, &j.InstrumentID, &j.Kind, &j.From, &j.To, &j.Status, &j.Attempts, &j.ErrorCode, &j.AvailableAt, &j.CoverageState, &j.ObservationCount, &j.SearchQuery, &j.InstrumentKind, &j.InstrumentCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return j, md.ErrNotFound
	}
	return j, err
}
