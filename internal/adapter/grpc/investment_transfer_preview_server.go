package grpcadapter

import (
	"context"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func protoInvestmentTiming(v *d.InvestmentOperationTiming) *pb.InvestmentOperationTiming {
	if v == nil {
		return nil
	}
	return &pb.InvestmentOperationTiming{Timezone: v.Timezone, OccurredDate: v.OccurredDate, SettlementDate: v.SettlementDate, SourceDate: v.SourceDate}
}
func parseInvestmentTiming(v *pb.InvestmentOperationTiming) *d.InvestmentOperationTiming {
	if v == nil {
		return nil
	}
	return &d.InvestmentOperationTiming{Timezone: v.GetTimezone(), OccurredDate: v.GetOccurredDate(), SettlementDate: v.GetSettlementDate(), SourceDate: v.GetSourceDate()}
}

type transferMutationRequest interface {
	GetTransfer() *pb.InvestmentTransfer
	GetAction() string
	GetExpectedVersion() int64
	GetExpectedFromVersion() int64
	GetExpectedToVersion() int64
	GetRequestKey() string
	GetReason() string
	GetPreviewHash() string
}
type cashTransferMutationRequest interface {
	GetTransfer() *pb.AssetTransfer
	GetTiming() *pb.InvestmentOperationTiming
	GetAction() string
	GetExpectedVersion() int64
	GetExpectedLedgerVersion() int64
	GetExpectedFromAccountVersion() int64
	GetExpectedToAccountVersion() int64
	GetRequestKey() string
	GetReason() string
	GetPreviewHash() string
}

func parseTransferMutation(req transferMutationRequest) (inv.TransferMutation, error) {
	p := req.GetTransfer()
	v := d.InvestmentTransfer{Timing: parseInvestmentTiming(p.GetTiming()), ID: p.GetId(), FromLedgerID: p.GetFromLedgerId(), ToLedgerID: p.GetToLedgerId(), Currency: p.GetCurrency(), Amount: p.GetAmount(), InstrumentKey: p.GetInstrumentKey(), Quantity: p.GetQuantity(), Note: p.GetNote()}
	if req.GetAction() != "void" && (p.GetOccurredAt() != nil || v.Timing == nil || v.Timing.OccurredDate == "") {
		var e error
		v.OccurredAt, e = assetTimestamp(p.GetOccurredAt())
		if e != nil {
			return inv.TransferMutation{}, e
		}
	}
	return inv.TransferMutation{Action: req.GetAction(), Transfer: v, ExpectedVersion: req.GetExpectedVersion(), ExpectedFromVersion: req.GetExpectedFromVersion(), ExpectedToVersion: req.GetExpectedToVersion(), RequestKey: req.GetRequestKey(), Reason: req.GetReason(), PreviewHash: req.GetPreviewHash()}, nil
}
func parseCashTransferMutation(req cashTransferMutationRequest, t, u string) (inv.CashTransferMutation, error) {
	p := req.GetTransfer()
	timing := parseInvestmentTiming(req.GetTiming())
	v := d.AssetTransfer{ID: p.GetId(), FromAccountID: p.GetFromAccountId(), ToAccountID: p.GetToAccountId()}
	if req.GetAction() != "void" {
		if p.GetOccurredAt() == nil && timing != nil && timing.OccurredDate != "" {
			o := d.InvestmentOperation{Kind: "deposit", Currency: "RUB", Amount: "1", Timing: timing}
			if e := inv.ValidateOperation(&o); e != nil {
				return inv.CashTransferMutation{}, investmentRPCError(e)
			}
			p = &pb.AssetTransfer{Id: p.GetId(), FromAccountId: p.GetFromAccountId(), ToAccountId: p.GetToAccountId(), FromAmount: p.GetFromAmount(), ToAmount: p.GetToAmount(), Note: p.GetNote(), OccurredAt: timestamppb.New(o.OccurredAt)}
		}
		var e error
		v, e = fromProtoAssetTransfer(p, t, u)
		if e != nil {
			return inv.CashTransferMutation{}, e
		}
	}
	return inv.CashTransferMutation{Action: req.GetAction(), Transfer: v, Timing: timing, ExpectedVersion: req.GetExpectedVersion(), ExpectedLedgerVersion: req.GetExpectedLedgerVersion(), ExpectedFromAccountVersion: req.GetExpectedFromAccountVersion(), ExpectedToAccountVersion: req.GetExpectedToAccountVersion(), Reason: req.GetReason(), RequestKey: req.GetRequestKey(), PreviewHash: req.GetPreviewHash()}, nil
}
func (s *InvestmentServer) PreviewInvestmentTransfer(ctx context.Context, req *pb.PreviewInvestmentTransferRequest) (*pb.PreviewInvestmentTransferResponse, error) {
	t, u, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	m, e := parseTransferMutation(req)
	if e != nil {
		return nil, e
	}
	v, e := s.svc.PreviewTransfer(ctx, t, u, m)
	if e != nil {
		return nil, investmentRPCError(e)
	}
	return &pb.PreviewInvestmentTransferResponse{Transfer: protoInvestmentTransfer(v.Transfer), Preview: protoMutationPreview(v.Preview)}, nil
}
func (s *InvestmentServer) PreviewInvestmentCashTransfer(ctx context.Context, req *pb.PreviewInvestmentCashTransferRequest) (*pb.PreviewInvestmentCashTransferResponse, error) {
	t, u, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	m, e := parseCashTransferMutation(req, t, u)
	if e != nil {
		return nil, e
	}
	v, e := s.svc.PreviewCashTransfer(ctx, t, u, m)
	if e != nil {
		return nil, investmentRPCError(e)
	}
	out := &pb.PreviewInvestmentCashTransferResponse{Transfer: protoCashTransfer(v.Transfer), Preview: protoMutationPreview(v.Preview)}
	for _, a := range v.Accounts {
		out.Accounts = append(out.Accounts, &pb.InvestmentTransferAssetComparison{Stage: a.Stage, AsOf: timestamppb.New(a.At), AccountId: a.AccountID, Currency: a.Currency, BeforeKnown: a.BeforeKnown, AfterKnown: a.AfterKnown, BeforeMinor: a.BeforeMinor, AfterMinor: a.AfterMinor})
	}
	return out, nil
}
