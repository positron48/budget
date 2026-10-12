package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

// Resolve both current revision pointers in one statement. An old enabled
// benchmark or price profile must not keep a revoked listing in demand.
const benchmarkMarketDemandSQL = `SELECT br.data,pr.data
 FROM investment_benchmarks b
 JOIN investment_benchmark_revisions br ON br.tenant_id=b.tenant_id AND br.benchmark_key=b.benchmark_key AND br.version=b.version
 JOIN investment_price_conventions p ON p.tenant_id=b.tenant_id AND p.profile_key=b.price_profile_key
 JOIN investment_price_convention_revisions pr ON pr.tenant_id=p.tenant_id AND pr.profile_key=p.profile_key AND pr.version=p.version
 WHERE b.tenant_id=$1 AND br.data->>'Enabled'='true' AND pr.data->>'Enabled'='true'
 ORDER BY b.benchmark_key`

func benchmarkMarketPeriods(ctx context.Context, tx pgx.Tx, scope md.Scope) ([]nav.PriceConventionPeriod, error) {
	rows, err := tx.Query(ctx, benchmarkMarketDemandSQL, scope.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []nav.PriceConventionPeriod
	for rows.Next() {
		var benchmarkRaw, profileRaw []byte
		var b nav.BenchmarkProfile
		var p nav.ConventionProfile
		if err = rows.Scan(&benchmarkRaw, &profileRaw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(benchmarkRaw, &b); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(profileRaw, &p); err != nil {
			return nil, err
		}
		for _, period := range p.Periods[b.InstrumentKey] {
			if period.Scope == scope && period.Policy.Kind == "daily_candle" && period.Policy.SourceKind == "exchange" && period.Policy.Adjustment == "unadjusted" {
				result = append(result, period)
			}
		}
	}
	return result, rows.Err()
}

func benchmarkHistoryRanges(periods []nav.PriceConventionPeriod, scope md.Scope, at time.Time) []md.JobRequest {
	var result []md.JobRequest
	for _, p := range periods {
		// The approved listing's beginning owns benchmark history, independently
		// of portfolio positions. Never infer a ticker, proxy or earlier listing.
		m := md.Mapping{Periods: []md.MappingPeriod{{Provider: p.Provider, AccessScope: p.AccessScope, InstrumentID: p.InstrumentID, From: p.From, To: p.To}}}
		result = append(result, md.MappedHistoryRanges(m, scope, p.From, p.From, at, true)...)
	}
	return result
}
