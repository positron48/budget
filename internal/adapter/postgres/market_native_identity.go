package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/positron48/budget/internal/adapter/nativehistory"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

// NativeMarketInstrument can recover query identity from canonical mappings
// after licensed catalog/cache removal. It never searches by an economic ticker.
func (r *MarketDataRepo) NativeMarketInstrument(ctx context.Context, s md.Scope, id string) (md.Instrument, error) {
	if md.ValidateScope(s) != nil || s.Provider == "tinvest" || md.ValidateRequest(md.JobRequest{InstrumentID: id, Kind: "last_price"}) != nil {
		return md.Instrument{}, md.ErrInvalid
	}
	var raw []byte
	e := r.pool.DB.QueryRow(ctx, `SELECT i.data FROM market_provider_instruments i JOIN market_data_sources s USING(tenant_id,provider,access_scope) WHERE i.tenant_id=$1 AND i.provider=$2 AND i.access_scope=$3 AND i.instrument_id=$4 AND s.enabled`, s.TenantID, s.Provider, s.AccessScope, id).Scan(&raw)
	if e == nil {
		var i md.Instrument
		if json.Unmarshal(raw, &i) != nil || i.ID != id || nativehistory.Identity(s.Provider, i) != nil {
			return i, md.ErrInvalid
		}
		return i, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return md.Instrument{}, e
	}
	var enabled bool
	if e = r.pool.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM market_data_sources WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 AND enabled)`, s.TenantID, s.Provider, s.AccessScope).Scan(&enabled); e != nil {
		return md.Instrument{}, e
	}
	if enabled {
		for _, b := range nativehistory.Benchmarks() {
			if b.Provider == s.Provider && b.AccessScope == s.AccessScope && b.Instrument.ID == id {
				return b.Instrument, nil
			}
		}
	}
	rows, e := r.pool.DB.Query(ctx, `SELECT p->'NativeInstrument' FROM investment_market_mappings m JOIN market_data_sources s ON s.tenant_id=m.tenant_id AND s.provider=$2 AND s.access_scope=$3 AND s.enabled CROSS JOIN LATERAL jsonb_array_elements(COALESCE(m.data->'HistoricalPeriods','[]'::jsonb)) p WHERE m.tenant_id=$1 AND p->>'Provider'=$2 AND p->>'AccessScope'=$3 AND p->>'InstrumentID'=$4 AND p ? 'NativeInstrument' ORDER BY m.instrument_key`, s.TenantID, s.Provider, s.AccessScope, id)
	if e != nil {
		return md.Instrument{}, e
	}
	defer rows.Close()
	var found md.Instrument
	for rows.Next() {
		var i md.Instrument
		if e = rows.Scan(&raw); e != nil {
			return found, e
		}
		if json.Unmarshal(raw, &i) != nil || i.ID != id || nativehistory.Identity(s.Provider, i) != nil {
			return found, md.ErrInvalid
		}
		if found.ID != "" && (found.Ticker != i.Ticker || found.ClassCode != i.ClassCode || found.ISIN != i.ISIN || found.Currency != i.Currency || found.Kind != i.Kind) {
			return found, md.ErrInvalid
		}
		found = i
	}
	if e = rows.Err(); e != nil {
		return found, e
	}
	if found.ID == "" {
		return found, md.ErrNotFound
	}
	return found, nil
}
