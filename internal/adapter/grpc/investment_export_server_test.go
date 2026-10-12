package grpcadapter

import (
	"context"
	"testing"
	"time"

	pb "github.com/positron48/budget/gen/go/budget/v1"
	d "github.com/positron48/budget/internal/domain"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	exp "github.com/positron48/budget/internal/usecase/investmentexport"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type exportRPCRepo struct {
	exp.Repository
	tenant, actor, key string
	offset             int64
	err                error
}

func (r *exportRPCRepo) StartExport(_ context.Context, t, u, k string) (exp.Job, error) {
	r.tenant, r.actor, r.key = t, u, k
	return exp.Job{ID: "job", State: "pending", CreatedAt: time.Now()}, r.err
}
func (r *exportRPCRepo) GetExport(_ context.Context, t, u, k string) (exp.Job, error) {
	return r.StartExport(context.Background(), t, u, k)
}
func (r *exportRPCRepo) ReadExportChunk(_ context.Context, t, u, k string, o int64) (exp.Chunk, error) {
	r.tenant, r.actor, r.key, r.offset = t, u, k, o
	return exp.Chunk{Offset: o, Total: 9007199254740993, SHA256: "digest", Data: []byte{0, 255, 42}}, r.err
}
func TestInvestmentExportRPCIdentityAndExactChunk(t *testing.T) {
	repo := &exportRPCRepo{}
	server := NewInvestmentServerWithExport(nil, repo)
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), "tenant"), "actor")
	if _, e := server.StartInvestmentExport(context.Background(), &pb.StartInvestmentExportRequest{}); status.Code(e) != codes.Unauthenticated {
		t.Fatal(e)
	}
	j, e := server.StartInvestmentExport(ctx, &pb.StartInvestmentExportRequest{RequestKey: "request"})
	if e != nil || j.Job.State != "pending" || repo.tenant != "tenant" || repo.actor != "actor" || repo.key != "request" {
		t.Fatal(j, e)
	}
	if _, e = server.GetInvestmentExport(ctx, &pb.GetInvestmentExportRequest{JobId: "job"}); e != nil || repo.key != "job" {
		t.Fatal(e)
	}
	c, e := server.DownloadInvestmentExport(ctx, &pb.DownloadInvestmentExportRequest{JobId: "job", Offset: 262144})
	if e != nil || c.Offset != 262144 || c.TotalBytes != 9007199254740993 || string(c.Data) != string([]byte{0, 255, 42}) || repo.offset != 262144 {
		t.Fatal(c, e)
	}
	repo.err = d.ErrInvestmentForbidden
	if _, e = server.DownloadInvestmentExport(ctx, &pb.DownloadInvestmentExportRequest{JobId: "job"}); status.Code(e) != codes.PermissionDenied {
		t.Fatal(e)
	}
	repo.err = d.ErrInvestmentPrecondition
	if _, e = server.DownloadInvestmentExport(ctx, &pb.DownloadInvestmentExportRequest{JobId: "job"}); status.Code(e) != codes.FailedPrecondition {
		t.Fatal(e)
	}
}
