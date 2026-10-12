package grpcadapter

import (
	"context"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	inv "github.com/positron48/budget/internal/usecase/investment"
	exp "github.com/positron48/budget/internal/usecase/investmentexport"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func NewInvestmentServerWithExport(svc *inv.Service, repo exp.Repository) *InvestmentServer {
	return &InvestmentServer{svc: svc, exports: repo}
}
func protoExport(j exp.Job) *pb.InvestmentExportJob {
	out := &pb.InvestmentExportJob{Id: j.ID, State: j.State, Sha256: j.SHA256, ByteSize: j.ByteSize, ErrorCode: j.ErrorCode, CreatedAt: timestamppb.New(j.CreatedAt)}
	if j.CompletedAt != nil {
		out.CompletedAt = timestamppb.New(*j.CompletedAt)
	}
	if j.ExpiresAt != nil {
		out.ExpiresAt = timestamppb.New(*j.ExpiresAt)
	}
	return out
}
func (s *InvestmentServer) StartInvestmentExport(ctx context.Context, req *pb.StartInvestmentExportRequest) (*pb.StartInvestmentExportResponse, error) {
	tenant, actor, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	if s.exports == nil {
		return nil, status.Error(codes.Unavailable, "investment exports unavailable")
	}
	j, e := s.exports.StartExport(ctx, tenant, actor, req.GetRequestKey())
	if e != nil {
		return nil, investmentRPCError(e)
	}
	return &pb.StartInvestmentExportResponse{Job: protoExport(j)}, nil
}
func (s *InvestmentServer) GetInvestmentExport(ctx context.Context, req *pb.GetInvestmentExportRequest) (*pb.GetInvestmentExportResponse, error) {
	tenant, actor, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	if s.exports == nil {
		return nil, status.Error(codes.Unavailable, "investment exports unavailable")
	}
	j, e := s.exports.GetExport(ctx, tenant, actor, req.GetJobId())
	if e != nil {
		return nil, investmentRPCError(e)
	}
	return &pb.GetInvestmentExportResponse{Job: protoExport(j)}, nil
}
func (s *InvestmentServer) DownloadInvestmentExport(ctx context.Context, req *pb.DownloadInvestmentExportRequest) (*pb.DownloadInvestmentExportResponse, error) {
	tenant, actor, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	if s.exports == nil {
		return nil, status.Error(codes.Unavailable, "investment exports unavailable")
	}
	chunk, e := s.exports.ReadExportChunk(ctx, tenant, actor, req.GetJobId(), req.GetOffset())
	if e != nil {
		return nil, investmentRPCError(e)
	}
	return &pb.DownloadInvestmentExportResponse{Data: chunk.Data, Offset: chunk.Offset, TotalBytes: chunk.Total, Sha256: chunk.SHA256}, nil
}
