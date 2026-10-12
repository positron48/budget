package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"time"
)

// Prices use the same reviewed mapping, calendars, quote selection and FX
// policy as NAV, but never depend on owning a position or its quantity.
func (r *MarketDataRepo) InvestmentInstrumentPrices(ctx context.Context, tenant, actor, key, instrument, from, to string, selectedGroups []string) (nav.InstrumentPriceHistory, error) {
	out := nav.InstrumentPriceHistory{InstrumentKey: instrument, Days: []nav.InstrumentPriceDay{}}
	if instrument == "" || strings.TrimSpace(instrument) != instrument || len(instrument) > 200 || strings.ContainsAny(instrument, "\x00\r\n") {
		return out, d.ErrInvestmentInvalid
	}
	tx, e := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if e = groupAccess(ctx, tx, tenant, actor, false); e != nil {
		return out, e
	}
	q, start, end, e := readNAVRegisteredRequest(ctx, tx, tenant, actor, key)
	if e != nil {
		return out, e
	}
	first, e := nav.DayCutoff(from)
	if e != nil {
		return out, d.ErrInvestmentInvalid
	}
	last, e := nav.DayCutoff(to)
	if e != nil || from < start || to > end || to < from || last.After(time.Now()) || last.Sub(first) > 3660*24*time.Hour {
		return out, d.ErrInvestmentInvalid
	}
	groups, e := readInstrumentGroups(ctx, tx, tenant)
	if e != nil {
		return out, e
	}
	selectedGroups, e = nav.NormalizeGroupSelection(selectedGroups, groups)
	if e != nil {
		return out, e
	}
	result, e := readInvestmentInstrumentPriceDays(ctx, tx, tenant, q, instrument, from, to, selectedGroups, groups, true)
	if e != nil {
		return result, e
	}
	return result, tx.Commit(ctx)
}
func readInvestmentInstrumentPriceDays(ctx context.Context, tx pgx.Tx, tenant string, q nav.Request, instrument, from, to string, selectedGroups []string, groups nav.InstrumentGroups, withExtras bool) (nav.InstrumentPriceHistory, error) {
	out := nav.InstrumentPriceHistory{InstrumentKey: instrument, Days: []nav.InstrumentPriceDay{}}
	last, e := nav.DayCutoff(to)
	if e != nil {
		return out, e
	}
	out.Currency = q.Currency
	out.FXPolicyKey = q.FXPolicyKey
	var mapping md.Mapping
	var raw []byte
	e = tx.QueryRow(ctx, `SELECT data FROM investment_market_mappings WHERE tenant_id=$1 AND instrument_key=$2`, tenant, instrument).Scan(&raw)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return out, e
	}
	if e == nil {
		if e = json.Unmarshal(raw, &mapping); e != nil {
			return out, e
		}
	}
	var profile nav.SavedConventionProfile
	if q.ConventionKey != "" {
		profile, e = readPriceConventions(ctx, tx, tenant, q.ConventionKey)
		if e != nil {
			return out, e
		}
		out.PriceProfileVersion = profile.Version
	}
	var policy md.SavedFXPolicy
	var points []md.SavedFXPoint
	if withExtras {
		policy, points, e = readFXSnapshot(ctx, tx, tenant, q.FXPolicyKey, last, time.Time{})
	}
	if e != nil && !errors.Is(e, md.ErrNotFound) {
		if errors.Is(e, md.ErrLimit) {
			return out, d.ErrInvestmentLimit
		}
		return out, e
	}
	policyRef := q.FXPolicyKey
	fixings := []calc.FXFixing{}
	if e == nil {
		policyRef = md.FXPolicyRef(policy.Key, policy.Version)
		for _, p := range points {
			fixings = append(fixings, p.Fixing())
		}
	}
	cache := map[string][]md.ScopedObservation{}
	for day := from; day <= to; {
		at, _ := nav.DayCutoff(day)
		point := nav.InstrumentPriceDay{Date: day, State: "unavailable", Issues: []calc.QualityIssue{}}
		reason := ""
		terms, known := mapping.TermsAt(at)
		var period md.MappingPeriod
		for _, p := range mapping.Periods {
			if !at.Before(p.From) && (p.To.IsZero() || at.Before(p.To)) {
				period = p
				break
			}
		}
		if !known {
			reason = "mapping_period_missing"
		} else {
			point.NativeCurrency = terms.Currency
			seriesPolicy, found := q.Series[instrument]
			if q.ConventionKey != "" {
				var convention nav.PriceConventionPeriod
				convention, found = profile.At(instrument, at)
				seriesPolicy = convention.Policy
				if found && (convention.Provider != period.Provider || convention.AccessScope != period.AccessScope || convention.InstrumentID != period.InstrumentID) {
					found = false
					reason = "price_policy_scope_mismatch"
				}
			}
			if !found && reason == "" {
				reason = "price_policy_missing"
			}
			if found {
				selection := md.PriceSelectionRequest{Scope: md.Scope{TenantID: tenant, Provider: period.Provider, AccessScope: period.AccessScope}, InstrumentID: terms.InstrumentID, Kind: seriesPolicy.Kind, SourceKind: seriesPolicy.SourceKind, Adjustment: seriesPolicy.Adjustment, At: at, Calendar: seriesPolicy.Calendar, MaxQuoteAge: seriesPolicy.MaxQuoteAge, Retrospective: seriesPolicy.Retrospective}
				_, err := md.SelectValuationPrice(selection, nil)
				if err != nil {
					return out, err
				}
				if selection.Kind == "daily_candle" && selection.Retrospective == nil && (selection.Calendar == nil || at.Before(selection.Calendar.From) || at.After(selection.Calendar.Through)) {
					reason = "calendar_outside_coverage"
				} else {
					cacheKey := investmentHash([]string{period.Provider, period.AccessScope, terms.InstrumentID, selection.Kind, selection.SourceKind, selection.Adjustment})
					observations, exists := cache[cacheKey]
					if !exists {
						readSelection := selection
						readSelection.At = last
						from := maxPriceHistoryFrom(period.From, last)
						if selection.Retrospective != nil {
							from = from.Add(-24 * time.Hour)
						}
						observations, e = readPriceEvidence(ctx, tx, readSelection, from)
						if e != nil {
							return out, e
						}
						cache[cacheKey] = observations
					}
					selected, err := md.SelectValuationPrice(selection, observations)
					if err != nil {
						return out, err
					}
					selected = profile.AdjustSelectedPrice(instrument, at, selection.Scope, terms, selected, observations, profile.Version)
					if selected.State != "fresh" || selected.Fact == nil || len(selected.Issues) > 0 {
						reason = "price_unavailable"
						for _, code := range selected.Issues {
							point.Issues = append(point.Issues, calc.QualityIssue{ComponentID: instrument, Code: code})
						}
					} else {
						var accrued *calc.AccruedFact
						if terms.Kind == "bond" {
							accrued = profile.AccruedAt(instrument, at, selection.Scope, terms.InstrumentID, profile.Version)
						}
						normalized, err := calc.NormalizePrice(selected.Fact, &terms, accrued, at)
						if err != nil {
							return out, err
						}
						point.AdjustmentProof = selected.AdjustmentProof
						point.Accrued = accrued
						point.NativeClean = normalized.Clean
						point.NativeDirty = normalized.Dirty
						point.PriceAt = selected.Fact.EffectiveAt
						point.PriceEvidence = fmt.Sprintf("%s/%s:v%d/profile:%s:v%d/terms:%s:v%d", selected.Fact.SourceKey, selected.ObservationID, selected.ObservationRevision, q.ConventionKey, profile.Version, terms.ID, terms.Revision)
						if accrued != nil {
							point.PriceEvidence += "/accrued:" + accrued.SourceID
						}
						if selected.AdjustmentProof != nil {
							point.PriceEvidence += "/adjustment:" + selected.AdjustmentProof.Reference
						}
						point.PriceSource = selected.Fact.SourceKey
						point.Adjustment = selected.Fact.Adjustment
						if selected.AdjustmentProof != nil {
							point.Adjustment = "reconstructed_unadjusted"
							if selected.AdjustmentProof.Basis == nav.NativeBondPercentBasis {
								point.Adjustment = "reviewed_native_bond_percent"
							}
						}
						if terms.Kind == "bond" && terms.Unit == "percent_of_nominal" && normalized.Clean != nil {
							point.Percent, _ = new(big.Rat).SetString(selected.Fact.Price)
						}
						convert := func(amount *big.Rat) (*big.Rat, error) {
							if !withExtras {
								return nil, nil
							}
							if amount == nil {
								return nil, nil
							}
							v, err := calc.ValueExact([]calc.ExactComponent{{ID: instrument, Currency: terms.Currency, Amount: amount}}, fixings, q.Currency, policyRef, at)
							if err != nil {
								return nil, err
							}
							if !v.Complete {
								point.Issues = append(point.Issues, v.Issues...)
								return nil, nil
							}
							return v.KnownValue, nil
						}
						point.TargetClean, e = convert(normalized.Clean)
						if e != nil {
							return out, e
						}
						point.TargetDirty, e = convert(normalized.Dirty)
						if e != nil {
							return out, e
						}
						for _, code := range normalized.Issues {
							point.Issues = append(point.Issues, calc.QualityIssue{ComponentID: instrument, Code: code})
						}
						point.State = "ready"
						if normalized.Clean == nil || point.TargetClean == nil {
							point.State = "incomplete"
						}
					}
				}
			}
		}
		if reason != "" {
			point.Issues = append(point.Issues, calc.QualityIssue{ComponentID: instrument, Code: reason})
		}
		out.Days = append(out.Days, point)
		parsed, _ := time.Parse(time.DateOnly, day)
		day = parsed.AddDate(0, 0, 1).Format(time.DateOnly)
	}
	if !withExtras {
		return out, nil
	}
	allowed := instrumentGroupLedgers(q.LedgerIDs, instrument, groups, selectedGroups)
	parsed, _ := time.Parse(time.DateOnly, from)
	previous, _ := nav.DayCutoff(parsed.AddDate(0, 0, -1).Format(time.DateOnly))
	rows, e := tx.Query(ctx, `SELECT data FROM investment_operations WHERE tenant_id=$1 AND ledger_id=ANY($2::uuid[]) AND data->>'InstrumentKey'=$3 AND kind IN ('buy','sell') AND status='posted' AND occurred_at>$4 AND occurred_at<=$5 ORDER BY occurred_at,ledger_id,sequence,id LIMIT 10001`, tenant, allowed, instrument, previous, last)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var op d.InvestmentOperation
		if e = rows.Scan(&raw); e != nil {
			return out, e
		}
		if e = json.Unmarshal(raw, &op); e != nil {
			return out, e
		}
		out.Markers = append(out.Markers, op)
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	if len(out.Markers) > 10000 {
		return out, d.ErrInvestmentLimit
	}
	return out, nil
}

func maxPriceHistoryFrom(from, last time.Time) time.Time {
	min := last.Add(-3660 * 24 * time.Hour)
	if from.Before(min) {
		return min
	}
	return from
}
func instrumentGroupLedgers(ledgers []string, instrument string, groups nav.InstrumentGroups, selected []string) []string {
	if len(selected) == 0 {
		return ledgers
	}
	wanted := map[string]bool{}
	for _, id := range selected {
		wanted[id] = true
	}
	out := []string{}
	for _, ledger := range ledgers {
		group := ""
		for _, m := range groups.Memberships {
			if m.LedgerID == ledger && m.InstrumentKey == instrument {
				group = m.GroupID
				break
			}
		}
		if wanted[group] {
			out = append(out, ledger)
		}
	}
	return out
}
