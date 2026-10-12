package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

// RegisterFXPolicy is an operator workflow, not a provider-worker capability.
// Explicit reviewed conventions and tenant owner/admin membership are required.
func (r *MarketDataRepo) RegisterFXPolicy(ctx context.Context, t, u string, p md.FXPolicy, expected int64) (md.SavedFXPolicy, error) {
	out := md.SavedFXPolicy{FXPolicy: p, ActorID: u}
	if md.ValidateFXPolicy(p) != nil || expected < 0 || expected == 1<<63-1 {
		return out, md.ErrInvalid
	}
	for _, v := range []string{t, u} {
		if id, e := uuid.Parse(v); e != nil || id.String() != v {
			return out, md.ErrInvalid
		}
	}
	tx, e := r.pool.DB.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var role string
	e = tx.QueryRow(ctx, `SELECT role FROM user_tenants WHERE tenant_id=$1 AND user_id=$2 FOR SHARE`, t, u).Scan(&role)
	if e != nil || (role != "owner" && role != "admin") {
		return out, md.ErrForbidden
	}
	if len(p.Series) > 0 {
		if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "market:"+t+":"+p.Source+":"+p.AccessScope); e != nil {
			return out, e
		}
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "investment-fx:"+t+":"+p.Key); e != nil {
		return out, e
	}
	hash := investmentHash(struct {
		Policy md.FXPolicy
		Actor  string
	}{p, u})
	var version int64
	e = tx.QueryRow(ctx, `SELECT version FROM investment_fx_policies WHERE tenant_id=$1 AND policy_key=$2 FOR UPDATE`, t, p.Key).Scan(&version)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return out, e
	}
	if version != expected {
		var raw []byte
		var oldHash string
		e = tx.QueryRow(ctx, `SELECT data,payload_hash,actor_id,recorded_at FROM investment_fx_policy_revisions WHERE tenant_id=$1 AND policy_key=$2 AND version=$3`, t, p.Key, expected+1).Scan(&raw, &oldHash, &out.ActorID, &out.RecordedAt)
		if e == nil && oldHash == hash {
			out.Version = expected + 1
			e = json.Unmarshal(raw, &out.FXPolicy)
			return out, e
		}
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return out, e
		}
		return out, md.ErrConflict
	}
	out.Version = version + 1
	raw, e := json.Marshal(p)
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO investment_fx_policies(tenant_id,policy_key,version) VALUES($1,$2,$3) ON CONFLICT(tenant_id,policy_key) DO UPDATE SET version=EXCLUDED.version`, t, p.Key, out.Version); e != nil {
		return out, e
	}
	e = tx.QueryRow(ctx, `INSERT INTO investment_fx_policy_revisions(tenant_id,policy_key,version,data,payload_hash,actor_id) VALUES($1,$2,$3,$4,$5,$6) RETURNING recorded_at`, t, p.Key, out.Version, raw, hash, u).Scan(&out.RecordedAt)
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO investment_fx_changes(tenant_id,policy_key,policy_version) VALUES($1,$2,$3)`, t, p.Key, out.Version); e != nil {
		return out, e
	}
	if e = publishStoredFXSeries(ctx, tx, t, out); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}

// SaveFXPoints is for a trusted source adapter bound to an approved policy
// revision. A publication cannot alter or implicitly upgrade that policy.
func (r *MarketDataRepo) SaveFXPoints(ctx context.Context, t, key string, version int64, points []md.FXPoint) ([]md.SavedFXPoint, error) {
	tx, e := r.pool.DB.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	out, e := saveFXPoints(ctx, tx, t, key, version, points)
	if e != nil {
		return nil, e
	}
	return out, tx.Commit(ctx)
}
func saveFXPoints(ctx context.Context, tx pgx.Tx, t, key string, version int64, points []md.FXPoint) ([]md.SavedFXPoint, error) {
	if id, e := uuid.Parse(t); e != nil || id.String() != t || version < 1 || len(points) < 1 || len(points) > 1000 {
		return nil, md.ErrInvalid
	}
	normalized := make([]md.FXPoint, len(points))
	seen := map[string]bool{}
	for i, p := range points {
		v, e := md.NormalizeFXPoint(p)
		if e != nil {
			return nil, e
		}
		identity := v.From + "/" + v.To + "/" + v.At.Format(time.RFC3339Nano)
		if seen[identity] {
			return nil, md.ErrInvalid
		}
		seen[identity] = true
		normalized[i] = v
	}
	var e error
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "investment-fx:"+t+":"+key); e != nil {
		return nil, e
	}
	var current int64
	var enabled bool
	e = tx.QueryRow(ctx, `SELECT version,enabled FROM investment_fx_policies WHERE tenant_id=$1 AND policy_key=$2 FOR SHARE`, t, key).Scan(&current, &enabled)
	if errors.Is(e, pgx.ErrNoRows) || e == nil && !enabled {
		return nil, md.ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	if current != version {
		return nil, md.ErrConflict
	}
	out := make([]md.SavedFXPoint, 0, len(points))
	for _, p := range normalized {
		point := md.SavedFXPoint{FXPoint: p, PolicyKey: key, PolicyVersion: version}
		raw, e := json.Marshal(p)
		if e != nil {
			return nil, e
		}
		hash := investmentHash(p)
		var oldHash string
		e = tx.QueryRow(ctx, `SELECT id,revision,payload_hash,recorded_at FROM investment_fx_points WHERE tenant_id=$1 AND policy_key=$2 AND policy_version=$3 AND from_currency=$4 AND to_currency=$5 AND effective_at=$6 FOR UPDATE`, t, key, version, p.From, p.To, p.At).Scan(&point.ID, &point.Revision, &oldHash, &point.RecordedAt)
		if errors.Is(e, pgx.ErrNoRows) {
			e = tx.QueryRow(ctx, `INSERT INTO investment_fx_points(tenant_id,policy_key,policy_version,from_currency,to_currency,effective_at,revision,data,payload_hash) VALUES($1,$2,$3,$4,$5,$6,1,$7,$8) RETURNING id,revision,recorded_at`, t, key, version, p.From, p.To, p.At, raw, hash).Scan(&point.ID, &point.Revision, &point.RecordedAt)
		} else if e == nil && oldHash != hash {
			e = tx.QueryRow(ctx, `UPDATE investment_fx_points SET revision=revision+1,data=$3,payload_hash=$4,recorded_at=clock_timestamp() WHERE tenant_id=$1 AND id=$2 RETURNING revision,recorded_at`, t, point.ID, raw, hash).Scan(&point.Revision, &point.RecordedAt)
		}
		if e != nil {
			return nil, e
		}
		if oldHash != hash {
			if _, e = tx.Exec(ctx, `INSERT INTO investment_fx_point_revisions(tenant_id,point_id,revision,data,recorded_at) VALUES($1,$2,$3,$4,$5)`, t, point.ID, point.Revision, raw, point.RecordedAt); e != nil {
				return nil, e
			}
			if _, e = tx.Exec(ctx, `INSERT INTO investment_fx_changes(tenant_id,policy_key,policy_version,point_id,point_revision,affected_at) VALUES($1,$2,$3,$4,$5,$6)`, t, key, version, point.ID, point.Revision, p.At); e != nil {
				return nil, e
			}
		}
		out = append(out, point)
	}
	return out, nil
}

