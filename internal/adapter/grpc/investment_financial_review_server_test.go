package grpcadapter

import (
	"context"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	d "github.com/positron48/budget/internal/domain"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"testing"
)

type financialReviewStub struct {
	imp.ImportRepo
	tenant, actor string
	request       imp.FinancialReviewRequest
}

func (r *financialReviewStub) PreviewFinancialImport(_ context.Context, t, u string, in imp.FinancialReviewRequest) (imp.FinancialReview, error) {
	r.tenant = t
	r.actor = u
	r.request = in
	return imp.FinancialReview{Ready: 1, Total: 1, Rows: []imp.FinancialCandidate{{Record: 1, Operation: &d.InvestmentOperation{Amount: "9007199254740993.123456789012"}}}}, nil
}
func TestFinancialReviewRPCGuardsAndExactMoney(t *testing.T) {
	tenant := "11111111-1111-4111-8111-111111111111"
	actor := "22222222-2222-4222-8222-222222222222"
	repo := &financialReviewStub{}
	server := NewInvestmentImportServer(imp.NewImportService(repo, batchRoles{d.TenantRoleOwner}))
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), tenant), actor)
	req := &pb.PreviewInvestmentFinancialMappingRequest{ImportId: "44444444-4444-4444-8444-444444444444", ExpectedImportVersion: 1, RequestKey: "prepare", MappingJson: []byte(`{"timezone":"UTC"}`)}
	response, err := server.PreviewInvestmentFinancialMapping(ctx, req)
	if err != nil || response.Rows[0].Operation.Amount != "9007199254740993.123456789012" || repo.tenant != tenant || repo.actor != actor || repo.request.Limit != 200 {
		t.Fatal(response, err)
	}
	if _, err = server.PreviewInvestmentFinancialMapping(context.Background(), req); status.Code(err) != codes.Unauthenticated {
		t.Fatal(err)
	}
	member := NewInvestmentImportServer(imp.NewImportService(repo, batchRoles{d.TenantRoleMember}))
	if _, err = member.PreviewInvestmentFinancialMapping(ctx, req); status.Code(err) != codes.PermissionDenied {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"timezone":"UTC","unknown":true}`, `{} {}`, `not json`} {
		req.MappingJson = []byte(raw)
		if _, err = server.PreviewInvestmentFinancialMapping(ctx, req); status.Code(err) != codes.InvalidArgument {
			t.Fatal(raw, err)
		}
	}
}
