package postgres

import (
	"context"
	"errors"
	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"strings"
	"testing"
	"time"
)

// Extends the real journal/profile fixture: source snapshots alone must not
// complete dirty NAV, and reviewed historical nominal differs from current data.
func assertInvestmentReviewedAccrued(t *testing.T, ctx context.Context, r *MarketDataRepo, q nav.Request, s md.Scope, instrument md.Instrument, day time.Time) {
	t.Helper()
	tenant, actor := q.TenantID, q.UserID
	mapping, err := r.GetMapping(ctx, tenant, "S")
	if err != nil {
		t.Fatal(err)
	}
	instrument.Kind = "bond"
	instrument.Detailed = true
	request := md.JobRequest{Kind: "bond_facts", InstrumentID: instrument.ID, From: day, To: day.Add(48 * time.Hour)}
	job, err := r.Enqueue(ctx, s, request)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := r.Claim(ctx, s, time.Minute)
	if err != nil || claimed.ID != job.ID {
		t.Fatal(claimed, err)
	}
	five := "5.000000001"
	zero := "0"
	facts := md.BondFacts{Instrument: instrument, From: request.From, To: request.To, AsOf: time.Now().UTC().Truncate(time.Microsecond), Terms: md.BondTerms{Nominal: &md.BondAmount{Amount: "999", Currency: "USD"}}, Accrued: []md.BondAccrued{{Date: day, Value: &five}, {Date: day.Add(24 * time.Hour), Value: &zero}, {Date: day.Add(36 * time.Hour)}}}
	if err = r.CompleteBondFacts(ctx, claimed, facts); err != nil {
		t.Fatal(err)
	}
	saved, err := r.BondFacts(ctx, s, request)
	if err != nil || saved == nil {
		t.Fatal(saved, err)
	}
	periods := []md.MappingPeriod{{Provider: s.Provider, AccessScope: s.AccessScope, InstrumentID: instrument.ID, Kind: "bond", Currency: "RUB", Unit: "percent_of_nominal", Nominal: "1000", NominalCurrency: "RUB", From: day, To: day.Add(24 * time.Hour), Evidence: "Verified historical nominal 1000"}, {Provider: s.Provider, AccessScope: s.AccessScope, InstrumentID: instrument.ID, Kind: "bond", Currency: "RUB", Unit: "percent_of_nominal", Nominal: "500", NominalCurrency: "RUB", From: day.Add(24 * time.Hour), Evidence: "Verified historical nominal 500"}}
	if _, err = r.SaveMapping(ctx, tenant, actor, md.MappingChange{InstrumentKey: "S", ExpectedVersion: mapping.Version, RequestKey: "bond-mapping", Reason: "Verified historical nominal schedule", Periods: periods}); err != nil {
		t.Fatal(err)
	}
	price := md.Observation{InstrumentID: instrument.ID, Kind: "last_price", At: day.Add(4 * time.Hour), SourceKind: "exchange", PriceUnit: "percent_of_nominal", Adjustment: "unadjusted", Currency: "RUB", Price: "120", Complete: true}
	if _, err = r.SaveObservations(ctx, s, []md.Observation{price}); err != nil {
		t.Fatal(err)
	}
	price.At = day.Add(28 * time.Hour)
	if _, err = r.SaveObservations(ctx, s, []md.Observation{price}); err != nil {
		t.Fatal(err)
	}
	profile := nav.ConventionProfile{Key: "reviewed-bond", Title: "Reviewed bond price and accrued", Evidence: "Synthetic historical terms", Enabled: true, Periods: map[string][]nav.PriceConventionPeriod{"S": {{Scope: s, InstrumentID: instrument.ID, From: day, Policy: nav.SeriesPolicy{Kind: "last_price", SourceKind: "exchange", Adjustment: "unadjusted", MaxQuoteAge: 24 * time.Hour}}}}}
	if _, err = r.RegisterPriceConventions(ctx, tenant, actor, profile, 0); err != nil {
		t.Fatal(err)
	}
	q.ConventionKey = profile.Key
	q.Series = nil
	q.At, _ = nav.DayCutoff("2024-01-01")
	if _, err = r.EnqueueDailyNAV(ctx, q, "2024-01-01", "2024-01-02"); err != nil {
		t.Fatal(err)
	}
	got, err := r.ReadInvestmentValuation(ctx, q)
	if err != nil || got.Complete {
		t.Fatal("raw source point completed NAV", got, err)
	}
	if _, err = r.BuildDailyNAV(ctx, q, "2024-01-01"); err != nil {
		t.Fatal(err)
	}
	key, _, _ := navSeries(q)
	review := nav.ReviewedAccruedPeriod{Scope: s, InstrumentID: instrument.ID, SnapshotID: saved.ID, SnapshotHash: saved.Hash, PointAt: day, From: day, To: day.Add(24 * time.Hour), Currency: "RUB", AmountPerUnit: five, Evidence: "Verified RUB per security and UTC date validity, not current nominal currency"}
	profile.Accrued = map[string][]nav.ReviewedAccruedPeriod{"S": {review}}
	// Forged amount/hash, missing point and foreign snapshot cannot be approved.
	for _, mutate := range []func(*nav.ReviewedAccruedPeriod){func(v *nav.ReviewedAccruedPeriod) { v.AmountPerUnit = "6" }, func(v *nav.ReviewedAccruedPeriod) { v.SnapshotHash = strings.Repeat("a", 64) }, func(v *nav.ReviewedAccruedPeriod) { v.SnapshotID = uuid.NewString() }, func(v *nav.ReviewedAccruedPeriod) { v.PointAt = day.Add(time.Hour) }, func(v *nav.ReviewedAccruedPeriod) {
		v.From = day.Add(24 * time.Hour)
		v.To = day.Add(48 * time.Hour)
		v.PointAt = day.Add(36 * time.Hour)
	}} {
		bad := review
		mutate(&bad)
		profile.Accrued["S"] = []nav.ReviewedAccruedPeriod{bad}
		if _, err = r.RegisterPriceConventions(ctx, tenant, actor, profile, 1); !errors.Is(err, d.ErrInvestmentPrecondition) {
			t.Fatal("forged source accepted", err)
		}
	}

	profile.Accrued["S"] = []nav.ReviewedAccruedPeriod{review}
	other := s
	other.AccessScope = "other"
	if err = r.ConfigureSource(ctx, other, "Other source", true); err != nil {
		t.Fatal(err)
	}
	setup, err := r.pool.DB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = saveMarketInstrument(ctx, setup, other, instrument); err != nil {
		t.Fatal(err)
	}
	if err = setup.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	borrowed := review
	borrowed.Scope = other
	otherPrice := profile.Periods["S"][0]
	otherPrice.Scope = other
	profile.Periods["OTHER"] = []nav.PriceConventionPeriod{otherPrice}
	profile.Accrued["OTHER"] = []nav.ReviewedAccruedPeriod{borrowed}
	if _, err = r.RegisterPriceConventions(ctx, tenant, actor, profile, 1); !errors.Is(err, d.ErrInvestmentPrecondition) {
		t.Fatal("cached snapshot crossed entitlement scope", err)
	}
	delete(profile.Periods, "OTHER")
	delete(profile.Accrued, "OTHER")
	var before, after int64
	if err = r.pool.DB.QueryRow(ctx, `SELECT generation FROM investment_valuation_generations WHERE tenant_id=$1`, tenant).Scan(&before); err != nil {
		t.Fatal(err)
	}
	updated, err := r.RegisterPriceConventions(ctx, tenant, actor, profile, 1)
	if err != nil || updated.Version != 2 {
		t.Fatal(updated, err)
	}
	if err = r.pool.DB.QueryRow(ctx, `SELECT generation FROM investment_valuation_generations WHERE tenant_id=$1`, tenant).Scan(&after); err != nil || after <= before {
		t.Fatal("no invalidation", before, after, err)
	}
	if retry, err := r.RegisterPriceConventions(ctx, tenant, actor, profile, 1); err != nil || retry.Version != 2 {
		t.Fatal("retry changed review", retry, err)
	}
	got, err = r.ReadInvestmentValuation(ctx, q)
	if err != nil || !got.Complete || got.KnownValue.RatString() != "2105000000001/1000000000" {
		t.Fatal("dirty NAV", got, err)
	}
	evidence := got.InstrumentFacts["S"].Accrued
	if evidence == nil || !strings.Contains(evidence.SourceID, saved.Hash) || !strings.Contains(evidence.SourceID, ":2/") {
		t.Fatal("missing provenance", evidence)
	}
	if _, err = r.BuildDailyNAV(ctx, q, "2024-01-01"); err != nil {
		t.Fatal(err)
	}
	q.At, _ = nav.DayCutoff("2024-01-02")
	got, err = r.ReadInvestmentValuation(ctx, q)
	if err != nil || got.Complete {
		t.Fatal("carried prior day accrued", got, err)
	}
	second := review
	second.PointAt = day.Add(24 * time.Hour)
	second.From = second.PointAt
	second.To = second.From.Add(24 * time.Hour)
	second.AmountPerUnit = "0"
	profile.Accrued["S"] = []nav.ReviewedAccruedPeriod{review, second}
	if _, err = r.RegisterPriceConventions(ctx, tenant, actor, profile, 2); err != nil {
		t.Fatal(err)
	}
	got, err = r.ReadInvestmentValuation(ctx, q)
	if err != nil || !got.Complete || got.KnownValue.RatString() != "1500" {
		t.Fatal("zero accrued or historical nominal", got, err)
	}
	if _, err = r.BuildDailyNAV(ctx, q, "2024-01-02"); err != nil {
		t.Fatal(err)
	}
	history, err := r.InvestmentInstrumentPrices(ctx, tenant, actor, key, "S", "2024-01-01", "2024-01-02", nil)
	if err != nil || len(history.Days) != 2 || history.Days[0].NativeClean.RatString() != "1200" || history.Days[0].NativeDirty.RatString() != "1205000000001/1000000000" || history.Days[1].NativeDirty.RatString() != "600" || history.Days[1].Accrued.AmountPerUnit != "0" {
		t.Fatal("price chart disagrees with NAV", history, err)
	}

	// Convert complete dirty values using each historical day's market fixing.
	fx := md.FXPolicy{Key: "bond-market", Mode: "market", Source: "test", AccessScope: "personal", SourceReference: "Synthetic", TimeConvention: "Explicit", ValueConvention: "Per unit", ValidityConvention: "Explicit"}
	if _, err = r.RegisterFXPolicy(ctx, tenant, actor, fx, 0); err != nil {
		t.Fatal(err)
	}
	for i, rate := range []string{"10", "20"} {
		at := day.Add(time.Duration(i) * 24 * time.Hour)
		if _, err = r.SaveFXPoints(ctx, tenant, fx.Key, 1, []md.FXPoint{{From: "USD", To: "RUB", Rate: rate, At: at, AvailableAt: at, ValidUntil: at.Add(24 * time.Hour), EvidenceSHA256: strings.Repeat("b", 64), RecordReference: uuid.NewString()}}); err != nil {
			t.Fatal(err)
		}
	}
	usd := q
	usd.Currency = "USD"
	usd.FXPolicyKey = fx.Key
	if _, err = r.EnqueueDailyNAV(ctx, usd, "2024-01-01", "2024-01-02"); err != nil {
		t.Fatal(err)
	}
	usdKey, _, _ := navSeries(usd)
	converted, err := r.InvestmentInstrumentPrices(ctx, tenant, actor, usdKey, "S", "2024-01-01", "2024-01-02", nil)
	if err != nil || converted.Days[0].TargetDirty.RatString() != "1205000000001/10000000000" || converted.Days[1].TargetDirty.RatString() != "30" {
		t.Fatal("dirty historical FX", converted, err)
	}
	// A corrected raw snapshot cannot silently alter an already reviewed amount.
	_, err = r.Enqueue(ctx, s, request)
	if err != nil {
		t.Fatal(err)
	}
	next, err := r.Claim(ctx, s, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	six := "6"
	facts.Accrued[0].Value = &six
	if err = r.CompleteBondFacts(ctx, next, facts); err != nil {
		t.Fatal(err)
	}
	q.At, _ = nav.DayCutoff("2024-01-01")
	got, err = r.ReadInvestmentValuation(ctx, q)
	if err != nil || !got.Complete || got.KnownValue.RatString() != "2105000000001/1000000000" {
		t.Fatal("raw correction bypassed review", got, err)
	}
	bundle, err := NewInvestmentRepo(r.pool).ExportInvestmentArchive(ctx, tenant, actor)
	if err != nil {
		t.Fatal(err)
	}
	proofExported := false
	for _, table := range bundle.Data.Tables {
		if table.Name == "investment_price_convention_revisions" {
			for _, row := range table.Rows {
				if strings.Contains(string(row), saved.Hash) && strings.Contains(string(row), "AmountPerUnit") {
					proofExported = true
				}
			}
		}
	}
	if !proofExported {
		t.Fatal("reviewed proof missing from archive")
	}
	q.At, _ = nav.DayCutoff("2024-01-02")
	// Explicitly mismatched accrued currency stays incomplete, never uses FX implicitly.
	profile.Accrued["S"][1].Currency = "USD"
	if _, err = r.RegisterPriceConventions(ctx, tenant, actor, profile, 3); err != nil {
		t.Fatal(err)
	}
	got, err = r.ReadInvestmentValuation(ctx, q)
	if err != nil || got.Complete {
		t.Fatal("implicit contractual FX", got, err)
	}

	// Simulate an archive restore with its reviewed profile and no market cache.
	// This is a disposable test database; the application never truncates evidence.
	if _, err = r.pool.DB.Exec(ctx, `TRUNCATE market_bond_snapshots`); err != nil {
		t.Fatal(err)
	}
	profile.Title = "Restored reviewed bond"
	if _, err = r.RegisterPriceConventions(ctx, tenant, actor, profile, 4); err != nil {
		t.Fatal("unchanged archived review needs missing raw cache", err)
	}
	original := profile.Accrued["S"][0]
	profile.Accrued["S"][0].Currency = "EUR"
	if _, err = r.RegisterPriceConventions(ctx, tenant, actor, profile, 5); !errors.Is(err, d.ErrInvestmentPrecondition) {
		t.Fatal("altered archived review bypassed source verification", err)
	}
	profile.Accrued["S"][0] = original
	// Revocation is possible without rewriting historical source evidence.
	profile.Accrued = nil
	if _, err = r.RegisterPriceConventions(ctx, tenant, actor, profile, 5); err != nil {
		t.Fatal(err)
	}
	got, err = r.ReadInvestmentValuation(ctx, q)
	if err != nil || got.Complete {
		t.Fatal("revoked accrued used", got, err)
	}
}
