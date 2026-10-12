package grpcadapter

import (
	"context"
	"errors"

	budgetv1 "github.com/positron48/budget/gen/go/budget/v1"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type InvestmentImportServer struct {
	budgetv1.UnimplementedInvestmentImportServiceServer
	svc *imp.ImportService
}

func NewInvestmentImportServer(svc *imp.ImportService) *InvestmentImportServer {
	return &InvestmentImportServer{svc: svc}
}
func investmentImportError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, imp.ErrInvalid):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, imp.ErrForbidden):
		return status.Error(codes.PermissionDenied, "investment import access denied")
	case errors.Is(err, imp.ErrNotFound):
		return status.Error(codes.NotFound, "investment import not found")
	case errors.Is(err, imp.ErrConflict):
		return status.Error(codes.Aborted, "investment import version or request conflict")
	default:
		return investmentRPCError(err)
	}
}
func protoInvestmentImport(v imp.Import) *budgetv1.InvestmentImport {
	out := &budgetv1.InvestmentImport{Id: v.ID, Filename: v.Filename, SourceSha256: v.SourceSHA256, PreviewHash: v.PreviewHash, State: v.State, RowCount: int32(v.Rows), Version: v.Version, CreatedAt: timestamppb.New(v.CreatedAt)}
	for _, a := range v.Accounts {
		out.Accounts = append(out.Accounts, &budgetv1.InvestmentImportAccount{Id: a.ID, SourceKey: a.SourceKey, Name: a.Name, RowCount: int32(a.Rows)})
	}
	return out
}
func protoInvestmentRow(v imp.StoredRow) *budgetv1.InvestmentImportRow {
	out := &budgetv1.InvestmentImportRow{Record: int32(v.Record), SourceRowId: v.SourceRowID, AccountId: v.AccountID, Version: v.Version, MappingEvidence: v.MappingEvidence, SourceFields: v.Source.Raw, Decimals: v.Source.Decimals, Issues: v.Preview.Issues, LedgerId: v.LedgerID, OperationId: v.OperationID, OperationStatus: v.OperationStatus}
	for _, i := range v.Preview.IdenticalRecords {
		out.IdenticalRecords = append(out.IdenticalRecords, int32(i))
	}
	return out
}
func (s *InvestmentImportServer) UploadInvestmentImport(ctx context.Context, req *budgetv1.UploadInvestmentImportRequest) (*budgetv1.UploadInvestmentImportResponse, error) {
	t, u, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	in := imp.Upload{Filename: req.GetFilename(), CSV: req.GetCsv(), RequestKey: req.GetRequestKey(), MappingSourceSHA256: req.GetMappingSourceSha256()}
	for _, a := range req.GetAccounts() {
		in.Accounts = append(in.Accounts, imp.ImportAccount{SourceKey: a.GetSourceKey(), Name: a.GetName()})
	}
	for _, a := range req.GetAssignments() {
		in.Assignments = append(in.Assignments, imp.Assignment{Record: int(a.GetRecord()), AccountKey: a.GetAccountKey(), Evidence: a.GetEvidence()})
	}
	v, err := s.svc.Upload(ctx, t, u, in)
	if err != nil {
		return nil, investmentImportError(err)
	}
	return &budgetv1.UploadInvestmentImportResponse{Import: protoInvestmentImport(v)}, nil
}
func (s *InvestmentImportServer) ListInvestmentImports(ctx context.Context, req *budgetv1.ListInvestmentImportsRequest) (*budgetv1.ListInvestmentImportsResponse, error) {
	t, u, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	limit := int(req.GetLimit())
	if limit == 0 {
		limit = 50
	}
	values, err := s.svc.List(ctx, t, u, limit, int(req.GetOffset()))
	if err != nil {
		return nil, investmentImportError(err)
	}
	out := &budgetv1.ListInvestmentImportsResponse{}
	for _, v := range values {
		out.Imports = append(out.Imports, protoInvestmentImport(v))
	}
	return out, nil
}
func (s *InvestmentImportServer) GetInvestmentImport(ctx context.Context, req *budgetv1.GetInvestmentImportRequest) (*budgetv1.GetInvestmentImportResponse, error) {
	t, u, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	v, err := s.svc.Get(ctx, t, u, req.GetId())
	if err != nil {
		return nil, investmentImportError(err)
	}
	return &budgetv1.GetInvestmentImportResponse{Import: protoInvestmentImport(v)}, nil
}
func (s *InvestmentImportServer) ListInvestmentImportRows(ctx context.Context, req *budgetv1.ListInvestmentImportRowsRequest) (*budgetv1.ListInvestmentImportRowsResponse, error) {
	t, u, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	limit := int(req.GetLimit())
	if limit == 0 {
		limit = 50
	}
	values, err := s.svc.Rows(ctx, t, u, req.GetImportId(), limit, int(req.GetOffset()))
	if err != nil {
		return nil, investmentImportError(err)
	}
	out := &budgetv1.ListInvestmentImportRowsResponse{}
	for _, v := range values {
		out.Rows = append(out.Rows, protoInvestmentRow(v))
	}
	return out, nil
}
func (s *InvestmentImportServer) ReassignInvestmentImportRow(ctx context.Context, req *budgetv1.ReassignInvestmentImportRowRequest) (*budgetv1.ReassignInvestmentImportRowResponse, error) {
	t, u, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	v, err := s.svc.Reassign(ctx, t, u, req.GetImportId(), int(req.GetRecord()), req.GetAccountId(), req.GetExpectedVersion(), req.GetReason())
	if err != nil {
		return nil, investmentImportError(err)
	}
	return &budgetv1.ReassignInvestmentImportRowResponse{Row: protoInvestmentRow(v)}, nil
}
