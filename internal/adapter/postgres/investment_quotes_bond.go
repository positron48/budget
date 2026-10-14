package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

const portfolioBondFactsSQL = `SELECT b.job_id,b.data,b.payload_hash,b.recorded_at FROM market_bond_snapshots b
JOIN market_data_sources s USING(tenant_id,provider,access_scope)
WHERE b.tenant_id=$1 AND b.provider=$2 AND b.access_scope=$3 AND b.instrument_id=$4 AND s.enabled
AND b.from_at<=$5 AND b.to_at>=$5 AND b.recorded_at<=$6
ORDER BY b.recorded_at DESC,b.job_id DESC LIMIT 1`

// Use the same read snapshot and entitlement as the selected exchange price.
// A newer saved response with missing nominal does not revive an older response.
func readPortfolioBondFacts(ctx context.Context, tx pgx.Tx, scope md.Scope, instrument string, quoteAt, readAt time.Time) (*md.SavedBondFacts, error) {
	var result md.SavedBondFacts
	var raw []byte
	e := tx.QueryRow(ctx, portfolioBondFactsSQL, scope.TenantID, scope.Provider, scope.AccessScope, instrument, quoteAt.UTC().Truncate(24*time.Hour), readAt).Scan(&result.ID, &raw, &result.Hash, &result.RecordedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	if json.Unmarshal(raw, &result.BondFacts) != nil || result.Hash != investmentHash(result.BondFacts) {
		return nil, d.ErrInvestmentPrecondition
	}
	if _, e = md.NormalizeBondFacts(result.BondFacts); e != nil {
		return nil, d.ErrInvestmentPrecondition
	}
	return &result, nil
}
