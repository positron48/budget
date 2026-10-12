package postgres

import (
	"context"
	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	"sort"
	"time"
)

// CurrentNAVForSeries is a separate non-persistent, read-only snapshot. Neither
// daily projection rows nor return/benchmark histories consume this result.
func (r *MarketDataRepo) CurrentNAVForSeries(ctx context.Context, tenant, actor, key string) (nav.CurrentNAV, error) {
	tx, e := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nav.CurrentNAV{}, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	out, e := readCurrentNAV(ctx, tx, tenant, actor, key, time.Now().UTC().Truncate(time.Microsecond))
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func readCurrentNAV(ctx context.Context, tx pgx.Tx, tenant, actor, key string, now time.Time) (nav.CurrentNAV, error) {
	out := nav.CurrentNAV{SeriesKey: key, LastCompletedDate: nav.LastCompletedDate(now)}
	q, _, _, e := readNAVRegisteredRequest(ctx, tx, tenant, actor, key)
	if e != nil {
		return out, e
	}
	var active bool
	if e = tx.QueryRow(ctx, `SELECT enabled FROM investment_nav_watches WHERE tenant_id=$1 AND actor_id=$2 AND series_key=$3`, tenant, actor, key).Scan(&active); e != nil {
		return out, e
	}
	if !active {
		return out, d.ErrInvestmentPrecondition
	}
	q.At = now
	var evidence nav.CurrentEvidence
	out.Result, e = readInvestmentValuationWithCurrent(ctx, tx, q, &evidence)
	out.Quotes = evidence.Quotes
	out.FXFixings = evidence.FXFixings
	sort.Slice(out.Quotes, func(i, j int) bool { return out.Quotes[i].InstrumentKey < out.Quotes[j].InstrumentKey })
	return out, e
}
