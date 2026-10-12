package grpcadapter

import (
	"context"
	"errors"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"strings"
	"testing"
	"time"
)

type dividendReadStub struct {
	marketReadStub
	facts *md.SavedDividendFacts
}

func (r dividendReadStub) DividendFacts(context.Context, md.Scope, md.JobRequest) (*md.SavedDividendFacts, error) {
	return r.facts, r.err
}
func TestMarketDividendRPCOptionalityAuthAndErrors(t *testing.T) {
	const id = "11111111-1111-4111-8111-111111111111"
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), id), id)
	req := &pb.GetMarketDividendFactsRequest{Source: &pb.MarketSourceKey{Provider: "tinvest", AccessScope: "personal"}, InstrumentId: id, From: timestamppb.New(at), To: timestamppb.New(at.Add(time.Hour))}
	repo := dividendReadStub{marketReadStub: marketReadStub{enabled: true}, facts: &md.SavedDividendFacts{ID: "snapshot", Hash: "hash", DividendFacts: md.DividendFacts{Instrument: md.Instrument{ID: id}, AsOf: at, From: at, To: at.Add(time.Hour), Dividends: []md.Dividend{{RecordDate: at, DividendType: "Regular Cash", DividendNet: &md.BondAmount{Amount: "9007199254740993.000000001", Currency: "RUB"}}, {RecordDate: at, DividendType: "Cancelled", DividendNet: &md.BondAmount{Amount: "0", Currency: "USD"}}, {RecordDate: at, DividendType: "Return of Capital"}}}}}
	server := NewMarketDataServer(md.NewService(repo, marketRolesStub{}), nil)
	got, err := server.GetMarketDividendFacts(ctx, req)
	if err != nil || got.SnapshotId != "snapshot" || got.CoverageState != "observations_only" || len(got.Dividends) != 3 || got.Dividends[0].DividendNet.Amount != "9007199254740993.000000001" || got.Dividends[1].DividendNet.Amount != "0" || got.Dividends[2].DividendNet != nil || got.Dividends[2].DividendType != "Return of Capital" || got.Dividends[0].PaymentDate != nil {
		t.Fatal(got, err)
	}

	if !investmentReadMethods["/budget.v1.MarketDataService/GetMarketDividendFacts"] {
		t.Fatal("read-only blocked")
	}
	if _, err = server.GetMarketDividendFacts(context.Background(), req); status.Code(err) != codes.Unauthenticated {
		t.Fatal(err)
	}
	repo.facts = nil
	server = NewMarketDataServer(md.NewService(repo, marketRolesStub{}), nil)
	got, err = server.GetMarketDividendFacts(ctx, req)
	if err != nil || got.CoverageState != "unavailable" {
		t.Fatal(got, err)
	}
	repo.err = errors.New("secret database")
	server = NewMarketDataServer(md.NewService(repo, marketRolesStub{}), nil)
	if _, err = server.GetMarketDividendFacts(ctx, req); status.Code(err) != codes.Internal || strings.Contains(err.Error(), "secret") {
		t.Fatal(err)
	}
	req.From = nil
	if _, err = server.GetMarketDividendFacts(ctx, req); status.Code(err) != codes.InvalidArgument {
		t.Fatal(err)
	}
}
