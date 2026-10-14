package postgres

import (
	"encoding/json"
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

func TestInvestmentNativeHistoryImportAtomicReplayAndIsolation(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	repo := NewMarketDataRepo(pool)
	svc := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	day := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	l, err := svc.CreateLedgerWithDetails(ctx, tenant, actor, d.InvestmentLedger{Name: "Native history", StartedAt: day.Add(-24 * time.Hour), PortfolioTracking: true}, "ledger")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: "buy", ExpectedLedgerVersion: 1, Operation: d.InvestmentOperation{LedgerID: l.ID, Kind: "buy", InstrumentKey: "S", Currency: "USD", Quantity: "2", Amount: "20", OccurredAt: day.Add(time.Hour)}}); err != nil {
		t.Fatal(err)
	}
	liveScope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal"}
	if err = repo.ConfigureSource(ctx, liveScope, "Synthetic live broker", true); err != nil {
		t.Fatal(err)
	}
	instrument := md.Instrument{Detailed: true, ID: uuid.NewString(), Ticker: "S", Name: "Synthetic share", Kind: "share", Currency: "USD"}
	tx, err := pool.DB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = saveMarketInstrument(ctx, tx, liveScope, instrument); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	live := md.MappingPeriod{Provider: "tinvest", AccessScope: "personal", InstrumentID: instrument.ID, Kind: "share", Currency: "USD", Unit: "currency_per_unit", From: day.Add(-48 * time.Hour), Evidence: "Synthetic live price"}
	if _, err = repo.SaveMapping(ctx, tenant, actor, md.MappingChange{InstrumentKey: "S", RequestKey: "live", Reason: "Live broker listing", Periods: []md.MappingPeriod{live}}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err = repo.SaveObservations(ctx, liveScope, []md.Observation{{InstrumentID: instrument.ID, Kind: "last_price", At: now, SourceKind: "exchange", Currency: "USD", PriceUnit: "currency_per_unit", Adjustment: "unadjusted", Price: "40", Complete: true}}); err != nil {
		t.Fatal(err)
	}
	period := live
	period.Provider = "twelvedata"
	observation := md.Observation{InstrumentID: instrument.ID, Kind: "daily_candle", At: day.Add(-24 * time.Hour), SourceKind: "exchange", Currency: "USD", PriceUnit: "currency_per_unit", Adjustment: "unadjusted", Price: "10", Open: "10", High: "10", Low: "10", Volume: "1", Complete: true}
	batch := HistoricalPriceBatch{Version: 1, Listings: []HistoricalPriceListing{{Provider: "twelvedata", AccessScope: "personal", Instrument: instrument, Observations: []md.Observation{observation}, ResponseHashes: []string{strings.Repeat("a", 64)}}}, Mappings: []md.MappingChange{{InstrumentKey: "S", ExpectedVersion: 1, RequestKey: "historical-batch", Reason: "Reviewed native source", HistoricalPeriods: &[]md.MappingPeriod{period}}}}
	bookHash := func() string {
		var value string
		if err := pool.DB.QueryRow(ctx, `SELECT md5(COALESCE(jsonb_agg(to_jsonb(o) ORDER BY id)::text,'')) FROM investment_operations o WHERE tenant_id=$1`, tenant).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := bookHash()
	result, err := repo.ImportHistoricalPrices(ctx, tenant, actor, batch, HistoricalPricesSHA256(batch), false)
	if err != nil || result.Applied || result.Reused || result.Observations != 1 {
		t.Fatal(result, err)
	}
	mapping, err := repo.GetMapping(ctx, tenant, "S")
	if err != nil || mapping.Version != 1 || len(mapping.HistoricalPeriods) != 0 {
		t.Fatal("rehearsal wrote mapping", mapping, err)
	}
	if enabled, err := repo.SourceEnabled(ctx, md.Scope{TenantID: tenant, Provider: "twelvedata", AccessScope: "personal"}); err != nil || enabled {
		t.Fatal("rehearsal provisioned source", enabled, err)
	}
	bad := batch
	bad.Mappings = append([]md.MappingChange{}, batch.Mappings...)
	bad.Mappings[0].ExpectedVersion = 100
	if _, err = repo.ImportHistoricalPrices(ctx, tenant, actor, bad, HistoricalPricesSHA256(bad), true); !errors.Is(err, md.ErrConflict) {
		t.Fatal("late failure accepted", err)
	}
	var count int
	if err = pool.DB.QueryRow(ctx, `SELECT count(*) FROM market_provider_observations WHERE tenant_id=$1 AND provider='twelvedata'`, tenant).Scan(&count); err != nil || count != 0 {
		t.Fatal("late failure leaked prices", count, err)
	}
	result, err = repo.ImportHistoricalPrices(ctx, tenant, actor, batch, HistoricalPricesSHA256(batch), true)
	if err != nil || !result.Applied || result.Reused {
		t.Fatal(result, err)
	}
	nativeMapping, err := repo.GetMapping(ctx, tenant, "S")
	if err != nil || nativeMapping.HistoricalPeriods[0].NativeInstrument == nil || nativeMapping.HistoricalPeriods[0].NativeInstrument.Ticker != "S" {
		t.Fatal("canonical native identity omitted", nativeMapping, err)
	}
	if (*batch.Mappings[0].HistoricalPeriods)[0].NativeInstrument != nil {
		t.Fatal("saving metadata mutated the caller's checksummed batch")
	}
	bundle, err := NewInvestmentRepo(pool).ExportInvestmentArchive(ctx, tenant, actor)
	if err != nil {
		t.Fatal(err)
	}
	canonicalReference := false
	for _, table := range bundle.Data.Tables {
		if table.Name == "investment_market_mappings" {
			for _, raw := range table.Rows {
				var row struct {
					Key  string     `json:"instrument_key"`
					Data md.Mapping `json:"data"`
				}
				if json.Unmarshal(raw, &row) != nil {
					t.Fatal("invalid canonical mapping row")
				}
				if row.Key == "S" && len(row.Data.HistoricalPeriods) == 1 && row.Data.HistoricalPeriods[0].NativeInstrument != nil && row.Data.HistoricalPeriods[0].NativeInstrument.ID == instrument.ID {
					canonicalReference = true
				}
			}
		}
	}
	if !canonicalReference {
		t.Fatal("canonical export cannot identify the native source after cache loss")
	}
	quotes, err := repo.InvestmentQuotes(ctx, tenant, actor, []string{"S"})
	if err != nil || len(quotes.Quotes) != 1 || quotes.Quotes[0].CleanPrice == nil || quotes.Quotes[0].CleanPrice.RatString() != "40" || quotes.Quotes[0].Source != "tinvest/personal" {
		t.Fatal("historical import replaced live quote", quotes, err)
	}
	policy := md.FXPolicy{Key: "native-history", Mode: "market", Source: "synthetic", AccessScope: "personal", SourceReference: "Synthetic", TimeConvention: "Explicit", ValueConvention: "One unit", ValidityConvention: "Explicit"}
	if _, err = repo.RegisterFXPolicy(ctx, tenant, actor, policy, 0); err != nil {
		t.Fatal(err)
	}
	q := nav.TrackerHistoryRequest{LedgerIDs: []string{l.ID}, Currency: "USD", FXPolicyKey: policy.Key, From: "2020-01-01", To: "2020-01-01"}
	history, err := repo.TrackerHistory(ctx, tenant, actor, q)
	if err != nil || history.Points[0].State != "ready" || history.Points[0].KnownValue.RatString() != "20" {
		t.Fatal("actual positions did not use imported history", history, err)
	}
	// Correct the provider cache after import, then retry the original batch.
	// A replay must preserve the correction, not rewind history to ten dollars.
	observation.Price, observation.Open, observation.High, observation.Low = "12", "12", "12", "12"
	if _, err = repo.SaveObservations(ctx, md.Scope{TenantID: tenant, Provider: "twelvedata", AccessScope: "personal"}, []md.Observation{observation}); err != nil {
		t.Fatal(err)
	}
	result, err = repo.ImportHistoricalPrices(ctx, tenant, actor, batch, HistoricalPricesSHA256(batch), true)
	if err != nil || !result.Reused || result.Applied {
		t.Fatal("completed import not idempotent", result, err)
	}
	history, err = repo.TrackerHistory(ctx, tenant, actor, q)
	if err != nil || history.Points[0].KnownValue.RatString() != "24" {
		t.Fatal("replay rewound corrected history", history, err)
	}
	observation.Open, observation.QualityIssue = "13", "provider_ohlc_conflict"
	if _, err = repo.SaveObservations(ctx, md.Scope{TenantID: tenant, Provider: "twelvedata", AccessScope: "personal"}, []md.Observation{observation}); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.ImportHistoricalPrices(ctx, tenant, actor, batch, HistoricalPricesSHA256(batch), true); err != nil {
		t.Fatal(err)
	}
	history, err = repo.TrackerHistory(ctx, tenant, actor, q)
	if err != nil || history.Points[0].State != "incomplete" || history.Points[0].KnownValue.Sign() != 0 || history.Points[0].PricedPositions != 0 {
		t.Fatal("invalid source record valued or overwritten by retry", history, err)
	}
	correction := batch
	correction.Listings = append([]HistoricalPriceListing(nil), batch.Listings...)
	corrected := observation
	corrected.Price, corrected.Open, corrected.High, corrected.Low, corrected.QualityIssue = "15", "15", "15", "15", ""
	correction.Listings[0].Observations = []md.Observation{corrected}
	correction.Listings[0].ResponseHashes = []string{strings.Repeat("b", 64)}
	correction.Mappings = append([]md.MappingChange(nil), batch.Mappings...)
	correction.Mappings[0].RequestKey, correction.Mappings[0].ExpectedVersion = "native-correction", 2
	if _, err = repo.ImportHistoricalPrices(ctx, tenant, actor, correction, HistoricalPricesSHA256(correction), true); err != nil {
		t.Fatal(err)
	}
	var revision int64
	if err = pool.DB.QueryRow(ctx, `SELECT revision FROM market_provider_observations WHERE tenant_id=$1 AND provider='twelvedata'`, tenant).Scan(&revision); err != nil || revision != 4 {
		t.Fatal("bulk correction lost revision history", revision, err)
	}
	if err = pool.DB.QueryRow(ctx, `SELECT count(*) FROM market_provider_observation_revisions r JOIN market_provider_observations o ON o.tenant_id=r.tenant_id AND o.id=r.observation_id WHERE o.tenant_id=$1 AND o.provider='twelvedata'`, tenant).Scan(&count); err != nil || count != 4 {
		t.Fatal("bulk correction missing immutable revisions", count, err)
	}
	if _, err = repo.ImportHistoricalPrices(ctx, tenant, actor, correction, HistoricalPricesSHA256(correction), true); err != nil {
		t.Fatal(err)
	}
	if err = pool.DB.QueryRow(ctx, `SELECT revision FROM market_provider_observations WHERE tenant_id=$1 AND provider='twelvedata'`, tenant).Scan(&revision); err != nil || revision != 4 {
		t.Fatal("identical bulk retry incremented revision", revision, err)
	}
	correction.Mappings[0].RequestKey, correction.Mappings[0].ExpectedVersion = "same-source-new-review", 3
	if _, err = repo.ImportHistoricalPrices(ctx, tenant, actor, correction, HistoricalPricesSHA256(correction), true); err != nil {
		t.Fatal(err)
	}
	if err = pool.DB.QueryRow(ctx, `SELECT revision FROM market_provider_observations WHERE tenant_id=$1 AND provider='twelvedata'`, tenant).Scan(&revision); err != nil || revision != 4 {
		t.Fatal("unchanged source row got a new revision", revision, err)
	}
	if err = pool.DB.QueryRow(ctx, `SELECT count(*) FROM market_data_changes c JOIN market_provider_observations o ON o.tenant_id=c.tenant_id AND o.id=c.observation_id WHERE o.tenant_id=$1 AND o.provider='twelvedata'`, tenant).Scan(&count); err != nil || count != 4 {
		t.Fatal("bulk write lost or duplicated invalidations", count, err)
	}
	// Read-only EXPLAIN of the exact write shape with a 500-row synthetic batch.
	explainInput := []md.Observation{}
	for i := 0; i < 500; i++ {
		o := corrected
		o.At = day.AddDate(0, 0, -i-1)
		explainInput = append(explainInput, o)
	}
	encoded, err := historicalObservationRows(explainInput)
	if err != nil {
		t.Fatal(err)
	}
	planRows, err := pool.DB.Query(ctx, "EXPLAIN "+historicalObservationsUpsertSQL, tenant, "twelvedata", "personal", encoded)
	if err != nil {
		t.Fatal(err)
	}
	for planRows.Next() {
		var line string
		if err = planRows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		t.Log(line)
	}
	if err = planRows.Err(); err != nil {
		t.Fatal(err)
	}
	planRows.Close()
	if before != bookHash() {
		t.Fatal("history import changed book")
	}
	// Shared instruments count once, but each account's recorded split basis
	// must be applied before combining its value with another account.
	otherLedger, err := svc.CreateLedgerWithDetails(ctx, tenant, actor, d.InvestmentLedger{Name: "Second account", StartedAt: day.Add(-24 * time.Hour), PortfolioTracking: true}, "second-ledger")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: "second-buy", ExpectedLedgerVersion: 1, Operation: d.InvestmentOperation{LedgerID: otherLedger.ID, Kind: "buy", InstrumentKey: "S", Currency: "USD", Quantity: "3", Amount: "45", OccurredAt: day.Add(2 * time.Hour)}}); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: "first-account-split", ExpectedLedgerVersion: 2, Operation: d.InvestmentOperation{LedgerID: l.ID, Kind: "split", InstrumentKey: "S", SplitNew: "2", SplitOld: "1", OccurredAt: day.Add(20 * time.Hour)}}); err != nil {
		t.Fatal(err)
	}
	aggregate := q
	aggregate.LedgerIDs = []string{l.ID, otherLedger.ID}
	history, err = repo.TrackerHistory(ctx, tenant, actor, aggregate)
	if err != nil || history.Points[0].Positions != 1 || history.Points[0].PricedPositions != 1 || history.Points[0].KnownValue.RatString() != "75" {
		t.Fatal("instrument aggregation mixed account split bases", history, err)
	}
	_, foreign, outsider := acceptanceOwner(t, pool)
	if _, err = repo.ImportHistoricalPrices(ctx, foreign, actor, batch, HistoricalPricesSHA256(batch), true); !errors.Is(err, d.ErrInvestmentForbidden) {
		t.Fatal("foreign write allowed", err)
	}
	if _, err = repo.RegisterFXPolicy(ctx, foreign, outsider, policy, 0); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.TrackerHistory(ctx, foreign, outsider, q); !errors.Is(err, d.ErrInvestmentNotFound) {
		t.Fatal("foreign history leaked", err)
	}
}
