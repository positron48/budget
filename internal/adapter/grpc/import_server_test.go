package grpcadapter

import (
    "context"
    "testing"

    budgetv1 "github.com/positron48/budget/gen/go/budget/v1"
)

func TestImportServer_Flow_NoOp(t *testing.T) {
    s := NewImportServer()
    ctx := context.Background()
    st, err := s.StartCsvImport(ctx, &budgetv1.StartCsvImportRequest{Filename: "f.csv"})
    if err != nil || st.GetImportId() == "" { t.Fatalf("start: %v %#v", err, st) }
    up, err := s.UploadCsvChunk(ctx, &budgetv1.UploadCsvChunkRequest{ImportId: st.GetImportId(), Chunk: []byte("a,b")})
    if err != nil || up.GetReceivedBytes() != 3 { t.Fatalf("upload: %v %#v", err, up) }
    cfg, err := s.ConfigureCsvMapping(ctx, &budgetv1.ConfigureCsvMappingRequest{ImportId: st.GetImportId()})
    if err != nil || cfg == nil { t.Fatalf("cfg: %v %#v", err, cfg) }
    pr, err := s.PreviewCsvImport(ctx, &budgetv1.PreviewCsvImportRequest{ImportId: st.GetImportId()})
    if err != nil || pr.GetTotalRows() != 0 { t.Fatalf("preview: %v %#v", err, pr) }
    cm, err := s.CommitCsvImport(ctx, &budgetv1.CommitCsvImportRequest{ImportId: st.GetImportId()})
    if err != nil || cm.GetInserted() != 0 { t.Fatalf("commit: %v %#v", err, cm) }
}


