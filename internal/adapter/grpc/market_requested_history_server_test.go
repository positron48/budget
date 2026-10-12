package grpcadapter

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	pb "github.com/positron48/budget/gen/go/budget/v1"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type requestedHistoryStub struct {
	marketReadStub
	calls *int
}

func (r requestedHistoryStub) RequestHistory(_ context.Context, scope md.Scope, q md.JobRequest) ([]md.Job, error) {
	*r.calls++
	if r.err != nil {
		return nil, r.err
	}
	requests, err := md.PlanRequestedHistory(q)
	if err != nil {
		return nil, err
	}
	var jobs []md.Job
	for _, q := range requests {
		jobs = append(jobs, md.Job{ID: "job", Scope: scope, JobRequest: q, Status: "pending"})
	}
	return jobs, nil
}
func TestMarketRequestedHistoryRPCBoundsAndAuthorization(t *testing.T) {
	const id = "11111111-1111-4111-8111-111111111111"
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), id), id)
	from := time.Date(2019, 10, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	calls := 0
	repo := requestedHistoryStub{marketReadStub: marketReadStub{enabled: true}, calls: &calls}
	server := NewMarketDataServer(md.NewService(repo, marketRolesStub{}), nil)
	req := &pb.RequestMarketHistoryRequest{Source: &pb.MarketSourceKey{Provider: "tinvest", AccessScope: "personal"}, InstrumentId: id, From: timestamppb.New(from), To: timestamppb.New(to)}
	out, err := server.RequestMarketHistory(ctx, req)
	if err != nil || len(out.Jobs) != 8 || out.CoverageState != "requests_only" || calls != 1 {
		t.Fatal(out, err, calls)
	}
	if !out.Jobs[0].From.AsTime().Equal(from) || !out.Jobs[7].To.AsTime().Equal(to) {
		t.Fatal("range lost")
	}
	if _, err = server.RequestMarketHistory(context.Background(), req); status.Code(err) != codes.Unauthenticated {
		t.Fatal(err)
	}
	if investmentReadMethods["/budget.v1.MarketDataService/RequestMarketHistory"] {
		t.Fatal("history loader must remain a write")
	}
	req.To = timestamppb.New(from.AddDate(30, 0, 0).Add(time.Second))
	if _, err = server.RequestMarketHistory(ctx, req); status.Code(err) != codes.InvalidArgument || calls != 1 {
		t.Fatal("unbounded request forwarded", err, calls)
	}
	req.To = timestamppb.New(to)
	req.From = nil
	if _, err = server.RequestMarketHistory(ctx, req); status.Code(err) != codes.InvalidArgument || calls != 1 {
		t.Fatal(err, calls)
	}
	req.From = timestamppb.New(from)
	repo.err = errors.New("secret failure")
	server = NewMarketDataServer(md.NewService(repo, marketRolesStub{}), nil)
	if _, err = server.RequestMarketHistory(ctx, req); status.Code(err) != codes.Internal || strings.Contains(err.Error(), "secret") {
		t.Fatal(err)
	}
}
