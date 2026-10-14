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
	InvestmentMode, InvestmentLedgerID, InvestmentConventionKey    string
	LedgerStartedAt, LedgerValuationFrom, ValuationAsOf            time.Time
	ValuationStatus                                                string
	ValuationIssues                                                []string
	ValuationIncomplete                                            bool
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
	InvestmentCashTransfer                                             bool `json:",omitempty"`
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

// Activation is a one-way, explicitly reviewed change of the valuation source.
type InvestmentAssetActivation struct {
	AccountID, LedgerID, ConventionKey, Evidence, PreviewHash, RequestKey string
	ValuationFrom                                                         time.Time
	ExpectedAccountVersion, ExpectedLedgerVersion                         int64
}
type InvestmentAssetActivationPreview struct {
	EvidenceJSON                  []byte `json:"-"` // immutable internal comparison evidence; not an API field
	Account                       AssetAccount
	Ledger                        InvestmentLedger
	ValuationFrom                 time.Time
	ManualValue, LedgerKnownValue Money
	Complete, Matches             bool
	Issues                        []string
	InventoryCount                int64
	PreviewHash                   string
}
