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

func TestTradeFinancialRPCExactNativeProofsAndDerivedSummaries(t *testing.T) {
	const tenant = "11111111-1111-4111-8111-111111111111"
	const actor = "22222222-2222-4222-8222-222222222222"
	const ledger = "33333333-3333-4333-8333-333333333333"
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	repo := &mutationPreviewRPCRepo{result: inv.MutationPreview{CheckedAt: at, DirtyFrom: at}}
	server := NewInvestmentServer(inv.NewService(repo, batchRoles{d.TenantRoleMember}))
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), tenant), actor)
	f := &pb.InvestmentTradeFinancials{Price: "50", PriceUnit: "currency_per_unit", ActualSettlement: "9090", Principal: &pb.InvestmentTradeMoney{Amount: "9000", Currency: "RUB", FxNumerator: "1", FxDenominator: "90", FxSource: "Contract", FxEvidence: "Synthetic report"}, Accrued: &pb.InvestmentTradeMoney{Amount: "90", Currency: "RUB", FxNumerator: "1", FxDenominator: "90", FxSource: "Contract", FxEvidence: "Synthetic report"}, Fee: &pb.InvestmentTradeMoney{Amount: "1", Currency: "EUR", FxNumerator: "1", FxDenominator: "3", FxSource: "Reviewed rate", FxEvidence: "Synthetic fee rate"}, Tax: &pb.InvestmentTradeMoney{Amount: "2", Currency: "USD"}}
	req := &pb.PreviewInvestmentOperationRequest{Action: "create", ExpectedLedgerVersion: 1, RequestKey: "native", Operation: &pb.InvestmentOperation{LedgerId: ledger, Kind: "buy", InstrumentKey: "stock", Currency: "USD", Quantity: "2", TradeFinancials: f, Timing: &pb.InvestmentOperationTiming{Timezone: "UTC", OccurredDate: "2024-01-02", SettlementDate: "2024-01-04"}}}
	if _, e := server.PreviewInvestmentOperation(ctx, req); e != nil || repo.input.Operation.Amount != "100" || repo.input.Operation.Fee != "0.333333333333" {
		t.Fatal(repo.input, e)
	}
	round := protoInvestmentOperation(repo.input.Operation).GetTradeFinancials()
	if round.GetFee().GetFxDenominator() != "3" || round.GetPrincipal().GetCurrency() != "RUB" || round.GetPrincipal().GetFxAt().GetNanos() != 999999000 || round.GetCalculatedSettlement() != "9090" || round.GetPrincipalDifferenceDenominator() != "1" {
		t.Fatal("captured terms lost", round)
	}
	f.Fee.At = &timestamppb.Timestamp{Seconds: 253402300800}
	if _, e := server.PreviewInvestmentOperation(ctx, req); status.Code(e) != codes.InvalidArgument {
		t.Fatal("invalid component date", e)
	}
	f.Fee.At = nil
	f.Fee = nil
	if _, e := server.PreviewInvestmentOperation(ctx, req); status.Code(e) != codes.InvalidArgument {
		t.Fatal("missing component accepted", e)
	}
}
