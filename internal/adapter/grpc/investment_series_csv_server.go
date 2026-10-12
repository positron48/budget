package grpcadapter

import (
	"context"
	pb "github.com/positron48/budget/gen/go/budget/v1"
)

func (s *InvestmentNAVServer) ExportInvestmentSeries(ctx context.Context, req *pb.ExportInvestmentSeriesRequest) (*pb.ExportInvestmentSeriesResponse, error) {
	tenant, actor, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	v, e := s.repo.ExportSeries(ctx, tenant, actor, req.GetSeriesKey(), req.GetFromDate(), req.GetToDate())
	if e != nil {
		return nil, investmentRPCError(e)
	}
	return &pb.ExportInvestmentSeriesResponse{Filename: v.Filename, Data: v.Data, Encoding: "gzip", Sha256: v.SHA256, RowCount: int32(v.Rows), UncompressedSize: int64(v.UncompressedSize), Generation: v.Generation}, nil
}
