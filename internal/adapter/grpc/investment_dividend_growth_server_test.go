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

type dividendRPCRepo struct {
	inv.Repo
	report        inv.DividendGrowthReport
	tenant, actor string
	instrument    string
	expected      int64
	writes        int
	err           error
}

func (r *dividendRPCRepo) DividendGrowth(_ context.Context, t, u string) (inv.DividendGrowthReport, error) {
	r.tenant, r.actor = t, u
	return r.report, r.err
}

func (r *dividendRPCRepo) DividendGrowthForInstrument(ctx context.Context, t, u, key string) (inv.DividendGrowthReport, error) {
	r.instrument = key
	return r.DividendGrowth(ctx, t, u)
}

func (r *dividendRPCRepo) SaveDividendHistory(_ context.Context, t, u string, h inv.DividendHistory, v int64) (inv.DividendHistory, error) {
	r.tenant, r.actor, r.expected = t, u, v
	r.writes++
	h.Version = v + 1
	return h, r.err
}

func TestDividendGrowthRPCExactRolesAndMalformedTimestamp(t *testing.T) {
	const tenant = "11111111-1111-4111-8111-111111111111"
	const actor = "22222222-2222-4222-8222-222222222222"
	at := time.Date(2025, 2, 1, 0, 0, 0, 123456000, time.UTC)
	amount, _ := new(big.Rat).SetString("9007199254740993.123456789012")
	h := inv.DividendHistory{ID: tenant, InstrumentKey: "STOCK", Currency: "USD", Timezone: "Europe/Moscow", BasisAt: at, ReviewedFrom: at.AddDate(-2, 0, 0), SourceReference: "Issuer", Evidence: "Complete unit history", Reason: "Fixture", Version: 9007199254740993}
	repo := &dividendRPCRepo{report: inv.DividendGrowthReport{AsOf: at, Histories: []inv.DividendHistoryReport{{History: h, Years: []inv.DividendYearGrowth{{Year: 2024, Regular: amount, Special: big.NewRat(1, 3), Status: "incomplete_history"}}}}}}
	server := NewInvestmentServer(inv.NewService(repo, batchRoles{d.TenantRoleOwner}))
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), tenant), actor)
	out, e := server.GetInvestmentDividendGrowth(ctx, &pb.GetInvestmentDividendGrowthRequest{})
	if e != nil || repo.tenant != tenant || repo.actor != actor || out.Histories[0].History.Version != h.Version || out.Histories[0].Years[0].Regular.Decimal != "9007199254740993.123456789012" || out.Histories[0].Years[0].PercentChange != nil || out.Histories[0].Years[0].Special.Denominator != "3" {
		t.Fatal(e, out)
	}
	filtered, e := server.GetInvestmentDividendGrowth(ctx, &pb.GetInvestmentDividendGrowthRequest{InstrumentKey: "STOCK"})
	if e != nil || repo.instrument != "STOCK" || filtered.Histories[0].History.InstrumentKey != "STOCK" {
		t.Fatal(filtered, e)
	}
	if _, e = server.GetInvestmentDividendGrowth(ctx, &pb.GetInvestmentDividendGrowthRequest{InstrumentKey: " STOCK"}); status.Code(e) != codes.InvalidArgument {
		t.Fatal("malformed key accepted", e)
	}
	saved, e := server.SaveInvestmentDividendHistory(ctx, &pb.SaveInvestmentDividendHistoryRequest{History: protoDividendHistory(h), ExpectedVersion: h.Version})
	if e != nil || saved.History.Version != h.Version+1 || repo.expected != h.Version || saved.History.BasisAt.Nanos != 123456000 {
		t.Fatal(e, saved)
	}
	raw, _ := proto.Marshal(saved)
	var decoded pb.SaveInvestmentDividendHistoryResponse
	if e = proto.Unmarshal(raw, &decoded); e != nil || decoded.History.Version != h.Version+1 {
		t.Fatal(e)
	}
	invalid := protoDividendHistory(h)
	invalid.BasisAt = &timestamppb.Timestamp{Seconds: at.Unix(), Nanos: 1000000000}
	if _, e = server.SaveInvestmentDividendHistory(ctx, &pb.SaveInvestmentDividendHistoryRequest{History: invalid}); status.Code(e) != codes.InvalidArgument {
		t.Fatal(e)
	}
	member := NewInvestmentServer(inv.NewService(repo, batchRoles{d.TenantRoleMember}))
	before := repo.writes
	if _, e = member.SaveInvestmentDividendHistory(ctx, &pb.SaveInvestmentDividendHistoryRequest{History: protoDividendHistory(h)}); status.Code(e) != codes.PermissionDenied || repo.writes != before {
		t.Fatal(e)
	}
	if _, e = member.GetInvestmentDividendGrowth(ctx, &pb.GetInvestmentDividendGrowthRequest{}); e != nil {
		t.Fatal(e)
	}
	if _, e = server.GetInvestmentDividendGrowth(context.Background(), &pb.GetInvestmentDividendGrowthRequest{}); status.Code(e) != codes.Unauthenticated {
		t.Fatal(e)
	}
	repo.err = d.ErrInvestmentConflict
	if _, e = server.SaveInvestmentDividendHistory(ctx, &pb.SaveInvestmentDividendHistoryRequest{History: protoDividendHistory(h), ExpectedVersion: h.Version}); status.Code(e) != codes.Aborted {
		t.Fatal(e)
	}
}
