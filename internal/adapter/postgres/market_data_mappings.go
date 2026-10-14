package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func (r *MarketDataRepo) GetMapping(ctx context.Context, tenant, key string) (md.Mapping, error) {
	var raw []byte
	err := r.pool.DB.QueryRow(ctx, `SELECT data FROM investment_market_mappings WHERE tenant_id=$1 AND instrument_key=$2`, tenant, key).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return md.Mapping{}, md.ErrNotFound
	}
	var out md.Mapping
	if err == nil {
		err = json.Unmarshal(raw, &out)
	}
	return out, err
}

// SaveMapping requires application authorization. All schedule revisions and
// invalidation are published together; source/catalog checks share this TX.
func (r *MarketDataRepo) SaveMapping(ctx context.Context, tenant, actor string, c md.MappingChange) (md.Mapping, error) {
	var out md.Mapping
	if e := md.ValidateMappingChange(tenant, c); e != nil {
		return out, e
	}
	if id, e := uuid.Parse(actor); e != nil || id.String() != actor {
		return out, md.ErrInvalid
	}
	if id, e := uuid.Parse(tenant); e != nil || id.String() != tenant {
		return out, md.ErrInvalid
	}
	tx, err := r.pool.DB.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	out, err = saveMarketMapping(ctx, tx, tenant, actor, c)
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

func saveMarketMapping(ctx context.Context, tx pgx.Tx, tenant, actor string, c md.MappingChange) (md.Mapping, error) {
	var out md.Mapping
	if e := md.ValidateMappingChange(tenant, c); e != nil {
		return out, e
	}
	if id, e := uuid.Parse(actor); e != nil || id.String() != actor {
		return out, md.ErrInvalid
	}
	var err error
	// Tenant lock also serializes request keys across different instrument keys.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "market-mapping:"+tenant); err != nil {
		return out, err
	}
	hash := investmentHash(struct {
		Actor  string
		Change md.MappingChange
	}{actor, c})
	var oldHash string
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT request_hash,data FROM investment_market_mapping_revisions WHERE tenant_id=$1 AND request_key=$2`, tenant, c.RequestKey).Scan(&oldHash, &raw)
	if err == nil {
		if oldHash != hash {
			return out, md.ErrConflict
		}
		err = json.Unmarshal(raw, &out)
		return out, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	var old md.Mapping
	err = tx.QueryRow(ctx, `SELECT data FROM investment_market_mappings WHERE tenant_id=$1 AND instrument_key=$2 FOR UPDATE`, tenant, c.InstrumentKey).Scan(&raw)
	if err == nil {
		if err = json.Unmarshal(raw, &old); err != nil {
			return out, err
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	if old.Version != c.ExpectedVersion {
		return out, md.ErrConflict
	}
	current := slices.Clone(c.Periods)
	history := slices.Clone(old.HistoricalPeriods)
	if c.HistoricalPeriods != nil {
		history = slices.Clone(*c.HistoricalPeriods)
	}
	for _, schedule := range [][]md.MappingPeriod{current, history} {
		for i := range schedule {
			p := &schedule[i]
			var enabled bool
			err = tx.QueryRow(ctx, `SELECT enabled FROM market_data_sources WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 FOR SHARE`, tenant, p.Provider, p.AccessScope).Scan(&enabled)
			if errors.Is(err, pgx.ErrNoRows) || err == nil && !enabled {
				return out, md.ErrNotFound
			}
			if err != nil {
				return out, err
			}
			var data []byte
			err = tx.QueryRow(ctx, `SELECT data FROM market_provider_instruments WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 AND instrument_id=$4 FOR SHARE`, tenant, p.Provider, p.AccessScope, p.InstrumentID).Scan(&data)
			if errors.Is(err, pgx.ErrNoRows) {
				return out, md.ErrNotFound
			}
			if err != nil {
				return out, err
			}
			var instrument md.Instrument
			if json.Unmarshal(data, &instrument) != nil || md.ValidateInstrument(instrument) != nil || instrument.ID != p.InstrumentID || instrument.Kind != p.Kind {
				return out, md.ErrInvalid
			}
			if p.Provider == "moex" || p.Provider == "twelvedata" {
				if !instrument.Detailed || instrument.Currency != p.Currency {
					return out, md.ErrInvalid
				}
				p.NativeInstrument = &instrument
			}
		}
	}
	out = md.Mapping{InstrumentKey: c.InstrumentKey, Version: old.Version + 1, Periods: current, HistoricalPeriods: history, ActorID: actor, Reason: c.Reason}
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&out.RecordedAt); err != nil {
		return out, err
	}
	raw, err = json.Marshal(out)
	if err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO investment_market_mappings(tenant_id,instrument_key,version,data) VALUES($1,$2,$3,$4) ON CONFLICT(tenant_id,instrument_key) DO UPDATE SET version=EXCLUDED.version,data=EXCLUDED.data`, tenant, c.InstrumentKey, out.Version, raw); err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO investment_market_mapping_revisions(tenant_id,instrument_key,version,request_key,request_hash,data,actor_id,reason,recorded_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, tenant, c.InstrumentKey, out.Version, c.RequestKey, hash, raw, actor, c.Reason, out.RecordedAt); err != nil {
		return out, err
	}
	var affected *time.Time
	for _, schedule := range [][]md.MappingPeriod{old.Periods, c.Periods, old.HistoricalPeriods, history} {
		for _, p := range schedule {
			if affected == nil || p.From.Before(*affected) {
				v := p.From
				affected = &v
			}
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO investment_market_mapping_changes(tenant_id,instrument_key,version,affected_from) VALUES($1,$2,$3,$4)`, tenant, c.InstrumentKey, out.Version, affected); err != nil {
		return out, err
	}
	return out, nil
}
