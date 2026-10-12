package grpcadapter

import (
	"context"
	"errors"
	"time"

	pb "github.com/positron48/budget/gen/go/budget/v1"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	exp "github.com/positron48/budget/internal/usecase/investmentexport"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type InvestmentServer struct {
	pb.UnimplementedInvestmentServiceServer
	svc     *inv.Service
	exports exp.Repository
}

func NewInvestmentServer(svc *inv.Service) *InvestmentServer { return &InvestmentServer{svc: svc} }
func investmentRPCError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, d.ErrInvestmentInvalid), errors.Is(err, d.ErrAssetInvalid):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, d.ErrInvestmentPrecondition), errors.Is(err, d.ErrAssetPrecondition):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, d.ErrInvestmentConflict), errors.Is(err, d.ErrAssetConflict):
		return status.Error(codes.Aborted, "investment journal changed; refresh and retry")
	case errors.Is(err, d.ErrInvestmentNotFound):
		return status.Error(codes.NotFound, "investment ledger or operation not found")
	case errors.Is(err, d.ErrInvestmentForbidden):
		return status.Error(codes.PermissionDenied, "investment access denied")
	case errors.Is(err, d.ErrInvestmentLimit):
		return status.Error(codes.ResourceExhausted, "investment journal limit exceeded")
	default:
		return status.Error(codes.Internal, "investment journal storage failed")
	}
}
func protoLedger(l d.InvestmentLedger) *pb.InvestmentLedger {
	return &pb.InvestmentLedger{Broker: l.Broker, AccountType: l.AccountType, Comment: l.Comment, DefaultCurrency: l.DefaultCurrency, Archived: l.Archived, MetadataVersion: l.MetadataVersion, Id: l.ID, Name: l.Name, StartedAt: timestamppb.New(l.StartedAt), Version: l.Version, CreatedAt: timestamppb.New(l.CreatedAt)}
}
func protoInvestmentOperation(o d.InvestmentOperation) *pb.InvestmentOperation {
	out := &pb.InvestmentOperation{BudgetTransferId: o.BudgetTransferID, Id: o.ID, LedgerId: o.LedgerID, Kind: o.Kind, OpeningCostKnown: o.OpeningCostKnown, OpeningAccruedKnown: o.OpeningAccruedKnown, AcquiredAt: optionalInvestmentTimestamp(o.AcquiredAt), TransferId: o.TransferID, CounterpartyLedgerId: o.CounterpartyLedgerID, ToCurrency: o.ToCurrency, ToAmount: o.ToAmount, NominalBefore: o.NominalBefore, NominalAfter: o.NominalAfter, InstrumentKey: o.InstrumentKey, Currency: o.Currency, Quantity: o.Quantity, Amount: o.Amount, Accrued: o.Accrued, Fee: o.Fee, Tax: o.Tax, SplitNew: o.SplitNew, SplitOld: o.SplitOld, OccurredAt: timestamppb.New(o.OccurredAt), SettlementAt: timestamppb.New(o.SettlementAt), Sequence: o.Sequence, Version: o.Version, Status: o.Status, Note: o.Note, SettlementAssumed: o.SettlementAssumed, Source: protoInvestmentSource(o.Source)}
	if terms := o.CashReconciliation; terms != nil {
		out.CashReconciliation = &pb.InvestmentCashReconciliation{ExpectedSettled: terms.ExpectedSettled, ObservedSettled: terms.ObservedSettled}
	}
	if timing := o.Timing; timing != nil {
		out.Timing = &pb.InvestmentOperationTiming{Timezone: timing.Timezone, OccurredDate: timing.OccurredDate, SettlementDate: timing.SettlementDate, SourceDate: timing.SourceDate}
	}
	out.ImportedCashDeficitReason = o.ImportedCashDeficitReason
	out.TradeFinancials = protoTradeFinancials(o.TradeFinancials)
	out.LotConversion = protoLotConversion(o.LotConversion)
	out.TaxReviewRequired = o.TaxReviewRequired
	if t := o.Distribution; t != nil {
		out.Distribution = &pb.InvestmentDistribution{EventKey: t.EventKey, TargetInstrumentKey: t.TargetInstrumentKey, NewUnits: t.NewUnits, OldUnits: t.OldUnits, CostNumerator: t.CostNumerator, CostDenominator: t.CostDenominator, ConsumeSource: t.ConsumeSource, MarketValue: t.MarketValue, MarketValueEvidence: t.MarketValueEvidence}
	}
	out.CouponAllocationConfirmed = o.CouponAllocationConfirmed
	for _, a := range o.AccruedRecoveries {
		out.AccruedRecoveries = append(out.AccruedRecoveries, &pb.InvestmentAccruedRecovery{OriginId: a.OriginID, Amount: a.Amount, AllRemaining: a.AllRemaining})
	}
	return out
}
func protoInvestmentSource(s *d.InvestmentSource) *pb.InvestmentOperationSource {
	if s == nil {
		return nil
	}
	return &pb.InvestmentOperationSource{ImportId: s.ImportID, Record: int32(s.Record), ExpectedVersion: s.ExpectedVersion, ReviewReason: s.ReviewReason}
}
func parseInvestmentSource(s *pb.InvestmentOperationSource) *d.InvestmentSource {
	if s == nil {
		return nil
	}
	return &d.InvestmentSource{ImportID: s.GetImportId(), Record: int(s.GetRecord()), ExpectedVersion: s.GetExpectedVersion(), ReviewReason: s.GetReviewReason()}
}
func parseInvestmentOperation(p *pb.InvestmentOperation) (d.InvestmentOperation, error) {
	o := d.InvestmentOperation{BudgetTransferID: p.GetBudgetTransferId(), ID: p.GetId(), LedgerID: p.GetLedgerId(), Kind: p.GetKind(), OpeningCostKnown: p.GetOpeningCostKnown(), OpeningAccruedKnown: p.GetOpeningAccruedKnown(), ToCurrency: p.GetToCurrency(), ToAmount: p.GetToAmount(), NominalBefore: p.GetNominalBefore(), NominalAfter: p.GetNominalAfter(), InstrumentKey: p.GetInstrumentKey(), Currency: p.GetCurrency(), Quantity: p.GetQuantity(), Amount: p.GetAmount(), Accrued: p.GetAccrued(), Fee: p.GetFee(), Tax: p.GetTax(), SplitNew: p.GetSplitNew(), SplitOld: p.GetSplitOld(), Note: p.GetNote()}
	if terms := p.GetCashReconciliation(); terms != nil {
		o.CashReconciliation = &d.InvestmentCashReconciliation{ExpectedSettled: terms.GetExpectedSettled(), ObservedSettled: terms.GetObservedSettled()}
	}
	if timing := p.GetTiming(); timing != nil {
		o.Timing = &d.InvestmentOperationTiming{Timezone: timing.GetTimezone(), OccurredDate: timing.GetOccurredDate(), SettlementDate: timing.GetSettlementDate(), SourceDate: timing.GetSourceDate()}
	}
	lotConversion, conversionErr := parseLotConversion(p.GetLotConversion())
	if conversionErr != nil {
		return o, investmentRPCError(conversionErr)
	}
	o.LotConversion = lotConversion
	financials, financialErr := parseTradeFinancials(p.GetTradeFinancials())
	if financialErr != nil {
		return o, investmentRPCError(financialErr)
	}
	o.ImportedCashDeficitReason = p.GetImportedCashDeficitReason()
	o.TradeFinancials = financials
	o.TaxReviewRequired = p.GetTaxReviewRequired()
	if t := p.GetDistribution(); t != nil {
		o.Distribution = &d.InvestmentDistribution{EventKey: t.EventKey, TargetInstrumentKey: t.TargetInstrumentKey, NewUnits: t.NewUnits, OldUnits: t.OldUnits, CostNumerator: t.CostNumerator, CostDenominator: t.CostDenominator, ConsumeSource: t.ConsumeSource, MarketValue: t.MarketValue, MarketValueEvidence: t.MarketValueEvidence}
	}
	o.CouponAllocationConfirmed = p.GetCouponAllocationConfirmed()
	for _, a := range p.GetAccruedRecoveries() {
		o.AccruedRecoveries = append(o.AccruedRecoveries, d.InvestmentAccruedRecovery{OriginID: a.GetOriginId(), Amount: a.GetAmount(), AllRemaining: a.GetAllRemaining()})
	}
	var err error
	if p.GetOccurredAt() != nil || o.Timing == nil || o.Timing.OccurredDate == "" {
		o.OccurredAt, err = assetTimestamp(p.GetOccurredAt())
	}
	if err != nil {
		return o, err
	}
	if p.GetSettlementAt() != nil {
		o.SettlementAt, err = assetTimestamp(p.GetSettlementAt())
	}
	if err == nil && p.GetAcquiredAt() != nil {
		o.AcquiredAt, err = assetTimestamp(p.GetAcquiredAt())
	}
	return o, err
}
func (s *InvestmentServer) CreateInvestmentLedger(ctx context.Context, req *pb.CreateInvestmentLedgerRequest) (*pb.CreateInvestmentLedgerResponse, error) {
	t, u, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	at, err := assetTimestamp(req.GetStartedAt())
	if err != nil {
		return nil, err
	}
	l, err := s.svc.CreateLedgerWithDetails(ctx, t, u, d.InvestmentLedger{Name: req.GetName(), StartedAt: at, Broker: req.GetBroker(), AccountType: req.GetAccountType(), Comment: req.GetComment(), DefaultCurrency: req.GetDefaultCurrency()}, req.GetRequestKey())
	if err != nil {
		return nil, investmentRPCError(err)
	}
	return &pb.CreateInvestmentLedgerResponse{Ledger: protoLedger(l)}, nil
}
func (s *InvestmentServer) ListInvestmentLedgers(ctx context.Context, req *pb.ListInvestmentLedgersRequest) (*pb.ListInvestmentLedgersResponse, error) {
	t, u, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	limit := int(req.GetLimit())
	if limit == 0 {
		limit = 50
	}
	ledgers, err := s.svc.ListWithArchive(ctx, t, u, limit, int(req.GetOffset()), req.GetIncludeArchived())
	if err != nil {
		return nil, investmentRPCError(err)
	}
	out := &pb.ListInvestmentLedgersResponse{}
	for _, l := range ledgers {
		out.Ledgers = append(out.Ledgers, protoLedger(l))
	}
	return out, nil
}
func (s *InvestmentServer) GetInvestmentSnapshot(ctx context.Context, req *pb.GetInvestmentSnapshotRequest) (*pb.GetInvestmentSnapshotResponse, error) {
	t, u, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	at := time.Now().UTC()
	if req.GetAsOf() != nil {
		at, err = assetTimestamp(req.GetAsOf())
		if err != nil {
			return nil, err
		}
	}
	v, err := s.svc.Snapshot(ctx, t, u, req.GetLedgerId(), at)
	if err != nil {
		return nil, investmentRPCError(err)
	}
	return &pb.GetInvestmentSnapshotResponse{Snapshot: protoInvestmentSnapshot(v)}, nil
}
func protoInvestmentSnapshot(v d.InvestmentSnapshot) *pb.InvestmentSnapshot {
	out := &pb.InvestmentSnapshot{Ledger: protoLedger(v.Ledger), AsOf: timestamppb.New(v.AsOf), ValuationStatus: v.ValuationStatus, Issues: v.Issues}
	for _, c := range v.Cash {
		out.Cash = append(out.Cash, &pb.InvestmentCashBalance{Currency: c.Currency, Settled: c.Settled, Receivable: c.Receivable, Payable: c.Payable, Economic: c.Economic})
	}
	for _, p := range v.Positions {
		position := &pb.InvestmentPosition{OutstandingNominal: p.OutstandingNominal, InstrumentKey: p.InstrumentKey, Currency: p.Currency, Quantity: p.Quantity, CleanCost: p.CleanCost, AcquiredAccrued: p.AcquiredAccrued, RealizedClean: p.RealizedClean, RealizedAccrued: p.RealizedAccrued, CouponGross: p.CouponGross, CouponTax: p.CouponTax, CouponRecovered: p.CouponRecovered, CouponEarned: p.CouponEarned}
		for _, lot := range p.Lots {
			position.Lots = append(position.Lots, &pb.InvestmentLot{OriginId: lot.OriginID, AcquisitionOperationId: lot.AcquisitionOperationID, OriginLedgerId: lot.OriginLedgerID, OriginSequence: lot.OriginSequence, Quantity: lot.Quantity, CleanCost: lot.CleanCost, AcquiredAccrued: lot.AcquiredAccrued, AcquiredAt: optionalInvestmentTimestamp(lot.AcquiredAt)})
		}
		out.Positions = append(out.Positions, position)
	}
	return out
}

