package grpcadapter

import (
	"context"
	"errors"
	"time"

	pb "github.com/positron48/budget/gen/go/budget/v1"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func protoForecastEvent(e inv.ForecastEvent) *pb.InvestmentForecastEvent {
	return &pb.InvestmentForecastEvent{Id: e.ID, Version: e.Version, InstrumentKey: e.InstrumentKey, Kind: e.Kind, Nature: e.Nature, Currency: e.Currency, PerShare: e.PerShare, ExDate: e.ExDate, RecordDate: e.RecordDate, PaymentDate: e.PaymentDate, Timezone: e.Timezone, EntitlementRule: e.EntitlementRule, EntitlementAt: optionalInvestmentTimestamp(e.EntitlementAt), SourceReference: e.SourceReference, Evidence: e.Evidence, Reason: e.Reason, Cancelled: e.Cancelled, Origin: e.Origin}
}
func parseForecastEvent(e *pb.InvestmentForecastEvent) (inv.ForecastEvent, error) {
	if e == nil {
		return inv.ForecastEvent{}, d.ErrInvestmentInvalid
	}
	var at time.Time
	if e.EntitlementAt != nil {
		if err := e.EntitlementAt.CheckValid(); err != nil {
			return inv.ForecastEvent{}, d.ErrInvestmentInvalid
		}
		at = e.EntitlementAt.AsTime()
	}
	return inv.NormalizeForecastEvent(inv.ForecastEvent{ID: e.Id, Version: e.Version, InstrumentKey: e.InstrumentKey, Kind: e.Kind, Nature: e.Nature, Currency: e.Currency, PerShare: e.PerShare, ExDate: e.ExDate, RecordDate: e.RecordDate, PaymentDate: e.PaymentDate, Timezone: e.Timezone, EntitlementRule: e.EntitlementRule, EntitlementAt: at, SourceReference: e.SourceReference, Evidence: e.Evidence, Reason: e.Reason, Cancelled: e.Cancelled, Origin: e.Origin})
}
func protoForecastLink(l inv.ForecastLink) *pb.InvestmentForecastLink {
	return &pb.InvestmentForecastLink{EventId: l.EventID, EventVersion: l.EventVersion, LedgerId: l.LedgerID, OperationId: l.OperationID, OperationVersion: l.OperationVersion, Version: l.Version, Complete: l.Complete, Cancelled: l.Cancelled, Reason: l.Reason}
}
func parseForecastLink(l *pb.InvestmentForecastLink) (inv.ForecastLink, error) {
	if l == nil {
		return inv.ForecastLink{}, d.ErrInvestmentInvalid
	}
	return inv.NormalizeForecastLink(inv.ForecastLink{EventID: l.EventId, EventVersion: l.EventVersion, LedgerID: l.LedgerId, OperationID: l.OperationId, OperationVersion: l.OperationVersion, Version: l.Version, Complete: l.Complete, Cancelled: l.Cancelled, Reason: l.Reason})
}
func (s *InvestmentServer) GetInvestmentForecast(ctx context.Context, req *pb.GetInvestmentForecastRequest) (*pb.GetInvestmentForecastResponse, error) {
	t, u, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	report, e := s.svc.Forecast(ctx, t, u, inv.ForecastQuery{LedgerIDs: req.GetLedgerIds(), FromMonth: req.GetFromMonth(), Currency: req.GetCurrency(), TaxRate: req.GetTaxRate(), FXPolicy: req.GetFxPolicyKey()})
	if errors.Is(e, md.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "forecast FX policy not found")
	}
	if errors.Is(e, md.ErrLimit) {
		return nil, status.Error(codes.ResourceExhausted, "forecast FX evidence limit exceeded")
	}
	if e != nil {
		return nil, investmentRPCError(e)
	}
	return protoForecastReport(report), nil
}
func protoForecastReport(v inv.ForecastReport) *pb.GetInvestmentForecastResponse {
	out := &pb.GetInvestmentForecastResponse{FromMonth: v.FromMonth, ToMonth: v.ToMonth, Currency: v.Currency, FxPolicy: v.FXPolicy, TaxRate: v.TaxRate, AsOf: optionalInvestmentTimestamp(v.AsOf), Gross: protoPayoutAmount(v.Gross), EstimatedNet: protoPayoutAmount(v.EstimatedNet), MonthlyAverageGross: protoPayoutAmount(v.MonthlyAverageGross), MonthlyAverageNet: protoPayoutAmount(v.MonthlyAverageNet), Announced: int32(v.Announced), Projected: int32(v.Projected), Unknown: int32(v.Unknown), Paid: int32(v.Paid), Warnings: v.Warnings}
	for _, e := range v.Events {
		out.Events = append(out.Events, protoForecastEvent(e))
	}
	for _, l := range v.Links {
		out.Links = append(out.Links, protoForecastLink(l))
	}
	for _, l := range v.Ledgers {
		out.Ledgers = append(out.Ledgers, protoLedger(l))
	}
	for _, m := range v.Months {
		out.Months = append(out.Months, &pb.InvestmentForecastMonth{Month: m.Month, KnownGross: protoPayoutAmount(m.KnownGross), KnownEstimatedNet: protoPayoutAmount(m.KnownEstimatedNet), Gross: protoPayoutAmount(m.Gross), EstimatedNet: protoPayoutAmount(m.EstimatedNet), Announced: int32(m.Announced), Projected: int32(m.Projected), Unknown: int32(m.Unknown), Paid: int32(m.Paid)})
	}
	for _, r := range v.Rows {
		row := &pb.InvestmentForecastRow{EventId: r.EventID, EventVersion: r.EventVersion, LedgerId: r.LedgerID, InstrumentKey: r.InstrumentKey, Kind: r.Kind, Nature: r.Nature, PaymentDate: r.PaymentDate, Currency: r.Currency, Quantity: protoPayoutAmount(r.Quantity), Gross: protoPayoutAmount(r.Gross), EstimatedTax: protoPayoutAmount(r.EstimatedTax), EstimatedNet: protoPayoutAmount(r.EstimatedNet), ConvertedGross: protoPayoutAmount(r.ConvertedGross), ConvertedNet: protoPayoutAmount(r.ConvertedNet), Entitlement: r.Entitlement, Status: r.Status, ActualNet: protoPayoutAmount(r.ActualNet), ActualOperationIds: r.ActualOperationIDs, Issues: r.Issues}
		for _, l := range r.FXLegs {
			row.FxLegs = append(row.FxLegs, &pb.InvestmentForecastFXLeg{Id: l.ID, Revision: l.Revision, FromCurrency: l.From, ToCurrency: l.To, Policy: l.Policy, Inverse: l.Inverse, AsOf: optionalInvestmentTimestamp(l.At), AvailableAt: optionalInvestmentTimestamp(l.AvailableAt), ValidUntil: optionalInvestmentTimestamp(l.ValidUntil)})
		}
		out.Rows = append(out.Rows, row)
	}
	return out
}
func (s *InvestmentServer) SaveInvestmentForecastEvent(ctx context.Context, req *pb.SaveInvestmentForecastEventRequest) (*pb.SaveInvestmentForecastEventResponse, error) {
	t, u, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	event, e := parseForecastEvent(req.GetEvent())
	if e != nil {
		return nil, investmentRPCError(e)
	}
	event, e = s.svc.SaveForecastEvent(ctx, t, u, event, req.GetExpectedVersion())
	if e != nil {
		return nil, investmentRPCError(e)
	}
	return &pb.SaveInvestmentForecastEventResponse{Event: protoForecastEvent(event)}, nil
}
func (s *InvestmentServer) SaveInvestmentForecastLink(ctx context.Context, req *pb.SaveInvestmentForecastLinkRequest) (*pb.SaveInvestmentForecastLinkResponse, error) {
	t, u, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	link, e := parseForecastLink(req.GetLink())
	if e != nil {
		return nil, investmentRPCError(e)
	}
	link, e = s.svc.SaveForecastLink(ctx, t, u, link, req.GetExpectedVersion())
	if e != nil {
		return nil, investmentRPCError(e)
	}
	return &pb.SaveInvestmentForecastLinkResponse{Link: protoForecastLink(link)}, nil
}
