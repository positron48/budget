package domain

import "time"

type PaymentTemplate struct {
	ID, TenantID, Name, CategoryID, CurrencyCode, AccountID, Note, Timezone string
	AmountMode                                                              string
	FixedAmount, InitialEstimate                                            *int64
	FirstMonth, LastMonth, EffectiveMonth                                   string
	IntervalMonths, Day, PeriodOffset                                       int
	LastDay, Archived                                                       bool
	Version                                                                 int64
	UserID                                                                  string
	CreatedAt, UpdatedAt                                                    time.Time
}
type PaymentOccurrence struct {
	ID, TenantID, TemplateID, ScheduledMonth, PeriodMonth, DueDate        string
	Name, CategoryID, CurrencyCode, AccountID, Note, Timezone, AmountMode string
	ExactAmount, EstimateOverride, InitialEstimate, ExpectedAmount        *int64
	EstimateSource, Status, SkipReason                                    string
	Skipped, Manual                                                       bool
	Version                                                               int64
	UserID                                                                string
	UpdatedAt                                                             time.Time
	Transaction                                                           *Transaction
	ConfirmedAmount                                                       *int64
	ConfirmedCurrency, ConfirmedType, ConfirmedBy                         string
	ConfirmedAt                                                           *time.Time
	DisplayAmount                                                         *Money
	FXDate                                                                string
	Incomplete                                                            bool
}
type PaymentSummary struct {
	Exact, Estimated, Paid                              Money
	Unknown, NeedsReview, MissingFX, Total, Outstanding int
}
