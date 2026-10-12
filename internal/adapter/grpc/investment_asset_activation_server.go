package grpcadapter

import (
	"context"

	pb "github.com/positron48/budget/gen/go/budget/v1"
	d "github.com/positron48/budget/internal/domain"
)

func parseAssetActivation(p *pb.InvestmentAssetActivation) (d.InvestmentAssetActivation, error) {
	at, e := assetTimestamp(p.GetValuationFrom())
	if e != nil {
		return d.InvestmentAssetActivation{}, e
	}
	return d.InvestmentAssetActivation{AccountID: p.GetAccountId(), LedgerID: p.GetLedgerId(), ConventionKey: p.GetConventionKey(), ValuationFrom: at, PreviewHash: p.GetPreviewHash(), ExpectedAccountVersion: p.GetExpectedAccountVersion(), ExpectedLedgerVersion: p.GetExpectedLedgerVersion(), Evidence: p.GetEvidence(), RequestKey: p.GetRequestKey()}, nil
}
func (s *InvestmentServer) PrepareInvestmentAssetActivation(ctx context.Context, req *pb.PrepareInvestmentAssetActivationRequest) (*pb.PrepareInvestmentAssetActivationResponse, error) {
	t, u, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	m, e := parseAssetActivation(req.GetActivation())
	if e != nil {
		return nil, e
	}
	p, _, e := s.svc.AssetActivation(ctx, t, u, m, false)
	if e != nil {
		return nil, investmentRPCError(e)
	}
	return &pb.PrepareInvestmentAssetActivationResponse{Preview: &pb.InvestmentAssetActivationPreview{Account: toProtoAssetAccount(p.Account), Ledger: protoLedger(p.Ledger), ValuationFrom: optionalInvestmentTimestamp(p.ValuationFrom), ManualValue: protoAssetMoney(p.ManualValue), LedgerKnownValue: protoAssetMoney(p.LedgerKnownValue), Complete: p.Complete, Matches: p.Matches, Issues: p.Issues, InventoryCount: p.InventoryCount, PreviewHash: p.PreviewHash}}, nil
}
func (s *InvestmentServer) ActivateInvestmentAsset(ctx context.Context, req *pb.ActivateInvestmentAssetRequest) (*pb.ActivateInvestmentAssetResponse, error) {
	t, u, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	m, e := parseAssetActivation(req.GetActivation())
	if e != nil {
		return nil, e
	}
	_, a, e := s.svc.AssetActivation(ctx, t, u, m, true)
	if e != nil {
		return nil, investmentRPCError(e)
	}
	return &pb.ActivateInvestmentAssetResponse{Account: toProtoAssetAccount(a)}, nil
}
