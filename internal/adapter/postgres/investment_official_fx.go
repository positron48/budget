package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"time"
)

// SaveOfficialFX publishes the whole fetched CBR range in one transaction.
// Version fencing prevents an obsolete importer from writing a changed policy.
func (r *MarketDataRepo) SaveOfficialFX(ctx context.Context, t, key string, version int64, points []md.FXPoint) error {
	if len(points) < 2 || len(points) > 7322 {
		return md.ErrInvalid
	}
	tx, e := r.pool.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "investment-fx:"+t+":"+key); e != nil {
		return e
	}
	var current int64
	var raw []byte
	e = tx.QueryRow(ctx, `SELECT p.version,r.data FROM investment_fx_policies p JOIN investment_fx_policy_revisions r ON r.tenant_id=p.tenant_id AND r.policy_key=p.policy_key AND r.version=p.version WHERE p.tenant_id=$1 AND p.policy_key=$2 AND p.enabled FOR SHARE OF p`, t, key).Scan(&current, &raw)
	if errors.Is(e, pgx.ErrNoRows) {
		return md.ErrNotFound
	}
	if e != nil {
		return e
	}
	if current != version {
		return md.ErrConflict
	}
	var p md.FXPolicy
	if e = json.Unmarshal(raw, &p); e != nil {
		return e
	}
	from, to := points[0].At.Add(3*time.Hour), points[0].At.Add(3*time.Hour)
	for _, point := range points {
		day := point.At.Add(3 * time.Hour)
		if day.Before(from) {
			from = day
		}
		if day.After(to) {
			to = day
		}
	}
	today := time.Now().UTC().Add(3 * time.Hour).Truncate(24 * time.Hour)
	if to.After(today) || md.ValidateOfficialFXPoints(p, points, from, to) != nil {
		return md.ErrInvalid
	}
	for len(points) > 0 {
		n := min(len(points), 1000)
		if _, e = saveFXPoints(ctx, tx, t, key, version, points[:n]); e != nil {
			return e
		}
		points = points[n:]
	}
	return tx.Commit(ctx)
}
