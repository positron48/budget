package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

type trackerHistorySeries struct {
	Provider string `json:"provider"`
	Scope    string `json:"scope"`
	ID       string `json:"id"`
}

const trackerHistorySeriesSQL = `WITH requested AS (
 SELECT * FROM jsonb_to_recordset($2::jsonb) AS x(provider text,scope text,id uuid)
)
SELECT o.provider,o.access_scope,o.instrument_id,o.id,o.data,o.revision,o.received_at
FROM requested x
JOIN market_provider_observations o ON o.tenant_id=$1 AND o.provider=x.provider AND o.access_scope=x.scope AND o.instrument_id=x.id
JOIN market_data_sources s ON s.tenant_id=o.tenant_id AND s.provider=o.provider AND s.access_scope=o.access_scope
WHERE o.kind='daily_candle' AND o.source_kind='exchange'
AND o.adjustment IN ('unadjusted','provider_unspecified')
AND o.observed_at BETWEEN $3 AND $4 AND s.enabled
ORDER BY o.provider,o.access_scope,o.instrument_id,o.observed_at,o.id LIMIT 100001`

// A current observation and its immutable latest revision are written together.
// Under the caller's repeatable-read snapshot this is the same evidence as the
// latest-revision lookup, without a round trip per paper and adjustment.
func readTrackerHistorySeries(ctx context.Context, tx pgx.Tx, tenant string, result map[string]trackerHistoryMarket, from, to, readAt time.Time) (map[string]trackerHistoryMarket, error) {
	listings, bonds := []trackerHistorySeries{}, []trackerHistorySeries{}
	seen, bondKeys := map[string]bool{}, map[string]bool{}
	for _, entry := range result {
		for _, p := range entry.mapping.HistoryPeriods() {
			if p.From.After(to) || !p.To.IsZero() && !p.To.After(from) {
				continue
			}
			key := historyPeriodKey(p)
			x := trackerHistorySeries{p.Provider, p.AccessScope, p.InstrumentID}
			if !seen[key] {
				listings = append(listings, x)
				seen[key] = true
			}
			if p.Kind == "bond" && p.Unit == "percent_of_nominal" && !bondKeys[key] {
				bonds = append(bonds, x)
				bondKeys[key] = true
			}
		}
	}
	if len(listings) > 2000 {
		return nil, d.ErrInvestmentLimit
	}
	lower := from.UTC().Truncate(24 * time.Hour).Add(-5 * 24 * time.Hour)
	observations := map[string][]md.ScopedObservation{}
	if len(listings) > 0 {
		raw, e := json.Marshal(listings)
		if e != nil {
			return nil, e
		}
		rows, e := tx.Query(ctx, trackerHistorySeriesSQL, tenant, raw, lower, to)
		if e != nil {
			return nil, e
		}
		count := 0
		for rows.Next() {
			o := md.ScopedObservation{Scope: md.Scope{TenantID: tenant}}
			var id string
			var data []byte
			if e = rows.Scan(&o.Provider, &o.AccessScope, &id, &o.ID, &data, &o.Revision, &o.ReceivedAt); e != nil {
				rows.Close()
				return nil, e
			}
			if e = json.Unmarshal(data, &o.Observation); e != nil {
				rows.Close()
				return nil, e
			}
			if o.InstrumentID != id {
				rows.Close()
				return nil, d.ErrInvestmentPrecondition
			}
			key := o.Provider + "/" + o.AccessScope + "/" + id
			if o.Adjustment == "provider_unspecified" {
				key += "/unknown"
			}
			observations[key] = append(observations[key], o)
			count++
			if count > 100000 || len(observations[key]) > 10000 {
				rows.Close()
				return nil, d.ErrInvestmentLimit
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
	}
	facts := map[string][]md.SavedBondFacts{}
	if len(bonds) > 0 {
		raw, e := json.Marshal(bonds)
		if e != nil {
			return nil, e
		}
		rows, e := tx.Query(ctx, `WITH requested AS (SELECT * FROM jsonb_to_recordset($2::jsonb) AS x(provider text,scope text,id uuid))
SELECT b.provider,b.access_scope,b.instrument_id,b.job_id,b.data,b.payload_hash,b.recorded_at
FROM requested x JOIN market_bond_snapshots b ON b.tenant_id=$1 AND b.provider=x.provider AND b.access_scope=x.scope AND b.instrument_id=x.id
JOIN market_data_sources s ON s.tenant_id=b.tenant_id AND s.provider=b.provider AND s.access_scope=b.access_scope
WHERE s.enabled AND b.from_at<=$3 AND b.to_at>=$4 AND b.recorded_at<=$5
ORDER BY b.recorded_at DESC,b.job_id DESC LIMIT 100001`, tenant, raw, to, from.Add(-5*24*time.Hour), readAt)
		if e != nil {
			return nil, e
		}
		count := 0
		for rows.Next() {
			var provider, scope, id string
			var f md.SavedBondFacts
			var data []byte
			if e = rows.Scan(&provider, &scope, &id, &f.ID, &data, &f.Hash, &f.RecordedAt); e != nil {
				rows.Close()
				return nil, e
			}
			if json.Unmarshal(data, &f.BondFacts) != nil || f.Hash != investmentHash(f.BondFacts) {
				rows.Close()
				return nil, d.ErrInvestmentPrecondition
			}
			if _, e = md.NormalizeBondFacts(f.BondFacts); e != nil {
				rows.Close()
				return nil, e
			}
			key := provider + "/" + scope + "/" + id
			facts[key] = append(facts[key], f)
			count++
			if count > 100000 || len(facts[key]) > 1000 {
				rows.Close()
				return nil, d.ErrInvestmentLimit
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
	}
	prepared := map[string]*md.PriceSeries{}
	for _, listing := range listings {
		key := listing.Provider + "/" + listing.Scope + "/" + listing.ID
		period := md.MappingPeriod{Provider: listing.Provider, AccessScope: listing.Scope, InstrumentID: listing.ID}
		for _, adjustment := range []string{"unadjusted", "provider_unspecified"} {
			cache := key
			if adjustment == "provider_unspecified" {
				cache += "/unknown"
			}
			q := historyPriceSelection(tenant, period, to)
			q.Adjustment = adjustment
			series, e := md.NewPriceSeries(q, observations[cache])
			if e != nil {
				return nil, e
			}
			prepared[cache] = series
		}
	}
	for key, entry := range result {
		entry.observations = observations
		entry.series = prepared
		entry.bonds = facts
		result[key] = entry
	}
	return result, nil
}
