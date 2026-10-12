package grpcadapter

import (
	"context"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	inv "github.com/positron48/budget/internal/usecase/investment"
	"google.golang.org/protobuf/types/known/timestamppb"
	"math/big"
	"strings"
)

func protoPayoutAmount(v *big.Rat) *pb.InvestmentPayoutAmount {
	if v == nil {
		return nil
	}
	return &pb.InvestmentPayoutAmount{Numerator: v.Num().String(), Denominator: v.Denom().String(), Decimal: strings.TrimRight(strings.TrimRight(v.FloatString(12), "0"), ".")}
}
func protoPayoutTotals(v inv.PayoutTotals) *pb.InvestmentPayoutTotals {
	return &pb.InvestmentPayoutTotals{Month: v.Month, Currency: v.Currency, Gross: protoPayoutAmount(v.Gross), Tax: protoPayoutAmount(v.Tax), IncomeNet: protoPayoutAmount(v.IncomeNet), Fees: protoPayoutAmount(v.Fees), StandaloneTax: protoPayoutAmount(v.StandaloneTax), TaxRefund: protoPayoutAmount(v.TaxRefund), PrincipalReturn: protoPayoutAmount(v.PrincipalReturn), NetCash: protoPayoutAmount(v.NetCash)}
}
func (s *InvestmentServer) GetInvestmentPayouts(ctx context.Context, req *pb.GetInvestmentPayoutsRequest) (*pb.GetInvestmentPayoutsResponse, error) {
	t, u, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	v, e := s.svc.Payouts(ctx, t, u, inv.PayoutQuery{LedgerIDs: req.GetLedgerIds(), FromDate: req.GetFromDate(), ToDate: req.GetToDate(), InstrumentKey: req.GetInstrumentKey(), Kinds: req.GetKinds(), Cursor: req.GetCursor(), Limit: int(req.GetLimit())})
	if e != nil {
		return nil, investmentRPCError(e)
	}
	out := &pb.GetInvestmentPayoutsResponse{TotalCount: v.TotalCount, NextCursor: v.NextCursor, AsOf: timestamppb.New(v.AsOf), Warnings: v.Warnings}
	for _, g := range v.Growth {
		out.Growth = append(out.Growth, protoPayoutGrowth(g))
	}
	for _, l := range v.Ledgers {
		out.Ledgers = append(out.Ledgers, protoLedger(l))
	}
	for _, t := range v.Totals {
		out.Totals = append(out.Totals, protoPayoutTotals(t))
	}
	for _, m := range v.Months {
		out.Months = append(out.Months, protoPayoutTotals(m))
	}
	for _, p := range v.Entries {
		out.Entries = append(out.Entries, &pb.InvestmentPayoutEntry{Id: p.ID, OperationId: p.OperationID, LedgerId: p.LedgerID, LedgerName: p.LedgerName, InstrumentKey: p.InstrumentKey, Kind: p.Kind, Currency: p.Currency, Note: p.Note, At: timestamppb.New(p.At), OperationVersion: p.OperationVersion, Gross: protoPayoutAmount(p.Gross), Tax: protoPayoutAmount(p.Tax), Net: protoPayoutAmount(p.Net), SettlementAssumed: p.SettlementAssumed, TaxReviewRequired: p.TaxReviewRequired})
	}
	return out, nil
}

func protoPayoutGrowth(g inv.PayoutGrowth) *pb.InvestmentPayoutGrowth {
	return &pb.InvestmentPayoutGrowth{Month: g.Month, PreviousMonth: g.PreviousMonth, Currency: g.Currency, IncomeNet: protoPayoutAmount(g.IncomeNet), PreviousIncomeNet: protoPayoutAmount(g.PreviousIncomeNet), Change: protoPayoutAmount(g.Change), PercentChange: protoPayoutAmount(g.PercentChange), Status: g.Status}
}
