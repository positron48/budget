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

func TestInvestmentReviewedAdjustmentCorrectRestoreAndReReview(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	day := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	ledger, err := svc.CreateLedger(ctx, tenant, actor, "Synthetic split", day, "ledger")
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
	instrument := md.Instrument{ID: uuid.NewString(), Ticker: "S", Name: "Synthetic", Kind: "share", Currency: "RUB"}
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
	if _, err = repo.SaveMapping(ctx, tenant, actor, md.MappingChange{InstrumentKey: "S", RequestKey: "mapping", Reason: "Synthetic source terms", Periods: []md.MappingPeriod{{Provider: scope.Provider, AccessScope: scope.AccessScope, InstrumentID: instrument.ID, Kind: "share", Currency: "RUB", Unit: "currency_per_unit", From: day, Evidence: "Synthetic"}}}); err != nil {
		t.Fatal(err)
	}
	source := md.Observation{InstrumentID: instrument.ID, Kind: "daily_candle", At: day, SourceKind: "exchange", Adjustment: "provider_unspecified", PriceUnit: "currency_per_unit", Currency: "RUB", Price: "12", Open: "12", Low: "12", High: "12", Volume: "1", Complete: true}
	saved, err := repo.SaveObservations(ctx, scope, []md.Observation{source})
	if err != nil {
		t.Fatal(err)
	}
	review := nav.ReviewedPriceAdjustment{Scope: scope, ObservationID: saved[0].ID, ObservationRevision: saved[0].Revision, ObservationHash: nav.PriceObservationHash(saved[0].Observation), Source: saved[0].Observation, From: day.Add(24 * time.Hour), To: day.Add(48 * time.Hour), Kind: "share", Multiplier: "10", Divisor: "1", ResolvedPrice: "120", SourceReference: "https://issuer.example.test/split", EvidenceSHA256: strings.Repeat("a", 64), Evidence: "Synthetic exact issuer factor and provider adjustment contract"}
	profile := nav.ConventionProfile{Key: "split", Title: "Reviewed split synthetic", Evidence: "Synthetic", Enabled: true, Periods: map[string][]nav.PriceConventionPeriod{"S": {{Scope: scope, InstrumentID: instrument.ID, From: day, Policy: nav.SeriesPolicy{Kind: "daily_candle", SourceKind: "exchange", Adjustment: "provider_unspecified", Retrospective: &md.RetrospectiveDailyPolicy{Convention: md.RetrospectiveCloseNextUTCDay, MaxAge: 96 * time.Hour}}}}}}
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
				warned = warned || w.Code == "reconstructed_price_valuation"
			}
			if !warned {
				t.Fatal("reconstructed quote hidden")
			}
			found := false
			for _, v := range got.Result.Evidence {
				if v.AdjustmentProof != nil {
					found = true
					if v.AdjustmentProof.SourcePrice != "12.000000000" || v.AdjustmentProof.ResolvedPrice != "120" || v.AdjustmentProof.ObservationID != review.ObservationID {
						t.Fatal(v)
					}
				}
			}
			if !found {
				t.Fatal("adjustment proof missing")
			}
			fact := got.Result.InstrumentFacts["S"]
			if fact.Price == nil || fact.Price.Price != "120" || fact.Price.Adjustment != "unadjusted" {
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
	profile.Adjustments["S"][0] = review
	if _, err = repo.RegisterPriceConventions(ctx, tenant, actor, profile, 1); err != nil {
		t.Fatal(err)
	}
	check(repo, "120", true, true)
	if _, err = repo.EnqueueDailyNAV(ctx, q, "2025-01-02", "2025-01-03"); err != nil {
		t.Fatal(err)
	}
	key, _, err := navSeries(q)
	if err != nil {
		t.Fatal(err)
	}
	prices, err := repo.InvestmentInstrumentPrices(ctx, tenant, actor, key, "S", "2025-01-02", "2025-01-02", nil)
	if err != nil || len(prices.Days) != 1 || prices.Days[0].NativeClean == nil || prices.Days[0].NativeClean.RatString() != "120" || !strings.Contains(prices.Days[0].PriceEvidence, "/adjustment:split:v2/") || prices.Days[0].Adjustment != "reconstructed_unadjusted" || prices.Days[0].AdjustmentProof == nil || prices.Days[0].AdjustmentProof.ObservationHash != review.ObservationHash {
		t.Fatal(prices, err)
	}
	// Keep the query plan for the exact verification shape on representative rows.
	rows, err := pool.DB.Query(ctx, `EXPLAIN SELECT o.data,o.payload_hash,i.data->>'kind' FROM market_provider_observations o JOIN market_data_sources s USING(tenant_id,provider,access_scope) JOIN market_provider_instruments i ON i.tenant_id=o.tenant_id AND i.provider=o.provider AND i.access_scope=o.access_scope AND i.instrument_id=o.instrument_id WHERE o.tenant_id=$1 AND o.provider=$2 AND o.access_scope=$3 AND o.id=$4 AND o.revision=$5 AND o.instrument_id=$6 AND s.enabled FOR SHARE OF o,s,i`, tenant, scope.Provider, scope.AccessScope, review.ObservationID, review.ObservationRevision, instrument.ID)
	if err != nil {
		t.Fatal(err)
	}
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
	rows.Close()
	bundle, err := NewInvestmentRepo(pool).ExportInvestmentArchive(ctx, tenant, actor)
	if err != nil {
		t.Fatal(err)
	}
	target, cleanTarget := withPg(t)
	defer cleanTarget()
	if _, err = target.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Restored','RUB')`, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err = target.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,'adjustment-restore@example.test','test')`, actor); err != nil {
		t.Fatal(err)
	}
	if _, err = target.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); err != nil {
		t.Fatal(err)
	}
	if _, err = NewInvestmentRepo(target).RestoreInvestmentArchive(ctx, actor, bundle, bundle.SHA256); err != nil {
		t.Fatal(err)
	}
	restored := NewMarketDataRepo(target)
	check(restored, "0", false, false)
	if err = restored.ConfigureSource(ctx, scope, "Explicit rehydration", true); err != nil {
		t.Fatal(err)
	}
	restoreTx, err := target.DB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = saveMarketInstrument(ctx, restoreTx, scope, instrument); err != nil {
		t.Fatal(err)
	}
	if err = restoreTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	newSaved, err := restored.SaveObservations(ctx, scope, []md.Observation{source})
	if err != nil || newSaved[0].ID == review.ObservationID {
		t.Fatal("new cache identity expected", err)
	}
	check(restored, "120", true, true)
	// Same canonical review can be revised after restore; changed evidence cannot
	// inherit approval from the old entry without matching its original source pin.
	if _, err = restored.RegisterPriceConventions(ctx, tenant, actor, profile, 2); err != nil {
		t.Fatal("canonical review lost after restore", err)
	}
	profile.Adjustments["S"][0].Evidence = "Changed review needs verification"
	if _, err = restored.RegisterPriceConventions(ctx, tenant, actor, profile, 3); !errors.Is(err, d.ErrInvestmentPrecondition) {
		t.Fatal("changed review borrowed restored approval", err)
	}
	profile.Adjustments["S"][0] = review
	// Corrected prices invalidate NAV and cannot reuse the earlier coefficient pin.
	source.Price = "13"
	source.Open = "13"
	source.Low = "13"
	source.High = "13"
	saved, err = repo.SaveObservations(ctx, scope, []md.Observation{source})
	if err != nil {
		t.Fatal(err)
	}
	history, err := repo.DailyNAVHistory(ctx, q, "2025-01-02", "2025-01-02")
	if err != nil || len(history) != 1 || history[0].State != "stale" {
		t.Fatal("correction invalidation missing", history, err)
	}
	check(repo, "0", false, false)
	review.ObservationRevision = saved[0].Revision
	review.Source = saved[0].Observation
	review.ObservationHash = nav.PriceObservationHash(review.Source)
	review.ResolvedPrice = "130"
	profile.Adjustments["S"][0] = review
	if _, err = repo.RegisterPriceConventions(ctx, tenant, actor, profile, 2); err != nil {
		t.Fatal(err)
	}
	check(repo, "130", true, false)
}
