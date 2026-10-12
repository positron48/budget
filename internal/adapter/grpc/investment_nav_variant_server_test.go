package grpcadapter

import (
	"context"
	"errors"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strings"
	"testing"
)

type variantRepoStub struct {
	InvestmentNAVRepo
	tenant, actor, source, currency, fx string
	prepared                            bool
	err                                 error
}

func (r *variantRepoStub) NAVVariant(_ context.Context, tenant, actor, source, currency, fx string) (nav.Variant, error) {
	r.tenant, r.actor, r.source, r.currency, r.fx = tenant, actor, source, currency, fx
	return nav.Variant{Series: nav.SeriesSummary{Key: "target", LedgerIDs: []string{"a", "b"}, Currency: currency, FXPolicyKey: fx, From: "2024-01-01", To: "2024-01-02"}, Registered: r.prepared, QueuedDays: 2}, r.err
}
func (r *variantRepoStub) PrepareNAVVariant(ctx context.Context, tenant, actor, source, currency, fx string) (nav.Variant, error) {
	r.prepared = true
	return r.NAVVariant(ctx, tenant, actor, source, currency, fx)
}
func TestNAVVariantRPCIdentityAndWriteClassification(t *testing.T) {
	r := &variantRepoStub{}
	s := NewInvestmentNAVServer(r)
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), "tenant"), "actor")
	req := &pb.GetInvestmentNAVVariantRequest{SourceSeriesKey: "base", Currency: "EUR", FxPolicyKey: "cbr"}
	out, e := s.GetInvestmentNAVVariant(ctx, req)
	if e != nil || r.tenant != "tenant" || r.actor != "actor" || r.source != "base" || r.currency != "EUR" || r.fx != "cbr" || out.Registered || out.Series.Currency != "EUR" || len(out.Series.LedgerIds) != 2 {
		t.Fatal(out, e, r)
	}
	prepare := &pb.PrepareInvestmentNAVVariantRequest{SourceSeriesKey: req.SourceSeriesKey, Currency: req.Currency, FxPolicyKey: req.FxPolicyKey}
	prepared, e := s.PrepareInvestmentNAVVariant(ctx, prepare)
	if e != nil || !prepared.Registered || prepared.QueuedDays != 2 {
		t.Fatal(prepared, e)
	}
	if !investmentReadMethods["/budget.v1.InvestmentNAVService/GetInvestmentNAVVariant"] || investmentReadMethods["/budget.v1.InvestmentNAVService/PrepareInvestmentNAVVariant"] {
		t.Fatal("variant write classified as read")
	}
	if _, e = s.PrepareInvestmentNAVVariant(context.Background(), prepare); status.Code(e) != codes.Unauthenticated {
		t.Fatal(e)
	}
	r.err = errors.New("private credentials")
	if _, e = s.GetInvestmentNAVVariant(ctx, req); status.Code(e) != codes.Internal || strings.Contains(e.Error(), "private") {
		t.Fatal(e)
	}
}