// ReadFXSnapshot selects the policy and point revisions in one consistent DB
// snapshot. Current entitlement applies even to historical reconstruction.
// Expired points are retained so the resolver cannot resurrect older quotes.
func (r *MarketDataRepo) ReadFXSnapshot(ctx context.Context, t, key string, cutoff, knowledge time.Time) (md.SavedFXPolicy, []md.SavedFXPoint, error) {
	var policy md.SavedFXPolicy
	if id, e := uuid.Parse(t); e != nil || id.String() != t || cutoff.IsZero() {
		return policy, nil, md.ErrInvalid
	}
	tx, e := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return policy, nil, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	policy, points, e := readFXSnapshot(ctx, tx, t, key, cutoff, knowledge)
	if e != nil {
		return policy, nil, e
	}
	return policy, points, tx.Commit(ctx)
}

func readFXSnapshot(ctx context.Context, tx pgx.Tx, t, key string, cutoff, knowledge time.Time) (md.SavedFXPolicy, []md.SavedFXPoint, error) {
	var policy md.SavedFXPolicy
	var e error
	var know any
	if !knowledge.IsZero() {
		know = knowledge
	}
	var raw []byte
	e = tx.QueryRow(ctx, `SELECT h.data,h.version,h.actor_id,h.recorded_at FROM investment_fx_policies p JOIN LATERAL (SELECT * FROM investment_fx_policy_revisions WHERE tenant_id=p.tenant_id AND policy_key=p.policy_key AND ($3::timestamptz IS NULL OR recorded_at<=$3) ORDER BY version DESC LIMIT 1) h ON true WHERE p.tenant_id=$1 AND p.policy_key=$2 AND p.enabled`, t, key, know).Scan(&raw, &policy.Version, &policy.ActorID, &policy.RecordedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		return policy, nil, md.ErrNotFound
	}
	if e != nil {
		return policy, nil, e
	}
	if e = json.Unmarshal(raw, &policy.FXPolicy); e != nil {
		return policy, nil, e
	}
	if len(policy.Series) > 0 {
		var enabled bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM market_data_sources WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 AND enabled)`, t, policy.Source, policy.AccessScope).Scan(&enabled); e != nil {
			return policy, nil, e
		}
		if !enabled {
			return policy, nil, md.ErrNotFound
		}
	}
	rows, e := tx.Query(ctx, `SELECT p.id,h.revision,h.data,h.recorded_at FROM investment_fx_points p JOIN LATERAL (SELECT * FROM investment_fx_point_revisions WHERE tenant_id=p.tenant_id AND point_id=p.id AND ($5::timestamptz IS NULL OR recorded_at<=$5) ORDER BY revision DESC LIMIT 1) h ON true WHERE p.tenant_id=$1 AND p.policy_key=$2 AND p.policy_version=$3 AND p.effective_at<=$4 ORDER BY p.effective_at,p.from_currency,p.to_currency LIMIT 10001`, t, key, policy.Version, cutoff, know)
	if e != nil {
		return policy, nil, e
	}
	result := []md.SavedFXPoint{}
	for rows.Next() {
		p := md.SavedFXPoint{PolicyKey: key, PolicyVersion: policy.Version}
		if e = rows.Scan(&p.ID, &p.Revision, &raw, &p.RecordedAt); e != nil {
			rows.Close()
			return policy, nil, e
		}
		if e = json.Unmarshal(raw, &p.FXPoint); e != nil {
			rows.Close()
			return policy, nil, e
		}
		result = append(result, p)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return policy, nil, e
	}
	if len(result) > 10000 {
		return policy, nil, md.ErrLimit
	}
	return policy, result, nil
}
