package recurringpayment

import (
	"context"
	"github.com/google/uuid"
	"github.com/positron48/budget/internal/domain"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	"github.com/positron48/budget/internal/usecase/transaction"
	"github.com/positron48/budget/internal/usecase/transactionfx"
	"sort"
	"time"
)

type Change struct {
	ID                            string
	Version                       int64
	RequestKey                    string
	ExactAmount, Estimate         *int64
	DueDate, Note, Action, Reason string
	Skipped                       bool
}
type Confirmation struct {
	ID                                                                      string
	Version                                                                 int64
	RequestKey, TransactionID, CategoryID, AccountID, CurrencyCode, Comment string
	Amount                                                                  int64
	OccurredAt                                                              time.Time
	AcceptAmountChange                                                      bool
}
type TemplateChange struct {
	Template         domain.PaymentTemplate
	RequestKey       string
	Archive, Preview bool
}
type TemplateResult struct {
	Template                      domain.PaymentTemplate
	Affected, Preserved, Proposed []domain.PaymentOccurrence
}
type Repository interface {
	Templates(context.Context, string) ([]domain.PaymentTemplate, error)
	ChangeTemplate(context.Context, string, string, TemplateChange, time.Time) (TemplateResult, error)
	Read(context.Context, string, string, time.Time) ([]domain.PaymentOccurrence, error)
	ChangeOccurrence(context.Context, string, string, Change) (domain.PaymentOccurrence, error)
	Confirm(context.Context, string, string, Confirmation, time.Time, func(context.Context, domain.PaymentOccurrence) (domain.Transaction, error)) (domain.PaymentOccurrence, error)
}
type Service struct {
	repo Repository
	tx   *transaction.Service
	fx   transactionfx.Rates
	Now  func() time.Time
}