func (s *InvestmentServer) ListInvestmentOperations(ctx context.Context, req *pb.ListInvestmentOperationsRequest) (*pb.ListInvestmentOperationsResponse, error) {
	t, u, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	limit := int(req.GetLimit())
	if limit == 0 {
		limit = 50
	}
	operations, err := s.svc.Operations(ctx, t, u, req.GetLedgerId(), limit, int(req.GetOffset()))
	if err != nil {
		return nil, investmentRPCError(err)
	}
	out := &pb.ListInvestmentOperationsResponse{}
	for _, o := range operations {
		out.Operations = append(out.Operations, protoInvestmentOperation(o))
	}
	return out, nil
}
func (s *InvestmentServer) SaveInvestmentOperation(ctx context.Context, req *pb.SaveInvestmentOperationRequest) (*pb.SaveInvestmentOperationResponse, error) {
	t, u, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	o, err := parseInvestmentOperation(req.GetOperation())
	if err != nil {
		return nil, err
	}
	action := "create"
	if o.ID != "" {
		action = "amend"
	}
	result, err := s.svc.Mutate(ctx, t, u, inv.Mutation{PreviewHash: req.GetPreviewHash(), Action: action, Source: parseInvestmentSource(req.GetSource()), Operation: o, RequestKey: req.GetRequestKey(), Reason: req.GetReason(), ExpectedLedgerVersion: req.GetExpectedLedgerVersion(), ExpectedOperationVersion: req.GetExpectedOperationVersion()})
	if err != nil {
		return nil, investmentRPCError(err)
	}
	return &pb.SaveInvestmentOperationResponse{Operation: protoInvestmentOperation(result)}, nil
}
func (s *InvestmentServer) VoidInvestmentOperation(ctx context.Context, req *pb.VoidInvestmentOperationRequest) (*pb.VoidInvestmentOperationResponse, error) {
	t, u, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.svc.Mutate(ctx, t, u, inv.Mutation{PreviewHash: req.GetPreviewHash(), Action: "void", Operation: d.InvestmentOperation{ID: req.GetOperationId(), LedgerID: req.GetLedgerId()}, RequestKey: req.GetRequestKey(), Reason: req.GetReason(), ExpectedLedgerVersion: req.GetExpectedLedgerVersion(), ExpectedOperationVersion: req.GetExpectedOperationVersion()})
	if err != nil {
		return nil, investmentRPCError(err)
	}
	return &pb.VoidInvestmentOperationResponse{Operation: protoInvestmentOperation(result)}, nil
}

