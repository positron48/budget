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

type bondReadStub struct {
	marketReadStub
	facts *md.SavedBondFacts
}

func (r bondReadStub) BondFacts(context.Context, md.Scope, md.JobRequest) (*md.SavedBondFacts, error) {
	return r.facts, r.err
}
func TestMarketBondRPCOptionalityAuthAndErrors(t *testing.T) {
	const id = "11111111-1111-4111-8111-111111111111"
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), id), id)
	req := &pb.GetMarketBondFactsRequest{Source: &pb.MarketSourceKey{Provider: "tinvest", AccessScope: "personal"}, InstrumentId: id, From: timestamppb.New(at), To: timestamppb.New(at.Add(time.Hour))}
	zero := "0"
	no := false
	repo := bondReadStub{marketReadStub: marketReadStub{enabled: true}, facts: &md.SavedBondFacts{ID: "snapshot", Hash: "hash", BondFacts: md.BondFacts{Instrument: md.Instrument{ID: id}, AsOf: at, From: at, To: at.Add(time.Hour), Terms: md.BondTerms{Nominal: &md.BondAmount{Amount: "9007199254740993.000000001", Currency: "RUB"}, Floating: &no}, Accrued: []md.BondAccrued{{Date: at, Value: &zero}}}}}
	server := NewMarketDataServer(md.NewService(repo, marketRolesStub{}), nil)
	got, err := server.GetMarketBondFacts(ctx, req)
	if err != nil || got.SnapshotId != "snapshot" || got.CoverageState != "observations_only" || got.Terms.Nominal.Amount != "9007199254740993.000000001" || got.Terms.Floating == nil || *got.Terms.Floating || got.Terms.Perpetual != nil || got.Accrued[0].Value == nil || *got.Accrued[0].Value != "0" || got.Accrued[0].Percent != nil {
		t.Fatal(got, err)
	}
	if !investmentReadMethods["/budget.v1.MarketDataService/GetMarketBondFacts"] {
		t.Fatal("read-only blocked")
	}
	if _, err = server.GetMarketBondFacts(context.Background(), req); status.Code(err) != codes.Unauthenticated {
		t.Fatal(err)
	}
	repo.facts = nil
	server = NewMarketDataServer(md.NewService(repo, marketRolesStub{}), nil)
	got, err = server.GetMarketBondFacts(ctx, req)
	if err != nil || got.CoverageState != "unavailable" {
		t.Fatal(got, err)
	}
	repo.err = errors.New("secret database")
	server = NewMarketDataServer(md.NewService(repo, marketRolesStub{}), nil)
	if _, err = server.GetMarketBondFacts(ctx, req); status.Code(err) != codes.Internal || strings.Contains(err.Error(), "secret") {
		t.Fatal(err)
	}
	req.From = nil
	if _, err = server.GetMarketBondFacts(ctx, req); status.Code(err) != codes.InvalidArgument {
		t.Fatal(err)
	}
}

type bondRefreshStub struct {
	marketReadStub
	calls *int
}

func (r bondRefreshStub) RequestRefresh(_ context.Context, _ md.Scope, q md.JobRequest) (md.Job, error) {
	*r.calls++
	return md.Job{JobRequest: q}, nil
}
func TestMarketBondRefreshFutureHorizon(t *testing.T) {
	const id = "11111111-1111-4111-8111-111111111111"
	calls := 0
	svc := md.NewService(bondRefreshStub{marketReadStub: marketReadStub{enabled: true}, calls: &calls}, marketRolesStub{})
	scope := md.Scope{TenantID: id, Provider: "tinvest", AccessScope: "personal"}
	from := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	q := md.JobRequest{Kind: "bond_facts", InstrumentID: id, From: from, To: from.Add(24 * time.Hour)}
	if _, err := svc.Refresh(context.Background(), id, id, scope, q); err != nil || calls != 1 {
		t.Fatal("future bond calendar rejected", err, calls)
	}
	q.Kind = "daily_candle"
	if _, err := svc.Refresh(context.Background(), id, id, scope, q); !errors.Is(err, md.ErrInvalid) || calls != 1 {
		t.Fatal("future daily candle accepted", err, calls)
	}
	q.Kind = "bond_facts"
	q.From = from.AddDate(0, 0, 365)
	q.To = q.From.Add(24 * time.Hour)
	if _, err := svc.Refresh(context.Background(), id, id, scope, q); !errors.Is(err, md.ErrInvalid) || calls != 1 {
		t.Fatal("calendar horizon bypass", err, calls)
	}
}
