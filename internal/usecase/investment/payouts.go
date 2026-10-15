package investment

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"sort"
	"strings"
	"time"

	d "github.com/positron48/budget/internal/domain"
)

// Payouts are recorded cash facts, not forecasts or earned coupon profit.
const MaxPayoutOperations = 100000

type PayoutQuery struct {
	LedgerIDs                       []string
	FromDate, ToDate, InstrumentKey string
	Kinds                           []string
	Cursor                          string
	Limit                           int
}
type PayoutEntry struct {
	ID, OperationID, LedgerID, LedgerName, InstrumentKey, Kind, Currency, Note string
	At                                                                         time.Time
	Sequence, OperationVersion                                                 int64
	Gross, Tax, Net                                                            *big.Rat
	TaxReviewRequired                                                          bool
	SettlementAssumed                                                          bool
}
type PayoutTotals struct {
	Month, Currency                                                                 string
	Gross, Tax, IncomeNet, Fees, StandaloneTax, TaxRefund, PrincipalReturn, NetCash *big.Rat
}
type PayoutReport struct {
	Growth         []PayoutGrowth
	Entries        []PayoutEntry
	Totals, Months []PayoutTotals
	Ledgers        []d.InvestmentLedger
	TotalCount     int64
	NextCursor     string
	AsOf           time.Time
	Warnings       []string
}
type payoutRepo interface {
	Payouts(context.Context, string, string, PayoutQuery) (PayoutReport, error)
}

var payoutKinds = map[string]bool{"dividend": true, "coupon": true, "repo_income": true, "cash_interest": true, "fee": true, "tax": true, "tax_refund": true, "amortization": true, "redemption": true}

