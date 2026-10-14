package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

type historicalObservationRow struct {
	InstrumentID string          `json:"instrument_id"`
	Kind         string          `json:"kind"`
	At           time.Time       `json:"observed_at"`
	SourceKind   string          `json:"source_kind"`
	Adjustment   string          `json:"adjustment"`
	Data         json.RawMessage `json:"data"`
	Hash         string          `json:"payload_hash"`
}

func historicalObservationRows(input []md.Observation) ([]byte, error) {
	if len(input) == 0 || len(input) > 1000 {
		return nil, md.ErrInvalid
	}
	rows := make([]historicalObservationRow, 0, len(input))
	for _, o := range input {
		normal, err := md.Normalize(o)
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(normal)
		if err != nil {
			return nil, err
		}
		rows = append(rows, historicalObservationRow{InstrumentID: normal.InstrumentID, Kind: normal.Kind, At: normal.At, SourceKind: normal.SourceKind, Adjustment: normal.Adjustment, Data: raw, Hash: investmentHash(normal)})
	}
	return json.Marshal(rows)
}

// The caller holds the entitlement advisory lock. Parent observations, their
// immutable revisions and the invalidation feed are changed in one statement.
// Identical source rows do not increment revisions or generate invalidations.
func saveHistoricalObservations(ctx context.Context, tx pgx.Tx, scope md.Scope, input []md.Observation) error {
	if err := md.ValidateScope(scope); err != nil {
		return err
	}
	raw, err := historicalObservationRows(input)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, historicalObservationsUpsertSQL, scope.TenantID, scope.Provider, scope.AccessScope, raw)
	return err
}

const historicalObservationsUpsertSQL = `
WITH incoming AS (
 SELECT * FROM jsonb_to_recordset($4::jsonb) AS x(
 instrument_id uuid,kind text,observed_at timestamptz,source_kind text,
 adjustment text,data jsonb,payload_hash text)
), changed AS (
 INSERT INTO market_provider_observations(
 tenant_id,provider,access_scope,instrument_id,kind,observed_at,source_kind,
 adjustment,data,payload_hash,revision)
 SELECT $1::uuid,$2::text,$3::text,instrument_id,kind,observed_at,source_kind,
 adjustment,data,payload_hash,1 FROM incoming
 ON CONFLICT(tenant_id,provider,access_scope,instrument_id,kind,observed_at,source_kind,adjustment)
 DO UPDATE SET data=EXCLUDED.data,payload_hash=EXCLUDED.payload_hash,
 revision=market_provider_observations.revision+1,received_at=clock_timestamp()
 WHERE market_provider_observations.payload_hash IS DISTINCT FROM EXCLUDED.payload_hash
 RETURNING tenant_id,id,revision,data,received_at,observed_at
), archived AS (
 INSERT INTO market_provider_observation_revisions(
 tenant_id,observation_id,revision,data,received_at)
 SELECT tenant_id,id,revision,data,received_at FROM changed
 RETURNING tenant_id,observation_id,revision
)
INSERT INTO market_data_changes(tenant_id,observation_id,revision,affected_at)
SELECT c.tenant_id,c.id,c.revision,c.observed_at FROM changed c
JOIN archived a ON a.tenant_id=c.tenant_id AND a.observation_id=c.id AND a.revision=c.revision`
