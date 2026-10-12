package grpcadapter

import (
	"context"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	d "github.com/positron48/budget/internal/domain"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	inv "github.com/positron48/budget/internal/usecase/investment"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"testing"
	"time"
)

type transferPreviewRPCRepo struct {
	mutationPreviewRPCRepo
	transfer inv.TransferMutation
	cash     inv.CashTransferMutation
}

func (r *transferPreviewRPCRepo) PreviewTransfer(_ context.Context, _, _ string, m inv.TransferMutation) (inv.TransferPreview, error) {
	r.transfer = m
	return inv.TransferPreview{Transfer: m.Transfer, Preview: inv.MutationPreview{Valid: true, Hash: "hash", CheckedAt: time.Now()}}, nil
}
func (r *transferPreviewRPCRepo) PreviewCashTransfer(_ context.Context, _, _ string, m inv.CashTransferMutation) (inv.CashTransferPreview, error) {
	r.cash = m
	return inv.CashTransferPreview{Transfer: inv.CashTransfer{Transfer: m.Transfer, Timing: m.Timing}, Preview: inv.MutationPreview{Valid: true, Hash: "hash", CheckedAt: time.Now()}}, nil
}
func TestLinkedTransferRPCDateOnlyAndReadOnlyContracts(t *testing.T) {
	const tenant = "11111111-1111-4111-8111-111111111111"
	const actor = "22222222-2222-4222-8222-222222222222"
	const from = "33333333-3333-4333-8333-333333333333"
	const to = "44444444-4444-4444-8444-444444444444"
	repo := &transferPreviewRPCRepo{}
	server := NewInvestmentServer(inv.NewService(repo, batchRoles{d.TenantRoleMember}))
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), tenant), actor)
	req := &pb.PreviewInvestmentTransferRequest{Action: "create", RequestKey: "pair", ExpectedFromVersion: 1, ExpectedToVersion: 1, Transfer: &pb.InvestmentTransfer{FromLedgerId: from, ToLedgerId: to, InstrumentKey: "S", Quantity: "9007199254740993.125", Currency: "USD", Timing: &pb.InvestmentOperationTiming{Timezone: "Europe/Moscow", OccurredDate: "2024-01-02"}}}
	got, e := server.PreviewInvestmentTransfer(ctx, req)
	if e != nil || !got.Preview.Valid || repo.transfer.Transfer.OccurredAt.Format(time.RFC3339) != "2024-01-01T21:00:00Z" || got.Transfer.Timing.OccurredDate != "2024-01-02" {
		t.Fatal(got, e)
	}
	cash := &pb.PreviewInvestmentCashTransferRequest{Action: "create", RequestKey: "cash", ExpectedLedgerVersion: 1, ExpectedFromAccountVersion: 1, ExpectedToAccountVersion: 1, Timing: &pb.InvestmentOperationTiming{Timezone: "Europe/Moscow", OccurredDate: "2024-01-02"}, Transfer: &pb.AssetTransfer{FromAccountId: from, ToAccountId: to, FromAmount: &pb.Money{CurrencyCode: "USD", MinorUnits: 9007199254740993}, ToAmount: &pb.Money{CurrencyCode: "USD", MinorUnits: 9007199254740993}}}
	result, e := server.PreviewInvestmentCashTransfer(ctx, cash)
	if e != nil || !result.Preview.Valid || repo.cash.Transfer.OccurredAt.Format(time.RFC3339Nano) != "2024-01-02T20:59:59.999999Z" || repo.cash.Transfer.FromAmount.MinorUnits != 9007199254740993 {
		t.Fatal(result, e)
	}
	req.Transfer.Timing.Timezone = "Local"
	if _, e = server.PreviewInvestmentTransfer(ctx, req); status.Code(e) != codes.InvalidArgument {
		t.Fatal(e)
	}
	for _, method := range []string{"PreviewInvestmentTransfer", "PreviewInvestmentCashTransfer"} {
		if !investmentReadMethods["/budget.v1.InvestmentService/"+method] {
			t.Fatal(method)
		}
	}
}
