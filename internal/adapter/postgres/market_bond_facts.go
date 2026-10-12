package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func (r *MarketDataRepo) CompleteBondFacts(ctx context.Context, j md.Job, f md.BondFacts) error {
	if e := validateJobLease(j); e != nil {
		return e
	}
	normalized, e := md.NormalizeBondFacts(f)
	if e != nil {
		return e
	}
	tx, e := r.pool.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, e := scanMarketJob(tx.QueryRow(ctx, `SELECT `+marketJobColumns+` FROM market_data_jobs WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 AND id=$4 AND lease_token=$5 AND status='running' AND lease_until>clock_timestamp() FOR UPDATE`, j.Scope.TenantID, j.Scope.Provider, j.Scope.AccessScope, j.ID, j.LeaseToken))
	if errors.Is(e, pgx.ErrNoRows) {
		return md.ErrLeaseLost
	}
	if e != nil {
		return e
	}
	if current.Kind != "bond_facts" || normalized.Instrument.ID != current.InstrumentID || !normalized.From.Equal(current.From) || !normalized.To.Equal(current.To) {
		return md.ErrInvalid
	}
	var enabled bool
	e = tx.QueryRow(ctx, `SELECT enabled FROM market_data_sources WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 FOR SHARE`, current.Scope.TenantID, current.Scope.Provider, current.Scope.AccessScope).Scan(&enabled)
	if errors.Is(e, pgx.ErrNoRows) || e == nil && !enabled {
		return md.ErrNotFound
	}
	if e != nil {
		return e
	}
	if _, e = saveMarketInstrument(ctx, tx, current.Scope, normalized.Instrument); e != nil {
		return e
	}
	raw, e := json.Marshal(normalized)
	if e != nil {
		return e
	}
	if len(raw) > 4*1024*1024 {
		return md.ErrLimit
	}
	if _, e = tx.Exec(ctx, `INSERT INTO market_bond_snapshots(tenant_id,job_id,provider,access_scope,instrument_id,from_at,to_at,data,payload_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, current.Scope.TenantID, current.ID, current.Scope.Provider, current.Scope.AccessScope, current.InstrumentID, current.From, current.To, raw, investmentHash(normalized)); e != nil {
		return e
	}
	// Observed records do not establish that a schedule is complete or final.
	count := len(normalized.Coupons) + len(normalized.Events) + len(normalized.Accrued)
	if _, e = tx.Exec(ctx, `INSERT INTO market_data_coverage(tenant_id,job_id,state,observation_count,instrument_count) VALUES($1,$2,'observed',$3,1)`, current.Scope.TenantID, current.ID, count); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE market_data_jobs SET status='ready',lease_token=NULL,lease_until=NULL,error_code='',finished_at=clock_timestamp() WHERE tenant_id=$1 AND id=$2`, current.Scope.TenantID, current.ID); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

// Read only a single immutable snapshot covering the entire requested interval.
// An absent snapshot differs from a saved response with empty schedules.
func (r *MarketDataRepo) BondFacts(ctx context.Context, s md.Scope, q md.JobRequest) (*md.SavedBondFacts, error) {
	if md.ValidateScope(s) != nil || md.ValidateRequest(q) != nil || q.Kind != "bond_facts" {
		return nil, md.ErrInvalid
	}
	out := &md.SavedBondFacts{}
	var raw []byte
	err := r.pool.DB.QueryRow(ctx, `SELECT b.job_id,b.data,b.payload_hash,b.recorded_at FROM market_bond_snapshots b
 JOIN market_data_sources s USING(tenant_id,provider,access_scope)
 WHERE b.tenant_id=$1 AND b.provider=$2 AND b.access_scope=$3 AND b.instrument_id=$4 AND s.enabled
 AND b.from_at<=$5 AND b.to_at>=$6 ORDER BY b.recorded_at DESC,b.job_id DESC LIMIT 1`, s.TenantID, s.Provider, s.AccessScope, q.InstrumentID, q.From, q.To).Scan(&out.ID, &raw, &out.Hash, &out.RecordedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &out.BondFacts); err != nil {
		return nil, err
	}
	// A broader cached response must not leak events outside the requested window.
	coupons := out.Coupons[:0]
	for _, p := range out.Coupons {
		if !p.Date.Before(q.From) && !p.Date.After(q.To) {
			coupons = append(coupons, p)
		}
	}
	out.Coupons = coupons
	events := out.Events[:0]
	for _, p := range out.Events {
		if !p.Date.Before(q.From) && !p.Date.After(q.To) {
			events = append(events, p)
		}
	}
	out.Events = events
	accrued := out.Accrued[:0]
	for _, p := range out.Accrued {
		if !p.Date.Before(q.From) && !p.Date.After(q.To) {
			accrued = append(accrued, p)
		}
	}
	out.Accrued = accrued
	return out, nil
}
