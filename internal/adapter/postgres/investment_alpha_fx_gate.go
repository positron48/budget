package postgres

import (
	"context"
	"encoding/hex"
	"errors"

	"github.com/jackc/pgx/v5"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

// ReserveAlphaFXBatch admits at most one two-request batch per credential per
// six hours across replicas/restarts. Failed/partial attempts consume the slot.
// This caps this worker at eight requests per 24 hours without a new schema.
// Standalone probes and other applications must be budgeted independently.
func (r *MarketDataRepo) ReserveAlphaFXBatch(ctx context.Context, key string) (bool, error) {
	decoded, err := hex.DecodeString(key)
	if err != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != key {
		return false, md.ErrInvalid
	}
	var admitted int
	err = r.pool.DB.QueryRow(ctx, `INSERT INTO market_provider_limits(credential_key,next_at) VALUES($1,clock_timestamp()+interval '6 hours') ON CONFLICT(credential_key) DO UPDATE SET next_at=clock_timestamp()+interval '6 hours' WHERE market_provider_limits.next_at<=clock_timestamp() RETURNING 1`, key).Scan(&admitted)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}