func protoInvestmentTransfer(v d.InvestmentTransfer) *pb.InvestmentTransfer {
	return &pb.InvestmentTransfer{Timing: protoInvestmentTiming(v.Timing), InstrumentKey: v.InstrumentKey, Quantity: v.Quantity, Id: v.ID, FromLedgerId: v.FromLedgerID, ToLedgerId: v.ToLedgerID, FromOperationId: v.FromOperationID, ToOperationId: v.ToOperationID, Currency: v.Currency, Amount: v.Amount, OccurredAt: timestamppb.New(v.OccurredAt), Note: v.Note, Status: v.Status, Version: v.Version}
}
func (s *InvestmentServer) GetInvestmentTransfer(ctx context.Context, req *pb.GetInvestmentTransferRequest) (*pb.GetInvestmentTransferResponse, error) {
	t, u, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	v, err := s.svc.GetTransfer(ctx, t, u, req.GetId())
	if err != nil {
		return nil, investmentRPCError(err)
	}
	return &pb.GetInvestmentTransferResponse{Transfer: protoInvestmentTransfer(v)}, nil
}
func (s *InvestmentServer) MutateInvestmentTransfer(ctx context.Context, req *pb.MutateInvestmentTransferRequest) (*pb.MutateInvestmentTransferResponse, error) {
	t, u, err := assetContext(ctx)
	if err != nil {
		return nil, err
	}
	m, err := parseTransferMutation(req)
	if err != nil {
		return nil, err
	}
	result, err := s.svc.Transfer(ctx, t, u, m)
	if err != nil {
		return nil, investmentRPCError(err)
	}
	return &pb.MutateInvestmentTransferResponse{Transfer: protoInvestmentTransfer(result)}, nil
}

func optionalInvestmentTimestamp(at time.Time) *timestamppb.Timestamp {
	if at.IsZero() {
		return nil
	}
	return timestamppb.New(at)
}
