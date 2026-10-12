package postgres

import (
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	archive "github.com/positron48/budget/internal/usecase/investmentarchive"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"testing"
	"time"
)

func TestMarketFXSeriesAtomicPipelineDemandAndWithdrawal(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	repo := NewMarketDataRepo(pool)
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	if e := repo.ConfigureSource(ctx, scope, "Synthetic reviewed FX", true); e != nil {
		t.Fatal(e)
	}
	day := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	id := uuid.NewString()
	observation := md.Observation{InstrumentID: id, Kind: "daily_candle", At: day, SourceKind: "exchange", Currency: "RUB", PriceUnit: "provider_unspecified", Adjustment: "provider_unspecified", Price: "90000", Open: "90000", High: "90000", Low: "90000", Volume: "1", Complete: true}
	if _, e := repo.SaveObservations(ctx, scope, []md.Observation{observation}); e != nil {
		t.Fatal(e)
	}
	policy := md.FXPolicy{Key: "market", Mode: "market", Source: "tinvest", AccessScope: "personal", SourceReference: "Synthetic provider", TimeConvention: "Reviewed UTC candle end", ValueConvention: "Per USD, nominal 1000", ValidityConvention: "Reviewed three-day carry", Series: []md.FXSeries{{InstrumentID: id, FromCurrency: "USD", ToCurrency: "RUB", From: day, RateKind: "exchange_spot", SourceKind: "exchange", PriceUnit: "provider_unspecified", Adjustment: "provider_unspecified", Multiplier: "1", Divisor: "1000", EffectiveOffsetSeconds: 86400, AvailableOffsetSeconds: 86400, ValidSeconds: 3 * 86400, RequirePositiveVolume: true, SourceReference: "Synthetic historical terms", Evidence: "Verified source period, nominal, timestamps and validity"}}}
	registered, e := repo.RegisterFXPolicy(ctx, tenant, actor, policy, 0)
	if e != nil || registered.Version != 1 {
		t.Fatal(e, registered)
	}
	_, points, e := repo.ReadFXSnapshot(ctx, tenant, policy.Key, day.Add(48*time.Hour), time.Time{})
	if e != nil || len(points) != 1 || points[0].Rate != "90" || points[0].Withdrawn {
		t.Fatal(e, points)
	}
	pointID := points[0].ID
	if _, e = repo.SaveObservations(ctx, scope, []md.Observation{observation}); e != nil {
		t.Fatal(e)
	}
	_, points, e = repo.ReadFXSnapshot(ctx, tenant, policy.Key, day.Add(48*time.Hour), time.Time{})
	if e != nil || points[0].Revision != 1 {
		t.Fatal("retry revised FX", e, points)
	}
	// FX has demand without a security position or a manually opened chart.
	at := day.AddDate(0, 0, 15)
	if n, e := repo.ScheduleMarket(ctx, scope, at, 50); e != nil || n != 1 {
		t.Fatal("independent FX demand", n, e)
	}
	var fxDemand bool
	if e = pool.DB.QueryRow(ctx, `SELECT fx_demand FROM market_data_subscriptions WHERE tenant_id=$1 AND instrument_id=$2`, tenant, id).Scan(&fxDemand); e != nil || !fxDemand {
		t.Fatal(e)
	}
	assertInvestmentRollbackRefused(t, ctx, pool, "0057_fx_market_demand")
	j, e := repo.Claim(ctx, scope, time.Minute)
	if e != nil || j.Kind != "daily_candle" {
		t.Fatal(e, j)
	}
	// Worker completion publishes the corrected rate and raw observation together.
	correction := observation
	correction.At = j.From
	correction.Price = "91000"
	correction.Open = "91000"
	correction.High = "91000"
	correction.Low = "91000"
	if e = repo.Complete(ctx, j, []md.Observation{correction}); e != nil {
		t.Fatal(e)
	}
	if n, e := repo.ScheduleMarketHistory(ctx, scope, at, 50); e != nil || n != 1 {
		t.Fatal("FX history not hydrated", n, e)
	}
	// Retraction changes source revision and FX atomically, rather than retaining
	// an apparently fresh fixing from the formerly complete observation.
	observation.Complete = false
	if _, e = repo.SaveObservations(ctx, scope, []md.Observation{observation}); e != nil {
		t.Fatal(e)
	}
	_, points, e = repo.ReadFXSnapshot(ctx, tenant, policy.Key, day.Add(48*time.Hour), time.Time{})
	if e != nil || points[0].ID != pointID || points[0].Revision != 2 || !points[0].Withdrawn {
		t.Fatal(e, points)
	}
	r, e := calc.NewFXResolver([]calc.FXFixing{points[0].Fixing()}, md.FXPolicyRef(policy.Key, 1), day.Add(48*time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := r.Resolve("USD", "RUB"); ok {
		t.Fatal("withdrawn FX still usable")
	}
	// A publication failure cannot leave raw market corrections without the FX
	// change feed needed by already-published NAV projections.
	if _, e = pool.DB.Exec(ctx, `CREATE FUNCTION reject_series_fx() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture FX failure'; END $$; CREATE TRIGGER reject_series_fx BEFORE INSERT ON investment_fx_changes FOR EACH ROW EXECUTE FUNCTION reject_series_fx()`); e != nil {
		t.Fatal(e)
	}
	observation.Complete = true
	if _, e = repo.SaveObservations(ctx, scope, []md.Observation{observation}); e == nil {
		t.Fatal("partial publication committed")
	}
	var raw []byte
	if e = pool.DB.QueryRow(ctx, `SELECT data FROM market_provider_observations WHERE tenant_id=$1 AND instrument_id=$2 AND observed_at=$3`, tenant, id, day).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	var persisted md.Observation
	if e = json.Unmarshal(raw, &persisted); e != nil || persisted.Complete {
		t.Fatal("raw correction escaped rollback", e)
	}
	if _, e = pool.DB.Exec(ctx, `DROP TRIGGER reject_series_fx ON investment_fx_changes; DROP FUNCTION reject_series_fx()`); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.SaveObservations(ctx, scope, []md.Observation{observation}); e != nil {
		t.Fatal(e)
	}
	// Policy revision replays existing source observations under the NEW nominal.
	policy.Series[0].Divisor = "100"
	policy.Series[0].To = day.AddDate(0, 0, 5)
	if _, e = repo.RegisterFXPolicy(ctx, tenant, actor, policy, 1); e != nil {
		t.Fatal(e)
	}
	p, points, e := repo.ReadFXSnapshot(ctx, tenant, policy.Key, day.Add(48*time.Hour), time.Time{})
	if e != nil || p.Version != 2 || points[0].Rate != "900" {
		t.Fatal("new convention not replayed", e, p, points)
	}
	if n, e := repo.ScheduleMarket(ctx, scope, at.Add(7*time.Hour), 50); e != nil || n != 0 {
		t.Fatal("ended FX period polled", n, e)
	}
	if e = pool.DB.QueryRow(ctx, `SELECT fx_demand FROM market_data_subscriptions WHERE tenant_id=$1 AND instrument_id=$2`, tenant, id).Scan(&fxDemand); e != nil || fxDemand {
		t.Fatal("stale demand", e)
	}
	if e = repo.ConfigureSource(ctx, scope, "Revoked", false); e != nil {
		t.Fatal(e)
	}
	if _, _, e = repo.ReadFXSnapshot(ctx, tenant, policy.Key, day.Add(48*time.Hour), time.Time{}); !errors.Is(e, md.ErrNotFound) {
		t.Fatal("revoked source served FX", e)
	}
	bundle, e := NewInvestmentRepo(pool).ExportInvestmentArchive(ctx, tenant, actor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = archive.VerifyJournal(bundle.Data); e != nil {
		t.Fatal("FX binding archive", e)
	}
	target, cleanupTarget := withPg(t)
	defer cleanupTarget()
	if _, e = target.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'FX restore','RUB');`, tenant); e != nil {
		t.Fatal(e)
	}
	if _, e = target.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,'fx-restore@example.test','fixture')`, actor); e != nil {
		t.Fatal(e)
	}
	if _, e = target.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); e != nil {
		t.Fatal(e)
	}
	if _, e = NewInvestmentRepo(target).RestoreInvestmentArchive(ctx, actor, bundle, bundle.SHA256); e != nil {
		t.Fatal("restore FX reviews", e)
	}
	targetMarket := NewMarketDataRepo(target)
	if e = targetMarket.ConfigureSource(ctx, scope, "New licensed connection", true); e != nil {
		t.Fatal(e)
	}
	p, points, e = targetMarket.ReadFXSnapshot(ctx, tenant, "market", day.Add(48*time.Hour), time.Time{})
	if e != nil || p.Version != 2 || len(p.Series) != 1 || p.Series[0].Divisor != "100" || len(points) != 0 {
		t.Fatal("restored reviews must not carry licensed cache", e, p, points)
	}
	otherScope := scope
	otherScope.AccessScope = "unreviewed"
	if e = targetMarket.ConfigureSource(ctx, otherScope, "Other access scope", true); e != nil {
		t.Fatal(e)
	}
	if _, e = targetMarket.SaveObservations(ctx, otherScope, []md.Observation{observation}); e != nil {
		t.Fatal(e)
	}
	_, points, e = targetMarket.ReadFXSnapshot(ctx, tenant, "market", day.Add(48*time.Hour), time.Time{})
	if e != nil || len(points) != 0 {
		t.Fatal("other access scope published FX", e, points)
	}
	if _, e = targetMarket.SaveObservations(ctx, scope, []md.Observation{observation}); e != nil {
		t.Fatal(e)
	}
	_, points, e = targetMarket.ReadFXSnapshot(ctx, tenant, "market", day.Add(48*time.Hour), time.Time{})
	if e != nil || len(points) != 1 || points[0].Rate != "900" {
		t.Fatal("FX not regenerated from new licensed source", e, points)
	}
	for i := range bundle.Data.Tables {
		if bundle.Data.Tables[i].Name == "investment_fx_policy_revisions" {
			for j, row := range bundle.Data.Tables[i].Rows {
				var fields map[string]json.RawMessage
				if e = json.Unmarshal(row, &fields); e != nil {
					t.Fatal(e)
				}
				var data md.FXPolicy
				if e = json.Unmarshal(fields["data"], &data); e != nil {
					t.Fatal(e)
				}
				data.Series[0].Divisor = "0"
				fields["data"], _ = json.Marshal(data)
				fields["payload_hash"], _ = json.Marshal(investmentHash(struct {
					Policy md.FXPolicy
					Actor  string
				}{data, actor}))
				bundle.Data.Tables[i].Rows[j], _ = json.Marshal(fields)
			}
		}
	}
	if e = archive.VerifyFXPolicies(bundle.Data); e == nil {
		t.Fatal("resealed invalid FX binding accepted")
	}
}
