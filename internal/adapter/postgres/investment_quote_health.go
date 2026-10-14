package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

const mappedQuoteDemandSQL = `SELECT m.instrument_key,m.data FROM investment_market_mappings m WHERE m.tenant_id=$1
AND EXISTS(SELECT 1 FROM investment_security_legs s JOIN investment_ledgers l ON l.tenant_id=s.tenant_id AND l.id=s.ledger_id
WHERE s.tenant_id=m.tenant_id AND s.instrument_key=m.instrument_key AND s.effective_at<=$2 AND NOT l.archived
GROUP BY s.ledger_id HAVING sum(s.quantity)<>0) ORDER BY m.instrument_key`

// Report the same usable per-security quotes as the portfolio API, including
// nominal confirmation. Queue success and a new receipt timestamp are weaker
// evidence: a provider can successfully return an old or empty price series.
func (r *MarketDataRepo) readMappedQuoteHealth(ctx context.Context, scope md.Scope) (map[string]int64, error) {
	at := time.Now().UTC().Truncate(time.Microsecond)
	tx, e := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, e := tx.Query(ctx, mappedQuoteDemandSQL, scope.TenantID, at)
	if e != nil {
		return nil, e
	}
	keys := []string{}
	var gaps int64
	for rows.Next() {
		var key string
		var raw []byte
		var mapping md.Mapping
		if e = rows.Scan(&key, &raw); e != nil {
			rows.Close()
			return nil, e
		}
		if e = json.Unmarshal(raw, &mapping); e != nil {
			rows.Close()
			return nil, e
		}
		belongs, current := false, false
		for _, period := range mapping.Periods {
			inScope := period.Provider == scope.Provider && period.AccessScope == scope.AccessScope
			belongs = belongs || inScope
			if !at.Before(period.From) && (period.To.IsZero() || at.Before(period.To)) {
				current = true
				if inScope {
					keys = append(keys, key)
				}
			}
		}
		if belongs && !current {
			gaps++
		}
		if len(keys)+int(gaps) > 500 {
			rows.Close()
			return nil, md.ErrLimit
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	quotes, e := readPortfolioQuotes(ctx, tx, scope.TenantID, keys, at)
	if e != nil {
		return nil, e
	}
	states := map[string]int64{"fresh": 0, "expired": 0, "missing": gaps}
	for _, q := range quotes.Quotes {
		switch {
		case q.State == "fresh" && q.CleanPrice != nil:
			states["fresh"]++
		case q.State == "stale":
			states["expired"]++
		default:
			states["missing"]++
		}
	}
	return states, tx.Commit(ctx)
}
