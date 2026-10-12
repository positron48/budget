package grpcadapter

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	pb "github.com/positron48/budget/gen/go/budget/v1"
	d "github.com/positron48/budget/internal/domain"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	inv "github.com/positron48/budget/internal/usecase/investment"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type batchRepoStub struct {
	inv.Repo
	tenant, actor, hash string
	batch               inv.ImportBatch
	commit              bool
}

func (r *batchRepoStub) ImportBatch(_ context.Context, t, u string, b inv.ImportBatch, h string, commit bool) (inv.ImportBatchResult, error) {
	r.tenant, r.actor, r.hash, r.batch, r.commit = t, u, h, b, commit
	return inv.ImportBatchResult{Hash: strings.Repeat("a", 64), Committed: commit, Operations: []d.InvestmentOperation{b.Entries[0].Operation}}, nil
}

type batchRoles struct{ role d.TenantRole }

func (r batchRoles) GetUserRole(context.Context, string, string) (d.TenantRole, error) {
	return r.role, nil
}
func TestImportBatchRPCIdentityAndReview(t *testing.T) {
	tenant := "11111111-1111-4111-8111-111111111111"
	actor := "22222222-2222-4222-8222-222222222222"
	ledger := "33333333-3333-4333-8333-333333333333"
	importID := "44444444-4444-4444-8444-444444444444"
	repo := &batchRepoStub{}
	server := NewInvestmentServer(inv.NewService(repo, batchRoles{d.TenantRoleOwner}))
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), tenant), actor)
	input := &pb.InvestmentImportBatch{ImportId: importID, PreviewHash: strings.Repeat("b", 64), RequestKey: "batch", ExpectedImportVersion: 1, ExpectedLedgerVersions: map[string]int64{ledger: 1}, Entries: []*pb.InvestmentImportBatchEntry{{Record: 1, ExpectedRowVersion: 1, ReviewReason: "Confirmed amount", Operation: &pb.InvestmentOperation{Id: importID, LedgerId: ledger, Kind: "deposit", Currency: "RUB", Amount: "9007199254740993.123456789012", Status: "voided", Sequence: 999, Version: 99, OccurredAt: timestamppb.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))}}}}
	preview, e := server.PreviewInvestmentImportBatch(ctx, &pb.PreviewInvestmentImportBatchRequest{Batch: input})
	if e != nil || preview.Result.Committed || repo.tenant != tenant || repo.actor != actor || preview.Result.Operations[0].Amount != "9007199254740993.123456789012" {
		t.Fatal(preview, e)
	}
	op := repo.batch.Entries[0].Operation
	if op.ID != "" || op.Version != 0 || op.Sequence != 0 || op.Status != "" || op.ActorID != actor || op.TenantID != tenant {
		t.Fatal("client metadata trusted", op)
	}
	result, e := server.CommitInvestmentImportBatch(ctx, &pb.CommitInvestmentImportBatchRequest{Batch: input, ExpectedHash: preview.Result.Hash})
	if e != nil || !result.Result.Committed || !repo.commit || repo.hash != preview.Result.Hash {
		t.Fatal(result, e)
	}
	if _, e = server.CommitInvestmentImportBatch(ctx, &pb.CommitInvestmentImportBatchRequest{Batch: input}); status.Code(e) != codes.InvalidArgument {
		t.Fatal("missing preview hash", e)
	}
	if _, e = server.PreviewInvestmentImportBatch(context.Background(), nil); status.Code(e) != codes.Unauthenticated {
		t.Fatal(e)
	}
	memberServer := NewInvestmentServer(inv.NewService(repo, batchRoles{d.TenantRoleMember}))
	if _, e = memberServer.PreviewInvestmentImportBatch(ctx, &pb.PreviewInvestmentImportBatchRequest{Batch: input}); status.Code(e) != codes.PermissionDenied {
		t.Fatal(e)
	}
	input.Entries = append(input.Entries, input.Entries[0])
	if _, e = server.PreviewInvestmentImportBatch(ctx, &pb.PreviewInvestmentImportBatchRequest{Batch: input}); status.Code(e) != codes.InvalidArgument {
		t.Fatal("duplicate source", e)
	}
	input.Entries = input.Entries[:1]
	input.Entries[0].Operation.TransferId = importID
	if _, e = server.PreviewInvestmentImportBatch(ctx, &pb.PreviewInvestmentImportBatchRequest{Batch: input}); status.Code(e) != codes.InvalidArgument {
		t.Fatal("unpaired transfer", e)
	}
}

func TestSavedBatchRoundTripPreservesUnknownSettlement(t *testing.T) {
	tenant := "11111111-1111-4111-8111-111111111111"
	actor := "22222222-2222-4222-8222-222222222222"
	ledger := "33333333-3333-4333-8333-333333333333"
	batch, err := inv.NormalizeImportBatch(tenant, actor, inv.ImportBatch{PreparationEvidenceJSON: `{"Version":1,"ReviewHash":"fixture","Mapping":{"timezone":"UTC"}}`, ImportID: "44444444-4444-4444-8444-444444444444", PreviewHash: strings.Repeat("a", 64), RequestKey: "saved", ExpectedImportVersion: 1, ExpectedLedgerVersions: map[string]int64{ledger: 1}, Entries: []inv.ImportBatchEntry{{Record: 1, ExpectedRowVersion: 1, ReviewReason: "Reviewed", Operation: d.InvestmentOperation{LedgerID: ledger, Kind: "buy", InstrumentKey: "TEST", Currency: "USD", Quantity: "1", Amount: "100", OccurredAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}}}})
	if err != nil {
		t.Fatal(err)
	}
	if !batch.Entries[0].Operation.SettlementAssumed {
		t.Fatal("missing fixture quality flag")
	}
	wire := protoInvestmentDraft(inv.BatchDraft{ID: "55555555-5555-4555-8555-555555555555", Version: 1, Batch: batch, UpdatedAt: time.Now()})
	restored, err := parseInvestmentBatch(wire.Batch)
	if err != nil {
		t.Fatal(err)
	}
	restored, err = inv.NormalizeImportBatch(tenant, actor, restored)
	if err != nil || !reflect.DeepEqual(restored, batch) {
		t.Fatal("draft changed during protobuf round trip", err)
	}
	wire.Batch.Entries[0].Operation.SettlementAt = timestamppb.New(time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC))
	if _, err = parseInvestmentBatch(wire.Batch); status.Code(err) != codes.InvalidArgument {
		t.Fatal("inconsistent inferred settlement accepted", err)
	}
}
