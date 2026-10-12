package investment

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	d "github.com/positron48/budget/internal/domain"
	"math/big"
	"slices"
	"sort"
	"strings"
	"time"
)

const MaxJournalQueryOperations = 100000

var JournalKinds = []string{"cash_reconciliation", "deposit", "withdrawal", "buy", "sell", "dividend", "coupon", "fee", "tax", "tax_refund", "split", "amortization", "redemption", "fx", "transfer_out", "transfer_in", "opening_cash", "opening_position", "cash_interest", "repo_income", "distribution", "security_transfer_out", "security_transfer_in"}

type OperationsQuery struct {
	LedgerIDs                                                               []string
	FromDate, ToDate, InstrumentKey, Kind, Status, Origin, ImportID, Cursor string
	Limit                                                                   int
}
type OperationCash struct {
	Currency string
	Amount   *big.Rat
}
type OperationRow struct {
	Operation             d.InvestmentOperation
	LedgerName            string
	Origins               []string
	UnitCleanPrice        *big.Rat
	Cash                  []OperationCash
	BrokerEvidenceVersion int64
}
type OperationsPage struct {
	Entries    []OperationRow
	Ledgers    []d.InvestmentLedger
	TotalCount int64
	NextCursor string
}
type OperationRevision struct {
	Version                 int64
	Action, ActorID, Reason string
	RecordedAt              time.Time
	Before                  *d.InvestmentOperation
	After                   d.InvestmentOperation
}
type OperationHistoryQuery struct {
	LedgerID, OperationID         string
	AfterVersion, ExpectedVersion int64
	Limit                         int
}
type OperationHistory struct {
	Ledger           d.InvestmentLedger
	Current          d.InvestmentOperation
	Revisions        []OperationRevision
	NextAfterVersion int64
}
type operationsQueryRepo interface {
	QueryOperations(context.Context, string, string, OperationsQuery) (OperationsPage, error)
	OperationHistory(context.Context, string, string, OperationHistoryQuery) (OperationHistory, error)
}

