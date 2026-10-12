package postgres

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

type privatePriceReport struct {
	ConventionKey                                                                 string
	MappedKeys, MappedPositionDates, BondPointDates                               int
	FreshPriceDates, StalePriceDates, MissingPriceDates                           int
	NativeBondReviewDates, NativeNominalPointChecks                               int
	AdjustmentVerified                                                            bool
	PriceFactChecks, RestoredPriceFactChecks                                      int
	CarryNominalDates                                                             int
	ReviewedAdjustmentDates, AdjustmentProofChecks, RestoredAdjustmentProofChecks int
	expected                                                                      map[string]privateExpectedPrice
}

// This operator rehearsal binds only exact broker UIDs and checkpoint dates.
// It deliberately retains unknown provider adjustment; no adjusted price is
// silently approved as an unadjusted price for the historical journal quantity.
func privatePriceRehearsal(t *testing.T, pool *Pool, tenant, actor string, checkpoints []d.InvestmentSnapshot) *privatePriceReport {
	t.Helper()
	dir, bonds := os.Getenv("INVESTMENT_REHEARSAL_TINVEST_HISTORY_DIR"), os.Getenv("INVESTMENT_REHEARSAL_TINVEST_BONDS_DIR")
	if dir == "" && bonds == "" {
		return nil
	}
	if dir == "" || bonds == "" {
		t.Fatal("both private price and bond evidence required")
	}
	adjustments := loadPrivateAdjustmentPlan(t)
	replayPrivateMarketHistory(t, pool, tenant, dir)
	replayPrivateBondHistory(t, pool, tenant, bonds)
	var catalog struct {
		Instruments []struct {
			State      string
			Instrument md.Instrument
		}
	}
	readPrivateHistory(t, filepath.Join(dir, "coverage.json"), "", &catalog)
	instruments := map[string]md.Instrument{}
	for _, i := range catalog.Instruments {
		if i.State == "observed" {
			instruments[i.Instrument.ID] = i.Instrument
		}
	}
	raw, err := os.ReadFile(os.Getenv("INVESTMENT_REHEARSAL_MAPPING"))
	if err != nil {
		t.Fatal("read private mapping")
	}
	financial, err := imp.DecodeFinancialMapping(raw)
	if err != nil {
		t.Fatal("decode private mapping")
	}
	trades := map[string][]imp.TradeEvidence{}
	for _, v := range financial.TradeEvidence {
		trades[v.InstrumentUID] = append(trades[v.InstrumentUID], v)
	}
	ctx := context.Background()
	repo := NewMarketDataRepo(pool)
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal-rehearsal"}
	profile := nav.ConventionProfile{Key: "personal-retrospective", Title: "Personal retrospective; price adjustment unresolved", Evidence: "User-approved next UTC day valuation rule; exact broker UIDs; provider adjustment remains unspecified", Enabled: true, Periods: map[string][]nav.PriceConventionPeriod{}, Accrued: map[string][]nav.ReviewedAccruedPeriod{}, Adjustments: map[string][]nav.ReviewedPriceAdjustment{}}
	periods := map[string][]md.MappingPeriod{}
	seen := map[string]bool{}
	report := &privatePriceReport{ConventionKey: profile.Key, expected: map[string]privateExpectedPrice{}}
	for _, snapshot := range checkpoints {
		date := snapshot.AsOf.In(time.FixedZone("Moscow", 3*3600)).Format(time.DateOnly)
		from, err := time.Parse(time.DateOnly, date)
		if err != nil {
			t.Fatal(err)
		}
		to := from.Add(24 * time.Hour)
		cutoff, err := nav.DayCutoff(date)
		if err != nil {
			t.Fatal(err)
		}
		for _, position := range snapshot.Positions {
			if position.Quantity == "0" {
				continue
			}
			if !strings.HasPrefix(position.InstrumentKey, "tinvest:") {
				t.Fatal("checkpoint requires explicit non-UID identity mapping")
			}
			uid := strings.TrimPrefix(position.InstrumentKey, "tinvest:")
			instrument, ok := instruments[uid]
			if !ok {
				t.Fatal("checkpoint UID has no confirmed listing")
			}
			// Currency/kind are checked against original executed broker evidence,
			// rather than backfilled from present-day catalog metadata alone.
			found := false
			for _, trade := range trades[uid] {
				if trade.Currency != position.Currency || trade.Currency != instrument.Currency || trade.InstrumentType != instrument.Kind {
					t.Fatal("historical broker currency/kind conflicts with listing")
				}
				if !trade.BrokerDate.After(cutoff) {
					found = true
				}
			}
			if !found {
				t.Fatal("no historical executed trade confirms checkpoint listing")
			}
			identity := position.InstrumentKey + "/" + date
			if seen[identity] {
				continue
			}
			seen[identity] = true
			mapping := md.MappingPeriod{Provider: scope.Provider, AccessScope: scope.AccessScope, InstrumentID: uid, Kind: instrument.Kind, Currency: position.Currency, Unit: "currency_per_unit", From: from, To: to, Evidence: "Exact executed broker UID/kind/currency matches journal and listing; checkpoint-only interval; https://tinkoff.github.io/investAPI/table_order_currency/"}
			policy := nav.SeriesPolicy{Kind: "daily_candle", SourceKind: "exchange", Adjustment: "provider_unspecified", Retrospective: &md.RetrospectiveDailyPolicy{Convention: md.RetrospectiveCloseNextUTCDay, MaxAge: 96 * time.Hour}}
			if instrument.Kind == "bond" {
				facts, err := repo.BondFacts(ctx, scope, md.JobRequest{Kind: "bond_facts", InstrumentID: uid, From: from, To: from.Add(time.Second)})
				if err != nil || facts == nil {
					t.Fatal("missing historical bond snapshot", err)
				}
				var point *md.BondAccrued
				for i := range facts.Accrued {
					if facts.Accrued[i].Date.Equal(from) {
						point = &facts.Accrued[i]
						break
					}
				}
				if point == nil || point.Value == nil || point.Nominal == nil {
					t.Fatal("missing exact dated accrued/nominal")
				}
				mapping.Unit = "percent_of_nominal"
				mapping.Nominal = *point.Nominal
				mapping.NominalCurrency = position.Currency
				mapping.Evidence += "; nominal from dated GetAccruedInterests point, not current Terms; snapshot SHA256=" + facts.Hash
				profile.Accrued[position.InstrumentKey] = append(profile.Accrued[position.InstrumentKey], nav.ReviewedAccruedPeriod{Scope: scope, InstrumentID: uid, SnapshotID: facts.ID, SnapshotHash: facts.Hash, PointAt: point.Date, From: from, To: to, Currency: position.Currency, AmountPerUnit: *point.Value, Evidence: "Checkpoint-only RUB interpretation: exact broker executed UID/currency, matching listing, dated GetAccruedInterests value/nominal. No current accrued backfill; not a payout entitlement."})
				report.BondPointDates++
			}
			selection := md.PriceSelectionRequest{Scope: scope, InstrumentID: uid, Kind: policy.Kind, SourceKind: policy.SourceKind, Adjustment: policy.Adjustment, At: cutoff, Retrospective: policy.Retrospective}
			observations, err := repo.PriceEvidence(ctx, selection, from.Add(-30*24*time.Hour))
			if err != nil {
				t.Fatal("read exact source prices", err)
			}
			selected, err := md.SelectValuationPrice(selection, observations)
			if err != nil {
				t.Fatal(err)
			}
			switch selected.State {
			case "fresh":
				report.FreshPriceDates++
			case "stale":
				report.StalePriceDates++
			default:
				report.MissingPriceDates++
			}
			if selected.Fact != nil && selected.Fact.Adjustment != "provider_unspecified" {
				t.Fatal("source adjustment silently approved")
			}

			// A carried close needs confirmed terms at both effective time and
			// valuation time. For bonds prove constant nominal on every intervening
			// UTC date; never extend today's nominal over an unknown amortization.
			if selected.Fact != nil && selected.Fact.EffectiveAt.Before(mapping.From) {
				mapping.From = selected.Fact.EffectiveAt
				if instrument.Kind == "bond" {
					facts, err := repo.BondFacts(ctx, scope, md.JobRequest{Kind: "bond_facts", InstrumentID: uid, From: mapping.From, To: from.Add(time.Second)})
					if err != nil || facts == nil {
						t.Fatal("missing carried-close nominal evidence", err)
					}
					for day := mapping.From; day.Before(from); day = day.Add(24 * time.Hour) {
						found := false
						for _, p := range facts.Accrued {
							if p.Date.Equal(day) && p.Nominal != nil && nav.SameAccruedAmount(*p.Nominal, mapping.Nominal) {
								found = true
								break
							}
						}
						if !found {
							t.Fatal("cannot carry price across unknown or changed nominal")
						}
						report.CarryNominalDates++
					}
					mapping.Evidence += "; constant dated nominal verified from carried close effective day through cutoff; snapshot SHA256=" + facts.Hash
				}
			}
			periods[position.InstrumentKey] = append(periods[position.InstrumentKey], mapping)
			profile.Periods[position.InstrumentKey] = append(profile.Periods[position.InstrumentKey], nav.PriceConventionPeriod{Scope: scope, InstrumentID: uid, From: from, To: to, Policy: policy})
			expected := privateExpectedPrice{Price: selected.Fact, Mapping: mapping}
			if entry, ok := adjustments[uid+"/"+date]; ok {
				review := privateAdjustment(t, repo, mapping, entry, instrument, scope, selected, observations, from, to)
				profile.Adjustments[position.InstrumentKey] = append(profile.Adjustments[position.InstrumentKey], review)
				resolved := *selected.Fact
				resolved.Price = review.ResolvedPrice
				resolved.Adjustment = "unadjusted"
				expected.Price = &resolved
				expected.Review = &review
				report.ReviewedAdjustmentDates++
				if review.NativeBond != nil {
					report.NativeBondReviewDates++
					report.NativeNominalPointChecks += len(review.NativeBond.Points)
				}
				delete(adjustments, uid+"/"+date)
			}
			if instrument.Kind == "bond" {
				expected.Accrued = profile.Accrued[position.InstrumentKey][len(profile.Accrued[position.InstrumentKey])-1].AmountPerUnit
			}
			report.expected[identity] = expected
			report.MappedPositionDates++
		}
	}
	if len(adjustments) != 0 {
		t.Fatal("private adjustment plan contains unused entries")
	}
	keys := make([]string, 0, len(periods))
	for key := range periods {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		sort.Slice(periods[key], func(i, j int) bool { return periods[key][i].From.Before(periods[key][j].From) })
		sort.Slice(profile.Periods[key], func(i, j int) bool { return profile.Periods[key][i].From.Before(profile.Periods[key][j].From) })
		sort.Slice(profile.Adjustments[key], func(i, j int) bool { return profile.Adjustments[key][i].From.Before(profile.Adjustments[key][j].From) })
		sort.Slice(profile.Accrued[key], func(i, j int) bool { return profile.Accrued[key][i].From.Before(profile.Accrued[key][j].From) })
		if _, err := repo.SaveMapping(ctx, tenant, actor, md.MappingChange{InstrumentKey: key, RequestKey: "checkpoint-" + strings.TrimPrefix(key, "tinvest:"), Reason: "Private exact broker evidence and dated bond facts; checkpoint-only mapping", Periods: periods[key]}); err != nil {
			t.Fatal("register checkpoint mapping", err)
		}
	}
	report.MappedKeys = len(keys)
	if _, err := repo.RegisterPriceConventions(ctx, tenant, actor, profile, 0); err != nil {
		t.Fatal("register private retrospective profile", err)
	}
	return report
}

