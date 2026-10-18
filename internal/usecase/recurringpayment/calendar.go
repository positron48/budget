package recurringpayment

import (
	"errors"
	"github.com/google/uuid"
	"github.com/positron48/budget/internal/domain"
	"strings"
	"time"
	_ "time/tzdata"
)

var (
	ErrInvalid      = errors.New("payment_invalid")
	ErrConflict     = errors.New("payment_version_conflict")
	ErrPrecondition = errors.New("payment_precondition")
	ErrNotFound     = errors.New("payment_not_found")
)

func Month(s string) (time.Time, error) {
	t, e := time.Parse("2006-01", s)
	if e != nil || t.Format("2006-01") != s {
		return t, ErrInvalid
	}
	return t, nil
}
func Date(s string) (time.Time, error) {
	t, e := time.Parse("2006-01-02", s)
	if e != nil || t.Format("2006-01-02") != s {
		return t, ErrInvalid
	}
	return t, nil
}
func Validate(t domain.PaymentTemplate) error {
	if strings.TrimSpace(t.Name) == "" || len(t.Name) > 200 || (t.IntervalMonths != 1 && t.IntervalMonths != 3 && t.IntervalMonths != 12) || t.Day < 1 || t.Day > 31 || (t.PeriodOffset != 0 && t.PeriodOffset != -1) || (t.PeriodOffset != 0 && t.IntervalMonths != 1) {
		return ErrInvalid
	}
	if _, e := uuid.Parse(t.CategoryID); e != nil {
		return ErrInvalid
	}
	if t.AccountID != "" {
		if _, e := uuid.Parse(t.AccountID); e != nil {
			return ErrInvalid
		}
	}
	if len(t.CurrencyCode) != 3 || strings.Trim(t.CurrencyCode, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") != "" {
		return ErrInvalid
	}
	first, e := Month(t.FirstMonth)
	if e != nil {
		return e
	}
	if first.Year() < 1 || first.Year() > 9999 {
		return ErrInvalid
	}
	if t.LastMonth != "" {
		last, e := Month(t.LastMonth)
		if e != nil || last.Before(first) || last.Year() > 9999 {
			return ErrInvalid
		}
	}
	if _, e := time.LoadLocation(t.Timezone); e != nil {
		return ErrInvalid
	}
	if t.AmountMode != "fixed" && t.AmountMode != "variable" {
		return ErrInvalid
	}
	if t.AmountMode == "fixed" && (t.FixedAmount == nil || *t.FixedAmount <= 0) {
		return ErrInvalid
	}
	for _, v := range []*int64{t.FixedAmount, t.InitialEstimate} {
		if v != nil && *v <= 0 {
			return ErrInvalid
		}
	}
	return nil
}

// Always start from the original month/day, never from February's clipped date.
func Occurrence(t domain.PaymentTemplate, month time.Time) domain.PaymentOccurrence {
	last := time.Date(month.Year(), month.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
	day := t.Day
	if t.LastDay || day > last {
		day = last
	}
	o := domain.PaymentOccurrence{TenantID: t.TenantID, TemplateID: t.ID, ScheduledMonth: month.Format("2006-01"), PeriodMonth: month.AddDate(0, t.PeriodOffset, 0).Format("2006-01"), DueDate: time.Date(month.Year(), month.Month(), day, 0, 0, 0, 0, time.UTC).Format("2006-01-02"), Name: t.Name, CategoryID: t.CategoryID, CurrencyCode: t.CurrencyCode, AccountID: t.AccountID, Note: t.Note, Timezone: t.Timezone, AmountMode: t.AmountMode, InitialEstimate: t.InitialEstimate, Version: 1, UserID: t.UserID}
	if t.AmountMode == "fixed" {
		o.ExactAmount = t.FixedAmount
	}
	return o
}
func Status(o domain.PaymentOccurrence, now time.Time) string {
	if o.Transaction != nil {
		t := o.Transaction
		if o.ConfirmedAmount == nil || t.Amount.MinorUnits != *o.ConfirmedAmount || t.Amount.CurrencyCode != o.ConfirmedCurrency || string(t.Type) != o.ConfirmedType || t.Type != domain.TransactionTypeExpense || t.Amount.MinorUnits <= 0 || t.Amount.CurrencyCode != o.CurrencyCode || t.OccurredAt.After(now) {
			return "needs_review"
		}
		return "paid"
	}
	if o.Skipped {
		return "skipped"
	}
	loc, e := time.LoadLocation(o.Timezone)
	if e != nil {
		return "needs_review"
	}
	if now.In(loc).Format("2006-01-02") > o.DueDate {
		return "overdue"
	}
	return "planned"
}
func Expected(o domain.PaymentOccurrence, previous *int64) (*int64, string) {
	if o.ExactAmount != nil {
		return o.ExactAmount, "exact"
	}
	if o.EstimateOverride != nil {
		return o.EstimateOverride, "manual"
	}
	if previous != nil {
		return previous, "previous_payment"
	}
	if o.InitialEstimate != nil {
		return o.InitialEstimate, "initial"
	}
	return nil, "unknown"
}
