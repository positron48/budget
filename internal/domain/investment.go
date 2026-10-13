package domain

import (
	"errors"
	"time"
)

var (
	ErrInvestmentInvalid      = errors.New("invalid investment operation")
	ErrInvestmentConflict     = errors.New("investment journal changed")
	ErrInvestmentNotFound     = errors.New("investment ledger not found")
	ErrInvestmentForbidden    = errors.New("investment access denied")
	ErrInvestmentPrecondition = errors.New("investment history is inconsistent")
	ErrInvestmentLimit        = errors.New("investment journal limit exceeded")
)

// Ledger is a working book. Linking it to AssetService requires a separate
// reconciled activation; it must not replace a manual valuation implicitly.
type InvestmentLedger struct {
	// PortfolioTracking tracks securities and payouts without a broker cash book.
	// False retains the interpretation of legacy accounting archives.
	PortfolioTracking                             bool `json:",omitempty"`
	ID, TenantID, Name                            string
	Broker, AccountType, Comment, DefaultCurrency string
	Archived                                      bool
	MetadataVersion                               int64
	StartedAt                                     time.Time
	Version                                       int64
	CreatedAt                                     time.Time
}

// InvestmentOperation stores confirmed facts. Amount is clean principal for
// trades, gross for income, and positive cash for cash operations. Accrued is
// separate from clean cost. Price is not inferred from these actual amounts.
// Fees and tax currently must be in Currency, no implicit FX conversion.
// InvestmentDistribution records explicit, reviewed corporate-action terms.
// EventKey identifies the event within a ledger independently of operation IDs.
type InvestmentDistribution struct {
	// Optional reviewed total fair value of the securities received, in the
	// operation Currency at OccurredAt. This is analytics evidence, not FIFO
	// cost, cash, taxable income, or a per-unit quote. Empty means unknown.
	MarketValue, MarketValueEvidence                   string `json:",omitempty"`
	EventKey, TargetInstrumentKey                      string
	NewUnits, OldUnits, CostNumerator, CostDenominator string
	ConsumeSource                                      bool
}

// InvestmentCashReconciliation preserves reviewed settled cash before/after a
// timestamp. It is not an inferred investor payment or investment return.
type InvestmentCashReconciliation struct{ ExpectedSettled, ObservedSettled string }

// OperationTiming keeps explicit civil-date precision. Empty dates mean timestamp
// precision; nil means legacy timing provenance was not recorded.
type InvestmentOperationTiming struct{ Timezone, OccurredDate, SettlementDate, SourceDate string }

type InvestmentLotConversion struct {
	Lots, LotSize, TermsID, Provider, AccessScope, InstrumentID, Evidence string
	MappingVersion                                                        int64
	From, To                                                              time.Time
}

// TradeMoney preserves native cash and the explicitly reviewed conversion to
// the operation price/book currency. Numerator/denominator avoid rounded FX.
type InvestmentTradeMoney struct {
	Amount, Currency                                 string
	At                                               time.Time
	FXNumerator, FXDenominator, FXSource, FXEvidence string
	FXAt                                             time.Time
}
type InvestmentTradeFinancials struct {
	ActualSettlementIncludesExpenses                                                                         bool
	PriceCurrency, PriceFXNumerator, PriceFXDenominator, PriceFXSource, PriceFXEvidence                      string
	PriceFXAt                                                                                                time.Time
	CalculatedSettlement, SettlementDifference, PrincipalDifferenceNumerator, PrincipalDifferenceDenominator string
	Price, PriceUnit, Nominal, ActualSettlement, DifferenceReason                                            string
	Principal, Accrued, Fee, Tax                                                                             InvestmentTradeMoney
}

type InvestmentOperation struct {
	// Reviewed import exception, scoped to this operation and its native cash legs.
	ImportedCashDeficitReason string `json:",omitempty"`

	TradeFinancials *InvestmentTradeFinancials `json:",omitempty"`

	LotConversion      *InvestmentLotConversion      `json:",omitempty"`
	Timing             *InvestmentOperationTiming    `json:",omitempty"`
	CashReconciliation *InvestmentCashReconciliation `json:",omitempty"`

	BudgetTransferID string `json:",omitempty"` // immutable Budget cash document provenance

	Distribution                                            *InvestmentDistribution `json:",omitempty"`
	TaxReviewRequired                                       bool                    `json:",omitempty"`
	OpeningCostKnown, OpeningAccruedKnown                   bool
	AcquiredAt                                              time.Time
	TransferID, CounterpartyLedgerID                        string
	ToCurrency, ToAmount                                    string // actual FX settlement currency and amount
	CouponAllocationConfirmed                               bool
	AccruedRecoveries                                       []InvestmentAccruedRecovery
	NominalBefore, NominalAfter                             string
	ID, TenantID, LedgerID, Kind, InstrumentKey, Currency   string
	Quantity, Amount, Accrued, Fee, Tax, SplitNew, SplitOld string
	OccurredAt, SettlementAt                                time.Time
	SettlementAssumed                                       bool
	Sequence, Version                                       int64
	Status, Note, ActorID                                   string
	CreatedAt                                               time.Time
	Source                                                  *InvestmentSource
}

// Immutable provenance. Financial corrections retain the original review.
type InvestmentSource struct {
	ImportID, ReviewReason string
	Record                 int
	ExpectedVersion        int64
}
type InvestmentCashLeg struct {
	OperationID, Currency, Bucket, Component, Amount string
	EffectiveAt                                      time.Time
}
type InvestmentSecurityLeg struct {
	OperationID, InstrumentKey, Quantity string
	EffectiveAt                          time.Time
}
type InvestmentCash struct{ Currency, Settled, Receivable, Payable, Economic string }
type InvestmentAccruedRecovery struct {
	OriginID, Amount string
	AllRemaining     bool
}
type InvestmentLot struct {
	AcquisitionOperationID                         string `json:",omitempty"`
	OriginLedgerID                                 string `json:",omitempty"`
	OriginSequence                                 int64  `json:",omitempty"`
	OriginID, Quantity, CleanCost, AcquiredAccrued string
	AcquiredAt                                     time.Time
}
type InvestmentPosition struct {
	Lots                                                  []InvestmentLot
	CouponGross, CouponTax, CouponRecovered, CouponEarned string

	OutstandingNominal string // unknown until a confirmed nominal event; not a market quote

	InstrumentKey, Currency, Quantity, CleanCost, AcquiredAccrued, RealizedClean, RealizedAccrued string
}
type InvestmentSnapshot struct {
	Ledger    InvestmentLedger
	AsOf      time.Time
	Cash      []InvestmentCash
	Positions []InvestmentPosition
	// Quantities and cash are known. NAV is deliberately not returned without
	// prices, instrument terms and FX coverage.
	ValuationStatus string
	Issues          []string
}

// InvestmentTransfer links both cash or security movements; neither side is independently editable.
type InvestmentTransfer struct {
	Timing                                                                 *InvestmentOperationTiming `json:",omitempty"`
	InstrumentKey, Quantity                                                string                     `json:",omitempty"`
	ID, TenantID, FromLedgerID, ToLedgerID, FromOperationID, ToOperationID string
	Currency, Amount, Note, Status                                         string
	Version                                                                int64
	OccurredAt                                                             time.Time
}