func reloadPrivatePriceCache(t *testing.T, pool *Pool, tenant string) {
	t.Helper()
	var count int
	if err := pool.DB.QueryRow(context.Background(), `SELECT count(*) FROM market_provider_observations WHERE tenant_id=$1`, tenant).Scan(&count); err != nil || count != 0 {
		t.Fatal("canonical archive unexpectedly included licensed price cache", err)
	}
	replayPrivateMarketHistory(t, pool, tenant, os.Getenv("INVESTMENT_REHEARSAL_TINVEST_HISTORY_DIR"))
	replayPrivateBondHistory(t, pool, tenant, os.Getenv("INVESTMENT_REHEARSAL_TINVEST_BONDS_DIR"))
}

type privateExpectedPrice struct {
	Price   *calc.PriceFact
	Mapping md.MappingPeriod
	Accrued string
	Review  *nav.ReviewedPriceAdjustment
}

// Verify actual NAV input facts, including unknown adjustment, independently
// against the exact source selection retained before the financial NAV read.
func verifyPrivatePriceFacts(t *testing.T, report *privatePriceReport, result *nav.Result, date string, restored bool) {
	t.Helper()
	for key, fact := range result.InstrumentFacts {
		expected, ok := report.expected[key+"/"+date]
		if !ok || !reflect.DeepEqual(fact.Price, expected.Price) || fact.Terms.Nominal != expected.Mapping.Nominal || fact.Terms.NominalCurrency != expected.Mapping.NominalCurrency || fact.Terms.Currency != expected.Mapping.Currency || fact.Terms.Unit != expected.Mapping.Unit {
			t.Fatal("NAV input differs from exact dated source price/terms")
		}
		if expected.Mapping.Kind == "bond" && (fact.Accrued == nil || fact.Accrued.AmountPerUnit != expected.Accrued || fact.Accrued.Currency != expected.Mapping.Currency) {
			t.Fatal("NAV accrued differs from dated provider point")
		}
		if expected.Review != nil {
			found := false
			for component, provenance := range result.Evidence {
				if result.Components[component].Key != key {
					continue
				}
				proof := provenance.AdjustmentProof
				review := expected.Review
				if proof == nil || proof.ObservationHash != review.ObservationHash || proof.SourcePrice != review.Source.Price || proof.ResolvedPrice != review.ResolvedPrice || proof.Multiplier != review.Multiplier || proof.Divisor != review.Divisor || proof.EvidenceSHA256 != review.EvidenceSHA256 || (review.NativeBond != nil && (proof.Basis != nav.NativeBondPercentBasis || proof.Nominal != review.NativeBond.Nominal || proof.NominalCurrency != review.NativeBond.Currency)) {
					t.Fatal("reviewed adjustment provenance changed")
				}
				found = true
			}
			if !found {
				t.Fatal("reviewed adjustment provenance missing")
			}
			if restored {
				report.RestoredAdjustmentProofChecks++
			} else {
				report.AdjustmentProofChecks++
			}
		}
		if restored {
			report.RestoredPriceFactChecks++
		} else {
			report.PriceFactChecks++
		}
	}
}
