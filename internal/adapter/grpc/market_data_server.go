package grpcadapter

import (
	"context"
	"errors"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"time"
)

type MarketDataServer struct {
	pb.UnimplementedMarketDataServiceServer
	svc      *md.Service
	mappings *md.MappingService
}

func NewMarketDataServer(svc *md.Service, mappings *md.MappingService) *MarketDataServer {
	return &MarketDataServer{svc: svc, mappings: mappings}
}
func marketRPCError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, md.ErrInvalid):
		return status.Error(codes.InvalidArgument, "invalid market data request")
	case errors.Is(err, md.ErrForbidden):
		return status.Error(codes.PermissionDenied, "market data access denied")
	case errors.Is(err, md.ErrNotFound):
		return status.Error(codes.NotFound, "market data source or job not found")
	case errors.Is(err, md.ErrConflict):
		return status.Error(codes.Aborted, "market mapping version conflict")
	case errors.Is(err, md.ErrLimit):
		return status.Error(codes.ResourceExhausted, "market data request limit")
	default:
		return status.Error(codes.Internal, "market data operation failed")
	}
}
func marketScope(t string, key *pb.MarketSourceKey) md.Scope {
	return md.Scope{TenantID: t, Provider: key.GetProvider(), AccessScope: key.GetAccessScope()}
}
func protoMarketKey(s md.Scope) *pb.MarketSourceKey {
	return &pb.MarketSourceKey{Provider: s.Provider, AccessScope: s.AccessScope}
}
func optionalMarketTime(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}
func protoMarketJob(j md.JobStatus) *pb.MarketDataJob {
	return &pb.MarketDataJob{Id: j.ID, Source: protoMarketKey(j.Scope), InstrumentId: j.InstrumentID, Kind: j.Kind, From: optionalMarketTime(j.From), To: optionalMarketTime(j.To), Status: j.Status, Attempts: int32(j.Attempts), ErrorCode: j.ErrorCode, AvailableAt: optionalMarketTime(j.AvailableAt), CoverageState: j.CoverageState, ObservationCount: int32(j.ObservationCount), SearchQuery: j.SearchQuery, InstrumentKind: j.InstrumentKind, InstrumentCount: int32(j.InstrumentCount)}
}
func (s *MarketDataServer) ListMarketSources(ctx context.Context, req *pb.ListMarketSourcesRequest) (*pb.ListMarketSourcesResponse, error) {
	t, u, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	sources, err := s.svc.Sources(ctx, t, u)
	if err != nil {
		return nil, marketRPCError(err)
	}
	out := &pb.ListMarketSourcesResponse{}
	for _, source := range sources {
		out.Sources = append(out.Sources, &pb.MarketSource{Source: protoMarketKey(source.Scope), Title: source.Title})
	}
	return out, nil
}
func (s *MarketDataServer) GetMarketHistory(ctx context.Context, req *pb.GetMarketHistoryRequest) (*pb.GetMarketHistoryResponse, error) {
	t, u, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	from, err := assetTimestamp(req.GetFrom())
	if err != nil {
		return nil, err
	}
	to, err := assetTimestamp(req.GetTo())
	if err != nil {
		return nil, err
	}
	observations, err := s.svc.History(ctx, t, u, marketScope(t, req.GetSource()), req.GetInstrumentId(), req.GetKind(), from, to)
	if err != nil {
		return nil, marketRPCError(err)
	}
	out := &pb.GetMarketHistoryResponse{CoverageState: "unavailable"}
	if len(observations) > 0 {
		out.CoverageState = "observations_only"
	}
	for _, o := range observations {
		out.Observations = append(out.Observations, &pb.MarketObservation{Id: o.ID, InstrumentId: o.InstrumentID, Kind: o.Kind, ObservedAt: timestamppb.New(o.At), SourceKind: o.SourceKind, Currency: o.Currency, PriceUnit: o.PriceUnit, Adjustment: o.Adjustment, Price: o.Price, Open: o.Open, High: o.High, Low: o.Low, Volume: o.Volume, Complete: o.Complete, Revision: o.Revision, ReceivedAt: timestamppb.New(o.ReceivedAt)})
	}
	return out, nil
}
func (s *MarketDataServer) RequestMarketRefresh(ctx context.Context, req *pb.RequestMarketRefreshRequest) (*pb.RequestMarketRefreshResponse, error) {
	t, u, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	q := md.JobRequest{InstrumentID: req.GetInstrumentId(), Kind: req.GetKind(), SearchQuery: req.GetSearchQuery(), InstrumentKind: req.GetInstrumentKind()}
	if req.GetFrom() != nil {
		q.From, err = assetTimestamp(req.GetFrom())
		if err != nil {
			return nil, err
		}
	}
	if req.GetTo() != nil {
		q.To, err = assetTimestamp(req.GetTo())
		if err != nil {
			return nil, err
		}
	}
	job, err := s.svc.Refresh(ctx, t, u, marketScope(t, req.GetSource()), q)
	if err != nil {
		return nil, marketRPCError(err)
	}
	saved, err := s.svc.Job(ctx, t, u, job.ID)
	if err != nil {
		return nil, marketRPCError(err)
	}
	return &pb.RequestMarketRefreshResponse{Job: protoMarketJob(saved)}, nil
}
func (s *MarketDataServer) GetMarketDataJob(ctx context.Context, req *pb.GetMarketDataJobRequest) (*pb.GetMarketDataJobResponse, error) {
	t, u, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	job, err := s.svc.Job(ctx, t, u, req.GetId())
	if err != nil {
		return nil, marketRPCError(err)
	}
	return &pb.GetMarketDataJobResponse{Job: protoMarketJob(job)}, nil
}

