package grpcadapter

import (
	"context"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	d "github.com/positron48/budget/internal/domain"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	inv "github.com/positron48/budget/internal/usecase/investment"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"math/big"
	"testing"
	"time"
)

type lotRepoStub struct {
	InvestmentNAVRepo
	tenant, actor, key, date, instrument, cursor string
	groups                                       []string
	err                                          error
}

func (r *lotRepoStub) InvestmentLots(_ context.Context, t, a, k, date, i, c string, g []string) (nav.LotDetails, error) {
	r.tenant, r.actor, r.key, r.date, r.instrument, r.cursor, r.groups = t, a, k, date, i, c, g
	lot := inv.RemainingLot{OriginID: "buy", LotID: "slice", OriginLedgerID: "source", OriginSequence: 9007199254740993, Quantity: big.NewRat(9007199254740993, 1), CleanCost: big.NewRat(2, 3), AcquiredAccrued: nil}
	return nav.LotDetails{Date: date, Currency: "USD", FXPolicyKey: "market", NextCursor: "next", Total: 51, Rows: []nav.LotView{{LedgerID: "ledger", Lot: lot, HistoricalCost: nav.LotCostSummary{Clean: big.NewRat(1, 3), CleanComplete: true, Reasons: []string{"book_value_unknown"}}, FXAt: time.Unix(1704142799, 999999999), FX: &calc.ResolvedFX{Rate: big.NewRat(1, 2), Legs: []calc.FXLeg{{ID: "fx", Revision: 2, Inverse: true}}}, Changes: []inv.LotChange{{Kind: "buy", After: &lot}}}}}, r.err
}
func TestLotsRPCIdentityFractionsUnknownAndErrors(t *testing.T) {
	repo := &lotRepoStub{}
	s := NewInvestmentNAVServer(repo)
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), "tenant"), "actor")
	v, e := s.GetInvestmentLots(ctx, &pb.GetInvestmentLotsRequest{SeriesKey: "series", Date: "2024-01-01", InstrumentKey: "S", Cursor: "cursor", GroupIds: []string{"g"}})
	if e != nil || repo.tenant != "tenant" || repo.actor != "actor" || repo.key != "series" || repo.instrument != "S" || repo.cursor != "cursor" || repo.groups[0] != "g" || v.Total != 51 || v.NextCursor != "next" {
		t.Fatal(v, e, repo)
	}
	row := v.Lots[0]
	if row.Lot.LotId != "slice" || row.Lot.OriginLedgerId != "source" || row.Lot.OriginSequence != 9007199254740993 || row.Lot.Quantity.Numerator != "9007199254740993" || row.Lot.CleanCost.Denominator != "3" || row.Lot.AcquiredAccrued != nil || row.Lot.AcquiredAt != nil || row.Origin != nil || row.Changes[0].Before != nil || row.Changes[0].After.CleanCost.Denominator != "3" || row.FxLegs[0].Revision != 2 || !row.FxLegs[0].Inverse {
		t.Fatal(row)
	}
	repo.err = d.ErrInvestmentConflict
	if _, e = s.GetInvestmentLots(ctx, &pb.GetInvestmentLotsRequest{}); status.Code(e) != codes.Aborted {
		t.Fatal(e)
	}
	if _, e = s.GetInvestmentLots(context.Background(), &pb.GetInvestmentLotsRequest{}); status.Code(e) != codes.Unauthenticated {
		t.Fatal(e)
	}
}
