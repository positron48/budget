package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func verifyNativeBondNominals(ctx context.Context, tx pgx.Tx, tenant string, v nav.ReviewedPriceAdjustment, cache map[string]md.SavedBondFacts) error {
	if v.NativeBond == nil {
		return nil
	}
	for _, point := range v.NativeBond.Points {
		key := point.SnapshotID + "/" + v.Provider + "/" + v.AccessScope + "/" + v.Source.InstrumentID
		facts, ok := cache[key]
		if !ok {
			var raw []byte
			var provider, scope, id string
			err := tx.QueryRow(ctx, `SELECT data,payload_hash,provider,access_scope,instrument_id FROM market_bond_snapshots WHERE tenant_id=$1 AND job_id=$2`, tenant, point.SnapshotID).Scan(&raw, &facts.Hash, &provider, &scope, &id)
			if errors.Is(err, pgx.ErrNoRows) {
				return d.ErrInvestmentPrecondition
			}
			if err != nil {
				return err
			}
			if err = json.Unmarshal(raw, &facts.BondFacts); err != nil {
				return err
			}
			if provider != v.Provider || scope != v.AccessScope || id != v.Source.InstrumentID || facts.Instrument.Kind != "bond" || facts.Instrument.ID != id {
				return d.ErrInvestmentPrecondition
			}
			cache[key] = facts
		}
		if facts.Hash != point.SnapshotHash {
			return d.ErrInvestmentPrecondition
		}
		found := false
		for _, observation := range facts.Accrued {
			if observation.Date.Equal(point.Date) && observation.Nominal != nil && nav.SameAccruedAmount(*observation.Nominal, v.NativeBond.Nominal) {
				found = true
				break
			}
		}
		if !found {
			return d.ErrInvestmentPrecondition
		}
	}
	return nil
}