func (s *MarketDataServer) RequestMarketHistory(ctx context.Context, req *pb.RequestMarketHistoryRequest) (*pb.RequestMarketHistoryResponse, error) {
	t, u, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	from, err := assetTimestamp(req.GetFrom())
	if err != nil {
		return nil, err
	}
	to, err := assetTimestamp(req.GetTo())
	if err != nil {
		return nil, err
	}
	jobs, err := s.svc.RequestHistory(ctx, t, u, marketScope(t, req.GetSource()), md.JobRequest{InstrumentID: req.GetInstrumentId(), Kind: "daily_candle", From: from, To: to})
	if err != nil {
		return nil, marketRPCError(err)
	}
	out := &pb.RequestMarketHistoryResponse{CoverageState: "requests_only"}
	for _, j := range jobs {
		out.Jobs = append(out.Jobs, protoMarketJob(md.JobStatus{Job: j}))
	}
	return out, nil
}

func (s *MarketDataServer) SearchMarketInstruments(ctx context.Context, req *pb.SearchMarketInstrumentsRequest) (*pb.SearchMarketInstrumentsResponse, error) {
	t, u, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	limit := int(req.GetLimit())
	if limit == 0 {
		limit = 25
	}
	instruments, err := s.svc.Search(ctx, t, u, marketScope(t, req.GetSource()), req.GetQuery(), limit, int(req.GetOffset()))
	if err != nil {
		return nil, marketRPCError(err)
	}
	out := &pb.SearchMarketInstrumentsResponse{CatalogState: "cached_only"}
	for _, i := range instruments {
		out.Instruments = append(out.Instruments, &pb.MarketInstrument{Detailed: i.Detailed, InstrumentId: i.ID, Figi: i.FIGI, Isin: i.ISIN, Ticker: i.Ticker, ClassCode: i.ClassCode, Name: i.Name, Kind: i.Kind, Currency: i.Currency, Revision: i.Revision, ReceivedAt: timestamppb.New(i.ReceivedAt)})
	}
	return out, nil
}
