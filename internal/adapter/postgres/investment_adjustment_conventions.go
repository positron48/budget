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

// The pinned source must be the current immutable revision when first reviewed.
// Exact canonical confirmations may survive archive restore without cache, but
// remain unusable until the source is explicitly enabled and rehydrated.
func verifyReviewedAdjustments(ctx context.Context, tx pgx.Tx, tenant string, p nav.ConventionProfile) error {
	if len(p.Adjustments) == 0 {
		return nil
	}
	approved := map[string]bool{}
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT r.data FROM investment_price_conventions p JOIN investment_price_convention_revisions r ON r.tenant_id=p.tenant_id AND r.profile_key=p.profile_key AND r.version=p.version WHERE p.tenant_id=$1 AND p.profile_key=$2`, tenant, p.Key).Scan(&raw)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil {
		var prior nav.ConventionProfile
		if err = json.Unmarshal(raw, &prior); err != nil {
			return err
		}
		if nav.ValidateConventionProfile(tenant, prior) != nil {
			return d.ErrInvestmentPrecondition
		}
		for key, periods := range prior.Adjustments {
			for _, v := range periods {
				approved[key+":"+investmentHash(v)] = true
			}
		}
	}
	nominalCache := map[string]md.SavedBondFacts{}
	for key, periods := range p.Adjustments {
		for _, v := range periods {
			if approved[key+":"+investmentHash(v)] {
				continue
			}
			var source md.Observation
			var data []byte
			var hash, kind string
			err = tx.QueryRow(ctx, `SELECT o.data,o.payload_hash,i.data->>'kind' FROM market_provider_observations o JOIN market_data_sources s USING(tenant_id,provider,access_scope) JOIN market_provider_instruments i ON i.tenant_id=o.tenant_id AND i.provider=o.provider AND i.access_scope=o.access_scope AND i.instrument_id=o.instrument_id WHERE o.tenant_id=$1 AND o.provider=$2 AND o.access_scope=$3 AND o.id=$4 AND o.revision=$5 AND o.instrument_id=$6 AND s.enabled FOR SHARE OF o,s,i`, tenant, v.Provider, v.AccessScope, v.ObservationID, v.ObservationRevision, v.Source.InstrumentID).Scan(&data, &hash, &kind)
			if errors.Is(err, pgx.ErrNoRows) {
				return d.ErrInvestmentPrecondition
			}
			if err != nil {
				return err
			}
			if err = json.Unmarshal(data, &source); err != nil {
				return err
			}
			if kind != v.Kind || source != v.Source || hash != v.ObservationHash || hash != nav.PriceObservationHash(source) {
				return d.ErrInvestmentPrecondition
			}
			if err = verifyNativeBondNominals(ctx, tx, tenant, v, nominalCache); err != nil {
				return err
			}
		}
	}
	return nil
}
