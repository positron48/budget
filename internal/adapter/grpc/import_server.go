//go:build !ignore
// +build !ignore

package grpcadapter

import (
	"context"

	budgetv1 "github.com/positron48/budget/gen/go/budget/v1"
)

// Minimal no-op scaffold for future CSV import flow
type ImportServer struct {
	budgetv1.UnimplementedImportServiceServer
	// TODO: wire storage (for temp file), parser, mapper, and transaction service
}

func NewImportServer() *ImportServer { return &ImportServer{} }

func (s *ImportServer) StartCsvImport(ctx context.Context, req *budgetv1.StartCsvImportRequest) (*budgetv1.StartCsvImportResponse, error) {
	// For now, return deterministic ID for testing. Later: allocate and persist session state.
	return &budgetv1.StartCsvImportResponse{ImportId: "imp-1"}, nil
}

func (s *ImportServer) UploadCsvChunk(ctx context.Context, req *budgetv1.UploadCsvChunkRequest) (*budgetv1.UploadCsvChunkResponse, error) {
	return &budgetv1.UploadCsvChunkResponse{ReceivedBytes: int64(len(req.GetChunk()))}, nil
}

func (s *ImportServer) ConfigureCsvMapping(ctx context.Context, req *budgetv1.ConfigureCsvMappingRequest) (*budgetv1.ConfigureCsvMappingResponse, error) {
	return &budgetv1.ConfigureCsvMappingResponse{}, nil
}

func (s *ImportServer) PreviewCsvImport(ctx context.Context, req *budgetv1.PreviewCsvImportRequest) (*budgetv1.PreviewCsvImportResponse, error) {
	return &budgetv1.PreviewCsvImportResponse{TotalRows: 0, ValidRows: 0, InvalidRows: 0}, nil
}

func (s *ImportServer) CommitCsvImport(ctx context.Context, req *budgetv1.CommitCsvImportRequest) (*budgetv1.CommitCsvImportResponse, error) {
	return &budgetv1.CommitCsvImportResponse{Inserted: 0, Failed: 0}, nil
}
