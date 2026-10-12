package grpcadapter

import (
	"context"
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

type mutationPreviewRPCRepo struct {
	inv.Repo
	tenant, actor string
	input         inv.Mutation
	result        inv.MutationPreview
}

func (r *mutationPreviewRPCRepo) PreviewMutation(_ context.Context, t, u string, m inv.Mutation) (inv.MutationPreview, error) {
	r.tenant = t
	r.actor = u
	r.input = m
	return r.result, nil
}
func TestOperationPreviewRPCAuthNormalizationAndExactAmounts(t *testing.T) {
	const tenant = "11111111-1111-4111-8111-111111111111"
	const actor = "22222222-2222-4222-8222-222222222222"
	const ledger = "33333333-3333-4333-8333-333333333333"
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	cash := d.InvestmentCash{Currency: "USD", Settled: "-0.000000000001", Receivable: "0", Payable: "0", Economic: "-0.000000000001"}
	repo := &mutationPreviewRPCRepo{result: inv.MutationPreview{CheckedAt: at, DirtyFrom: at, Issues: []inv.MutationPreviewIssue{{Code: "negative_cash", At: at, Cash: &cash}}, Comparisons: []inv.MutationComparison{{Stage: "operation", At: at, Before: d.InvestmentSnapshot{Ledger: d.InvestmentLedger{StartedAt: at, CreatedAt: at}}, After: d.InvestmentSnapshot{Ledger: d.InvestmentLedger{StartedAt: at, CreatedAt: at}}, CashChange: []d.InvestmentCash{cash}}}}}
	server := NewInvestmentServer(inv.NewService(repo, batchRoles{d.TenantRoleOwner}))
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), tenant), actor)
	req := &pb.PreviewInvestmentOperationRequest{Action: "create", ExpectedLedgerVersion: 1, RequestKey: "preview", Operation: &pb.InvestmentOperation{LedgerId: ledger, Kind: "fee", Currency: "USD", Amount: "9007199254740993.123456789012", OccurredAt: timestamppb.New(at), Version: 999, Sequence: 999, Status: "voided"}}
	response, e := server.PreviewInvestmentOperation(ctx, req)
	if e != nil || repo.tenant != tenant || repo.actor != actor || repo.input.Operation.Amount != req.Operation.Amount || repo.input.Operation.Version != 0 || repo.input.Operation.Sequence != 0 || response.Issues[0].Cash.Settled != "-0.000000000001" || response.Comparisons[0].CashChange[0].Settled != "-0.000000000001" {
		t.Fatal(e, response, repo.input)
	}
	if _, e = server.PreviewInvestmentOperation(context.Background(), req); status.Code(e) != codes.Unauthenticated {
		t.Fatal(e)
	}
	req.Operation = &pb.InvestmentOperation{LedgerId: ledger, Kind: "cash_reconciliation", Currency: "USD", Amount: "0", OccurredAt: timestamppb.New(at), Note: "Statement", CashReconciliation: &pb.InvestmentCashReconciliation{ExpectedSettled: "9007199254740993.123456789012", ObservedSettled: "9007199254740993"}}
	if _, e = server.PreviewInvestmentOperation(ctx, req); e != nil || repo.input.Operation.Amount != "0.123456789012" {
		t.Fatal("reconciliation RPC terms", e, repo.input)
	}
	roundTrip := protoInvestmentOperation(repo.input.Operation)
	if roundTrip.GetCashReconciliation().GetExpectedSettled() != req.Operation.CashReconciliation.ExpectedSettled {
		t.Fatal("lost observation precision", roundTrip)
	}
	req.Operation = &pb.InvestmentOperation{LedgerId: ledger, Kind: "buy", InstrumentKey: "stock", Currency: "USD", Quantity: "1", Amount: "10", Timing: &pb.InvestmentOperationTiming{Timezone: "Europe/Moscow", OccurredDate: "2024-01-02", SettlementDate: "2024-01-03"}}
	if _, e = server.PreviewInvestmentOperation(ctx, req); e != nil || repo.input.Operation.OccurredAt.Format(time.RFC3339) != "2024-01-01T21:00:00Z" {
		t.Fatal("date-only RPC normalization", e, repo.input)
	}
	timing := protoInvestmentOperation(repo.input.Operation).GetTiming()
	if timing.GetOccurredDate() != "2024-01-02" || timing.GetSettlementDate() != "2024-01-03" {
		t.Fatal("timing round trip", timing)
	}
	req.Action = "void"
	req.Operation = &pb.InvestmentOperation{Id: "44444444-4444-4444-8444-444444444444", LedgerId: ledger}
	req.ExpectedOperationVersion = 1
	req.Reason = "Duplicate"
	if _, e = server.PreviewInvestmentOperation(ctx, req); e != nil || repo.input.Action != "void" || !repo.input.Operation.OccurredAt.IsZero() {
		t.Fatal("void should not require invented financial dates", e)
	}
	req.Operation = nil
	if _, e = server.PreviewInvestmentOperation(ctx, req); status.Code(e) != codes.InvalidArgument {
		t.Fatal("missing operation", e)
	}
}
