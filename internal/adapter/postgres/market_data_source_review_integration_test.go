package postgres

import (
	"errors"
	"github.com/google/uuid"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	rpc "github.com/positron48/budget/internal/adapter/grpc"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"google.golang.org/protobuf/types/known/timestamppb"
	"testing"
	"time"
)

func TestInvestmentReviewedSourceSchedulesPersistThroughAPI(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx, tenant, actor := acceptanceOwner(t, pool)
	ctx = ctxutil.WithUserID(ctxutil.WithTenantID(ctx, tenant), actor)
	repo := NewMarketDataRepo(pool)
	server := rpc.NewMarketDataServer(nil, md.NewMappingService(repo, NewTenantRepo(pool)))
	id := uuid.NewString()
	for _, provider := range []string{"tinvest", "moex"} {
		scope := md.Scope{TenantID: tenant, Provider: provider, AccessScope: "personal"}
		if e := repo.ConfigureSource(ctx, scope, "Synthetic", true); e != nil {
			t.Fatal(e)
		}
		tx, e := pool.DB.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = saveMarketInstrument(ctx, tx, scope, md.Instrument{ID: id, Ticker: "S", Name: "Synthetic", Kind: "share", Currency: "RUB", Detailed: true}); e != nil {
			t.Fatal(e)
		}
		if e = tx.Commit(ctx); e != nil {
			t.Fatal(e)
		}
	}
	period := func(provider string, year int) *pb.InvestmentMarketPeriod {
		return &pb.InvestmentMarketPeriod{Source: &pb.MarketSourceKey{Provider: provider, AccessScope: "personal"}, InstrumentId: id, Kind: "share", Currency: "RUB", Unit: "currency_per_unit", From: timestamppb.New(time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)), Evidence: "Synthetic reviewed listing"}
	}
	q := &pb.SaveInvestmentMarketMappingRequest{InstrumentKey: "S", RequestKey: "source-1", Reason: "Reviewed", Periods: []*pb.InvestmentMarketPeriod{period("tinvest", 2020)}, ReplaceHistorical: true, HistoricalPeriods: []*pb.InvestmentMarketPeriod{period("moex", 2019)}}
	out, e := server.SaveInvestmentMarketMapping(ctx, q)
	if e != nil || out.Mapping.Version != 1 {
		t.Fatal(out, e)
	}
	read, e := server.GetInvestmentMarketMapping(ctx, &pb.GetInvestmentMarketMappingRequest{InstrumentKey: "S"})
	if e != nil || len(read.Mapping.HistoricalPeriods) != 1 || read.Mapping.Periods[0].Source.Provider != "tinvest" || read.Mapping.HistoricalPeriods[0].Source.Provider != "moex" {
		t.Fatal(read, e)
	}
	stored, e := repo.GetMapping(ctx, tenant, "S")
	if e != nil || stored.HistoricalPeriods[0].NativeInstrument == nil || stored.HistoricalPeriods[0].NativeInstrument.Ticker != "S" {
		t.Fatal("native identity not retained", e)
	}
	if again, e := server.SaveInvestmentMarketMapping(ctx, q); e != nil || again.Mapping.Version != 1 {
		t.Fatal("reviewed retry added a revision", again, e)
	}
	// Legacy clients can update current periods without erasing native history.
	q.ExpectedVersion = 1
	q.RequestKey = "source-2"
	q.ReplaceHistorical = false
	q.HistoricalPeriods = nil
	q.Periods = []*pb.InvestmentMarketPeriod{period("tinvest", 2021)}
	if _, e = server.SaveInvestmentMarketMapping(ctx, q); e != nil {
		t.Fatal(e)
	}
	stored, e = repo.GetMapping(ctx, tenant, "S")
	if e != nil || stored.Version != 2 || stored.HistoricalPeriods[0].From.Year() != 2019 {
		t.Fatal("current edit changed historical schedule", e)
	}
	// One invalid native period must roll back the whole change and audit.
	q.ExpectedVersion = 2
	q.RequestKey = "source-invalid"
	q.ReplaceHistorical = true
	q.HistoricalPeriods = []*pb.InvestmentMarketPeriod{period("moex", 2018)}
	q.HistoricalPeriods[0].InstrumentId = uuid.NewString()
	if _, e = server.SaveInvestmentMarketMapping(ctx, q); e == nil {
		t.Fatal("unknown native listing accepted")
	}
	stored, e = repo.GetMapping(ctx, tenant, "S")
	if e != nil || stored.Version != 2 {
		t.Fatal("partial source write", e)
	}
	var audits int
	if e = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_market_mapping_revisions WHERE tenant_id=$1 AND instrument_key='S'`, tenant).Scan(&audits); e != nil || audits != 2 {
		t.Fatal(audits, e)
	}
	// Currency identity on native listings is checked in the same transaction.
	q.HistoricalPeriods[0].InstrumentId = id
	q.HistoricalPeriods[0].Currency = "USD"
	if _, e = repo.SaveMapping(ctx, tenant, actor, md.MappingChange{InstrumentKey: "S", ExpectedVersion: 2, RequestKey: "wrong-currency", Reason: "Synthetic", Periods: stored.Periods, HistoricalPeriods: &[]md.MappingPeriod{{Provider: "moex", AccessScope: "personal", InstrumentID: id, Kind: "share", Currency: "USD", Unit: "currency_per_unit", From: time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC), Evidence: "Synthetic"}}}); !errors.Is(e, md.ErrInvalid) {
		t.Fatal("native currency drift accepted", e)
	}
}
