package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

// ReadInvestmentValuation assembles corrected history at a financial cutoff in
// one repeatable-read snapshot. No provider network request occurs in this TX.
func (r *MarketDataRepo) ReadInvestmentValuation(ctx context.Context, q nav.Request) (nav.Result, error) {
	tx, e := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nav.Result{}, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, e := readInvestmentValuation(ctx, tx, q)
	if e != nil {
		return result, e
	}
	return result, tx.Commit(ctx)
}
func readInvestmentValuation(ctx context.Context, tx pgx.Tx, q nav.Request) (nav.Result, error) {
	return readInvestmentValuationWithCurrent(ctx, tx, q, nil)
}
func readInvestmentValuationWithCurrent(ctx context.Context, tx pgx.Tx, q nav.Request, current *nav.CurrentEvidence) (nav.Result, error) {
	return readInvestmentValuationPolicy(ctx, tx, q, current, nil)
}
func readInvestmentValuationPolicy(ctx context.Context, tx pgx.Tx, q nav.Request, current *nav.CurrentEvidence, assetPool *Pool) (nav.Result, error) {
	var out nav.Result
	for _, id := range append([]string{q.TenantID, q.UserID}, q.LedgerIDs...) {
		if parsed, e := uuid.Parse(id); e != nil || parsed.String() != id {
			return out, d.ErrInvestmentInvalid
		}
	}
	if len(q.LedgerIDs) < 1 || len(q.LedgerIDs) > 20 || q.At.IsZero() || q.At.After(time.Now().UTC()) || q.At.Nanosecond()%1000 != 0 || q.FXPolicyKey == "" {
		return out, d.ErrInvestmentInvalid
	}
	if q.ConventionKey != "" && len(q.Series) != 0 {
		return out, d.ErrInvestmentInvalid
	}
	seen := map[string]bool{}
	for _, id := range q.LedgerIDs {
		if seen[id] {
			return out, d.ErrInvestmentInvalid
		}
		seen[id] = true
	}
	var role string
	e := tx.QueryRow(ctx, `SELECT role FROM user_tenants WHERE tenant_id=$1 AND user_id=$2`, q.TenantID, q.UserID).Scan(&role)
	if errors.Is(e, pgx.ErrNoRows) || e == nil && role != "owner" && role != "admin" && role != "member" {
		return out, d.ErrInvestmentForbidden
	}
	if e != nil {
		return out, e
	}
	q.LedgerIDs = append([]string{}, q.LedgerIDs...)
	sort.Strings(q.LedgerIDs)
	var generation int64
	if e = tx.QueryRow(ctx, `SELECT COALESCE((SELECT generation FROM investment_valuation_generations WHERE tenant_id=$1),0)`, q.TenantID).Scan(&generation); e != nil {
		return out, e
	}
	snapshots := []d.InvestmentSnapshot{}
	instruments := map[string]bool{}
	for _, id := range q.LedgerIDs {
		ledger, e := scanLedger(tx.QueryRow(ctx, `SELECT `+ledgerColumns+` FROM investment_ledgers WHERE tenant_id=$1 AND id=$2`, q.TenantID, id))
		if e != nil {
			return out, e
		}
		if q.At.Before(ledger.StartedAt) {
			snapshots = append(snapshots, d.InvestmentSnapshot{Ledger: ledger, AsOf: q.At})
			continue
		}
		ops, e := loadInvestmentOperations(ctx, tx, q.TenantID, id)
		if e != nil {
			return out, e
		}
		replay, e := replayInvestmentLedgerTx(ctx, tx, q.TenantID, ledger, ops, q.At)
		if e != nil {
			return out, e
		}
		snapshots = append(snapshots, replay.Snapshot)
		for _, p := range replay.Snapshot.Positions {
			if p.Quantity != "0" {
				instruments[p.InstrumentKey] = true
			}
		}
	}
	var profile nav.SavedConventionProfile
	if q.ConventionKey != "" {
		profile, e = readPriceConventions(ctx, tx, q.TenantID, q.ConventionKey)
		if e != nil {
			return out, e
		}
	}
	evidence := map[string]nav.InstrumentEvidence{}
	for key := range instruments {
		var raw []byte
		e := tx.QueryRow(ctx, `SELECT data FROM investment_market_mappings WHERE tenant_id=$1 AND instrument_key=$2`, q.TenantID, key).Scan(&raw)
		if errors.Is(e, pgx.ErrNoRows) {
			continue
		}
		if e != nil {
			return out, e
		}
		ev := nav.InstrumentEvidence{TenantID: q.TenantID}
		if e = json.Unmarshal(raw, &ev.Mapping); e != nil {
			return out, e
		}
		evidence[key] = ev
		terms, ok := ev.Mapping.TermsAt(q.At)
		if !ok {
			continue
		}
		// TermsAt and this loop use exactly the same interval. Scope must come from
		// the reviewed mapping, not from an independently supplied caller override.
		var period md.MappingPeriod
		for _, p := range ev.Mapping.Periods {
			if !q.At.Before(p.From) && (p.To.IsZero() || q.At.Before(p.To)) {
				period = p
				break
			}
		}
		policy, ok := q.Series[key]
		if q.ConventionKey != "" {
			var convention nav.PriceConventionPeriod
			convention, ok = profile.At(key, q.At)
			if ok && (convention.Provider != period.Provider || convention.AccessScope != period.AccessScope || convention.InstrumentID != period.InstrumentID) {
				ev.Price = md.SelectedPrice{State: "unavailable", Issues: []string{"price_policy_scope_mismatch"}}
				evidence[key] = ev
				continue
			}
			policy = convention.Policy
		}
		if !ok {
			ev.Price = md.SelectedPrice{State: "unavailable", Issues: []string{"price_policy_missing"}}
			evidence[key] = ev
			continue
		}
		if current != nil {
			var permitted bool
			policy, permitted = policy.CurrentPolicy()
			if !permitted {
				ev.Price = md.SelectedPrice{State: "unavailable", Issues: []string{"current_price_policy_missing"}}
				evidence[key] = ev
				continue
			}
		}
		selection := md.PriceSelectionRequest{Scope: md.Scope{TenantID: q.TenantID, Provider: period.Provider, AccessScope: period.AccessScope}, InstrumentID: terms.InstrumentID, Kind: policy.Kind, SourceKind: policy.SourceKind, Adjustment: policy.Adjustment, At: q.At, Calendar: policy.Calendar, MaxQuoteAge: policy.MaxQuoteAge, Retrospective: policy.Retrospective}
		from := terms.From
		if selection.Retrospective != nil {
			// Terms apply to effective time D; the source candle identity is D-1.
			from = from.Add(-24 * time.Hour)
		}
		if selection.Kind == "last_price" && q.At.Add(-30*24*time.Hour).After(from) {
			from = q.At.Add(-30 * 24 * time.Hour)
		}
		if selection.Kind == "daily_candle" && selection.Calendar != nil && selection.Calendar.From.After(from) {
			from = selection.Calendar.From
		}
		// Validate policy/calendar before issuing a potentially broad history read.
		preliminary, e := md.SelectValuationPrice(selection, nil)
		if e != nil {
			return out, e
		}
		if policy.Kind == "daily_candle" && selection.Retrospective == nil && (selection.Calendar == nil || q.At.Before(selection.Calendar.From) || q.At.After(selection.Calendar.Through)) {
			ev.Price = preliminary
			evidence[key] = ev
			continue
		}
		observations, e := readPriceEvidenceMode(ctx, tx, selection, from, current != nil)
		if e != nil {
			return out, e
		}
		ev.Price, e = md.SelectValuationPrice(selection, observations)
		if e != nil {
			return out, e
		}
		if current == nil {
			ev.Price = profile.AdjustSelectedPrice(key, q.At, selection.Scope, terms, ev.Price, observations, profile.Version)
		}
		if terms.Kind == "bond" {
			ev.Accrued = profile.AccruedAt(key, q.At, selection.Scope, terms.InstrumentID, profile.Version)
		}
		evidence[key] = ev
		if current != nil {
			current.Quotes = append(current.Quotes, nav.CurrentQuote{InstrumentKey: key, Scope: selection.Scope, SourceKind: selection.SourceKind, Adjustment: selection.Adjustment, MaxAge: selection.MaxQuoteAge, Price: ev.Price})
		}
	}

	policyRef := q.FXPolicyKey
	fixings := []calc.FXFixing{}
	missingFX := false
	if assetPool != nil {
		fixings, e = readAssetInvestmentFX(ctx, tx, assetPool, snapshots, q.Currency, q.At)
		if e != nil {
			return out, e
		}
	} else {
		fxPolicy, points, err := readFXSnapshot(ctx, tx, q.TenantID, q.FXPolicyKey, q.At, time.Time{})
		missingFX = errors.Is(err, md.ErrNotFound)
		if err != nil && !missingFX {
			return out, err
		}
		if !missingFX {
			policyRef = md.FXPolicyRef(fxPolicy.Key, fxPolicy.Version)
			for _, point := range points {
				fixings = append(fixings, point.Fixing())
			}
		}
	}
	if current != nil {
		current.FXFixings = fixings
	}
	out, e = nav.Value(q.TenantID, snapshots, evidence, fixings, q.Currency, policyRef)
	out.Provisional = current != nil
	out.DataGeneration = generation
	out.ConventionKey = q.ConventionKey
	out.ConventionVersion = profile.Version
	if e == nil && missingFX {
		out.Warnings = append(out.Warnings, calc.QualityIssue{ComponentID: q.TenantID, Code: "fx_policy_unavailable"})
	}
	return out, e
}