func New(repo Repository, tx *transaction.Service, fx transactionfx.Rates) *Service {
	return &Service{repo: repo, tx: tx, fx: fx, Now: time.Now}
}
func (s *Service) Templates(ctx context.Context, tenant string) ([]domain.PaymentTemplate, error) {
	return s.repo.Templates(ctx, tenant)
}
func (s *Service) ChangeTemplate(ctx context.Context, tenant, user string, c TemplateChange) (TemplateResult, error) {
	if !c.Archive {
		if e := Validate(c.Template); e != nil {
			return TemplateResult{}, e
		}
	}
	return s.repo.ChangeTemplate(ctx, tenant, user, c, s.Now())
}
func (s *Service) Read(ctx context.Context, tenant, through, target string) ([]domain.PaymentOccurrence, error) {
	if target != "RUB" && target != "USD" && target != "EUR" {
		return nil, ErrInvalid
	}
	end, e := Month(through)
	if e != nil || end.Year() > 9999 || end.Year() < 1 {
		return nil, ErrInvalid
	}
	now := s.Now()
	all, e := s.repo.Read(ctx, tenant, through, now)
	if e != nil {
		return nil, e
	}
	// Read returns stable due order; estimates use scheduled order, not moved due dates.
	for i := range all {
		all[i].Status = Status(all[i], now)
	}
	indices := make([]int, len(all))
	for i := range all {
		indices[i] = i
	}
	sort.Slice(indices, func(i, j int) bool { return all[indices[i]].ScheduledMonth < all[indices[j]].ScheduledMonth })
	previous := map[string]*int64{}
	for _, i := range indices {
		o := &all[i]
		key := o.TemplateID + ":" + o.CurrencyCode
		o.ExpectedAmount, o.EstimateSource = Expected(*o, previous[key])
		if o.Status == "paid" {
			v := o.Transaction.Amount.MinorUnits
			previous[key] = &v
		}
	}
	currentRates := map[string]domain.AssetFxRate{}
	if rates, ok := s.fx.(interface {
		GetAssetRates(context.Context, []string, string, time.Time) ([]domain.AssetFxRate, error)
	}); ok {
		codes := []string{}
		seen := map[string]bool{}
		for _, o := range all {
			if o.CurrencyCode != target && !seen[o.CurrencyCode] {
				codes = append(codes, o.CurrencyCode)
				seen[o.CurrencyCode] = true
			}
		}
		if len(codes) > 0 {
			quotes, e := rates.GetAssetRates(ctx, codes, target, now)
			if e == nil {
				for _, q := range quotes {
					currentRates[q.From] = q
				}
			}
		}
	}
	converter := transactionfx.New(s.fx)
	for i := range all {
		o := &all[i]
		var tx domain.Transaction
		if o.Status == "paid" {
			tx = *o.Transaction
		} else if o.ExpectedAmount != nil {
			tx = domain.Transaction{Amount: domain.Money{CurrencyCode: o.CurrencyCode, MinorUnits: *o.ExpectedAmount}, OccurredAt: now}
		} else {
			continue
		}
		var m domain.Money
		var e error
		if q, ok := currentRates[o.CurrencyCode]; ok && o.Status != "paid" && o.CurrencyCode != target {
			var minor int64
			minor, e = transactionfx.ConvertMinor(tx.Amount.MinorUnits, q.RateDecimal)
			m = domain.Money{CurrencyCode: target, MinorUnits: minor}
		} else {
			m, e = converter.Convert(ctx, tx, target)
		}
		if e != nil {
			o.Incomplete = true
			continue
		}
		o.DisplayAmount = &m
		if o.Status != "paid" && o.CurrencyCode != target {
			if q, ok := currentRates[o.CurrencyCode]; ok {
				o.FXDate = q.AsOf.Format("2006-01-02")
			}
		}
	}
	return all, nil
}
func Summary(items []domain.PaymentOccurrence, target string) (domain.PaymentSummary, error) {
	s := domain.PaymentSummary{Exact: domain.Money{CurrencyCode: target}, Estimated: domain.Money{CurrencyCode: target}, Paid: domain.Money{CurrencyCode: target}}
	for _, o := range items {
		s.Total++
		if o.Status == "skipped" {
			continue
		}
		if o.Status == "needs_review" {
			s.NeedsReview++
			continue
		}
		if o.Status != "paid" {
			s.Outstanding++
		}
		if o.ExpectedAmount == nil && o.Status != "paid" {
			s.Unknown++
			continue
		}
		if o.DisplayAmount == nil {
			s.MissingFX++
			continue
		}
		var dst *int64
		switch {
		case o.Status == "paid":
			dst = &s.Paid.MinorUnits
		case o.EstimateSource == "exact":
			dst = &s.Exact.MinorUnits
		default:
			dst = &s.Estimated.MinorUnits
		}
		v, e := transactionfx.Add(*dst, o.DisplayAmount.MinorUnits)
		if e != nil {
			return s, e
		}
		*dst = v
	}
	return s, nil
}
func (s *Service) Change(ctx context.Context, tenant, user string, c Change) (domain.PaymentOccurrence, error) {
	if _, e := uuid.Parse(c.ID); e != nil {
		return domain.PaymentOccurrence{}, ErrInvalid
	}
	if c.RequestKey == "" || c.Version < 1 {
		return domain.PaymentOccurrence{}, ErrInvalid
	}
	if c.Action == "update" {
		if _, e := Date(c.DueDate); e != nil {
			return domain.PaymentOccurrence{}, e
		}
		for _, v := range []*int64{c.ExactAmount, c.Estimate} {
			if v != nil && *v <= 0 {
				return domain.PaymentOccurrence{}, ErrInvalid
			}
		}
	}
	return s.repo.ChangeOccurrence(ctx, tenant, user, c)
}
func (s *Service) Confirm(ctx context.Context, tenant, user string, c Confirmation) (domain.PaymentOccurrence, error) {
	for _, id := range []string{c.ID, c.TransactionID, c.CategoryID, c.AccountID} {
		if id != "" {
			if _, e := uuid.Parse(id); e != nil {
				return domain.PaymentOccurrence{}, ErrInvalid
			}
		}
	}
	if c.RequestKey == "" || c.Version < 1 || c.Amount <= 0 || c.OccurredAt.IsZero() || c.OccurredAt.After(s.Now()) {
		return domain.PaymentOccurrence{}, ErrInvalid
	}
	return s.repo.Confirm(ctx, tenant, user, c, s.Now(), func(ctx context.Context, o domain.PaymentOccurrence) (domain.Transaction, error) {
		cat := c.CategoryID
		if cat == "" {
			cat = o.CategoryID
		}
		ctx = ctxutil.WithAssetAccount(ctx, c.AccountID)
		return s.tx.CreateForUser(ctx, tenant, user, domain.TransactionTypeExpense, cat, domain.Money{CurrencyCode: c.CurrencyCode, MinorUnits: c.Amount}, c.OccurredAt, c.Comment, false)
	})
}
