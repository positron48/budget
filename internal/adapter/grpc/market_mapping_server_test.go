package grpcadapter

import (
	"context"
	"testing"
	"time"

	pb "github.com/positron48/budget/gen/go/budget/v1"
	d "github.com/positron48/budget/internal/domain"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type mappingRPCRepo struct {
	change        md.MappingChange
	tenant, actor string
	err           error
}

func (r *mappingRPCRepo) SaveMapping(_ context.Context, t, u string, c md.MappingChange) (md.Mapping, error) {
	r.change = c
	r.tenant = t
	r.actor = u
	return md.Mapping{InstrumentKey: c.InstrumentKey, Periods: c.Periods, Version: 4, ActorID: u, Reason: c.Reason}, r.err
}
func (r *mappingRPCRepo) GetMapping(context.Context, string, string) (md.Mapping, error) {
	return md.Mapping{Version: 4, Periods: r.change.Periods}, r.err
}

type mappingRPCRole d.TenantRole

func (r mappingRPCRole) GetUserRole(context.Context, string, string) (d.TenantRole, error) {
	return d.TenantRole(r), nil
}
func TestMappingRPCValidationAuthorizationAndExactTerms(t *testing.T) {
	const id = "11111111-1111-4111-8111-111111111111"
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), id), id)
	repo := &mappingRPCRepo{}
	server := NewMarketDataServer(nil, md.NewMappingService(repo, mappingRPCRole(d.TenantRoleOwner)))
	at := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	req := &pb.SaveInvestmentMarketMappingRequest{InstrumentKey: "BOND", ExpectedVersion: 3, RequestKey: "confirm", Reason: "Reviewed", Periods: []*pb.InvestmentMarketPeriod{{Source: &pb.MarketSourceKey{Provider: "tinvest", AccessScope: "personal"}, InstrumentId: id, Kind: "bond", Currency: "RUB", Unit: "percent_of_nominal", Nominal: "9007199254740993.000000001", NominalCurrency: "RUB", From: timestamppb.New(at), Evidence: "Historical nominal source"}}}
	got, err := server.SaveInvestmentMarketMapping(ctx, req)
	if err != nil || got.Mapping.Version != 4 || got.Mapping.Periods[0].Nominal != req.Periods[0].Nominal || repo.tenant != id || repo.actor != id || repo.change.ExpectedVersion != 3 {
		t.Fatal(got, err)
	}
	if _, err = server.SaveInvestmentMarketMapping(context.Background(), req); status.Code(err) != codes.Unauthenticated {
		t.Fatal(err)
	}
	server = NewMarketDataServer(nil, md.NewMappingService(repo, mappingRPCRole(d.TenantRoleMember)))
	if _, err = server.SaveInvestmentMarketMapping(ctx, req); status.Code(err) != codes.PermissionDenied {
		t.Fatal(err)
	}
	if _, err = server.GetInvestmentMarketMapping(ctx, &pb.GetInvestmentMarketMappingRequest{InstrumentKey: "BOND"}); err != nil {
		t.Fatal(err)
	}
	server = NewMarketDataServer(nil, md.NewMappingService(repo, mappingRPCRole(d.TenantRoleOwner)))
	req.Periods[0].From = nil
	if _, err = server.SaveInvestmentMarketMapping(ctx, req); status.Code(err) != codes.InvalidArgument {
		t.Fatal(err)
	}
	req.Periods[0].From = timestamppb.New(at)
	repo.err = md.ErrConflict
	if _, err = server.SaveInvestmentMarketMapping(ctx, req); status.Code(err) != codes.Aborted {
		t.Fatal(err)
	}
}
