package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"github.com/positron48/budget/internal/usecase/workerhealth"
)

// ReadAlphaFXWorkerHealth reads only the latest two eligible fixings. Loop
// success never substitutes for evidence freshness, including after a restart.
func (r *MarketDataRepo) ReadAlphaFXWorkerHealth(ctx context.Context, tenant, key string, now time.Time) (workerhealth.Snapshot, error) {
	enabled := false
	out := workerhealth.Snapshot{SourceEnabled: &enabled, FXStates: map[string]int64{"fresh": 0, "expired": 0, "missing": 2}}
	var version int64
	var raw []byte
	err := r.pool.DB.QueryRow(ctx, `SELECT p.version,r.data FROM investment_fx_policies p JOIN investment_fx_policy_revisions r ON r.tenant_id=p.tenant_id AND r.policy_key=p.policy_key AND r.version=p.version WHERE p.tenant_id=$1 AND p.policy_key=$2 AND p.enabled`, tenant, key).Scan(&version, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	var policy md.FXPolicy
	if json.Unmarshal(raw, &policy) != nil || policy.Alpha == nil || md.ValidateFXPolicy(policy) != nil {
		return out, md.ErrInvalid
	}
	enabled = true
	rows, err := r.pool.DB.Query(ctx, `SELECT DISTINCT ON (from_currency) data FROM investment_fx_points WHERE tenant_id=$1 AND policy_key=$2 AND policy_version=$3 AND from_currency IN ('USD','EUR') AND to_currency='RUB' AND effective_at<=$4 ORDER BY from_currency,effective_at DESC`, tenant, key, version, now)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var data []byte
		if err = rows.Scan(&data); err != nil {
			return out, err
		}
		var point md.FXPoint
		if json.Unmarshal(data, &point) != nil {
			return out, md.ErrInvalid
		}
		point, err = md.NormalizeFXPoint(point)
		if err != nil {
			return out, err
		}
		if point.Withdrawn || point.AvailableAt.After(now) {
			continue
		}
		out.FXStates["missing"]--
		if now.Before(point.ValidUntil) {
			out.FXStates["fresh"]++
		} else {
			out.FXStates["expired"]++
		}
	}
	return out, rows.Err()
}
