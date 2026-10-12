package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
)

// EnqueueDailyNAV accepts trusted convention definitions only. Each day is a
// durable checkpoint; a restart never requires repeating a completed range.
func (r *MarketDataRepo) EnqueueDailyNAV(ctx context.Context, q nav.Request, from, to string) (int, error) {
	start, e := time.Parse("2006-01-02", from)
	if e != nil {
		return 0, d.ErrInvestmentInvalid
	}
	end, e := time.Parse("2006-01-02", to)
	if e != nil || end.Before(start) || end.Sub(start) > 3660*24*time.Hour {
		return 0, d.ErrInvestmentInvalid
	}
	cutoff, e := nav.DayCutoff(to)
	if e != nil || cutoff.After(time.Now()) {
		return 0, d.ErrInvestmentInvalid
	}
	// Validate membership, ledger ownership and conventions using the same reader
	// as execution. This does not publish or post financial operations.
	q.At = cutoff
	if _, e = r.ReadInvestmentValuation(ctx, q); e != nil {
		return 0, e
	}
	tx, e := r.pool.DB.Begin(ctx)
	if e != nil {
		return 0, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	count, e := enqueueDailyNAV(ctx, tx, q, from, to, false)
	if e != nil {
		return 0, e
	}
	return count, tx.Commit(ctx)
}

// Caller has validated the request and range; this function performs only the
// transactional scheduling writes, shared by portfolio save and internal import.
func enqueueDailyNAV(ctx context.Context, tx pgx.Tx, q nav.Request, from, to string, portfolioManaged bool) (int, error) {
	start, _ := time.Parse(time.DateOnly, from)
	end, _ := time.Parse(time.DateOnly, to)
	key, _, e := navSeries(q)
	if e != nil {
		return 0, e
	}
	q.At = time.Time{}
	raw, e := json.Marshal(q)
	if e != nil {
		return 0, e
	}
	// Range registration and initial jobs commit together. The scheduler starts
	// at zero so a concurrent change during initial validation cannot be lost.
	if _, e = tx.Exec(ctx, `INSERT INTO investment_nav_watches(tenant_id,actor_id,series_key,request,from_date,to_date,portfolio_managed) VALUES($1,$2,$3,$4,$5::date,$6::date,$7)
 ON CONFLICT(tenant_id,actor_id,series_key) DO UPDATE SET from_date=LEAST(investment_nav_watches.from_date,EXCLUDED.from_date),to_date=GREATEST(investment_nav_watches.to_date,EXCLUDED.to_date),cursor_generation=0,enabled=true,portfolio_managed=investment_nav_watches.portfolio_managed AND EXCLUDED.portfolio_managed`, q.TenantID, q.UserID, key, raw, from, to, portfolioManaged); e != nil {
		return 0, e
	}
	count := 0
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		tag, err := tx.Exec(ctx, `INSERT INTO investment_nav_jobs(tenant_id,actor_id,series_key,request,report_date) VALUES($1,$2,$3,$4,$5::date) ON CONFLICT DO NOTHING`, q.TenantID, q.UserID, key, raw, day.Format("2006-01-02"))
		if err != nil {
			return 0, err
		}
		count += int(tag.RowsAffected())
	}
	return count, nil
}

type navJob struct {
	ID, Token, Date string
	Request         nav.Request
	Generation      int64
}

func (r *MarketDataRepo) claimDailyNAV(ctx context.Context, tenant string) (navJob, error) {
	var j navJob
	if id, e := uuid.Parse(tenant); e != nil || id.String() != tenant {
		return j, d.ErrInvestmentInvalid
	}
	tx, e := r.pool.DB.Begin(ctx)
	if e != nil {
		return j, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, e = tx.Exec(ctx, `UPDATE investment_nav_jobs SET status=CASE WHEN requested_generation>claimed_generation THEN 'pending' ELSE 'error' END,attempts=CASE WHEN requested_generation>claimed_generation THEN 0 ELSE attempts END,error_code='lease_exhausted',lease_token=NULL,lease_until=NULL WHERE tenant_id=$1 AND status='running' AND lease_until<=clock_timestamp() AND attempts=5`, tenant); e != nil {
		return j, e
	}
	var raw []byte
	e = tx.QueryRow(ctx, `WITH candidate AS (
 SELECT id FROM investment_nav_jobs WHERE tenant_id=$1 AND attempts<5 AND
 ((status='pending' AND available_at<=clock_timestamp()) OR (status='running' AND lease_until<=clock_timestamp()))
 ORDER BY available_at,created_at,id FOR UPDATE SKIP LOCKED LIMIT 1)
 UPDATE investment_nav_jobs j SET status='running',attempts=attempts+1,claimed_generation=requested_generation,lease_token=$2,lease_until=clock_timestamp()+interval '2 minutes'
 FROM candidate c WHERE j.id=c.id RETURNING j.id,j.lease_token,j.report_date::text,j.request,j.requested_generation`, tenant, uuid.NewString()).Scan(&j.ID, &j.Token, &j.Date, &raw, &j.Generation)
	if errors.Is(e, pgx.ErrNoRows) {
		return j, tx.Commit(ctx)
	}
	if e != nil {
		return j, e
	}
	if e = json.Unmarshal(raw, &j.Request); e != nil {
		return j, e
	}
	return j, tx.Commit(ctx)
}

func (r *MarketDataRepo) finishDailyNAV(ctx context.Context, j navJob, failure error) error {
	code := ""
	retry := false
	if failure != nil {
		code = "calculation_failed"
		retry = true
		if errors.Is(failure, d.ErrInvestmentForbidden) || errors.Is(failure, d.ErrInvestmentInvalid) {
			code = "request_rejected"
			retry = false
		}
		if errors.Is(failure, d.ErrInvestmentConflict) {
			code = "inputs_changed"
		}
	}
	tag, e := r.pool.DB.Exec(ctx, `UPDATE investment_nav_jobs SET status=CASE WHEN requested_generation>$6 AND $4<>'request_rejected' THEN 'pending' WHEN $4='' THEN 'ready' WHEN $5 AND attempts<5 THEN 'pending' ELSE 'error' END,attempts=CASE WHEN requested_generation>$6 AND $4<>'request_rejected' THEN 0 ELSE attempts END,error_code=$4,available_at=clock_timestamp()+interval '30 seconds',lease_token=NULL,lease_until=NULL WHERE tenant_id=$1 AND id=$2 AND lease_token=$3 AND status='running' AND lease_until>clock_timestamp()`, j.Request.TenantID, j.ID, j.Token, code, retry, j.Generation)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return d.ErrInvestmentConflict
	}
	return nil
}

// RunDailyNAVOnce is suitable for a bounded scheduler loop. Publication is
// generation-fenced independently of the job lease; a crash after publication
// safely repeats the identical daily build. Incomplete evidence is a successful
// calculation with explicit gaps, not an endlessly retried transport failure.
func (r *MarketDataRepo) RunDailyNAVOnce(ctx context.Context, tenant string) (bool, error) {
	j, e := r.claimDailyNAV(ctx, tenant)
	if e != nil || j.ID == "" {
		return false, e
	}
	work, cancel := context.WithTimeout(ctx, 90*time.Second)
	_, failure := r.BuildDailyNAV(work, j.Request, j.Date)
	cancel()
	// Parent cancellation leaves the lease for restart recovery, without changing
	// its state using an unbounded detached context.
	if e = r.finishDailyNAV(ctx, j, failure); e != nil {
		return true, e
	}
	return true, failure
}
