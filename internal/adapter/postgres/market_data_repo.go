package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

type MarketDataRepo struct{ pool *Pool }

func NewMarketDataRepo(p *Pool) *MarketDataRepo { return &MarketDataRepo{pool: p} }

// SaveObservations publishes a validated batch and its revision/invalidation
// records in one transaction. Identical retries do not create new revisions.
func (r *MarketDataRepo) SaveObservations(ctx context.Context, scope md.Scope, input []md.Observation) ([]md.SavedObservation, error) {
	tx, err := r.pool.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, err := saveMarketObservations(ctx, tx, scope, input)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func saveMarketObservations(ctx context.Context, tx pgx.Tx, scope md.Scope, input []md.Observation) ([]md.SavedObservation, error) {
	if err := md.ValidateScope(scope); err != nil {
		return nil, err
	}
	if len(input) == 0 || len(input) > 1000 {
		return nil, md.ErrInvalid
	}
	normalized := make([]md.Observation, len(input))
	seen := map[string]bool{}
	for i, o := range input {
		var err error
		normalized[i], err = md.Normalize(o)
		if err != nil {
			return nil, err
		}
		o = normalized[i]
		key := fmt.Sprintf("%s|%s|%s|%s|%s", o.InstrumentID, o.Kind, o.At.Format(time.RFC3339Nano), o.SourceKind, o.Adjustment)
		if seen[key] {
			return nil, md.ErrInvalid
		}
		seen[key] = true
	}
	var err error
	// Serializing one entitlement scope also prevents absent-row insertion races.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "market:"+scope.TenantID+":"+scope.Provider+":"+scope.AccessScope); err != nil {
		return nil, err
	}
	result := make([]md.SavedObservation, 0, len(input))
	for _, o := range normalized {
		raw, err := json.Marshal(o)
		if err != nil {
			return nil, err
		}
		hash := investmentHash(o)
		var id, savedHash string
		var revision int64
		var received time.Time
		err = tx.QueryRow(ctx, `SELECT id,payload_hash,revision,received_at FROM market_provider_observations WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 AND instrument_id=$4 AND kind=$5 AND observed_at=$6 AND source_kind=$7 AND adjustment=$8 FOR UPDATE`, scope.TenantID, scope.Provider, scope.AccessScope, o.InstrumentID, o.Kind, o.At, o.SourceKind, o.Adjustment).Scan(&id, &savedHash, &revision, &received)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		if errors.Is(err, pgx.ErrNoRows) {
			err = tx.QueryRow(ctx, `INSERT INTO market_provider_observations(tenant_id,provider,access_scope,instrument_id,kind,observed_at,source_kind,adjustment,data,payload_hash,revision) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,1) RETURNING id,revision,received_at`, scope.TenantID, scope.Provider, scope.AccessScope, o.InstrumentID, o.Kind, o.At, o.SourceKind, o.Adjustment, raw, hash).Scan(&id, &revision, &received)
		} else if savedHash != hash {
			err = tx.QueryRow(ctx, `UPDATE market_provider_observations SET data=$3,payload_hash=$4,revision=revision+1,received_at=clock_timestamp() WHERE tenant_id=$1 AND id=$2 RETURNING revision,received_at`, scope.TenantID, id, raw, hash).Scan(&revision, &received)
		}
		if err != nil {
			return nil, err
		}
		if savedHash != hash {
			if _, err = tx.Exec(ctx, `INSERT INTO market_provider_observation_revisions(tenant_id,observation_id,revision,data,received_at) VALUES($1,$2,$3,$4,$5)`, scope.TenantID, id, revision, raw, received); err != nil {
				return nil, err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO market_data_changes(tenant_id,observation_id,revision,affected_at) VALUES($1,$2,$3,$4)`, scope.TenantID, id, revision, o.At); err != nil {
				return nil, err
			}
		}
		result = append(result, md.SavedObservation{Observation: o, ID: id, Revision: revision, ReceivedAt: received})
	}
	if err = publishObservedFXSeries(ctx, tx, scope, result); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *MarketDataRepo) Observations(ctx context.Context, scope md.Scope, instrument, kind string, from, to time.Time) ([]md.SavedObservation, error) {
	if err := md.ValidateScope(scope); err != nil {
		return nil, err
	}
	if _, err := uuid.Parse(instrument); err != nil {
		return nil, md.ErrInvalid
	}
	if (kind != "last_price" && kind != "daily_candle") || from.IsZero() || to.Before(from) || to.After(from.AddDate(30, 0, 0)) {
		return nil, md.ErrInvalid
	}
	rows, err := r.pool.DB.Query(ctx, `SELECT id,data,revision,received_at FROM market_provider_observations WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 AND instrument_id=$4 AND kind=$5 AND observed_at BETWEEN $6 AND $7 ORDER BY observed_at,source_kind,adjustment LIMIT 30001`, scope.TenantID, scope.Provider, scope.AccessScope, instrument, kind, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []md.SavedObservation{}
	for rows.Next() {
		var o md.SavedObservation
		var raw []byte
		if err = rows.Scan(&o.ID, &raw, &o.Revision, &o.ReceivedAt); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &o.Observation); err != nil {
			return nil, err
		}
		result = append(result, o)
	}
	if len(result) > 30000 {
		return nil, md.ErrLimit
	}
	return result, rows.Err()
}
