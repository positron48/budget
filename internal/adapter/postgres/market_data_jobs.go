package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

const marketJobColumns = `id,tenant_id,provider,access_scope,COALESCE(instrument_id::text,''),kind,from_at,to_at,status,attempts,COALESCE(lease_token::text,''),COALESCE(lease_until,'0001-01-01'::timestamptz),search_query,instrument_kind`

func scanMarketJob(row pgx.Row) (md.Job, error) {
	var j md.Job
	err := row.Scan(&j.ID, &j.Scope.TenantID, &j.Scope.Provider, &j.Scope.AccessScope, &j.InstrumentID, &j.Kind, &j.From, &j.To, &j.Status, &j.Attempts, &j.LeaseToken, &j.LeaseUntil, &j.SearchQuery, &j.InstrumentKind)
	return j, err
}
func (r *MarketDataRepo) Enqueue(ctx context.Context, s md.Scope, q md.JobRequest) (md.Job, error) {
	q = md.NormalizeRequest(q)
	if err := md.ValidateScope(s); err != nil {
		return md.Job{}, err
	}
	if err := md.ValidateRequest(q); err != nil {
		return md.Job{}, err
	}
	return scanMarketJob(r.pool.DB.QueryRow(ctx, `INSERT INTO market_data_jobs(tenant_id,provider,access_scope,instrument_id,kind,from_at,to_at,search_query,instrument_kind) VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,$7,$8,$9) ON CONFLICT(tenant_id,provider,access_scope,instrument_id,kind,from_at,to_at,search_query,instrument_kind) WHERE status IN ('pending','running') DO UPDATE SET id=market_data_jobs.id RETURNING `+marketJobColumns, s.TenantID, s.Provider, s.AccessScope, q.InstrumentID, q.Kind, q.From, q.To, q.SearchQuery, q.InstrumentKind))
}
func validLeaseDuration(d time.Duration) bool { return d >= 30*time.Second && d <= 10*time.Minute }
func (r *MarketDataRepo) Claim(ctx context.Context, s md.Scope, d time.Duration) (md.Job, error) {
	if err := md.ValidateScope(s); err != nil {
		return md.Job{}, err
	}
	if !validLeaseDuration(d) {
		return md.Job{}, md.ErrInvalid
	}
	tx, err := r.pool.DB.Begin(ctx)
	if err != nil {
		return md.Job{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Crashes on the last attempt still reach a terminal state after lease expiry.
	if _, err = tx.Exec(ctx, `UPDATE market_data_jobs SET status='error',error_code='lease_exhausted',lease_token=NULL,lease_until=NULL,finished_at=clock_timestamp() WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 AND status='running' AND lease_until<=clock_timestamp() AND attempts>=5`, s.TenantID, s.Provider, s.AccessScope); err != nil {
		return md.Job{}, err
	}
	job, err := scanMarketJob(tx.QueryRow(ctx, `WITH candidate AS (SELECT id FROM market_data_jobs WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 AND attempts<5 AND ((status='pending' AND available_at<=clock_timestamp()) OR (status='running' AND lease_until<=clock_timestamp())) ORDER BY priority,available_at,created_at,id FOR UPDATE SKIP LOCKED LIMIT 1) UPDATE market_data_jobs j SET status='running',attempts=attempts+1,lease_token=$4,lease_until=clock_timestamp()+$5*interval '1 second' FROM candidate c WHERE j.id=c.id RETURNING `+prefixedMarketJobColumns(), s.TenantID, s.Provider, s.AccessScope, uuid.NewString(), d.Seconds()))
	if errors.Is(err, pgx.ErrNoRows) {
		if err = tx.Commit(ctx); err != nil {
			return md.Job{}, err
		}
		return md.Job{}, md.ErrNoJob
	}
	if err != nil {
		return md.Job{}, err
	}
	return job, tx.Commit(ctx)
}
func prefixedMarketJobColumns() string {
	return `j.id,j.tenant_id,j.provider,j.access_scope,COALESCE(j.instrument_id::text,''),j.kind,j.from_at,j.to_at,j.status,j.attempts,j.lease_token::text,j.lease_until,j.search_query,j.instrument_kind`
}
func validateJobLease(j md.Job) error {
	if err := md.ValidateScope(j.Scope); err != nil {
		return err
	}
	for _, id := range []string{j.ID, j.LeaseToken} {
		if _, err := uuid.Parse(id); err != nil {
			return md.ErrInvalid
		}
	}
	return nil
}
func (r *MarketDataRepo) Renew(ctx context.Context, j md.Job, d time.Duration) error {
	if err := validateJobLease(j); err != nil {
		return err
	}
	if !validLeaseDuration(d) {
		return md.ErrInvalid
	}
	command, err := r.pool.DB.Exec(ctx, `UPDATE market_data_jobs SET lease_until=clock_timestamp()+$6*interval '1 second' WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 AND id=$4 AND lease_token=$5 AND status='running' AND lease_until>clock_timestamp()`, j.Scope.TenantID, j.Scope.Provider, j.Scope.AccessScope, j.ID, j.LeaseToken, d.Seconds())
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return md.ErrLeaseLost
	}
	return nil
}
func (r *MarketDataRepo) Fail(ctx context.Context, j md.Job, code string, retry bool, delay time.Duration) error {
	if err := validateJobLease(j); err != nil {
		return err
	}
	if (code != "provider_temporary" && code != "provider_rejected" && code != "invalid_response") || delay < 0 || delay > 24*time.Hour {
		return md.ErrInvalid
	}
	command, err := r.pool.DB.Exec(ctx, `UPDATE market_data_jobs SET status=CASE WHEN $7 AND attempts<5 THEN 'pending' ELSE 'error' END,available_at=clock_timestamp()+$8*interval '1 second',error_code=$6,lease_token=NULL,lease_until=NULL,finished_at=CASE WHEN $7 AND attempts<5 THEN NULL ELSE clock_timestamp() END WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 AND id=$4 AND lease_token=$5 AND status='running' AND lease_until>clock_timestamp()`, j.Scope.TenantID, j.Scope.Provider, j.Scope.AccessScope, j.ID, j.LeaseToken, code, retry, delay.Seconds())
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return md.ErrLeaseLost
	}
	return nil
}
func (r *MarketDataRepo) Complete(ctx context.Context, j md.Job, observations []md.Observation) error {
	return r.completeMarketJob(ctx, j, nil, observations)
}
func (r *MarketDataRepo) CompleteWithInstrument(ctx context.Context, j md.Job, instrument md.Instrument, observations []md.Observation) error {
	return r.completeMarketJob(ctx, j, &instrument, observations)
}
func (r *MarketDataRepo) completeMarketJob(ctx context.Context, j md.Job, instrument *md.Instrument, observations []md.Observation) error {
	if err := validateJobLease(j); err != nil {
		return err
	}
	tx, err := r.pool.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Read the persisted request, not caller-supplied range or instrument fields.
	current, err := scanMarketJob(tx.QueryRow(ctx, `SELECT `+marketJobColumns+` FROM market_data_jobs WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 AND id=$4 AND lease_token=$5 AND status='running' AND lease_until>clock_timestamp() FOR UPDATE`, j.Scope.TenantID, j.Scope.Provider, j.Scope.AccessScope, j.ID, j.LeaseToken))
	if errors.Is(err, pgx.ErrNoRows) {
		return md.ErrLeaseLost
	}
	if err != nil {
		return err
	}
	if current.Kind == "catalog_search" || current.Kind == "bond_facts" || current.Kind == "dividend_facts" {
		return md.ErrInvalid
	}
	if instrument != nil {
		if instrument.ID != current.InstrumentID {
			return md.ErrInvalid
		}
		if _, err = saveMarketInstrument(ctx, tx, j.Scope, *instrument); err != nil {
			return err
		}
	}
	for _, o := range observations {
		if o.InstrumentID != current.InstrumentID || o.Kind != current.Kind || o.SourceKind != "exchange" || (current.Kind == "daily_candle" && (o.At.Before(current.From) || o.At.After(current.To))) {
			return md.ErrInvalid
		}
	}
	if len(observations) > 0 {
		if _, err = saveMarketObservations(ctx, tx, j.Scope, observations); err != nil {
			return err
		}
	}
	state := "observed"
	if len(observations) == 0 {
		state = "empty_unexplained"
	}
	if _, err = tx.Exec(ctx, `INSERT INTO market_data_coverage(tenant_id,job_id,state,observation_count) VALUES($1,$2,$3,$4)`, j.Scope.TenantID, j.ID, state, len(observations)); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE market_data_jobs SET status='ready',lease_token=NULL,lease_until=NULL,error_code='',finished_at=clock_timestamp() WHERE tenant_id=$1 AND id=$2`, j.Scope.TenantID, j.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
