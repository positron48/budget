package domain

import "time"

type CurrencyExchange struct {
	ID          string
	TenantID    string
	UserID      string
	FromAmount  Money
	ToAmount    Money
	RateDecimal string
	OccurredAt  time.Time
	Note        string
	CreatedAt   time.Time
}
