package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"time"
)

const fxSeriesPoliciesSQL = `SELECT p.policy_key,r.version,r.data FROM investment_fx_policies p JOIN investment_fx_policy_revisions r ON r.tenant_id=p.tenant_id AND r.policy_key=p.policy_key AND r.version=p.version WHERE p.tenant_id=$1 AND p.enabled AND r.data->>'Source'=$2 AND r.data->>'AccessScope'=$3 AND r.data ? 'Series' ORDER BY p.policy_key LIMIT 101`

func fxSeriesPolicies(ctx context.Context, tx pgx.Tx, scope md.Scope) ([]md.SavedFXPolicy, error) {
	rows, e := tx.Query(ctx, fxSeriesPoliciesSQL, scope.TenantID, scope.Provider, scope.AccessScope)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var result []md.SavedFXPolicy
	for rows.Next() {
		var p md.SavedFXPolicy
		var raw []byte
		if e = rows.Scan(&p.Key, &p.Version, &raw); e != nil {
			return nil, e
		}
		if e = json.Unmarshal(raw, &p.FXPolicy); e != nil {
			return nil, e
		}
		if e = md.ValidateFXPolicy(p.FXPolicy); e != nil {
			return nil, e
		}
		result = append(result, p)
	}
	if len(result) > 100 {
		return nil, md.ErrLimit
	}
	return result, rows.Err()
}
func publishFXSeries(ctx context.Context, tx pgx.Tx, t string, p md.SavedFXPolicy, observations []md.SavedObservation) error {
	var points []md.FXPoint
	for _, o := range observations {
		for _, s := range p.Series {
			point, matched, e := md.FXSeriesPoint(s, o)
			if e != nil {
				return e
			}
			if matched {
				points = append(points, point)
			}
		}
	}
	for len(points) > 0 {
		size := min(len(points), 1000)
		if _, e := saveFXPoints(ctx, tx, t, p.Key, p.Version, points[:size]); e != nil {
			return e
		}
		points = points[size:]
	}
	return nil
}
func publishObservedFXSeries(ctx context.Context, tx pgx.Tx, scope md.Scope, observations []md.SavedObservation) error {
	policies, e := fxSeriesPolicies(ctx, tx, scope)
	if e != nil {
		return e
	}
	for _, candidate := range policies {
		if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "investment-fx:"+scope.TenantID+":"+candidate.Key); e != nil {
			return e
		}
		// Re-read after the lock. Editing/revoking a source cannot publish under the
		// stale policy seen before waiting for another transaction.
		var p md.SavedFXPolicy
		var raw []byte
		e = tx.QueryRow(ctx, `SELECT r.version,r.data FROM investment_fx_policies p JOIN investment_fx_policy_revisions r ON r.tenant_id=p.tenant_id AND r.policy_key=p.policy_key AND r.version=p.version WHERE p.tenant_id=$1 AND p.policy_key=$2 AND p.enabled AND EXISTS(SELECT 1 FROM market_data_sources WHERE tenant_id=$1 AND provider=$3 AND access_scope=$4 AND enabled)`, scope.TenantID, candidate.Key, scope.Provider, scope.AccessScope).Scan(&p.Version, &raw)
		if errors.Is(e, pgx.ErrNoRows) {
			continue
		}
		if e != nil {
			return e
		}
		if e = json.Unmarshal(raw, &p.FXPolicy); e != nil {
			return e
		}
		if e = md.ValidateFXPolicy(p.FXPolicy); e != nil {
			return e
		}
		if p.Source != scope.Provider || p.AccessScope != scope.AccessScope {
			continue
		}
		if e = publishFXSeries(ctx, tx, scope.TenantID, p, observations); e != nil {
			return e
		}
	}
	return nil
}
func publishStoredFXSeries(ctx context.Context, tx pgx.Tx, t string, p md.SavedFXPolicy) error {
	if len(p.Series) == 0 {
		return nil
	}
	var enabled bool
	if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM market_data_sources WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 AND enabled)`, t, p.Source, p.AccessScope).Scan(&enabled); e != nil {
		return e
	}
	if !enabled {
		return nil
	}
	ids := []string{}
	for _, s := range p.Series {
		ids = append(ids, s.InstrumentID)
	}
	rows, e := tx.Query(ctx, `SELECT id,revision,received_at,data FROM market_provider_observations WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 AND instrument_id::text=ANY($4::text[]) AND kind='daily_candle' ORDER BY instrument_id,observed_at LIMIT 30001`, t, p.Source, p.AccessScope, ids)
	if e != nil {
		return e
	}
	var observations []md.SavedObservation
	for rows.Next() {
		var o md.SavedObservation
		var raw []byte
		if e = rows.Scan(&o.ID, &o.Revision, &o.ReceivedAt, &raw); e == nil {
			e = json.Unmarshal(raw, &o.Observation)
		}
		if e != nil {
			break
		}
		observations = append(observations, o)
	}
	rowErr := rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if rowErr != nil {
		return rowErr
	}
	if len(observations) > 30000 {
		return md.ErrLimit
	}
	return publishFXSeries(ctx, tx, t, p, observations)
}
func fxSeriesHistoryRanges(policies []md.SavedFXPolicy, at time.Time) []md.JobRequest {
	var result []md.JobRequest
	for _, p := range policies {
		for _, s := range p.Series {
			end := at.UTC().Truncate(24 * time.Hour)
			if !s.To.IsZero() && s.To.Before(end) {
				end = s.To.UTC()
			}
			if end.After(s.From) {
				result = append(result, md.JobRequest{InstrumentID: s.InstrumentID, Kind: "daily_candle", From: s.From.UTC(), To: end})
			}
		}
	}
	return result
}
