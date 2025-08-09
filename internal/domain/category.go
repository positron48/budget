package domain

import "time"

type CategoryKind string

const (
	CategoryKindIncome  CategoryKind = "income"
	CategoryKindExpense CategoryKind = "expense"
)

type CategoryTranslation struct {
	Locale      string
	Name        string
	Description string
}

type Category struct {
	ID           string
	TenantID     string
	Kind         CategoryKind
	Code         string
	ParentID     *string
	IsActive     bool
	CreatedAt    time.Time
	Translations []CategoryTranslation
}
