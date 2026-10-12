package postgres

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func TestInvestmentReviewedNativeBondNominalAndAccrued(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	day := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	ledger, err := svc.CreateLedger(ctx, tenant, actor, "Synthetic bond", day, "ledger")
	if err != nil {
		t.Fatal(err)
	}
	for i, op := range []d.InvestmentOperation{{Kind: "deposit", Amount: "100", OccurredAt: day}, {Kind: "buy", InstrumentKey: "S", Quantity: "1", Amount: "100", OccurredAt: day.Add(time.Hour)}} {
		op.LedgerID = ledger.ID
		op.Currency = "RUB"
		if _, err = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: uuid.NewString(), ExpectedLedgerVersion: int64(i + 1), Operation: op}); err != nil {
			t.Fatal(err)
		}
	}
	repo := NewMarketDataRepo(pool)
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	if err = repo.ConfigureSource(ctx, scope, "Synthetic", true); err != nil {
		t.Fatal(err)
	}
	instrument := md.Instrument{ID: uuid.NewString(), Ticker: "S", Name: "Synthetic", Kind: "bond", Currency: "RUB"}
	tx, err := pool.DB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = saveMarketInstrument(ctx, tx, scope, instrument); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.SaveMapping(ctx, tenant, actor, md.MappingChange{InstrumentKey: "S", RequestKey: "mapping", Reason: "Synthetic source terms", Periods: []md.MappingPeriod{{Provider: scope.Provider, AccessScope: scope.AccessScope, InstrumentID: instrument.ID, Kind: "bond", Currency: "RUB", Unit: "percent_of_nominal", Nominal: "500", NominalCurrency: "RUB", From: day, Evidence: "Synthetic"}}}); err != nil {
		t.Fatal(err)
	}
	source := md.Observation{InstrumentID: instrument.ID, Kind: "daily_candle", At: day, SourceKind: "exchange", Adjustment: "provider_unspecified", PriceUnit: "provider_unspecified", Currency: "RUB", Price: "95.000000001", Open: "95.000000001", Low: "95.000000001", High: "95.000000001", Volume: "1", Complete: true}
	saved, err := repo.SaveObservations(ctx, scope, []md.Observation{source})
	if err != nil {
		t.Fatal(err)
	}
	instrument.Detailed = true
	request := md.JobRequest{Kind: "bond_facts", InstrumentID: instrument.ID, From: day, To: day.Add(48 * time.Hour)}
	job, err := repo.Enqueue(ctx, scope, request)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.Claim(ctx, scope, time.Minute)
	if err != nil || claimed.ID != job.ID {
		t.Fatal(err)
	}
	five, nominal := "5.000000001", "500"
	facts := md.BondFacts{Instrument: instrument, From: request.From, To: request.To, AsOf: time.Now().UTC(), Terms: md.BondTerms{Nominal: &md.BondAmount{Amount: "999", Currency: "USD"}}, Accrued: []md.BondAccrued{{Date: day, Nominal: &nominal}, {Date: day.Add(24 * time.Hour), Value: &five, Nominal: &nominal}}}
	if err = repo.CompleteBondFacts(ctx, claimed, facts); err != nil {
		t.Fatal(err)
	}
	snapshot, err := repo.BondFacts(ctx, scope, request)
	if err != nil || snapshot == nil {
		t.Fatal(err)
	}
	review := nav.ReviewedPriceAdjustment{Scope: scope, ObservationID: saved[0].ID, ObservationRevision: saved[0].Revision, ObservationHash: nav.PriceObservationHash(saved[0].Observation), Source: saved[0].Observation, From: day.Add(24 * time.Hour), To: day.Add(48 * time.Hour), Kind: "bond", Basis: nav.NativeBondPercentBasis, NativeBond: &nav.ReviewedNativeBondBasis{Nominal: nominal, Currency: "RUB", Points: []nav.ReviewedBondNominalPoint{{SnapshotID: snapshot.ID, SnapshotHash: snapshot.Hash, Date: day}, {SnapshotID: snapshot.ID, SnapshotHash: snapshot.Hash, Date: day.Add(24 * time.Hour)}}}, Multiplier: "1", Divisor: "1", ResolvedPrice: "95.000000001", SourceReference: "https://issuer.example.test/split", EvidenceSHA256: strings.Repeat("a", 64), Evidence: "Synthetic exact issuer factor and provider adjustment contract"}
	profile := nav.ConventionProfile{Key: "split", Title: "Reviewed split synthetic", Evidence: "Synthetic", Enabled: true, Periods: map[string][]nav.PriceConventionPeriod{"S": {{Scope: scope, InstrumentID: instrument.ID, From: day, Policy: nav.SeriesPolicy{Kind: "daily_candle", SourceKind: "exchange", Adjustment: "provider_unspecified", Retrospective: &md.RetrospectiveDailyPolicy{Convention: md.RetrospectiveCloseNextUTCDay, MaxAge: 96 * time.Hour}}}}}}
	profile.Accrued = map[string][]nav.ReviewedAccruedPeriod{"S": {{Scope: scope, InstrumentID: instrument.ID, SnapshotID: snapshot.ID, SnapshotHash: snapshot.Hash, PointAt: day.Add(24 * time.Hour), From: day.Add(24 * time.Hour), To: day.Add(48 * time.Hour), Currency: "RUB", AmountPerUnit: five, Evidence: "Synthetic dated accrued"}}}
	if _, err = repo.RegisterPriceConventions(ctx, tenant, actor, profile, 0); err != nil {
		t.Fatal(err)
	}
	fx := md.FXPolicy{Key: "market", Mode: "market", Source: "test", AccessScope: "personal", SourceReference: "Synthetic", TimeConvention: "Explicit", ValueConvention: "Per unit", ValidityConvention: "Explicit"}
	if _, err = repo.RegisterFXPolicy(ctx, tenant, actor, fx, 0); err != nil {
		t.Fatal(err)
	}
	q := nav.Request{TenantID: tenant, UserID: actor, LedgerIDs: []string{ledger.ID}, Currency: "RUB", FXPolicyKey: fx.Key, ConventionKey: profile.Key}
	check := func(r *MarketDataRepo, value string, complete bool, proof bool) {
		t.Helper()
		got, e := r.BuildDailyNAV(ctx, q, "2025-01-02")
		if e != nil || got.Result == nil || got.Result.Complete != complete || got.Result.KnownValue.RatString() != value {
			t.Fatalf("NAV got=%+v err=%v", got.Result, e)
		}
		if proof {
			warned := false
			for _, w := range got.Result.Warnings {
				warned = warned || w.Code == "reviewed_native_bond_valuation"
			}
			if !warned {
				t.Fatal("reconstructed quote hidden")
			}
			found := false
			for _, v := range got.Result.Evidence {
				if v.AdjustmentProof != nil {
					found = true
					if v.AdjustmentProof.SourcePrice != "95.000000001" || v.AdjustmentProof.ResolvedPrice != "95.000000001" || v.AdjustmentProof.Basis != nav.NativeBondPercentBasis || v.AdjustmentProof.Nominal != nominal || v.AdjustmentProof.ObservationID != review.ObservationID {
						t.Fatal(v)
					}
				}
			}
			if !found {
				t.Fatal("adjustment proof missing")
			}
			fact := got.Result.InstrumentFacts["S"]
			if fact.Price == nil || fact.Price.Price != "95.000000001" || fact.Price.Adjustment != "unadjusted" {
				t.Fatal(fact)
			}
		}
	}
	check(repo, "0", false, false)
	profile.Adjustments = map[string][]nav.ReviewedPriceAdjustment{"S": {review}}
	bad := review
	bad.ObservationID = uuid.NewString()
	profile.Adjustments["S"][0] = bad
	if _, err = repo.RegisterPriceConventions(ctx, tenant, actor, profile, 1); !errors.Is(err, d.ErrInvestmentPrecondition) {
		t.Fatal("invented pin accepted", err)
	}
	for _, mutate := range []func(*nav.ReviewedNativeBondBasis){
		func(v *nav.ReviewedNativeBondBasis) { v.Points[0].SnapshotHash = strings.Repeat("b", 64) },
		func(v *nav.ReviewedNativeBondBasis) { v.Points[0].SnapshotID = uuid.NewString() },
		func(v *nav.ReviewedNativeBondBasis) { v.Nominal = "999" },
	} {
		bad := review
		copyBasis := *review.NativeBond
		copyBasis.Points = append([]nav.ReviewedBondNominalPoint(nil), review.NativeBond.Points...)
		mutate(&copyBasis)
		bad.NativeBond = &copyBasis
		profile.Adjustments["S"][0] = bad
		if _, err = repo.RegisterPriceConventions(ctx, tenant, actor, profile, 1); !errors.Is(err, d.ErrInvestmentPrecondition) {
			t.Fatal("forged nominal pin accepted", err)
		}
	}
	// Matching current/evaluation nominal must not hide amortization or a missing
	// nominal on the actual candle day. Each immutable dated fact is mandatory.
	for _, sourceNominal := range []*string{nil, func() *string { v := "1000"; return &v }()} {
		if _, err = repo.Enqueue(ctx, scope, request); err != nil {
			t.Fatal(err)
		}
		next, e := repo.Claim(ctx, scope, time.Minute)
		if e != nil {
			t.Fatal(e)
		}
		changed := facts
		changed.Accrued = append([]md.BondAccrued(nil), facts.Accrued...)
		changed.Accrued[0].Nominal = sourceNominal
		if err = repo.CompleteBondFacts(ctx, next, changed); err != nil {
			t.Fatal(err)
		}
		latest, e := repo.BondFacts(ctx, scope, request)
		if e != nil || latest == nil {
			t.Fatal(e)
		}
		bad := review
		copyBasis := *review.NativeBond
		copyBasis.Points = append([]nav.ReviewedBondNominalPoint(nil), review.NativeBond.Points...)
		for i := range copyBasis.Points {
			copyBasis.Points[i].SnapshotID = latest.ID
			copyBasis.Points[i].SnapshotHash = latest.Hash
		}
		bad.NativeBond = &copyBasis
		profile.Adjustments["S"][0] = bad
		if _, err = repo.RegisterPriceConventions(ctx, tenant, actor, profile, 1); !errors.Is(err, d.ErrInvestmentPrecondition) {
			t.Fatal("missing or amortized candle nominal accepted", err)
		}
	}
	profile.Adjustments["S"][0] = review
	if _, err = repo.RegisterPriceConventions(ctx, tenant, actor, profile, 1); err != nil {
		t.Fatal(err)
	}
	check(repo, "240000000003/500000000", true, true)
	if _, err = repo.EnqueueDailyNAV(ctx, q, "2025-01-02", "2025-01-03"); err != nil {
		t.Fatal(err)
	}
	key, _, err := navSeries(q)
	if err != nil {
		t.Fatal(err)
	}
	prices, err := repo.InvestmentInstrumentPrices(ctx, tenant, actor, key, "S", "2025-01-02", "2025-01-02", nil)
	if err != nil || len(prices.Days) != 1 || prices.Days[0].NativeClean == nil || prices.Days[0].NativeClean.RatString() != "95000000001/200000000" || !strings.Contains(prices.Days[0].PriceEvidence, "/adjustment:split:v2/") || prices.Days[0].Adjustment != "reviewed_native_bond_percent" || prices.Days[0].AdjustmentProof == nil || prices.Days[0].AdjustmentProof.ObservationHash != review.ObservationHash {
		t.Fatal(prices, err)
	}
	rows, err := pool.DB.Query(ctx, `EXPLAIN SELECT data,payload_hash,provider,access_scope,instrument_id FROM market_bond_snapshots WHERE tenant_id=$1 AND job_id=$2`, tenant, snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var line string
		if err = rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		t.Log(line)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
}
