package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"reflect"
)

func readLotMapping(ctx context.Context, tx pgx.Tx, t, key string, lock bool) (md.Mapping, error) {
	sql := `SELECT data FROM investment_market_mappings WHERE tenant_id=$1 AND instrument_key=$2`
	if lock {
		sql += ` FOR SHARE`
	}
	var raw []byte
	var out md.Mapping
	e := tx.QueryRow(ctx, sql, t, key).Scan(&raw)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, d.ErrInvestmentPrecondition
	}
	if e != nil {
		return out, e
	}
	e = json.Unmarshal(raw, &out)
	if e == nil && out.InstrumentKey != key {
		return out, d.ErrInvestmentPrecondition
	}
	return out, e
}
func conversionFromMapping(mapping md.Mapping, op d.InvestmentOperation, lots string) (d.InvestmentOperation, error) {
	for i, p := range mapping.Periods {
		if op.OccurredAt.Before(p.From) || !p.To.IsZero() && !op.OccurredAt.Before(p.To) {
			continue
		}
		// Derive independently of the submitted unit quantity, then compare both.
		op.Quantity = "0"
		op.LotConversion = &d.InvestmentLotConversion{Lots: lots, LotSize: p.LotSize, TermsID: fmt.Sprintf("%s:%d", mapping.InstrumentKey, i), MappingVersion: mapping.Version, Provider: p.Provider, AccessScope: p.AccessScope, InstrumentID: p.InstrumentID, Evidence: p.Evidence, From: p.From, To: p.To}
		if e := inv.ValidateOperation(&op); e != nil {
			return op, d.ErrInvestmentPrecondition
		}
		return op, nil
	}
	return op, d.ErrInvestmentPrecondition
}
func verifyOperationLotConversion(ctx context.Context, tx pgx.Tx, t string, op d.InvestmentOperation, lock bool) error {
	if op.LotConversion == nil {
		return nil
	}
	mapping, e := readLotMapping(ctx, tx, t, op.InstrumentKey, lock)
	if e != nil {
		return e
	}
	if mapping.Version != op.LotConversion.MappingVersion {
		return d.ErrInvestmentConflict
	}
	expected, e := conversionFromMapping(mapping, op, op.LotConversion.Lots)
	if e != nil {
		return e
	}
	if expected.Quantity != op.Quantity || !reflect.DeepEqual(expected.LotConversion, op.LotConversion) {
		return d.ErrInvestmentConflict
	}
	return nil
}
func (r *InvestmentRepo) PreviewLotQuantity(ctx context.Context, t, u string, q inv.LotQuantityRequest) (inv.LotQuantityPreview, error) {
	var out inv.LotQuantityPreview
	tx, e := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if e = checkJournalReader(ctx, tx, t, u); e != nil {
		return out, e
	}
	mapping, e := readLotMapping(ctx, tx, t, q.InstrumentKey, false)
	if e != nil {
		return out, e
	}
	op, e := conversionFromMapping(mapping, d.InvestmentOperation{Kind: "buy", InstrumentKey: q.InstrumentKey, Currency: "USD", Amount: "1", Quantity: "0", OccurredAt: q.OccurredAt}, q.Lots)
	if e != nil {
		return out, e
	}
	out = inv.LotQuantityPreview{Quantity: op.Quantity, Conversion: op.LotConversion, EffectiveAt: q.OccurredAt}
	return out, tx.Commit(ctx)
}
