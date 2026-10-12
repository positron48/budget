package postgres

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"time"
)

func (r *MarketDataRepo) ExportSeries(ctx context.Context, tenant, actor, key, from, to string) (nav.SeriesCSV, error) {
	tx, e := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nav.SeriesCSV{}, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	out, e := readInvestmentSeriesCSV(ctx, tx, tenant, actor, key, from, to)
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func readInvestmentSeriesCSV(ctx context.Context, tx pgx.Tx, tenant, actor, key, from, to string) (nav.SeriesCSV, error) {
	cutoff, e := nav.DayCutoff(to)
	if e != nil || cutoff.After(time.Now()) {
		return nav.SeriesCSV{}, d.ErrInvestmentInvalid
	}
	performance, e := readInvestmentPerformance(ctx, tx, tenant, actor, key, from, to)
	if e != nil {
		return nav.SeriesCSV{}, e
	}
	q, _, _, e := readNAVRegisteredRequest(ctx, tx, tenant, actor, key)
	if e != nil {
		return nav.SeriesCSV{}, e
	}
	history, e := readDailyNAVHistory(ctx, tx, q, from, to)
	if e != nil {
		return nav.SeriesCSV{}, e
	}
	policy, _, e := readFXSnapshot(ctx, tx, tenant, q.FXPolicyKey, cutoff, time.Time{})
	if e != nil && !errors.Is(e, md.ErrNotFound) {
		return nav.SeriesCSV{}, e
	}
	mode := "unknown"
	if e == nil {
		mode = policy.Mode
	}
	return nav.EncodeSeriesCSV(performance, history, mode)
}
