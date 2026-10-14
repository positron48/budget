package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func TestInvestmentHistoricalBulkInvalidatesWholeBatchAndRollsBack(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx, tenant, _ := acceptanceOwner(t, pool)
	scope := md.Scope{TenantID: tenant, Provider: "twelvedata", AccessScope: "personal"}
	if err := NewMarketDataRepo(pool).ConfigureSource(ctx, scope, "Synthetic batch", true); err != nil {
		t.Fatal(err)
	}
	var base int64
	err := pool.DB.QueryRow(ctx, `SELECT generation FROM investment_valuation_generations WHERE tenant_id=$1`, tenant).Scan(&base)
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.NewString()
	day := time.Date(2017, 1, 1, 0, 0, 0, 0, time.UTC)
	observations := []md.Observation{}
	for i := 0; i < 500; i++ {
		observations = append(observations, md.Observation{InstrumentID: id, Kind: "daily_candle", At: day.AddDate(0, 0, i), SourceKind: "exchange", Currency: "USD", PriceUnit: "currency_per_unit", Adjustment: "unadjusted", Price: "1", Open: "1", High: "1", Low: "1", Volume: "1", Complete: true})
	}
	for attempt := 0; attempt < 4; attempt++ {
		tx, err := pool.DB.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if attempt == 3 {
			for i := range observations {
				observations[i].Price, observations[i].Open, observations[i].High, observations[i].Low = "2", "2", "2", "2"
			}
		}
		if err = saveHistoricalObservations(ctx, tx, scope, observations); err != nil {
			t.Fatal(err)
		}
		gen, err := investmentGenerationTx(ctx, tx, tenant)
		if err != nil {
			t.Fatal(err)
		}
		want := base + 1
		if attempt == 3 {
			want = base + 2
		}
		if gen != want {
			t.Fatal("missing or per-row generation event", attempt, gen, want)
		}
		var dirty time.Time
		if err = tx.QueryRow(ctx, `SELECT affected_from FROM investment_nav_invalidations WHERE tenant_id=$1 AND generation=$2`, tenant, gen).Scan(&dirty); err != nil || !dirty.Equal(day) {
			t.Fatal("earliest affected day lost", dirty, err)
		}
		if attempt == 0 {
			if err = tx.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			var after int64
			err := pool.DB.QueryRow(ctx, `SELECT generation FROM investment_valuation_generations WHERE tenant_id=$1`, tenant).Scan(&after)
			if err != nil || after != base {
				t.Fatal("rollback leaked generation", after, err)
			}
		} else if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err = pool.DB.QueryRow(ctx, `SELECT count(*) FROM market_data_changes WHERE tenant_id=$1`, tenant).Scan(&count); err != nil || count != 1000 {
		t.Fatal("bulk invalidation feed omitted source rows", count, err)
	}
}
