package grpcadapter

import (
	"context"
	"errors"
	budgetv1 "github.com/positron48/budget/gen/go/budget/v1"
	"github.com/positron48/budget/internal/domain"
	rp "github.com/positron48/budget/internal/usecase/recurringpayment"
	"github.com/positron48/budget/internal/usecase/transaction"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"strings"
	"time"
)

type RecurringPaymentServer struct {
	budgetv1.UnimplementedRecurringPaymentServiceServer
	svc *rp.Service
}

func NewRecurringPaymentServer(s *rp.Service) *RecurringPaymentServer {
	return &RecurringPaymentServer{svc: s}
}
func paymentRPCError(e error) error {
	if e == nil {
		return nil
	}
	switch {
	case errors.Is(e, rp.ErrInvalid), errors.Is(e, domain.ErrAssetInvalid):
		return status.Error(codes.InvalidArgument, "payment_invalid")
	case errors.Is(e, rp.ErrNotFound):
		return status.Error(codes.NotFound, "payment_not_found")
	case errors.Is(e, rp.ErrConflict), errors.Is(e, domain.ErrIdempotencyConflict):
		return status.Error(codes.Aborted, "payment_version_conflict")
	case errors.Is(e, rp.ErrPrecondition), errors.Is(e, domain.ErrAssetPrecondition), errors.Is(e, transaction.ErrFxRateNotFound), errors.Is(e, transaction.ErrInvalidCategory), errors.Is(e, transaction.ErrTypeMismatch):
		return status.Error(codes.FailedPrecondition, "payment_precondition")
	default:
		return status.Error(codes.Internal, "payment_failed")
	}
}
func paymentTemplate(p *budgetv1.RecurringPaymentTemplate) domain.PaymentTemplate {
	if p == nil {
		return domain.PaymentTemplate{}
	}
	return domain.PaymentTemplate{ID: p.Id, Name: p.Name, CategoryID: p.CategoryId, CurrencyCode: p.CurrencyCode, AccountID: p.AccountId, Note: p.Note, Timezone: p.Timezone, AmountMode: p.AmountMode, FixedAmount: p.FixedAmount, InitialEstimate: p.InitialEstimate, FirstMonth: p.FirstMonth, LastMonth: p.LastMonth, IntervalMonths: int(p.IntervalMonths), Day: int(p.Day), LastDay: p.LastDay, PeriodOffset: int(p.PeriodOffset), Archived: p.Archived, Version: p.Version}
}
func protoPaymentTemplate(t domain.PaymentTemplate) *budgetv1.RecurringPaymentTemplate {
	return &budgetv1.RecurringPaymentTemplate{Id: t.ID, Name: t.Name, CategoryId: t.CategoryID, CurrencyCode: t.CurrencyCode, AccountId: t.AccountID, Note: t.Note, Timezone: t.Timezone, AmountMode: t.AmountMode, FixedAmount: t.FixedAmount, InitialEstimate: t.InitialEstimate, FirstMonth: t.FirstMonth, LastMonth: t.LastMonth, IntervalMonths: int32(t.IntervalMonths), Day: int32(t.Day), LastDay: t.LastDay, PeriodOffset: int32(t.PeriodOffset), Archived: t.Archived, Version: t.Version}
}
func protoPayment(o domain.PaymentOccurrence) *budgetv1.PaymentOccurrence {
	p := &budgetv1.PaymentOccurrence{Id: o.ID, TemplateId: o.TemplateID, ScheduledMonth: o.ScheduledMonth, PeriodMonth: o.PeriodMonth, DueDate: o.DueDate, Name: o.Name, CategoryId: o.CategoryID, CurrencyCode: o.CurrencyCode, AccountId: o.AccountID, Note: o.Note, Timezone: o.Timezone, AmountMode: o.AmountMode, ExactAmount: o.ExactAmount, EstimateOverride: o.EstimateOverride, ExpectedAmount: o.ExpectedAmount, EstimateSource: o.EstimateSource, Status: o.Status, SkipReason: o.SkipReason, Manual: o.Manual, Version: o.Version, ConfirmedBy: o.ConfirmedBy, FxDate: o.FXDate, Incomplete: o.Incomplete}
	if o.Transaction != nil {
		p.Transaction = toProtoTx(*o.Transaction)
	}
	if o.ConfirmedAt != nil {
		p.ConfirmedAt = timestamppb.New(*o.ConfirmedAt)
	}
	if o.DisplayAmount != nil {
		p.DisplayAmount = protoAssetMoney(*o.DisplayAmount)
	}
	return p
}
func protoPayments(items []domain.PaymentOccurrence) []*budgetv1.PaymentOccurrence {
	out := make([]*budgetv1.PaymentOccurrence, 0, len(items))
	for _, o := range items {
		out = append(out, protoPayment(o))
	}
	return out
}
func protoPaymentSummary(s domain.PaymentSummary) *budgetv1.PaymentSummary {
	return &budgetv1.PaymentSummary{Exact: protoAssetMoney(s.Exact), Estimated: protoAssetMoney(s.Estimated), Paid: protoAssetMoney(s.Paid), Unknown: int32(s.Unknown), NeedsReview: int32(s.NeedsReview), MissingFx: int32(s.MissingFX), Total: int32(s.Total), Outstanding: int32(s.Outstanding)}
}
func (s *RecurringPaymentServer) ListRecurringPaymentTemplates(ctx context.Context, req *budgetv1.ListRecurringPaymentTemplatesRequest) (*budgetv1.ListRecurringPaymentTemplatesResponse, error) {
	tenant, _, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	items, e := s.svc.Templates(ctx, tenant)
	if e != nil {
		return nil, paymentRPCError(e)
	}
	out := &budgetv1.ListRecurringPaymentTemplatesResponse{}
	for _, t := range items {
		out.Templates = append(out.Templates, protoPaymentTemplate(t))
	}
	return out, nil
}
func (s *RecurringPaymentServer) CreateRecurringPaymentTemplate(ctx context.Context, req *budgetv1.CreateRecurringPaymentTemplateRequest) (*budgetv1.CreateRecurringPaymentTemplateResponse, error) {
	tenant, user, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	if req.Template == nil || req.Template.Id != "" {
		return nil, paymentRPCError(rp.ErrInvalid)
	}
	result, e := s.svc.ChangeTemplate(ctx, tenant, user, rp.TemplateChange{Template: paymentTemplate(req.Template), RequestKey: req.RequestKey})
	if e != nil {
		return nil, paymentRPCError(e)
	}
	return &budgetv1.CreateRecurringPaymentTemplateResponse{Template: protoPaymentTemplate(result.Template)}, nil
}
func (s *RecurringPaymentServer) UpdateRecurringPaymentTemplate(ctx context.Context, req *budgetv1.UpdateRecurringPaymentTemplateRequest) (*budgetv1.UpdateRecurringPaymentTemplateResponse, error) {
	tenant, user, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	if req.Template == nil || req.Template.Id == "" {
		return nil, paymentRPCError(rp.ErrInvalid)
	}
	result, e := s.svc.ChangeTemplate(ctx, tenant, user, rp.TemplateChange{Template: paymentTemplate(req.Template), RequestKey: req.RequestKey, Preview: req.Preview})
	if e != nil {
		return nil, paymentRPCError(e)
	}
	return &budgetv1.UpdateRecurringPaymentTemplateResponse{Template: protoPaymentTemplate(result.Template), Affected: protoPayments(result.Affected), Preserved: protoPayments(result.Preserved), Proposed: protoPayments(result.Proposed)}, nil
}
func (s *RecurringPaymentServer) ArchiveRecurringPaymentTemplate(ctx context.Context, req *budgetv1.ArchiveRecurringPaymentTemplateRequest) (*budgetv1.ArchiveRecurringPaymentTemplateResponse, error) {
	tenant, user, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	result, e := s.svc.ChangeTemplate(ctx, tenant, user, rp.TemplateChange{Template: domain.PaymentTemplate{ID: req.Id, Version: req.ExpectedVersion}, RequestKey: req.RequestKey, Archive: true})
	if e != nil {
		return nil, paymentRPCError(e)
	}
	return &budgetv1.ArchiveRecurringPaymentTemplateResponse{Template: protoPaymentTemplate(result.Template)}, nil
}
func (s *RecurringPaymentServer) ListPaymentOccurrences(ctx context.Context, req *budgetv1.ListPaymentOccurrencesRequest) (*budgetv1.ListPaymentOccurrencesResponse, error) {
	tenant, _, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	from, e := rp.Date(req.FromDate)
	if e != nil {
		return nil, paymentRPCError(e)
	}
	to, e := rp.Date(req.ToDate)
	if e != nil || !to.After(from) {
		return nil, paymentRPCError(rp.ErrInvalid)
	}
	all, e := s.svc.Read(ctx, tenant, to.Format("2006-01"), req.TargetCurrencyCode)
	if e != nil {
		return nil, paymentRPCError(e)
	}
	items := []domain.PaymentOccurrence{}
	for _, o := range all {
		if o.DueDate < req.FromDate || o.DueDate >= req.ToDate || !strings.Contains(strings.ToLower(o.Name), strings.ToLower(req.Search)) {
			continue
		}
		if len(req.Statuses) > 0 {
			match := false
			for _, v := range req.Statuses {
				if v == o.Status {
					match = true
				}
			}
			if !match {
				continue
			}
		}
		items = append(items, o)
	}
	summary, e := rp.Summary(items, req.TargetCurrencyCode)
	if e != nil {
		return nil, paymentRPCError(e)
	}
	total := len(items)
	size := int(req.PageSize)
	if size <= 0 {
		size = 50
	}
	if size > 200 {
		size = 200
	}
	page := int(req.Page)
	if page < 1 {
		page = 1
	}
	start := (page - 1) * size
	if start > total {
		start = total
	}
	end := start + size
	if end > total {
		end = total
	}
	return &budgetv1.ListPaymentOccurrencesResponse{Occurrences: protoPayments(items[start:end]), Summary: protoPaymentSummary(summary), Total: int32(total)}, nil
}
func (s *RecurringPaymentServer) GetPaymentOccurrence(ctx context.Context, req *budgetv1.GetPaymentOccurrenceRequest) (*budgetv1.GetPaymentOccurrenceResponse, error) {
	tenant, _, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	all, e := s.svc.Read(ctx, tenant, s.svc.Now().Format("2006-01"), req.TargetCurrencyCode)
	if e != nil {
		return nil, paymentRPCError(e)
	}
	for _, o := range all {
		if o.ID == req.Id {
			return &budgetv1.GetPaymentOccurrenceResponse{Occurrence: protoPayment(o)}, nil
		}
	}
	return nil, paymentRPCError(rp.ErrNotFound)
}
func (s *RecurringPaymentServer) GetPaymentOverview(ctx context.Context, req *budgetv1.GetPaymentOverviewRequest) (*budgetv1.GetPaymentOverviewResponse, error) {
	tenant, _, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	month, e := rp.Month(req.Month)
	if e != nil {
		return nil, paymentRPCError(e)
	}
	all, e := s.svc.Read(ctx, tenant, req.Month, req.TargetCurrencyCode)
	if e != nil {
		return nil, paymentRPCError(e)
	}
	current := []domain.PaymentOccurrence{}
	debts := []domain.PaymentOccurrence{}
	for _, o := range all {
		if o.DueDate >= req.Month+"-01" && o.DueDate < month.AddDate(0, 1, 0).Format("2006-01-02") {
			current = append(current, o)
		} else if o.DueDate < req.Month+"-01" && (o.Status == "overdue" || o.Status == "needs_review") {
			debts = append(debts, o)
		}
	}
	sum, e := rp.Summary(current, req.TargetCurrencyCode)
	if e != nil {
		return nil, paymentRPCError(e)
	}
	prior, e := rp.Summary(debts, req.TargetCurrencyCode)
	if e != nil {
		return nil, paymentRPCError(e)
	}
	unpaid := []domain.PaymentOccurrence{}
	for _, o := range current {
		if o.Status != "paid" && o.Status != "skipped" {
			unpaid = append(unpaid, o)
		}
	}
	if len(unpaid) > 5 {
		unpaid = unpaid[:5]
	}
	if len(debts) > 5 {
		debts = debts[:5]
	}
	return &budgetv1.GetPaymentOverviewResponse{Occurrences: protoPayments(unpaid), Summary: protoPaymentSummary(sum), PreviousDebts: protoPayments(debts), PreviousSummary: protoPaymentSummary(prior)}, nil
}
func (s *RecurringPaymentServer) UpdatePaymentOccurrence(ctx context.Context, req *budgetv1.UpdatePaymentOccurrenceRequest) (*budgetv1.UpdatePaymentOccurrenceResponse, error) {
	tenant, user, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	o, e := s.svc.Change(ctx, tenant, user, rp.Change{ID: req.Id, Version: req.ExpectedVersion, RequestKey: req.RequestKey, Action: "update", ExactAmount: req.ExactAmount, Estimate: req.EstimateOverride, DueDate: req.DueDate, Note: req.Note})
	if e != nil {
		return nil, paymentRPCError(e)
	}
	o.Status = rp.Status(o, s.svc.Now())
	return &budgetv1.UpdatePaymentOccurrenceResponse{Occurrence: protoPayment(o)}, nil
}
func (s *RecurringPaymentServer) UnlinkPayment(ctx context.Context, req *budgetv1.UnlinkPaymentRequest) (*budgetv1.UnlinkPaymentResponse, error) {
	tenant, user, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	o, e := s.svc.Change(ctx, tenant, user, rp.Change{ID: req.Id, Version: req.ExpectedVersion, RequestKey: req.RequestKey, Action: "unlink"})
	if e != nil {
		return nil, paymentRPCError(e)
	}
	o.Status = rp.Status(o, s.svc.Now())
	return &budgetv1.UnlinkPaymentResponse{Occurrence: protoPayment(o)}, nil
}
func (s *RecurringPaymentServer) SetPaymentSkipped(ctx context.Context, req *budgetv1.SetPaymentSkippedRequest) (*budgetv1.SetPaymentSkippedResponse, error) {
	tenant, user, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	o, e := s.svc.Change(ctx, tenant, user, rp.Change{ID: req.Id, Version: req.ExpectedVersion, RequestKey: req.RequestKey, Action: "skip", Skipped: req.Skipped, Reason: req.Reason})
	if e != nil {
		return nil, paymentRPCError(e)
	}
	o.Status = rp.Status(o, s.svc.Now())
	return &budgetv1.SetPaymentSkippedResponse{Occurrence: protoPayment(o)}, nil
}
func (s *RecurringPaymentServer) ConfirmPayment(ctx context.Context, req *budgetv1.ConfirmPaymentRequest) (*budgetv1.ConfirmPaymentResponse, error) {
	tenant, user, e := assetContext(ctx)
	if e != nil {
		return nil, e
	}
	c := rp.Confirmation{ID: req.OccurrenceId, Version: req.ExpectedVersion, RequestKey: req.RequestKey, AcceptAmountChange: req.AcceptAmountChange}
	switch x := req.Expense.(type) {
	case *budgetv1.ConfirmPaymentRequest_NewExpense:
		if x.NewExpense == nil {
			return nil, paymentRPCError(rp.ErrInvalid)
		}
		p := x.NewExpense
		at, e := assetTimestamp(p.OccurredAt)
		if e != nil {
			return nil, e
		}
		c.Amount = p.Amount
		c.CurrencyCode = p.CurrencyCode
		c.OccurredAt = at
		c.CategoryID = p.CategoryId
		c.AccountID = p.AccountId
		c.Comment = p.Comment
	case *budgetv1.ConfirmPaymentRequest_ExistingTransaction:
		if x.ExistingTransaction == nil {
			return nil, paymentRPCError(rp.ErrInvalid)
		}
		p := x.ExistingTransaction
		if p.TransactionId == "" {
			return nil, paymentRPCError(rp.ErrInvalid)
		}
		at, e := assetTimestamp(p.ExpectedOccurredAt)
		if e != nil {
			return nil, e
		}
		c.TransactionID = p.TransactionId
		c.Amount = p.ExpectedAmount
		c.CurrencyCode = p.ExpectedCurrencyCode
		c.OccurredAt = at
	default:
		return nil, paymentRPCError(rp.ErrInvalid)
	}
	o, e := s.svc.Confirm(ctx, tenant, user, c)
	if e != nil {
		return nil, paymentRPCError(e)
	}
	o.Status = rp.Status(o, time.Now())
	return &budgetv1.ConfirmPaymentResponse{Occurrence: protoPayment(o)}, nil
}
