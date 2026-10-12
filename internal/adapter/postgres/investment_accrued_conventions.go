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

// Verification is atomic with publication and audit. The review cannot invent
// an amount, borrow another source's point, or infer currency from metadata.
func verifyReviewedAccrued(ctx context.Context, tx pgx.Tx, tenant string, p nav.ConventionProfile) error {
	if len(p.Accrued) == 0 {
		return nil
	}

	// Archived review data is authoritative independently of the provider cache.
	// Only byte-equivalent existing confirmations may carry forward; changing any
	// field requires the immutable provider point to be verified again.
	approved := map[string]bool{}
	var priorRaw []byte
	err := tx.QueryRow(ctx, `SELECT r.data FROM investment_price_conventions p JOIN investment_price_convention_revisions r ON r.tenant_id=p.tenant_id AND r.profile_key=p.profile_key AND r.version=p.version WHERE p.tenant_id=$1 AND p.profile_key=$2`, tenant, p.Key).Scan(&priorRaw)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil {
		var prior nav.ConventionProfile
		if err = json.Unmarshal(priorRaw, &prior); err != nil {
			return err
		}
		if nav.ValidateConventionProfile(tenant, prior) != nil {
			return d.ErrInvestmentPrecondition
		}
		for _, periods := range prior.Accrued {
			for _, v := range periods {
				approved[investmentHash(v)] = true
			}
		}
	}
	cache := map[string]md.SavedBondFacts{}
	for _, periods := range p.Accrued {
		for _, v := range periods {
			if approved[investmentHash(v)] {
				var enabled bool
				err = tx.QueryRow(ctx, `SELECT enabled FROM market_data_sources WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 FOR SHARE`, tenant, v.Provider, v.AccessScope).Scan(&enabled)
				if errors.Is(err, pgx.ErrNoRows) || err == nil && !enabled {
					return d.ErrInvestmentPrecondition
				}
				if err != nil {
					return err
				}
				continue
			}
			cacheKey := v.SnapshotID + "/" + v.Provider + "/" + v.AccessScope + "/" + v.InstrumentID
			f, ok := cache[cacheKey]
			if !ok {
				var raw []byte
				var provider, scope, id string
				e := tx.QueryRow(ctx, `SELECT b.data,b.payload_hash,b.provider,b.access_scope,b.instrument_id FROM market_bond_snapshots b JOIN market_data_sources s USING(tenant_id,provider,access_scope) WHERE b.tenant_id=$1 AND b.job_id=$2 AND s.enabled FOR SHARE OF s`, tenant, v.SnapshotID).Scan(&raw, &f.Hash, &provider, &scope, &id)
				if errors.Is(e, pgx.ErrNoRows) {
					return d.ErrInvestmentPrecondition
				}
				if e != nil {
					return e
				}
				if e = json.Unmarshal(raw, &f.BondFacts); e != nil {
					return e
				}
				if provider != v.Provider || scope != v.AccessScope || id != v.InstrumentID {
					return d.ErrInvestmentPrecondition
				}
				cache[cacheKey] = f
			}
			if f.Hash != v.SnapshotHash || f.Instrument.ID != v.InstrumentID || f.Instrument.Kind != "bond" {
				return d.ErrInvestmentPrecondition
			}
			// The cache key includes the entire verified source/listing binding.
			if v.Provider != "tinvest" {
				return d.ErrInvestmentPrecondition
			}
			found := false
			for _, point := range f.Accrued {
				if point.Date.Equal(v.PointAt) && point.Value != nil && nav.SameAccruedAmount(*point.Value, v.AmountPerUnit) {
					found = true
					break
				}
			}
			if !found {
				return d.ErrInvestmentPrecondition
			}
		}
	}
	return nil
}
