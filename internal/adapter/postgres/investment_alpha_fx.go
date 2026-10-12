package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	av "github.com/positron48/budget/internal/adapter/alphavantage"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

// SaveAlphaFX revalidates both complete checkpoints under the locked policy and
// publishes all revisions atomically. A failed second series rolls back the first.
// It does not register/enable policies or manufacture missing daily observations.
func (r *MarketDataRepo) SaveAlphaFX(ctx context.Context, tenant, key string, version int64, audits []av.Audit) (int, error) {
	if len(audits) != 2 || version < 1 {
		return 0, md.ErrInvalid
	}
	tx, err := r.pool.DB.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "investment-fx:"+tenant+":"+key); err != nil {
		return 0, err
	}
	var current int64
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT p.version,r.data FROM investment_fx_policies p JOIN investment_fx_policy_revisions r ON r.tenant_id=p.tenant_id AND r.policy_key=p.policy_key AND r.version=p.version WHERE p.tenant_id=$1 AND p.policy_key=$2 AND p.enabled FOR SHARE OF p`, tenant, key).Scan(&current, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, md.ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if current != version {
		return 0, md.ErrConflict
	}
	var policy md.FXPolicy
	if json.Unmarshal(raw, &policy) != nil || policy.Alpha == nil || md.ValidateFXPolicy(policy) != nil {
		return 0, md.ErrInvalid
	}
	seen := map[string]bool{}
	total := 0
	for _, audit := range audits {
		if seen[audit.Pair] || audit.From != audits[0].From || audit.To != audits[0].To || audit.RetrievedAt.After(time.Now().UTC()) {
			return 0, md.ErrInvalid
		}
		seen[audit.Pair] = true
		points, e := av.RetrospectivePoints(policy, audit)
		if e != nil {
			return 0, e
		}
		// A supposedly complete overlapping checkpoint must not silently omit a
		// previously published date. Keep the old state and require review rather
		// than guessing a withdrawal or retaining it inside a successful update.
		from, _ := time.Parse(time.DateOnly, audit.From)
		to, _ := time.Parse(time.DateOnly, audit.To)
		present := make(map[time.Time]bool, len(points))
		for _, point := range points {
			present[point.At] = true
		}
		rows, e := tx.Query(ctx, `SELECT effective_at,data FROM investment_fx_points WHERE tenant_id=$1 AND policy_key=$2 AND policy_version=$3 AND from_currency=$4 AND to_currency='RUB' AND effective_at >= $5 AND effective_at <= $6`, tenant, key, version, points[0].From, from.Add(24*time.Hour), to.Add(24*time.Hour))
		if e != nil {
			return 0, e
		}
		missing := false
		unchanged := map[time.Time]bool{}
		proposed := map[time.Time]md.FXPoint{}
		for _, point := range points {
			proposed[point.At] = point
		}
		for rows.Next() {
			var at time.Time
			var oldRaw []byte
			if e = rows.Scan(&at, &oldRaw); e != nil {
				rows.Close()
				return 0, e
			}
			if !present[at.UTC()] {
				missing = true
			} else {
				var old md.FXPoint
				if json.Unmarshal(oldRaw, &old) != nil {
					rows.Close()
					return 0, md.ErrInvalid
				}
				old, e = md.NormalizeFXPoint(old)
				if e != nil {
					rows.Close()
					return 0, e
				}
				next := proposed[at.UTC()]
				// Preserve the first exact evidence witness while its economic fixing is
				// unchanged. A refreshed response checksum alone must not dirty all NAV.
				unchanged[at.UTC()] = old.From == next.From && old.To == next.To && old.Rate == next.Rate && old.Withdrawn == next.Withdrawn && old.At.Equal(next.At) && old.AvailableAt.Equal(next.AvailableAt) && old.ValidUntil.Equal(next.ValidUntil)
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return 0, e
		}
		if missing {
			return 0, md.ErrInvalid
		}
		total += len(points)
		writes := make([]md.FXPoint, 0, len(points))
		for _, point := range points {
			if !unchanged[point.At] {
				writes = append(writes, point)
			}
		}
		points = writes
		for len(points) > 0 {
			n := min(len(points), 1000)
			if _, err = saveFXPoints(ctx, tx, tenant, key, version, points[:n]); err != nil {
				return 0, err
			}
			points = points[n:]
		}
	}
	return total, tx.Commit(ctx)
}