func NormalizeOperationsQuery(q OperationsQuery) (OperationsQuery, error) {
	if q.Limit == 0 {
		q.Limit = 50
	}
	if q.Limit < 1 || q.Limit > 200 || len(q.LedgerIDs) > 100 || len(q.Cursor) > 4096 || len(q.InstrumentKey) > 200 || q.Kind != "" && !slices.Contains(JournalKinds, q.Kind) || q.Status != "" && q.Status != "posted" && q.Status != "voided" || q.Origin != "" && !slices.Contains([]string{"manual", "import", "broker", "budget", "transfer"}, q.Origin) || q.ImportID != "" && !validID(q.ImportID) {
		return q, d.ErrInvestmentInvalid
	}
	for _, date := range []string{q.FromDate, q.ToDate} {
		at, e := time.Parse(time.DateOnly, date)
		if e != nil || at.Year() < 1900 || at.Year() > 2200 {
			return q, d.ErrInvestmentInvalid
		}
	}
	if q.FromDate > q.ToDate {
		return q, d.ErrInvestmentInvalid
	}
	for _, id := range q.LedgerIDs {
		if !validID(id) {
			return q, d.ErrInvestmentInvalid
		}
	}
	q.LedgerIDs = append([]string{}, q.LedgerIDs...)
	sort.Strings(q.LedgerIDs)
	q.LedgerIDs = uniquePayoutStrings(q.LedgerIDs)
	q.InstrumentKey = strings.TrimSpace(q.InstrumentKey)
	return q, nil
}
func (s *Service) QueryOperations(ctx context.Context, t, u string, q OperationsQuery) (OperationsPage, error) {
	if e := s.authorize(ctx, t, u, false); e != nil {
		return OperationsPage{}, e
	}
	q, e := NormalizeOperationsQuery(q)
	if e != nil {
		return OperationsPage{}, e
	}
	repo, ok := s.repo.(operationsQueryRepo)
	if !ok {
		return OperationsPage{}, d.ErrInvestmentPrecondition
	}
	return repo.QueryOperations(ctx, t, u, q)
}
func (s *Service) OperationHistory(ctx context.Context, t, u string, q OperationHistoryQuery) (OperationHistory, error) {
	if e := s.authorize(ctx, t, u, false); e != nil {
		return OperationHistory{}, e
	}
	q, e := NormalizeOperationHistoryQuery(q)
	if e != nil {
		return OperationHistory{}, e
	}
	repo, ok := s.repo.(operationsQueryRepo)
	if !ok {
		return OperationHistory{}, d.ErrInvestmentPrecondition
	}
	return repo.OperationHistory(ctx, t, u, q)
}
func NormalizeOperationHistoryQuery(q OperationHistoryQuery) (OperationHistoryQuery, error) {
	if q.Limit == 0 {
		q.Limit = 50
	}
	if !validID(q.LedgerID) || !validID(q.OperationID) || q.AfterVersion < 0 || q.ExpectedVersion < 0 || q.Limit < 1 || q.Limit > 200 || q.AfterVersion > 0 && q.ExpectedVersion == 0 {
		return q, d.ErrInvestmentInvalid
	}
	return q, nil
}
func operationRow(o d.InvestmentOperation, name string, brokerVersion int64) (OperationRow, error) {
	if o.Status != "posted" && o.Status != "voided" {
		return OperationRow{}, d.ErrInvestmentPrecondition
	}
	if e := ValidateOperation(&o); e != nil {
		return OperationRow{}, d.ErrInvestmentPrecondition
	}
	row := OperationRow{Operation: o, LedgerName: name, BrokerEvidenceVersion: brokerVersion, Origins: []string{}, Cash: []OperationCash{}}
	if o.Source != nil {
		row.Origins = append(row.Origins, "import")
	}
	if brokerVersion > 0 {
		row.Origins = append(row.Origins, "broker")
	}
	if o.BudgetTransferID != "" {
		row.Origins = append(row.Origins, "budget")
	}
	if o.TransferID != "" {
		row.Origins = append(row.Origins, "transfer")
	}
	if len(row.Origins) == 0 {
		row.Origins = append(row.Origins, "manual")
	}
	add := func(currency string, v *big.Rat) { row.Cash = append(row.Cash, OperationCash{currency, v}) }
	amount := rational(o.Amount)
	switch o.Kind {
	case "cash_reconciliation":
		add(o.Currency, new(big.Rat).Sub(rational(o.CashReconciliation.ObservedSettled), rational(o.CashReconciliation.ExpectedSettled)))
	case "buy", "sell":
		if o.TradeFinancials != nil {
			row.UnitCleanPrice = tradeConverted(d.InvestmentTradeMoney{Amount: o.TradeFinancials.Price, Currency: o.TradeFinancials.PriceCurrency, FXNumerator: o.TradeFinancials.PriceFXNumerator, FXDenominator: o.TradeFinancials.PriceFXDenominator}, o.Currency)
			if o.TradeFinancials.PriceUnit == "percent_nominal" {
				row.UnitCleanPrice.Mul(row.UnitCleanPrice, new(big.Rat).Quo(rational(o.TradeFinancials.Nominal), big.NewRat(100, 1)))
			}
			totals := map[string]*big.Rat{}
			for _, p := range tradeCashParts(o) {
				if totals[p.currency] == nil {
					totals[p.currency] = new(big.Rat)
				}
				totals[p.currency].Add(totals[p.currency], rational(p.amount))
			}
			keys := []string{}
			for key := range totals {
				keys = append(keys, key)
			}
			slices.Sort(keys)
			for _, key := range keys {
				add(key, totals[key])
			}
			break
		}
		row.UnitCleanPrice = new(big.Rat).Quo(amount, rational(o.Quantity))
		cash := new(big.Rat).Add(amount, rational(o.Accrued))
		if o.Kind == "buy" {
			cash.Add(cash, rational(o.Fee))
			cash.Neg(cash)
		} else {
			cash.Sub(cash, rational(o.Fee))
		}
		add(o.Currency, cash)
	case "fx":
		add(o.Currency, new(big.Rat).Neg(new(big.Rat).Add(amount, rational(o.Fee))))
		add(o.ToCurrency, rational(o.ToAmount))
	case "fee", "tax", "withdrawal", "transfer_out":
		add(o.Currency, new(big.Rat).Neg(amount))
	case "dividend", "coupon", "repo_income":
		add(o.Currency, new(big.Rat).Sub(amount, rational(o.Tax)))
	case "deposit", "opening_cash", "cash_interest", "tax_refund", "transfer_in", "amortization", "redemption":
		add(o.Currency, amount)
	}
	// A voided row retains its former terms for audit; it has no current cash effect.
	if o.Status == "voided" {
		row.Cash = nil
	}
	return row, nil
}
func BuildOperationsPage(tenant string, ledgers []d.InvestmentLedger, ops []d.InvestmentOperation, brokerLinked map[string]int64, q OperationsQuery) (OperationsPage, error) {
	var out OperationsPage
	q, e := NormalizeOperationsQuery(q)
	if e != nil {
		return out, e
	}
	if len(ledgers) > 100 || len(ops) > MaxJournalQueryOperations {
		return out, d.ErrInvestmentLimit
	}
	loc, e := time.LoadLocation("Europe/Moscow")
	if e != nil {
		return out, e
	}
	from, _ := time.ParseInLocation(time.DateOnly, q.FromDate, loc)
	to, _ := time.ParseInLocation(time.DateOnly, q.ToDate, loc)
	to = to.AddDate(0, 0, 1)
	selected := map[string]d.InvestmentLedger{}
	for _, l := range ledgers {
		if l.TenantID != tenant {
			return out, d.ErrInvestmentForbidden
		}
		if len(q.LedgerIDs) == 0 || slices.Contains(q.LedgerIDs, l.ID) {
			selected[l.ID] = l
		}
	}
	if len(q.LedgerIDs) > 0 && len(selected) != len(q.LedgerIDs) {
		return out, d.ErrInvestmentNotFound
	}
	entries := []OperationRow{}
	for _, o := range ops {
		l, ok := selected[o.LedgerID]
		if !ok {
			continue
		}
		if o.TenantID != tenant {
			return out, d.ErrInvestmentForbidden
		}
		if o.OccurredAt.Before(from) || !o.OccurredAt.Before(to) || q.InstrumentKey != "" && o.InstrumentKey != q.InstrumentKey || q.Kind != "" && o.Kind != q.Kind || q.Status != "" && o.Status != q.Status || q.ImportID != "" && (o.Source == nil || o.Source.ImportID != q.ImportID) {
			continue
		}
		row, e := operationRow(o, l.Name, brokerLinked[o.ID])
		if e != nil {
			return out, e
		}
		if q.Origin != "" && !slices.Contains(row.Origins, q.Origin) {
			continue
		}
		entries = append(entries, row)
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i].Operation, entries[j].Operation
		if !a.OccurredAt.Equal(b.OccurredAt) {
			return a.OccurredAt.After(b.OccurredAt)
		}
		if a.Sequence != b.Sequence {
			return a.Sequence > b.Sequence
		}
		return a.ID < b.ID
	})
	// Broker linking can change a source filter without a financial ledger edit.
	// Include source classifications and matched operation versions in the fence.
	type head struct {
		ID            string
		Version       int64
		Origins       []string
		BrokerVersion int64
	}
	heads := []head{}
	for _, r := range entries {
		heads = append(heads, head{r.Operation.ID, r.Operation.Version, r.Origins, r.BrokerEvidenceVersion})
	}
	scopeQuery := q
	scopeQuery.Cursor = ""
	scopeQuery.Limit = 0
	sortedLedgers := make([]d.InvestmentLedger, 0, len(selected))
	for _, ledger := range selected {
		sortedLedgers = append(sortedLedgers, ledger)
	}
	sort.Slice(sortedLedgers, func(i, j int) bool { return sortedLedgers[i].ID < sortedLedgers[j].ID })
	raw, _ := json.Marshal(struct {
		Tenant  string
		Query   OperationsQuery
		Ledgers []d.InvestmentLedger
		Heads   []head
	}{tenant, scopeQuery, sortedLedgers, heads})
	sum := sha256.Sum256(raw)
	scope := hex.EncodeToString(sum[:])
	cursor := struct {
		Scope  string
		Offset int
	}{}
	if q.Cursor != "" {
		raw, e := base64.RawURLEncoding.DecodeString(q.Cursor)
		if e != nil || json.Unmarshal(raw, &cursor) != nil || cursor.Offset < 0 || cursor.Offset > MaxJournalQueryOperations {
			return out, d.ErrInvestmentInvalid
		}
		if cursor.Scope != scope || cursor.Offset > len(entries) {
			return out, d.ErrInvestmentConflict
		}
	}
	end := cursor.Offset + q.Limit
	if end > len(entries) {
		end = len(entries)
	}
	out.Entries = entries[cursor.Offset:end]
	out.TotalCount = int64(len(entries))
	out.Ledgers = ledgers
	if end < len(entries) {
		cursor.Scope = scope
		cursor.Offset = end
		raw, _ := json.Marshal(cursor)
		out.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return out, nil
}
