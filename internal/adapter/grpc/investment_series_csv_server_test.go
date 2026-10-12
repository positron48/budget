package grpcadapter

import (
	"context"
	"errors"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	d "github.com/positron48/budget/internal/domain"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strings"
	"testing"
)

type seriesCSVRepoStub struct {
	InvestmentNAVRepo
	tenant, actor, key, from, to string
	err                          error
}

func (r *seriesCSVRepoStub) ExportSeries(_ context.Context, tenant, actor, key, from, to string) (nav.SeriesCSV, error) {
	r.tenant, r.actor, r.key, r.from, r.to = tenant, actor, key, from, to
	return nav.SeriesCSV{Filename: "investments-USD-2024-01-01-2024-01-02.csv", SHA256: "checksum", Data: []byte{1, 2, 3}, Rows: 2, UncompressedSize: 500, Generation: 17}, r.err
}
func TestSeriesCSVEnvelopeAuthAndSanitizedErrors(t *testing.T) {
	r := &seriesCSVRepoStub{}
	s := NewInvestmentNAVServer(r)
	if _, e := s.ExportInvestmentSeries(context.Background(), nil); status.Code(e) != codes.Unauthenticated || r.tenant != "" {
		t.Fatal(e, r)
	}
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), "tenant"), "actor")
	out, e := s.ExportInvestmentSeries(ctx, &pb.ExportInvestmentSeriesRequest{SeriesKey: "series", FromDate: "2024-01-01", ToDate: "2024-01-02"})
	if e != nil || r.tenant != "tenant" || r.actor != "actor" || r.key != "series" || r.from != "2024-01-01" || r.to != "2024-01-02" || out.Encoding != "gzip" || out.Generation != 17 || out.RowCount != 2 || out.UncompressedSize != 500 || string(out.Data) != string([]byte{1, 2, 3}) || out.Sha256 != "checksum" {
		t.Fatal(out, e, r)
	}
	r.err = d.ErrInvestmentLimit
	if _, e = s.ExportInvestmentSeries(ctx, nil); status.Code(e) != codes.ResourceExhausted {
		t.Fatal(e)
	}
	r.err = errors.New("private credentials")
	if _, e = s.ExportInvestmentSeries(ctx, nil); status.Code(e) != codes.Internal || strings.Contains(e.Error(), "private") {
		t.Fatal(e)
	}
}
