package grpcadapter

import (
	"context"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *InvestmentImportServer) UploadBrokerSnapshot(ctx context.Context, r *pb.UploadBrokerSnapshotRequest) (*pb.UploadBrokerSnapshotResponse, error) {
	tenant, user, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	id, err := s.svc.UploadBrokerSnapshot(ctx, tenant, user, r.GetOriginalJson())
	if err != nil {
		return nil, investmentImportError(err)
	}
	return &pb.UploadBrokerSnapshotResponse{Id: id}, nil
}
func (s *InvestmentImportServer) GetBrokerSnapshot(ctx context.Context, r *pb.GetBrokerSnapshotRequest) (*pb.GetBrokerSnapshotResponse, error) {
	tenant, user, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := s.svc.GetBrokerSnapshot(ctx, tenant, user, r.GetId())
	if err != nil {
		return nil, investmentImportError(err)
	}
	return &pb.GetBrokerSnapshotResponse{OriginalJson: raw}, nil
}

func (s *InvestmentImportServer) ListBrokerSnapshots(ctx context.Context, r *pb.ListBrokerSnapshotsRequest) (*pb.ListBrokerSnapshotsResponse, error) {
	tenant, user, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	items, err := s.svc.ListBrokerSnapshots(ctx, tenant, user, int(r.GetLimit()), int(r.GetOffset()))
	if err != nil {
		return nil, investmentImportError(err)
	}
	response := &pb.ListBrokerSnapshotsResponse{}
	for _, item := range items {
		response.Snapshots = append(response.Snapshots, &pb.BrokerSnapshotInfo{Id: item.ID, AccountId: item.AccountID, SourceSha256: item.SHA256, RowCount: int32(item.Rows), CreatedAt: timestamppb.New(item.CreatedAt)})
	}
	return response, nil
}

func (s *InvestmentImportServer) ListBrokerSnapshotRows(ctx context.Context, r *pb.ListBrokerSnapshotRowsRequest) (*pb.ListBrokerSnapshotRowsResponse, error) {
	tenant, user, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	items, err := s.svc.BrokerSnapshotRows(ctx, tenant, user, r.GetId(), int(r.GetLimit()), int(r.GetOffset()))
	if err != nil {
		return nil, investmentImportError(err)
	}
	response := &pb.ListBrokerSnapshotRowsResponse{}
	for _, item := range items {
		response.Rows = append(response.Rows, &pb.BrokerSnapshotRow{Ordinal: int32(item.Ordinal), ObservationId: item.ObservationID, AccountId: item.AccountID, OperationId: item.OperationID, ContentSha256: item.SHA256, OriginalJson: item.Raw, HasOtherVersions: item.HasOtherVersions, LinkId: item.LinkID, LinkedLedgerId: item.LinkedLedgerID, LinkedOperationId: item.LinkedOperationID, LinkedStatus: item.LinkedStatus, LinkedVersion: item.LinkedVersion, CurrentVersion: item.CurrentVersion, ClaimedDifferentContent: item.ClaimedDifferentContent})
	}
	return response, nil
}

func (s *InvestmentImportServer) PreviewBrokerTrade(ctx context.Context, r *pb.PreviewBrokerTradeRequest) (*pb.PreviewBrokerTradeResponse, error) {
	tenant, user, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	candidate, err := s.svc.PreviewBrokerTrade(ctx, tenant, user, r.GetSnapshotId(), r.GetOperationId(), r.GetLedgerId())
	if err != nil {
		return nil, investmentImportError(err)
	}
	response := &pb.PreviewBrokerTradeResponse{Operation: protoInvestmentOperation(candidate.Operation), SourceIdentities: candidate.SourceIdentities, SourceHashes: candidate.SourceHashes, SnapshotSha256: candidate.SnapshotSHA256, PostingAllowed: candidate.PostingAllowed, ReplayLedgerVersion: candidate.Replay.LedgerVersion, ReplayValid: candidate.Replay.Valid, ReplayIssue: candidate.Replay.Issue}
	for _, match := range candidate.JournalMatches {
		response.JournalMatches = append(response.JournalMatches, &pb.BrokerJournalMatch{OperationVersion: match.OperationVersion, OperationId: match.OperationID, LedgerId: match.LedgerID, InstrumentKey: match.InstrumentKey, SameInstrument: match.SameInstrument, SameLedger: match.SameLedger, ExactTime: match.ExactTime, SnowballSource: match.SnowballSource})
	}
	return response, nil
}

func (s *InvestmentImportServer) LinkBrokerTrade(ctx context.Context, r *pb.LinkBrokerTradeRequest) (*pb.LinkBrokerTradeResponse, error) {
	tenant, user, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	id, err := s.svc.LinkBrokerTrade(ctx, tenant, user, imp.BrokerTradeLink{SnapshotID: r.GetSnapshotId(), BrokerOperationID: r.GetBrokerOperationId(), LedgerID: r.GetLedgerId(), OperationID: r.GetOperationId(), ExpectedOperationVersion: r.GetExpectedOperationVersion(), Reason: r.GetReason()})
	if err != nil {
		return nil, investmentImportError(err)
	}
	return &pb.LinkBrokerTradeResponse{LinkId: id}, nil
}

func (s *InvestmentImportServer) PostBrokerTrade(ctx context.Context, r *pb.PostBrokerTradeRequest) (*pb.PostBrokerTradeResponse, error) {
	tenant, user, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.svc.PostBrokerTrade(ctx, tenant, user, imp.BrokerTradePost{SnapshotID: r.GetSnapshotId(), BrokerOperationID: r.GetBrokerOperationId(), LedgerID: r.GetLedgerId(), ExpectedLedgerVersion: r.GetExpectedLedgerVersion(), RequestKey: r.GetRequestKey(), Reason: r.GetReason(), ReconciliationConfirmed: r.GetReconciliationConfirmed()})
	if err != nil {
		return nil, investmentImportError(err)
	}
	return &pb.PostBrokerTradeResponse{OperationId: result.OperationID, LinkId: result.LinkID}, nil
}
