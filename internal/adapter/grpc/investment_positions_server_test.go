package grpcadapter

import (
	"context"
	"errors"
	pb "github.com/positron48/budget/gen/go/budget/v1"
	d "github.com/positron48/budget/internal/domain"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"math/big"
	"strings"
	"testing"
	"time"
)

type positionRepoStub struct {
	InvestmentNAVRepo
	tenant, actor, key, date, instrument, cursor string
	groups                                       []string
	err                                          error
	details                                      nav.PositionDetails
	prices                                       nav.InstrumentPriceHistory
}

func (r *positionRepoStub) InvestmentPositions(_ context.Context, tenant, actor, key, date string, groups []string) (nav.PositionDetails, error) {
	r.tenant, r.actor, r.key, r.date, r.groups = tenant, actor, key, date, groups
	return r.details, r.err
}
func (r *positionRepoStub) InvestmentInstrument(ctx context.Context, tenant, actor, key, date, instrument, cursor string, groups []string) (nav.InstrumentDetails, error) {
	_, e := r.InvestmentPositions(ctx, tenant, actor, key, date, groups)
	r.instrument, r.cursor = instrument, cursor
	return nav.InstrumentDetails{PositionDetails: r.details, InstrumentKey: instrument, NextCursor: "page", AsOf: time.Unix(1704157199, 999999999), Operations: []d.InvestmentOperation{{ID: "trade", Kind: "buy", Quantity: "9007199254740993.000000000001"}}}, e
}
func (r *positionRepoStub) InvestmentInstrumentPrices(_ context.Context, tenant, actor, key, instrument, from, to string, groups []string) (nav.InstrumentPriceHistory, error) {
	r.tenant, r.actor, r.key, r.instrument, r.groups = tenant, actor, key, instrument, groups
	return r.prices, r.err
}
func TestPositionRPCIdentityUnknownValuesAndPrecision(t *testing.T) {
	repo := &positionRepoStub{details: nav.PositionDetails{Currency: "USD", Classification: nav.InstrumentGroups{Version: 9007199254740993}, Rows: []nav.PositionView{{LedgerID: "ledger", HistoricalCost: nav.LotCostSummary{Clean: big.NewRat(2, 3), Accrued: big.NewRat(0, 1), CleanComplete: false, AccruedComplete: true, Reasons: []string{"fx_missing"}}, Position: d.InvestmentPosition{InstrumentKey: "bond", Quantity: "9007199254740993.000000000001", Currency: "RUB", CleanCost: ""}, Value: big.NewRat(1, 3), UnrealizedClean: big.NewRat(0, 1), Facts: &nav.InstrumentFact{Terms: calc.PriceTerms{Unit: "percent_of_nominal"}, Price: &calc.PriceFact{Price: "95.1", Unit: "provider_unspecified"}}, NativePrice: calc.NormalizedPrice{Currency: "RUB", Clean: big.NewRat(951, 1)}, Payments: []nav.PositionPayment{{Currency: "EUR", Net: big.NewRat(1, 7)}}}}}}
	server := NewInvestmentNAVServer(repo)
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), "tenant"), "actor")
	out, e := server.GetInvestmentPositions(ctx, &pb.GetInvestmentPositionsRequest{SeriesKey: "series", Date: "2024-01-01", GroupIds: []string{"group"}})
	if e != nil || repo.tenant != "tenant" || repo.actor != "actor" || repo.key != "series" || repo.date != "2024-01-01" || len(repo.groups) != 1 {
		t.Fatal(out, e, repo)
	}
	row := out.Positions[0]
	if out.ClassificationVersion != 9007199254740993 || row.Quantity != "9007199254740993.000000000001" || row.CleanCost != "" || row.AcquiredAccrued != "" || row.UnrealizedDirty != nil || row.Value.Denominator != "3" || row.UnrealizedClean.Decimal != "0.000000000000" || row.Payments[0].Net.Denominator != "7" || row.Payments[0].Currency != "EUR" || row.PriceUnit != "percent_of_nominal" || row.ProviderPrice != "95.1" {
		t.Fatal(row)
	}
	if row.HistoricalCleanCost.Numerator != "2" || row.HistoricalCleanCost.Denominator != "3" || row.HistoricalCleanComplete || !row.HistoricalAccruedComplete || row.HistoricalAcquiredAccrued.Numerator != "0" || row.HistoricalCostReasons[0] != "fx_missing" {
		t.Fatal(row)
	}
	card, e := server.GetInvestmentInstrument(ctx, &pb.GetInvestmentInstrumentRequest{SeriesKey: "series", Date: "2024-01-01", InstrumentKey: "bond", Cursor: "cursor", GroupIds: []string{"group"}})
	if e != nil || repo.instrument != "bond" || repo.cursor != "cursor" || card.AsOf.Nanos != 999999999 || card.Operations[0].Quantity != row.Quantity || card.NextCursor != "page" {
		t.Fatal(card, e)
	}
	repo.prices = nav.InstrumentPriceHistory{Currency: "USD", Days: []nav.InstrumentPriceDay{{Date: "2024-01-01", Accrued: &calc.AccruedFact{SourceID: "reviewed:3/snapshot/hash", InstrumentID: "listing", Currency: "RUB", AmountPerUnit: "0", From: time.Unix(1704067200, 0), To: time.Unix(1704153600, 0)}, NativeClean: big.NewRat(120, 1), TargetClean: big.NewRat(1, 3)}, {Date: "2024-01-02", State: "unavailable"}}, Markers: []d.InvestmentOperation{{ID: "trade", Kind: "buy"}}}
	prices, e := server.GetInvestmentInstrumentPrices(ctx, &pb.GetInvestmentInstrumentPricesRequest{SeriesKey: "series", InstrumentKey: "bond", GroupIds: []string{"group"}})
	if e != nil || prices.Points[0].AccruedEvidence == nil || prices.Points[0].AccruedEvidence.AmountPerUnit != "0" || prices.Points[0].AccruedEvidence.SourceId != "reviewed:3/snapshot/hash" || prices.Points[1].AccruedEvidence != nil || prices.Points[1].NativeClean != nil || prices.Points[1].TargetClean != nil || prices.Points[0].TargetClean.Denominator != "3" || len(prices.Markers) != 1 || repo.groups[0] != "group" {
		t.Fatal(prices, e)
	}

	repo.prices = nav.InstrumentPriceHistory{Days: []nav.InstrumentPriceDay{{Date: "2025-01-02", AdjustmentProof: &calc.PriceAdjustmentProof{ProfileKey: "split", ProfileVersion: 2, ObservationID: "canonical-observation", ObservationRevision: 3, ObservationHash: "source-hash", SourcePrice: "12.000000001", Multiplier: "10", Divisor: "1", ResolvedPrice: "120.000000010", SourceReference: "https://issuer.example.test/split", EvidenceSHA256: "document-hash"}}, {Date: "2025-01-03"}}}
	adjusted, err := server.GetInvestmentInstrumentPrices(ctx, &pb.GetInvestmentInstrumentPricesRequest{SeriesKey: "series", InstrumentKey: "share"})
	if err != nil || adjusted.Points[0].AdjustmentEvidence == nil || adjusted.Points[1].AdjustmentEvidence != nil {
		t.Fatal(adjusted, err)
	}
	proof := adjusted.Points[0].AdjustmentEvidence
	if proof.SourcePrice != "12.000000001" || proof.ResolvedPrice != "120.000000010" || proof.Multiplier != "10" || proof.Divisor != "1" || proof.ProfileVersion != 2 || proof.ObservationRevision != 3 || proof.ObservationId != "canonical-observation" || proof.ObservationHash != "source-hash" || proof.EvidenceSha256 != "document-hash" || proof.SourceReference != "https://issuer.example.test/split" {
		t.Fatal(proof)
	}
	repo.prices.Days[0].AdjustmentProof.Basis = nav.NativeBondPercentBasis
	repo.prices.Days[0].AdjustmentProof.Nominal = "500.000000001"
	repo.prices.Days[0].AdjustmentProof.NominalCurrency = "RUB"
	native, err := server.GetInvestmentInstrumentPrices(ctx, &pb.GetInvestmentInstrumentPricesRequest{SeriesKey: "series", InstrumentKey: "bond"})
	if err != nil || native.Points[0].AdjustmentEvidence.Basis != nav.NativeBondPercentBasis || native.Points[0].AdjustmentEvidence.Nominal != "500.000000001" || native.Points[0].AdjustmentEvidence.NominalCurrency != "RUB" {
		t.Fatal(native, err)
	}
	for _, call := range []func(context.Context) error{
		func(c context.Context) error { _, e := server.GetInvestmentPositions(c, nil); return e },
		func(c context.Context) error { _, e := server.GetInvestmentInstrument(c, nil); return e },
		func(c context.Context) error { _, e := server.GetInvestmentInstrumentPrices(c, nil); return e },
	} {
		if e := call(context.Background()); status.Code(e) != codes.Unauthenticated {
			t.Fatal(e)
		}
		repo.err = errors.New("private database credentials")
		if e := call(ctx); status.Code(e) != codes.Internal || strings.Contains(e.Error(), "credentials") {
			t.Fatal(e)
		}
	}
}
