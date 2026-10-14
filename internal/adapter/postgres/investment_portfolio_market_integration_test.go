package postgres

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	av "github.com/positron48/budget/internal/adapter/alphavantage"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func syntheticPortfolioMarket() PortfolioMarketInput {
	day := time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC)
	input := PortfolioMarketInput{AlphaPolicy: md.FXPolicy{Key: "alpha-retrospective", Mode: "market", Source: "alphavantage", AccessScope: "personal", SourceReference: "Synthetic personal FX_DAILY", TimeConvention: md.AlphaTimeConvention, ValueConvention: md.AlphaValueConvention, ValidityConvention: md.AlphaValidityConvention, Alpha: &md.AlphaFXPolicy{From: day, Convention: md.AlphaRetrospectiveConvention}}}
	for _, pair := range []string{"USD/RUB", "EUR/RUB"} {
		input.AlphaAudits = append(input.AlphaAudits, av.Audit{Source: "alphavantage", Pair: pair, From: "2024-01-05", To: "2024-01-07", Timezone: "UTC", LastRefreshed: "2024-01-05", BodySHA256: strings.Repeat("a", 64), RetrievedAt: day.Add(96 * time.Hour), Bars: []av.Bar{{Date: "2024-01-05", Open: "90", High: "100", Low: "80", Close: "90"}}})
	}
	at := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	id := uuid.NewString()
	input.Quotes = []PortfolioQuoteSeed{{Instrument: md.Instrument{ID: id, Ticker: "TEST", Name: "Synthetic share", Kind: "share", ClassCode: "SPBXM", Currency: "USD", Detailed: true}, Observation: md.Observation{InstrumentID: id, Kind: "last_price", At: at, SourceKind: "exchange", Adjustment: "unadjusted", PriceUnit: "currency_per_unit", Currency: "USD", Price: "10", Complete: true}, Mapping: md.MappingChange{InstrumentKey: "TEST", RequestKey: "quote", Reason: "Synthetic source terms", Periods: []md.MappingPeriod{{Provider: "tinvest", AccessScope: "personal", InstrumentID: id, Kind: "share", Currency: "USD", Unit: "currency_per_unit", LotSize: "100", From: at.Add(-time.Hour), Evidence: "Synthetic per-security terms"}}}}}
	return input
}

func TestInvestmentReplacementPublishesMarketCacheAtomicallyAndFencesRetry(t *testing.T) {
	source, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, source)
	svc := inv.NewService(NewInvestmentRepo(source), NewTenantRepo(source))
	if _, e := svc.CreateLedgerWithDetails(ctx, tenant, actor, d.InvestmentLedger{Name: "Account", StartedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), PortfolioTracking: true}, "account"); e != nil {
		t.Fatal(e)
	}
	before, e := NewInvestmentRepo(source).ExportInvestmentArchive(ctx, tenant, actor)
	if e != nil {
		t.Fatal(e)
	}
	market := syntheticPortfolioMarket()
	if e = NewMarketDataRepo(source).ConfigurePortfolioMarket(ctx, tenant, actor, market); e != nil {
		t.Fatal(e)
	}
	candidate, e := NewInvestmentRepo(source).ExportInvestmentArchive(ctx, tenant, actor)
	if e != nil {
		t.Fatal(e)
	}
	target, targetDone := withPg(t)
	defer targetDone()
	if _, e = target.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Target','RUB')`, tenant); e != nil {
		t.Fatal(e)
	}
	if _, e = target.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,'market@example.test','test')`, actor); e != nil {
		t.Fatal(e)
	}
	if _, e = target.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); e != nil {
		t.Fatal(e)
	}
	repo := NewInvestmentRepo(target)
	if _, e = repo.RestoreInvestmentArchive(ctx, actor, before, before.SHA256); e != nil {
		t.Fatal(e)
	}
	snapshot, e := repo.ExportInvestmentArchive(ctx, tenant, actor)
	if e != nil {
		t.Fatal(e)
	}
	state, e := InvestmentArchiveStateSHA256(snapshot)
	if e != nil {
		t.Fatal(e)
	}
	options := PortfolioArchiveReplacement{ExpectedStateSHA256: state, ExpectedArchiveSHA256: candidate.SHA256, Market: &market, ExpectedMarketSHA256: PortfolioMarketSHA256(market)}
	bad := market
	bad.Quotes = append([]PortfolioQuoteSeed(nil), market.Quotes...)
	bad.Quotes[0].Mapping.Periods = append([]md.MappingPeriod(nil), market.Quotes[0].Mapping.Periods...)
	bad.Quotes[0].Mapping.Periods[0].Evidence = "Contradicts candidate archive"
	badOptions := options
	badOptions.Market = &bad
	badOptions.ExpectedMarketSHA256 = PortfolioMarketSHA256(bad)
	if _, _, e = repo.ReplacePortfolioArchive(ctx, actor, candidate, badOptions, true); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal("cache/archive mismatch accepted", e)
	}
	rolledBack, e := repo.ExportInvestmentArchive(ctx, tenant, actor)
	if e != nil {
		t.Fatal(e)
	}
	hash, e := InvestmentArchiveStateSHA256(rolledBack)
	if e != nil || hash != state {
		t.Fatal("cache failure changed canonical facts", e)
	}
	var points int
	if e = target.DB.QueryRow(ctx, `SELECT count(*) FROM investment_fx_points WHERE tenant_id=$1`, tenant).Scan(&points); e != nil || points != 0 {
		t.Fatal("cache failure retained FX points", e)
	}
	if _, retry, e := repo.ReplacePortfolioArchive(ctx, actor, candidate, options, true); e != nil || retry {
		t.Fatal("full market replacement", e)
	}
	quotes, e := NewMarketDataRepo(target).InvestmentQuotes(ctx, tenant, actor, []string{"TEST"})
	if e != nil || len(quotes.Quotes) != 1 || quotes.Quotes[0].CleanPrice == nil || quotes.Quotes[0].CleanPrice.RatString() != "10" {
		t.Fatal("normalized exchange quote missing or lot multiplied", e)
	}
	rates, e := NewMarketDataRepo(target).InvestmentExchangeRates(ctx, tenant, actor, market.AlphaPolicy.Key, "RUB", []string{"USD"}, []string{"2024-01-08"})
	if e != nil {
		t.Fatal(e)
	}
	for _, rate := range rates.Rates {
		if rate.Date == "2024-01-08" && (rate.Rate == nil || rate.Rate.RatString() != "90") {
			t.Fatal("historical market FX missing")
		}
	}
	changed := market
	changed.Quotes = append([]PortfolioQuoteSeed(nil), market.Quotes...)
	changed.Quotes[0].Observation.Price = "11"
	badOptions = options
	badOptions.Market = &changed
	badOptions.ExpectedMarketSHA256 = PortfolioMarketSHA256(changed)
	if _, _, e = repo.ReplacePortfolioArchive(ctx, actor, candidate, badOptions, true); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("retry changed market evidence", e)
	}
	if _, retry, e := repo.ReplacePortfolioArchive(ctx, actor, candidate, options, true); e != nil || !retry {
		t.Fatal("same evidence retry", e)
	}
}
