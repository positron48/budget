package grpcadapter

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	pb "github.com/positron48/budget/gen/go/budget/v1"
	d "github.com/positron48/budget/internal/domain"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type marketReadStub struct {
	md.ReadRepo
	enabled bool
	values  []md.SavedObservation
	err     error
}

func (r marketReadStub) SourceEnabled(context.Context, md.Scope) (bool, error) { return r.enabled, nil }
func (r marketReadStub) Observations(context.Context, md.Scope, string, string, time.Time, time.Time) ([]md.SavedObservation, error) {
	return r.values, r.err
}

type marketRolesStub struct{}

func (marketRolesStub) GetUserRole(context.Context, string, string) (d.TenantRole, error) {
	return d.TenantRoleMember, nil
}
func TestMarketHistoryRPCProvenanceAndErrors(t *testing.T) {
	const id = "11111111-1111-4111-8111-111111111111"
	at := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), id), id)
	req := &pb.GetMarketHistoryRequest{Source: &pb.MarketSourceKey{Provider: "tinvest", AccessScope: "personal"}, InstrumentId: id, Kind: "daily_candle", From: timestamppb.New(at), To: timestamppb.New(at)}
	repo := marketReadStub{enabled: true, values: []md.SavedObservation{{Observation: md.Observation{InstrumentID: id, At: at, Price: "9007199254740993.000000001", PriceUnit: "provider_unspecified", Adjustment: "provider_unspecified", Complete: false}, Revision: 2, ReceivedAt: at}}}
	server := NewMarketDataServer(md.NewService(repo, marketRolesStub{}), nil)
	result, err := server.GetMarketHistory(ctx, req)
	if err != nil || result.CoverageState != "observations_only" || len(result.Observations) != 1 || result.Observations[0].Price != "9007199254740993.000000001" || result.Observations[0].Complete || result.Observations[0].Revision != 2 {
		t.Fatal(result, err)
	}
	repo.values = nil
	server = NewMarketDataServer(md.NewService(repo, marketRolesStub{}), nil)
	result, err = server.GetMarketHistory(ctx, req)
	if err != nil || result.CoverageState != "unavailable" {
		t.Fatal(result, err)
	}
	if _, err = server.GetMarketHistory(context.Background(), req); status.Code(err) != codes.Unauthenticated {
		t.Fatal(err)
	}
	repo.err = errors.New("database secret details")
	server = NewMarketDataServer(md.NewService(repo, marketRolesStub{}), nil)
	_, err = server.GetMarketHistory(ctx, req)
	if status.Code(err) != codes.Internal || strings.Contains(err.Error(), "secret") {
		t.Fatal("unsafe database error", err)
	}
	req.From = nil
	if _, err = server.GetMarketHistory(ctx, req); status.Code(err) != codes.InvalidArgument {
		t.Fatal("missing date accepted", err)
	}
}
func TestMarketJobDoesNotExposeWorkerLease(t *testing.T) {
	raw, err := protojson.Marshal(protoMarketJob(md.JobStatus{Job: md.Job{ID: "public-id", LeaseToken: "private-worker-token", LeaseUntil: time.Now(), Status: "running"}}))
	if err != nil || strings.Contains(string(raw), "private-worker-token") || strings.Contains(string(raw), "lease") {
		t.Fatal("worker capability leaked", err)
	}
}
