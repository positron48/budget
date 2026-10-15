package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
)

func (r *InvestmentRepo) DividendGrowth(ctx context.Context, t, u string) (inv.DividendGrowthReport, error) {
	return r.readDividendGrowth(ctx, t, u, "")
}

func (r *InvestmentRepo) DividendGrowthForInstrument(ctx context.Context, t, u, key string) (inv.DividendGrowthReport, error) {
	return r.readDividendGrowth(ctx, t, u, key)
}

const dividendInstrumentGrowthSQL = `SELECT data FROM investment_dividend_histories WHERE tenant_id=$1 AND data->>'InstrumentKey'=$2 ORDER BY id LIMIT 4`

func (r *InvestmentRepo) readDividendGrowth(ctx context.Context, t, u, key string) (inv.DividendGrowthReport, error) {
	out := inv.DividendGrowthReport{}
	tx, e := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if e = forecastReadAccess(ctx, tx, t, u); e != nil {
		return out, e
	}
	if e = tx.QueryRow(ctx, `SELECT transaction_timestamp()`).Scan(&out.AsOf); e != nil {
		return out, e
	}
	query := `SELECT data FROM investment_dividend_histories WHERE tenant_id=$1 ORDER BY id LIMIT 101`
	args := []any{t}
	if key != "" {
		query = dividendInstrumentGrowthSQL
		args = append(args, key)
	}
	rows, e := tx.Query(ctx, query, args...)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var raw []byte
		var h inv.DividendHistory
		if e = rows.Scan(&raw); e == nil {
			e = json.Unmarshal(raw, &h)
		}
		if e != nil {
			break
		}
		var report inv.DividendHistoryReport
		report, e = inv.BuildDividendHistory(h, out.AsOf)
		if e != nil {
			break
		}
		out.Histories = append(out.Histories, report)
	}
	rowErr := rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if rowErr != nil {
		return out, rowErr
	}
	if len(out.Histories) > 100 || key != "" && len(out.Histories) > 3 {
		return out, d.ErrInvestmentLimit
	}
	return out, tx.Commit(ctx)
}

func (r *InvestmentRepo) SaveDividendHistory(ctx context.Context, t, u string, h inv.DividendHistory, expected int64) (inv.DividendHistory, error) {
	h, e := inv.NormalizeDividendHistory(h)
	if e != nil || expected < 0 || expected == 1<<63-1 || h.BasisAt.After(time.Now()) {
		return h, d.ErrInvestmentInvalid
	}
	h.Version = expected + 1
	tx, e := r.forecastWriteTx(ctx, t, u)
	if e != nil {
		return h, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	raw, e := json.Marshal(h)
	if e != nil {
		return h, e
	}
	hash := investmentHash(struct {
		History inv.DividendHistory
		Actor   string
	}{h, u})
	var version int64
	var oldHash string
	var oldRaw []byte
	e = tx.QueryRow(ctx, `SELECT version,payload_hash,data FROM investment_dividend_histories WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, t, h.ID).Scan(&version, &oldHash, &oldRaw)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return h, e
	}
	if version != expected {
		if version == h.Version && oldHash == hash {
			return h, tx.Commit(ctx)
		}
		return h, d.ErrInvestmentConflict
	}
	if version > 0 {
		var old inv.DividendHistory
		if e = json.Unmarshal(oldRaw, &old); e != nil {
			return h, e
		}
		if old.InstrumentKey != h.InstrumentKey || old.Currency != h.Currency {
			return h, d.ErrInvestmentInvalid
		}
	} else {
		var count int
		if e = tx.QueryRow(ctx, `SELECT count(*) FROM investment_dividend_histories WHERE tenant_id=$1`, t).Scan(&count); e != nil {
			return h, e
		}
		if count >= 100 {
			return h, d.ErrInvestmentLimit
		}
	}
	_, e = tx.Exec(ctx, `INSERT INTO investment_dividend_histories(tenant_id,id,version,data,payload_hash) VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,id) DO UPDATE SET version=EXCLUDED.version,data=EXCLUDED.data,payload_hash=EXCLUDED.payload_hash`, t, h.ID, h.Version, raw, hash)
	if e != nil {
		var p *pgconn.PgError
		if errors.As(e, &p) && p.Code == "23505" {
			return h, d.ErrInvestmentConflict
		}
		return h, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO investment_dividend_history_revisions(tenant_id,history_id,version,data,actor_id,reason) VALUES($1,$2,$3,$4,$5,$6)`, t, h.ID, h.Version, raw, u, h.Reason); e != nil {
		return h, e
	}
	return h, tx.Commit(ctx)
}
