package grpcadapter

import (
	"context"
	"math/big"
	"testing"
	"time"

	pb "github.com/positron48/budget/gen/go/budget/v1"
	d "github.com/positron48/budget/internal/domain"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	inv "github.com/positron48/budget/internal/usecase/investment"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type forecastRPCRepo struct {
	inv.Repo
	tenant, actor string
	query         inv.ForecastQuery
	report        inv.ForecastReport
	event         inv.ForecastEvent
	link          inv.ForecastLink
	expected      int64
	writes        int
	err           error
}

func (r *forecastRPCRepo) Forecast(_ context.Context, t, u string, q inv.ForecastQuery) (inv.ForecastReport, error) {
	r.tenant, r.actor, r.query = t, u, q
	return r.report, r.err
}
func (r *forecastRPCRepo) ForecastCatalog(context.Context, string, string) ([]inv.ForecastEvent, []inv.ForecastLink, error) {
	return nil, nil, r.err
}
func (r *forecastRPCRepo) SaveForecastEvent(_ context.Context, t, u string, e inv.ForecastEvent, v int64) (inv.ForecastEvent, error) {
	r.tenant, r.actor, r.event, r.expected = t, u, e, v
	r.writes++
	e.Version = v + 1
	return e, r.err
}
func (r *forecastRPCRepo) SaveForecastLink(_ context.Context, t, u string, l inv.ForecastLink, v int64) (inv.ForecastLink, error) {
	r.tenant, r.actor, r.link, r.expected = t, u, l, v
	r.writes++
	l.Version = v + 1
	return l, r.err
}

func TestForecastRPCExactSnapshotValidationAndRoles(t *testing.T) {
	const tenant = "11111111-1111-4111-8111-111111111111"
	const actor = "22222222-2222-4222-8222-222222222222"
	at := time.Date(2026, 10, 12, 12, 30, 0, 123456000, time.UTC)
	amount, _ := new(big.Rat).SetString("9007199254740993.123456789012")
	repo := &forecastRPCRepo{report: inv.ForecastReport{AsOf: at, Gross: amount, Rows: []inv.ForecastRow{{Quantity: big.NewRat(1, 3), Gross: amount, Issues: []string{"current_fx_missing"}}}}}
	server := NewInvestmentServer(inv.NewService(repo, batchRoles{d.TenantRoleOwner}))
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), tenant), actor)
	result, e := server.GetInvestmentForecast(ctx, &pb.GetInvestmentForecastRequest{FromMonth: "2026-10", Currency: "USD", TaxRate: "0"})
	if e != nil || repo.tenant != tenant || repo.actor != actor || repo.query.TaxRate != "0" || result.Gross.Decimal != "9007199254740993.123456789012" || result.EstimatedNet != nil || result.Rows[0].Quantity.Denominator != "3" || result.AsOf.Nanos != 123456000 {
		t.Fatal(e, result)
	}
	event := &pb.InvestmentForecastEvent{Id: tenant, InstrumentKey: "STOCK", Kind: "dividend", Nature: "announced", Currency: "USD", PerShare: "1.25", PaymentDate: "2026-11-05", Timezone: "Europe/Moscow", EntitlementRule: "settled_by_cutoff", EntitlementAt: timestamppb.New(at), SourceReference: "Synthetic source", Evidence: "Explicit fixture cutoff", Reason: "Reviewed fixture"}
	saved, e := server.SaveInvestmentForecastEvent(ctx, &pb.SaveInvestmentForecastEventRequest{Event: event, ExpectedVersion: 9007199254740993})
	if e != nil || repo.expected != 9007199254740993 || saved.Event.Version != 9007199254740994 || !repo.event.EntitlementAt.Equal(at) {
		t.Fatal(e, saved)
	}
	raw, e := proto.Marshal(saved)
	if e != nil {
		t.Fatal(e)
	}
	var decoded pb.SaveInvestmentForecastEventResponse
	if e = proto.Unmarshal(raw, &decoded); e != nil || decoded.Event.Version != saved.Event.Version {
		t.Fatal("wire version precision", e)
	}
	link := &pb.InvestmentForecastLink{EventId: tenant, EventVersion: 1, LedgerId: tenant, OperationId: actor, OperationVersion: 2, Reason: "Fixture statement", Complete: true}
	if _, e = server.SaveInvestmentForecastLink(ctx, &pb.SaveInvestmentForecastLinkRequest{Link: link}); e != nil || !repo.link.Complete {
		t.Fatal(e)
	}
	bad := proto.Clone(event).(*pb.InvestmentForecastEvent)
	bad.EntitlementAt = &timestamppb.Timestamp{Seconds: at.Unix(), Nanos: 1000000000}
	if _, e = server.SaveInvestmentForecastEvent(ctx, &pb.SaveInvestmentForecastEventRequest{Event: bad}); status.Code(e) != codes.InvalidArgument {
		t.Fatal("normalized malformed cutoff", e)
	}
	if _, e = server.SaveInvestmentForecastEvent(ctx, &pb.SaveInvestmentForecastEventRequest{Event: event, ExpectedVersion: 1<<63 - 1}); status.Code(e) != codes.InvalidArgument {
		t.Fatal("overflow version", e)
	}
	if _, e = server.GetInvestmentForecast(context.Background(), &pb.GetInvestmentForecastRequest{}); status.Code(e) != codes.Unauthenticated {
		t.Fatal(e)
	}
	member := NewInvestmentServer(inv.NewService(repo, batchRoles{d.TenantRoleMember}))
	before := repo.writes
	if _, e = member.SaveInvestmentForecastEvent(ctx, &pb.SaveInvestmentForecastEventRequest{Event: event}); status.Code(e) != codes.PermissionDenied || repo.writes != before {
		t.Fatal("member wrote catalog", e)
	}
	if _, e = member.GetInvestmentForecast(ctx, &pb.GetInvestmentForecastRequest{FromMonth: "2026-10", Currency: "USD"}); e != nil {
		t.Fatal(e)
	}
	repo.err = d.ErrInvestmentConflict
	if _, e = server.SaveInvestmentForecastLink(ctx, &pb.SaveInvestmentForecastLinkRequest{Link: link}); status.Code(e) != codes.Aborted {
		t.Fatal(e)
	}
	if !investmentReadMethods["/budget.v1.InvestmentService/GetInvestmentForecast"] || investmentReadMethods["/budget.v1.InvestmentService/SaveInvestmentForecastEvent"] || investmentReadMethods["/budget.v1.InvestmentService/SaveInvestmentForecastLink"] {
		t.Fatal("read-only classification")
	}
}
