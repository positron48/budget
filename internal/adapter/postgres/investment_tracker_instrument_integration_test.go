package postgres

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func TestInvestmentTrackerInstrumentWithoutHoldingsAndHistoricalFX(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	repo := NewMarketDataRepo(pool)
	day := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	if e := repo.ConfigureSource(ctx, scope, "Synthetic", true); e != nil {
		t.Fatal(e)
	}
	i := md.Instrument{ID: uuid.NewString(), Ticker: "S", Name: "Synthetic", Kind: "share", Currency: "USD", Detailed: true}
	tx, e := pool.DB.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = saveMarketInstrument(ctx, tx, scope, i); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	period := md.MappingPeriod{Provider: scope.Provider, AccessScope: scope.AccessScope, InstrumentID: i.ID, Kind: "share", Currency: "USD", Unit: "currency_per_unit", From: day.AddDate(0, 0, -1), Evidence: "Synthetic"}
	if _, e = repo.SaveMapping(ctx, tenant, actor, md.MappingChange{InstrumentKey: "S", RequestKey: "map", Reason: "Synthetic", Periods: []md.MappingPeriod{period}}); e != nil {
		t.Fatal(e)
	}
	makeObs := func(at time.Time, price string) md.Observation {
		return md.Observation{InstrumentID: i.ID, Kind: "daily_candle", At: at, SourceKind: "exchange", Currency: "USD", PriceUnit: "currency_per_unit", Adjustment: "unadjusted", Price: price, Open: price, High: price, Low: price, Volume: "1", Complete: true}
	}
	if _, e = repo.SaveObservations(ctx, scope, []md.Observation{makeObs(day, "10"), makeObs(day.AddDate(0, 0, 1), "12")}); e != nil {
		t.Fatal(e)
	}
	policy := md.FXPolicy{Key: "instrument-market", Mode: "market", Source: "synthetic", AccessScope: "personal", SourceReference: "Synthetic", TimeConvention: "Explicit", ValueConvention: "One unit", ValidityConvention: "Explicit"}
	if _, e = repo.RegisterFXPolicy(ctx, tenant, actor, policy, 0); e != nil {
		t.Fatal(e)
	}
	fx := []md.FXPoint{}
	for n, rate := range []string{"80", "90"} {
		at := day.AddDate(0, 0, n+1)
		fx = append(fx, md.FXPoint{From: "USD", To: "RUB", Rate: rate, At: at, AvailableAt: at, ValidUntil: at.Add(24 * time.Hour), EvidenceSHA256: strings.Repeat("a", 64), RecordReference: "synthetic/" + at.Format(time.DateOnly)})
	}
	if _, e = repo.SaveFXPoints(ctx, tenant, policy.Key, 1, fx); e != nil {
		t.Fatal(e)
	}
	q := nav.TrackerInstrumentRequest{InstrumentKey: "S", Currency: "RUB", FXPolicyKey: policy.Key, From: "2024-01-02", To: "2024-01-03"}
	r, e := repo.TrackerInstrumentPrices(ctx, tenant, actor, q)
	if e != nil || len(r.Points) != 2 || r.Points[0].Raw.RatString() != "10" || r.Points[0].Clean.RatString() != "800" || r.Points[1].Clean.RatString() != "1080" {
		t.Fatal(r, e)
	}
	if r.Instrument == nil || r.Instrument.ID != i.ID || r.ListingOnly {
		t.Fatal("metadata/owned mapping", r)
	}
	basis := r.BasisKey
	if _, e = repo.SaveObservations(ctx, scope, []md.Observation{makeObs(day.AddDate(0, 0, 4), "99")}); e != nil {
		t.Fatal(e)
	}
	r, e = repo.TrackerInstrumentPrices(ctx, tenant, actor, q)
	if e != nil || r.BasisKey != basis {
		t.Fatal("future candle changed past identity", r, e)
	}
	if _, e = repo.SaveObservations(ctx, scope, []md.Observation{makeObs(day, "11")}); e != nil {
		t.Fatal(e)
	}
	r, e = repo.TrackerInstrumentPrices(ctx, tenant, actor, q)
	if e != nil || r.BasisKey == basis || r.Points[0].Clean.RatString() != "880" {
		t.Fatal("corrected source failed to fence history", r, e)
	}
	direct := q
	direct.InstrumentKey = "tinvest:" + i.ID
	direct.Provider, direct.AccessScope, direct.InstrumentID = "tinvest", "personal", i.ID
	r, e = repo.TrackerInstrumentPrices(ctx, tenant, actor, direct)
	if e != nil || !r.ListingOnly || r.Points[0].Clean.RatString() != "880" {
		t.Fatal("explicit unowned listing", r, e)
	}
	q.FXPolicyKey = "absent"
	r, e = repo.TrackerInstrumentPrices(ctx, tenant, actor, q)
	if e != nil || r.Points[0].Raw == nil || r.Points[0].Clean != nil || r.Points[0].State != "incomplete" {
		t.Fatal("missing FX erased native series or became zero", r, e)
	}
	if _, e = repo.TrackerInstrumentPrices(ctx, tenant, uuid.NewString(), q); !errors.Is(e, d.ErrInvestmentForbidden) {
		t.Fatal(e)
	}
	if e = repo.ConfigureSource(ctx, scope, "Synthetic revoked", false); e != nil {
		t.Fatal(e)
	}
	r, e = repo.TrackerInstrumentPrices(ctx, tenant, actor, direct)
	if e != nil || r.Instrument != nil || r.Points[0].Raw != nil {
		t.Fatal("revoked source leaked cache", r, e)
	}
	var count int
	if e = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_operations WHERE tenant_id=$1`, tenant).Scan(&count); e != nil || count != 0 {
		t.Fatal("card wrote trades", count, e)
	}
}

func TestInvestmentTrackerInstrumentBondUsesDatedNominalAndAccrued(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	repo := NewMarketDataRepo(pool)
	day := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	if e := repo.ConfigureSource(ctx, scope, "Synthetic", true); e != nil {
		t.Fatal(e)
	}
	i := md.Instrument{ID: uuid.NewString(), Ticker: "B", Name: "Synthetic bond", Kind: "bond", Currency: "RUB", Detailed: true}
	tx, e := pool.DB.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = saveMarketInstrument(ctx, tx, scope, i); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	p := md.MappingPeriod{Provider: scope.Provider, AccessScope: scope.AccessScope, InstrumentID: i.ID, Kind: "bond", Currency: "USD", Unit: "percent_of_nominal", Nominal: "1000", NominalCurrency: "USD", From: day.AddDate(0, 0, -1), Evidence: "Synthetic"}
	if _, e = repo.SaveMapping(ctx, tenant, actor, md.MappingChange{InstrumentKey: "B", RequestKey: "map", Reason: "Synthetic", Periods: []md.MappingPeriod{p}}); e != nil {
		t.Fatal(e)
	}
	obs := []md.Observation{}
	for n, nominal := range []string{"1000", "900"} {
		obs = append(obs, md.Observation{InstrumentID: i.ID, Kind: "daily_candle", At: day.AddDate(0, 0, n), SourceKind: "exchange", Currency: "USD", PriceUnit: "percent_of_nominal", Adjustment: "unadjusted", Price: "90", Open: "90", High: "90", Low: "90", Volume: "1", Complete: true, Nominal: nominal, NominalCurrency: "USD"})
	}
	if _, e = repo.SaveObservations(ctx, scope, obs); e != nil {
		t.Fatal(e)
	}
	request := md.JobRequest{Kind: "bond_facts", InstrumentID: i.ID, From: day, To: day.AddDate(0, 0, 3)}
	if _, e = repo.Enqueue(ctx, scope, request); e != nil {
		t.Fatal(e)
	}
	job, e := repo.Claim(ctx, scope, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	n1, n2, v1, v2 := "1000", "900", "2", "3"
	facts := md.BondFacts{Instrument: i, From: request.From, To: request.To, AsOf: time.Now().UTC().Truncate(time.Microsecond), Terms: md.BondTerms{Nominal: &md.BondAmount{Amount: "9999", Currency: "USD"}, CurrentAccrued: &md.BondAmount{Amount: "99", Currency: "USD"}}, Accrued: []md.BondAccrued{{Date: day, Nominal: &n1, Value: &v1, ValueCurrency: "USD"}, {Date: day.AddDate(0, 0, 1), Nominal: &n2, Value: &v2, ValueCurrency: "USD"}}}
	if e = repo.CompleteBondFacts(ctx, job, facts); e != nil {
		t.Fatal(e)
	}
	q := nav.TrackerInstrumentRequest{InstrumentKey: "B", Currency: "USD", From: "2024-01-02", To: "2024-01-03"}
	r, e := repo.TrackerInstrumentPrices(ctx, tenant, actor, q)
	if e != nil || r.Points[0].Raw.RatString() != "90" || r.Points[0].NativeClean.RatString() != "900" || r.Points[1].NativeClean.RatString() != "810" || r.Points[0].Dirty.RatString() != "902" || r.Points[1].Dirty.RatString() != "813" {
		t.Fatal("today nominal/accrued borrowed for history", r, e)
	}
	if r.Bond == nil || r.Bond.Terms.Nominal.Amount != "9999" {
		t.Fatal("current dated metadata erased by the historical coupon window", r)
	}
	q.InstrumentKey = "tinvest:" + i.ID
	q.Provider, q.AccessScope, q.InstrumentID = "tinvest", "personal", i.ID
	r, e = repo.TrackerInstrumentPrices(ctx, tenant, actor, q)
	if e != nil || r.Points[0].Clean == nil || r.Points[0].Clean.RatString() != "900" || r.Points[0].NativeCurrency != "USD" {
		t.Fatal("settlement currency replaced dated nominal currency", r, e)
	}
	facts.Accrued[0].Value = nil
	facts.Accrued[0].ValueCurrency = ""
	facts.To = facts.To.AddDate(0, 0, 1)
	request.To = facts.To
	if _, e = repo.Enqueue(ctx, scope, request); e != nil {
		t.Fatal(e)
	}
	job, e = repo.Claim(ctx, scope, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	if e = repo.CompleteBondFacts(ctx, job, facts); e != nil {
		t.Fatal(e)
	}
	r, e = repo.TrackerInstrumentPrices(ctx, tenant, actor, q)
	if e != nil || r.Points[0].Dirty != nil || r.Points[0].Accrued != nil || r.Points[0].Clean.RatString() != "900" {
		t.Fatal("missing dated accrued revived older/current value", r, e)
	}
}
