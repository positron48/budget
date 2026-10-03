package domain

import (
	"errors"
	"time"
)

var (
	ErrAssetInvalid        = errors.New("invalid asset data")
	ErrAssetPrecondition   = errors.New("asset account is archived or has no opening balance")
	ErrAssetConflict       = errors.New("asset data changed; refresh and try again")
	ErrIdempotencyConflict = errors.New("request key was already used with different data")
)

type AssetAccount struct {
	ID, TenantID, Name, Kind, Institution, Note, FixedCurrencyCode string
	DepositRateDecimal, DepositOpenedOn, DepositMaturesOn          string
	Archived                                                       bool
	Version                                                        int64
	CreatedAt                                                      time.Time
	Balances                                                       []AssetBalance
}

type AssetBalance struct {
	Amount          Money
	ConfirmedAmount Money
	ConfirmedAt     time.Time
}

type AssetSnapshot struct {
	ID, TenantID, AccountID, UserID, Kind, Note string
	Amount                                      Money
	AsOf, CreatedAt                             time.Time
	Version                                     int64
}

type AssetTransfer struct {
	ID, TenantID, UserID, FromAccountID, ToAccountID, Note, RequestKey string
	FromAmount, ToAmount                                               Money
	OccurredAt, CreatedAt                                              time.Time
	Version                                                            int64
}

type AssetHistoryItem struct {
	ID, Kind, SourceID, Note, CounterpartyName, UserID string
	Amount                                             Money
	OccurredAt                                         time.Time
	Version                                            int64
	CalculatedAmount                                   *Money
	Snapshot                                           *AssetSnapshot
	Transfer                                           *AssetTransfer
}

type AssetFxRate struct {
	From, To, RateDecimal, Provider string
	AsOf                            time.Time
}

type AssetOverview struct {
	Accounts          []AssetAccount
	Total             Money
	Incomplete        bool
	MissingCurrencies []string
	Rates             []AssetFxRate
	AsOf              time.Time
}