func normalizePayoutQuery(q PayoutQuery) (PayoutQuery, error) {
	if q.Limit == 0 {
		q.Limit = 50
	}
	if q.Limit < 1 || q.Limit > 200 || len(q.LedgerIDs) > 100 || len(q.Kinds) > len(payoutKinds) || len(q.InstrumentKey) > 200 || len(q.Cursor) > 4096 {
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
	for _, kind := range q.Kinds {
		if !payoutKinds[kind] {
			return q, d.ErrInvestmentInvalid
		}
	}
	q.LedgerIDs = append([]string{}, q.LedgerIDs...)
	q.Kinds = append([]string{}, q.Kinds...)
	sort.Strings(q.LedgerIDs)
	q.LedgerIDs = uniquePayoutStrings(q.LedgerIDs)
	sort.Strings(q.Kinds)
	q.Kinds = uniquePayoutStrings(q.Kinds)
	q.InstrumentKey = strings.TrimSpace(q.InstrumentKey)
	return q, nil
}

func uniquePayoutStrings(v []string) []string {
	out := []string{}
	for _, s := range v {
		if len(out) == 0 || out[len(out)-1] != s {
			out = append(out, s)
		}
	}
	return out
}

func (s *Service) Payouts(ctx context.Context, t, u string, q PayoutQuery) (PayoutReport, error) {
	if e := s.authorize(ctx, t, u, false); e != nil {
		return PayoutReport{}, e
	}
	q, e := normalizePayoutQuery(q)
	if e != nil {
		return PayoutReport{}, e
	}
	repo, ok := s.repo.(payoutRepo)
	if !ok {
		return PayoutReport{}, d.ErrInvestmentPrecondition
	}
	return repo.Payouts(ctx, t, u, q)
}

type payoutCursor struct {
	Scope  string
	Offset int
	AsOf   time.Time
}

func newPayoutTotals(currency, month string) PayoutTotals {
	return PayoutTotals{Currency: currency, Month: month, Gross: new(big.Rat), Tax: new(big.Rat), IncomeNet: new(big.Rat), Fees: new(big.Rat), StandaloneTax: new(big.Rat), TaxRefund: new(big.Rat), PrincipalReturn: new(big.Rat), NetCash: new(big.Rat)}
}

func addPayoutTotal(t *PayoutTotals, e PayoutEntry) {
	t.NetCash.Add(t.NetCash, e.Net)
	switch e.Kind {
	case "dividend", "coupon", "repo_income", "cash_interest":
		t.IncomeNet.Add(t.IncomeNet, e.Net)
		if e.Gross == nil {
			t.Gross = nil
			t.Tax = nil
		} else {
			if t.Gross != nil {
				t.Gross.Add(t.Gross, e.Gross)
			}
			if t.Tax != nil {
				t.Tax.Add(t.Tax, e.Tax)
			}
		}
	case "fee":
		t.Fees.Sub(t.Fees, e.Net)
	case "tax":
		t.StandaloneTax.Sub(t.StandaloneTax, e.Net)
	case "tax_refund":
		t.TaxRefund.Add(t.TaxRefund, e.Net)
	case "amortization", "redemption":
		t.PrincipalReturn.Add(t.PrincipalReturn, e.Net)
	}
}

// BuildPayoutReport is deterministic over a tenant snapshot. Cursor scope includes
// financial versions and a frozen cash cutoff; an amendment invalidates paging.
func BuildPayoutReport(tenant string, ledgers []d.InvestmentLedger, ops []d.InvestmentOperation, q PayoutQuery, now time.Time) (PayoutReport, error) {
	var out PayoutReport
	q, e := normalizePayoutQuery(q)
	if e != nil {
		return out, e
	}
	if len(ledgers) > 100 || len(ops) > MaxPayoutOperations {
		return out, d.ErrInvestmentLimit
	}
	loc, e := time.LoadLocation("Europe/Moscow")
	if e != nil {
		return out, e
	}
	from, _ := time.ParseInLocation(time.DateOnly, q.FromDate, loc)
	to, _ := time.ParseInLocation(time.DateOnly, q.ToDate, loc)
	to = to.AddDate(0, 0, 1).Add(-time.Nanosecond)
	cutoff := now.UTC()
	if to.Before(cutoff) {
		cutoff = to
	}
	c := payoutCursor{AsOf: cutoff}
	if q.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(q.Cursor)
		if err != nil || json.Unmarshal(raw, &c) != nil || c.Offset < 0 || c.Offset > MaxPayoutOperations || c.AsOf.IsZero() || c.AsOf.After(cutoff) {
			return out, d.ErrInvestmentInvalid
		}
		cutoff = c.AsOf
	}
	selected := map[string]d.InvestmentLedger{}
	requested := map[string]bool{}
	for _, id := range q.LedgerIDs {
		requested[id] = true
	}
	versions := []struct {
		ID      string
		Version int64
	}{}
	for _, l := range ledgers {
		if l.TenantID != tenant {
			return out, d.ErrInvestmentForbidden
		}
		if len(requested) == 0 || requested[l.ID] {
			selected[l.ID] = l
			versions = append(versions, struct {
				ID      string
				Version int64
			}{l.ID, l.Version})
		}
	}
	if len(requested) > 0 && len(selected) != len(requested) {
		return out, d.ErrInvestmentNotFound
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].ID < versions[j].ID })
	scopeQuery := q
	scopeQuery.Cursor = ""
	scopeQuery.Limit = 0
	raw, _ := json.Marshal(struct {
		Tenant   string
		Query    PayoutQuery
		Versions any
		Cutoff   time.Time
	}{tenant, scopeQuery, versions, cutoff})
	sum := sha256.Sum256(raw)
	scope := hex.EncodeToString(sum[:])
	if q.Cursor != "" && c.Scope != scope {
		return out, d.ErrInvestmentConflict
	}
	out.Ledgers = ledgers
	out.AsOf = cutoff
	out.Entries = []PayoutEntry{}
	out.Warnings = []string{"recorded_journal_only", "coupon_cash_is_not_earned_income"}
	for _, l := range selected {
		if !l.PortfolioTracking && from.Before(l.StartedAt) {
			out.Warnings = append(out.Warnings, "ledger_history_missing")
			break
		}
	}
	totals := map[string]*PayoutTotals{}
	months := map[string]*PayoutTotals{}
	entries := []PayoutEntry{}
	kinds := map[string]bool{}
	for _, kind := range q.Kinds {
		kinds[kind] = true
	}
	growth := newPayoutGrowth(q, cutoff, loc, selected)
	add := func(e PayoutEntry) {
		if (len(kinds) == 0 || kinds[e.Kind]) && (q.InstrumentKey == "" || q.InstrumentKey == e.InstrumentKey) {
			growth.add(e)
		}
		if e.At.Before(from) || e.At.After(cutoff) || (len(kinds) > 0 && !kinds[e.Kind]) || (q.InstrumentKey != "" && q.InstrumentKey != e.InstrumentKey) {
			return
		}
		entries = append(entries, e)
		m := e.At.In(loc).Format("2006-01")

		if totals[e.Currency] == nil {
			v := newPayoutTotals(e.Currency, "")
			totals[e.Currency] = &v
		}
		key := m + ":" + e.Currency
		if months[key] == nil {
			v := newPayoutTotals(e.Currency, m)
			months[key] = &v
		}
		addPayoutTotal(totals[e.Currency], e)
		addPayoutTotal(months[key], e)
	}
	for _, original := range ops {
		l, ok := selected[original.LedgerID]
		if !ok {
			continue
		}
		if original.TenantID != tenant {
			return out, d.ErrInvestmentForbidden
		}
		if original.Status != "posted" {
			continue
		}
		if !payoutKinds[original.Kind] && original.TradeFinancials == nil && (original.Fee == "0" || original.Fee == "") {
			continue
		}
		o := original
		if err := ValidateOperation(&o); err != nil {
			return out, err
		}
		entry := PayoutEntry{ID: o.ID, OperationID: o.ID, LedgerID: o.LedgerID, LedgerName: l.Name, InstrumentKey: o.InstrumentKey, Kind: o.Kind, Currency: o.Currency, Note: o.Note, At: o.OccurredAt, Sequence: o.Sequence, OperationVersion: o.Version, TaxReviewRequired: o.TaxReviewRequired}
		if payoutKinds[o.Kind] {
			entry.Gross = rational(o.Amount)
			entry.Tax = rational(o.Tax)
			entry.Net = new(big.Rat).Sub(entry.Gross, entry.Tax)
			switch o.Kind {
			case "fee", "tax":
				entry.Gross, entry.Tax = nil, nil
				entry.Net.Neg(entry.Net)
			case "amortization", "redemption":
				entry.Gross, entry.Tax = nil, nil
				entry.At = o.SettlementAt
				entry.SettlementAssumed = o.SettlementAssumed
			case "tax_refund":
				entry.Gross, entry.Tax = nil, nil
			}
			if l.PortfolioTracking && (o.Kind == "dividend" || o.Kind == "coupon" || o.Kind == "repo_income" || o.Kind == "cash_interest") {
				// Tracker amounts are already recorded receipts, as in the overview.
				entry.Net = rational(o.Amount)
				entry.Tax, entry.Gross, entry.TaxReviewRequired = nil, nil, false
			}
			if o.TaxReviewRequired && !l.PortfolioTracking {
				entry.Gross = nil
				entry.Tax = nil
			}
			add(entry)
		}
		if f := o.TradeFinancials; f != nil {
			for _, part := range []struct {
				money d.InvestmentTradeMoney
				kind  string
			}{{f.Fee, "fee"}, {f.Tax, "tax"}} {
				if part.money.Amount == "0" {
					continue
				}
				native := entry
				native.ID = o.ID + ":" + part.kind
				native.Kind = part.kind
				native.At = part.money.At
				native.Currency = part.money.Currency
				native.Gross = nil
				native.Tax = nil
				native.TaxReviewRequired = false
				native.Net = new(big.Rat).Neg(rational(part.money.Amount))
				native.SettlementAssumed = o.SettlementAssumed
				add(native)
			}
		}
		if o.TradeFinancials == nil && rational(o.Fee).Sign() != 0 {
			entry.SettlementAssumed = o.SettlementAssumed
			entry.ID = o.ID + ":fee"
			entry.Kind = "fee"
			entry.At = o.SettlementAt
			entry.SettlementAssumed = o.SettlementAssumed
			entry.Gross = nil
			entry.Tax = nil
			entry.TaxReviewRequired = false
			entry.Net = new(big.Rat).Neg(rational(o.Fee))
			add(entry)
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if !a.At.Equal(b.At) {
			return a.At.After(b.At)
		}
		if a.LedgerID != b.LedgerID {
			return a.LedgerID < b.LedgerID
		}
		if a.Sequence != b.Sequence {
			return a.Sequence > b.Sequence
		}
		return a.ID < b.ID
	})
	out.Growth = growth.build()
	out.TotalCount = int64(len(entries))
	if c.Offset > len(entries) {
		return out, d.ErrInvestmentConflict
	}
	end := c.Offset + q.Limit
	if end > len(entries) {
		end = len(entries)
	}
	out.Entries = entries[c.Offset:end]
	if end < len(entries) {
		raw, _ := json.Marshal(payoutCursor{Scope: scope, Offset: end, AsOf: cutoff})
		out.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	for _, v := range totals {
		out.Totals = append(out.Totals, *v)
	}
	for _, v := range months {
		out.Months = append(out.Months, *v)
	}
	sort.Slice(out.Totals, func(i, j int) bool { return out.Totals[i].Currency < out.Totals[j].Currency })
	sort.Slice(out.Months, func(i, j int) bool {
		a, b := out.Months[i], out.Months[j]
		if a.Month != b.Month {
			return a.Month < b.Month
		}
		return a.Currency < b.Currency
	})
	return out, nil
}
