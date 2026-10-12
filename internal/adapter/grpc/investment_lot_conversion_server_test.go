package grpcadapter

import (
	"context"
	"errors"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	d "github.com/positron48/budget/internal/domain"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	inv "github.com/positron48/budget/internal/usecase/investment"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"testing"
	"time"
)

type lotPreviewRPCRepo struct {
	inv.Repo
	tenant, actor string
	input         inv.LotQuantityRequest
	result        inv.LotQuantityPreview
	err           error
}

func (r *lotPreviewRPCRepo) PreviewLotQuantity(_ context.Context, t, u string, q inv.LotQuantityRequest) (inv.LotQuantityPreview, error) {
	r.tenant = t
	r.actor = u
	r.input = q
	return r.result, r.err
}
func TestLotQuantityRPCDateOnlyExactUnitsPermissionsAndRoundTrip(t *testing.T) {
	const tenant = "11111111-1111-4111-8111-111111111111"
	const actor = "22222222-2222-4222-8222-222222222222"
	at := time.Date(2024, 1, 1, 21, 0, 0, 0, time.UTC)
	terms := &d.InvestmentLotConversion{Lots: "9007199254740993.123456789", LotSize: "10", TermsID: "stock:0", MappingVersion: 9007199254740993, Provider: "tinvest", AccessScope: "personal", InstrumentID: tenant, Evidence: "Confirmed historical lot", From: at.AddDate(-1, 0, 0), To: at.AddDate(1, 0, 0)}
	repo := &lotPreviewRPCRepo{result: inv.LotQuantityPreview{Quantity: "90071992547409931.23456789", Conversion: terms, EffectiveAt: at}}
	server := NewInvestmentServer(inv.NewService(repo, batchRoles{d.TenantRoleMember}))
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), tenant), actor)
	req := &pb.PreviewInvestmentLotQuantityRequest{InstrumentKey: "stock", Lots: "9007199254740993.1234567890", Timing: &pb.InvestmentOperationTiming{Timezone: "Europe/Moscow", OccurredDate: "2024-01-02"}}
	result, e := server.PreviewInvestmentLotQuantity(ctx, req)
	if e != nil || repo.tenant != tenant || repo.actor != actor || repo.input.Lots != terms.Lots || !repo.input.OccurredAt.Equal(at) || result.Quantity != repo.result.Quantity || result.Conversion.MappingVersion != terms.MappingVersion {
		t.Fatal(result, repo.input, e)
	}
	decoded, e := parseLotConversion(result.Conversion)
	if e != nil || decoded.Lots != terms.Lots || !decoded.From.Equal(terms.From) || decoded.MappingVersion != terms.MappingVersion {
		t.Fatal(decoded, e)
	}
	if !investmentReadMethods["/budget.v1.InvestmentService/PreviewInvestmentLotQuantity"] {
		t.Fatal("read-only resolver blocked")
	}
	if _, e = server.PreviewInvestmentLotQuantity(context.Background(), req); status.Code(e) != codes.Unauthenticated {
		t.Fatal(e)
	}
	repo.err = d.ErrInvestmentPrecondition
	if _, e = server.PreviewInvestmentLotQuantity(ctx, req); status.Code(e) != codes.FailedPrecondition {
		t.Fatal(e)
	}
	repo.err = nil
	req.OccurredAt = timestamppb.New(at.Add(time.Hour))
	if _, e = server.PreviewInvestmentLotQuantity(ctx, req); status.Code(e) != codes.InvalidArgument {
		t.Fatal("conflicting date precision", e)
	}
	req.OccurredAt = nil
	req.Timing = nil
	if _, e = server.PreviewInvestmentLotQuantity(ctx, req); status.Code(e) != codes.InvalidArgument {
		t.Fatal("missing trade time", e)
	}
	malformed := protoLotConversion(terms)
	malformed.From = &timestamppb.Timestamp{Seconds: 253402300800}
	if _, e = parseLotConversion(malformed); !errors.Is(e, d.ErrInvestmentInvalid) {
		t.Fatal("invalid timestamp coerced", e)
	}
	server = NewInvestmentServer(inv.NewService(repo, batchRoles{d.TenantRole("viewer")}))
	req.Timing = &pb.InvestmentOperationTiming{Timezone: "UTC", OccurredDate: "2024-01-02"}
	if _, e = server.PreviewInvestmentLotQuantity(ctx, req); status.Code(e) != codes.PermissionDenied {
		t.Fatal("unauthorized lookup", e)
	}
}

func TestOperationPreviewCarriesHistoricalLotProof(t *testing.T) {
	const tenant = "11111111-1111-4111-8111-111111111111"
	const actor = "22222222-2222-4222-8222-222222222222"
	const ledger = "33333333-3333-4333-8333-333333333333"
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	repo := &mutationPreviewRPCRepo{result: inv.MutationPreview{CheckedAt: at, DirtyFrom: at}}
	server := NewInvestmentServer(inv.NewService(repo, batchRoles{d.TenantRoleOwner}))
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), tenant), actor)
	terms := &pb.InvestmentLotConversion{Lots: "2", LotSize: "10", TermsId: "stock:0", MappingVersion: 1, Provider: "tinvest", AccessScope: "personal", InstrumentId: tenant, Evidence: "Reviewed historical terms", From: timestamppb.New(at), To: timestamppb.New(at.AddDate(1, 0, 0))}
	req := &pb.PreviewInvestmentOperationRequest{Action: "create", ExpectedLedgerVersion: 1, RequestKey: "lot-buy", Operation: &pb.InvestmentOperation{LedgerId: ledger, Kind: "buy", InstrumentKey: "stock", Quantity: "0", Amount: "10", Currency: "USD", OccurredAt: timestamppb.New(at), LotConversion: terms}}
	if _, e := server.PreviewInvestmentOperation(ctx, req); e != nil || repo.input.Operation.Quantity != "20" || repo.input.Operation.LotConversion == nil {
		t.Fatal(repo.input, e)
	}
	captured := protoInvestmentOperation(repo.input.Operation).GetLotConversion()
	if captured.GetLotSize() != "10" || captured.GetMappingVersion() != 1 || captured.GetEvidence() != terms.Evidence {
		t.Fatal("operation proof lost in round trip", captured)
	}
}
