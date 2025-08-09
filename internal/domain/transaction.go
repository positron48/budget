package domain

import "time"

type TransactionType string

const (
	TransactionTypeIncome  TransactionType = "income"
	TransactionTypeExpense TransactionType = "expense"
)

type FxInfo struct {
	FromCurrency string
	ToCurrency   string
	RateDecimal  string // e.g., "92.3456"
	AsOf         time.Time
	Provider     string
}

type Transaction struct {
	ID         string
	TenantID   string
	UserID     string
	CategoryID string
	Type       TransactionType
	Amount     Money
	BaseAmount Money
	Fx         *FxInfo
	OccurredAt time.Time
	Comment    string
	CreatedAt  time.Time
}
