package investment

import (
	"math/big"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
)

// ForecastEvent is reviewed event evidence, never a journal operation. An
// explicit entitlement instant/rule is independent of display ex/record dates:
// no exchange settlement calendar or cutoff hour is guessed from a ticker.
type ForecastEvent struct {
	ID, InstrumentKey, Kind, Nature, Currency, PerShare string
	ExDate, RecordDate, PaymentDate, Timezone           string
	EntitlementRule                                     string // settled_by_cutoff, trade_before_cutoff, or unknown
	EntitlementAt                                       time.Time
	SourceReference, Evidence, Reason                   string
	Version                                             int64
	Cancelled                                           bool
	Origin                                              string `json:",omitempty"`
}

type ForecastLink struct {
	EventID, LedgerID, OperationID string
	EventVersion, OperationVersion int64
	Complete                       bool // explicit statement reconciliation, not inferred from one credit
	Version                        int64
	Cancelled                      bool
	Reason                         string
}

type ForecastQuery struct {
	LedgerIDs                              []string
	FromMonth, Currency, TaxRate, FXPolicy string
}

type ForecastSecurityLeg struct {
	LedgerID string
	d.InvestmentSecurityLeg
}

type ForecastRow struct {
	EventID, LedgerID, InstrumentKey, Kind, Nature, PaymentDate, Currency     string
	EventVersion                                                              int64
	Quantity, Gross, EstimatedTax, EstimatedNet, ConvertedGross, ConvertedNet *big.Rat
	Entitlement, Status                                                       string
	ActualNet                                                                 *big.Rat
	ActualOperationIDs                                                        []string
	FXLegs                                                                    []calc.FXLeg
	Issues                                                                    []string
}

type ForecastMonth struct {
	Month                               string
	KnownGross, KnownEstimatedNet       *big.Rat
	Gross, EstimatedNet                 *big.Rat
	Announced, Projected, Unknown, Paid int
}

type ForecastReport struct {
	Events                                                      []ForecastEvent
	Links                                                       []ForecastLink
	Ledgers                                                     []d.InvestmentLedger
	FromMonth, ToMonth, Currency, FXPolicy, TaxRate             string
	AsOf                                                        time.Time
	Rows                                                        []ForecastRow
	Months                                                      []ForecastMonth
	Gross, EstimatedNet, MonthlyAverageGross, MonthlyAverageNet *big.Rat
	Announced, Projected, Unknown, Paid                         int
	Warnings                                                    []string
}

func forecastDate(s string, loc *time.Location) (time.Time, error) {
	at, e := time.ParseInLocation(time.DateOnly, s, loc)
	if e != nil || at.Year() < 1900 || at.Year() > 2200 {
		return at, d.ErrInvestmentInvalid
	}
	return at, nil
}

func NormalizeForecastEvent(e ForecastEvent) (ForecastEvent, error) {
	if !validID(e.ID) || len(e.InstrumentKey) < 1 || len(e.InstrumentKey) > 200 || strings.TrimSpace(e.InstrumentKey) != e.InstrumentKey || !utf8.ValidString(e.InstrumentKey) ||
		(e.Kind != "dividend" && e.Kind != "coupon") || (e.Nature != "announced" && e.Nature != "forecast") ||
		(e.Currency != "RUB" && e.Currency != "USD" && e.Currency != "EUR") || e.Version < 0 || e.Origin != "" && e.Origin != "provider" && e.Origin != "historical_projection" {
		return e, d.ErrInvestmentInvalid
	}
	loc, err := time.LoadLocation(e.Timezone)
	if err != nil || e.Timezone == "" || e.Timezone == "Local" {
		return e, d.ErrInvestmentInvalid
	}
	pay, err := forecastDate(e.PaymentDate, loc)
	if err != nil {
		return e, err
	}
	for _, date := range []string{e.ExDate, e.RecordDate} {
		if date != "" {
			at, err := forecastDate(date, loc)
			if err != nil || at.After(pay) {
				return e, d.ErrInvestmentInvalid
			}
		}
	}
	switch e.EntitlementRule {
	case "unknown":
		if !e.EntitlementAt.IsZero() {
			return e, d.ErrInvestmentInvalid
		}
	case "settled_by_cutoff", "trade_before_cutoff":
		if e.EntitlementAt.IsZero() || e.EntitlementAt.Year() < 1900 || e.EntitlementAt.After(pay.AddDate(0, 0, 1)) || e.EntitlementAt.Nanosecond()%1000 != 0 {
			return e, d.ErrInvestmentInvalid
		}
	default:
		return e, d.ErrInvestmentInvalid
	}
	for _, v := range []string{e.SourceReference, e.Evidence, e.Reason} {
		if len(strings.TrimSpace(v)) == 0 || len(v) > 4000 || !utf8.ValidString(v) || strings.ContainsRune(v, 0) {
			return e, d.ErrInvestmentInvalid
		}
	}
	if e.PerShare != "" {
		amount, err := normalize(e.PerShare, 36, 12)
		if err != nil {
			return e, err
		}
		e.PerShare = amount
	}
	e.EntitlementAt = e.EntitlementAt.UTC()
	return e, nil
}

func NormalizeForecastQuery(q ForecastQuery) (ForecastQuery, error) {
	first, e := time.Parse("2006-01", q.FromMonth)
	if e != nil || first.Year() < 1900 || first.Year() > 2199 || len(q.LedgerIDs) > 100 || len(q.FXPolicy) > 200 ||
		(q.Currency != "RUB" && q.Currency != "USD" && q.Currency != "EUR") {
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
	if q.TaxRate != "" {
		v, err := normalize(q.TaxRate, 16, 10)
		if err != nil || rational(v).Cmp(big.NewRat(100, 1)) > 0 {
			return q, d.ErrInvestmentInvalid
		}
		q.TaxRate = v
	}
	return q, nil
}

// forecastQuantity reconstructs economic or settled rights independently from
// current positions. Split deltas from the trade-date projection are ignored:
// each earlier movement is transformed by subsequent split ratios instead.
// This also handles pending buys/sells across a split without counting their
// unsafely projected split delta as settled ownership.
func forecastQuantity(ledger d.InvestmentLedger, event ForecastEvent, at time.Time, historical bool, startsFromZero bool, ops map[string]d.InvestmentOperation, legs []ForecastSecurityLeg) (*big.Rat, []string) {
	if at.Before(ledger.StartedAt) && !startsFromZero {
		return nil, []string{"ledger_history_missing"}
	}
	quantity := new(big.Rat)
	var splits []d.InvestmentOperation
	for _, o := range ops {
		if o.LedgerID == ledger.ID && o.Status == "posted" && o.Kind == "split" && o.InstrumentKey == event.InstrumentKey && !o.OccurredAt.After(at) {
			if historical && event.EntitlementRule == "trade_before_cutoff" && o.OccurredAt.Equal(at) {
				continue
			}
			if historical && o.Timing != nil && o.Timing.OccurredDate != "" {
				zone, err := time.LoadLocation(o.Timing.Timezone)
				if err != nil {
					return nil, []string{"operation_time_unconfirmed"}
				}
				start, end, err := civilDateInterval(o.Timing.OccurredDate, zone)
				if err != nil || !at.Before(start) && at.Before(end) {
					return nil, []string{"operation_time_unconfirmed"}
				}
			}
			splits = append(splits, o)
		}
	}
	for _, leg := range legs {
		if leg.LedgerID != ledger.ID || leg.InstrumentKey != event.InstrumentKey {
			continue
		}
		o, ok := ops[leg.OperationID]
		if !ok || o.LedgerID != ledger.ID || o.Status != "posted" {
			return nil, []string{"invalid_security_projection"}
		}
		if o.Kind == "split" {
			continue
		}
		effective := leg.EffectiveAt
		if historical && event.EntitlementRule == "settled_by_cutoff" {
			switch o.Kind {
			case "buy", "sell", "redemption":
				// An assumed future settlement cannot prove even that it has not settled.
				if !o.OccurredAt.After(at) && o.SettlementAssumed {
					return nil, []string{"settlement_unconfirmed"}
				}
				effective = o.SettlementAt
				if o.Timing != nil && o.Timing.SettlementDate != "" {
					zone, err := time.LoadLocation(o.Timing.Timezone)
					if err != nil {
						return nil, []string{"settlement_time_unconfirmed"}
					}
					start, end, err := civilDateInterval(o.Timing.SettlementDate, zone)
					if err != nil || !at.Before(start) && at.Before(end) {
						return nil, []string{"settlement_time_unconfirmed"}
					}
				}
			}
		}
		if effective.After(at) || (historical && event.EntitlementRule == "trade_before_cutoff" && !effective.Before(at)) {
			continue
		}
		if historical && o.Timing != nil && o.Timing.OccurredDate != "" {
			loc, err := time.LoadLocation(o.Timing.Timezone)
			if err != nil {
				return nil, []string{"operation_time_unconfirmed"}
			}
			date, end, err := civilDateInterval(o.Timing.OccurredDate, loc)
			if err != nil {
				return nil, []string{"operation_time_unconfirmed"}
			}
			if at.After(date) && at.Before(end) && (event.EntitlementRule == "trade_before_cutoff" || o.Kind == "split") {
				return nil, []string{"operation_time_unconfirmed"}
			}
		}
		value, ok := new(big.Rat).SetString(leg.Quantity)
		if !ok {
			return nil, []string{"invalid_security_projection"}
		}
		for _, split := range splits {
			// Sequence resolves same-timestamp corporate actions exactly as journal replay.
			if split.OccurredAt.After(o.OccurredAt) || split.OccurredAt.Equal(o.OccurredAt) && split.Sequence > o.Sequence {
				value.Mul(value, new(big.Rat).Quo(rational(split.SplitNew), rational(split.SplitOld)))
			}
		}
		quantity.Add(quantity, value)
	}
	if quantity.Sign() < 0 {
		return nil, []string{"negative_entitlement"}
	}
	return quantity, nil
}

// BuildForecast is a pure scenario over one committed tenant snapshot. Calling
// it never posts income and never treats the passage of payment date as cash.
func BuildForecast(tenant string, ledgers []d.InvestmentLedger, operations []d.InvestmentOperation, legs []ForecastSecurityLeg, events []ForecastEvent, links []ForecastLink, q ForecastQuery, asOf time.Time, fixings []calc.FXFixing) (ForecastReport, error) {
	var out ForecastReport
	q, err := NormalizeForecastQuery(q)
	if err != nil {
		return out, err
	}
	if len(ledgers) > 100 || len(operations) > MaxPayoutOperations || len(legs) > MaxPayoutOperations*4 || len(events) > 10000 || len(links) > MaxPayoutOperations || asOf.IsZero() {
		return out, d.ErrInvestmentLimit
	}
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		return out, err
	}
	first, _ := time.ParseInLocation("2006-01", q.FromMonth, loc)
	end := first.AddDate(1, 0, 0)
	if first.After(time.Date(asOf.In(loc).Year()+1, asOf.In(loc).Month(), 1, 0, 0, 0, 0, loc)) {
		return out, d.ErrInvestmentInvalid
	}
	requested := map[string]bool{}
	for _, id := range q.LedgerIDs {
		requested[id] = true
	}
	selected := map[string]d.InvestmentLedger{}
	for _, l := range ledgers {
		if l.TenantID != tenant {
			return out, d.ErrInvestmentForbidden
		}
		if len(requested) == 0 || requested[l.ID] {
			selected[l.ID] = l
		}
	}
	if len(requested) > 0 && len(selected) != len(requested) {
		return out, d.ErrInvestmentNotFound
	}
	ops := map[string]d.InvestmentOperation{}
	for _, o := range operations {
		if o.TenantID != tenant {
			return out, d.ErrInvestmentForbidden
		}
		if _, ok := ops[o.ID]; ok {
			return out, d.ErrInvestmentPrecondition
		}
		if o.Status == "posted" {
			copy := o
			if err = ValidateOperation(&copy); err != nil {
				return out, err
			}
			o = copy
		}
		ops[o.ID] = o
	}
	ledgerOps := map[string][]d.InvestmentOperation{}
	for _, o := range ops {
		ledgerOps[o.LedgerID] = append(ledgerOps[o.LedgerID], o)
	}
	startsFromZero := map[string]bool{}
	for id, ledger := range selected {
		startsFromZero[id] = RecordedTrackerStartsFromZero(ledger, ledgerOps[id])
	}
	type historyKey struct{ ledger, instrument string }
	type history struct {
		ops  map[string]d.InvestmentOperation
		legs []ForecastSecurityLeg
	}
	histories := map[historyKey]*history{}
	getHistory := func(key historyKey) *history {
		if histories[key] == nil {
			histories[key] = &history{ops: map[string]d.InvestmentOperation{}}
		}
		return histories[key]
	}
	for _, o := range ops {
		if o.Kind == "split" {
			getHistory(historyKey{o.LedgerID, o.InstrumentKey}).ops[o.ID] = o
		}
	}
	for _, leg := range legs {
		if _, ok := selected[leg.LedgerID]; !ok {
			continue
		}
		o, ok := ops[leg.OperationID]
		if !ok || o.LedgerID != leg.LedgerID {
			return out, d.ErrInvestmentPrecondition
		}
		h := getHistory(historyKey{leg.LedgerID, leg.InstrumentKey})
		h.ops[o.ID] = o
		h.legs = append(h.legs, leg)
	}
	var fx *calc.FXResolver
	if q.FXPolicy != "" {
		fx, err = calc.NewFXResolver(fixings, q.FXPolicy, asOf)
		if err != nil {
			return out, d.ErrInvestmentPrecondition
		}
	}
	out = ForecastReport{FromMonth: q.FromMonth, ToMonth: end.AddDate(0, -1, 0).Format("2006-01"), Currency: q.Currency, FXPolicy: q.FXPolicy, TaxRate: q.TaxRate, AsOf: asOf.UTC(), Rows: []ForecastRow{}, Months: []ForecastMonth{}, Warnings: []string{"unchanged_current_portfolio_scenario", "current_fx_is_not_future_fx", "journal_completeness_unconfirmed", "registered_events_only", "event_catalog_completeness_unconfirmed"}}
	out.Events = append([]ForecastEvent{}, events...)
	out.Links = append([]ForecastLink{}, links...)
	out.Ledgers = append([]d.InvestmentLedger{}, ledgers...)
	months := map[string]*ForecastMonth{}
	for month := first; month.Before(end); month = month.AddDate(0, 1, 0) {
		v := ForecastMonth{Month: month.Format("2006-01"), KnownGross: new(big.Rat), KnownEstimatedNet: new(big.Rat)}
		months[v.Month] = &v
	}
	type linkKey struct{ event, ledger string }
	byEvent := map[linkKey][]ForecastLink{}
	linkedOps := map[string]bool{}
	for _, link := range links {
		if link.Cancelled {
			continue
		}
		if linkedOps[link.OperationID] || link.OperationVersion < 1 || link.EventVersion < 1 {
			return out, d.ErrInvestmentPrecondition
		}
		linkedOps[link.OperationID] = true
		key := linkKey{link.EventID, link.LedgerID}
		byEvent[key] = append(byEvent[key], link)
	}
	seen := map[string]bool{}
	for _, original := range events {
		event, e := NormalizeForecastEvent(original)
		if e != nil || seen[event.ID] || event.Version < 1 {
			return out, d.ErrInvestmentInvalid
		}
		seen[event.ID] = true
		if event.Cancelled {
			continue
		}
		payment, _ := forecastDate(event.PaymentDate, loc)
		if payment.Before(first) || !payment.Before(end) {
			continue
		}
		for _, ledger := range selected {
			row := ForecastRow{EventID: event.ID, EventVersion: event.Version, LedgerID: ledger.ID, InstrumentKey: event.InstrumentKey, Kind: event.Kind, Nature: event.Nature, PaymentDate: event.PaymentDate, Currency: event.Currency, Status: "upcoming", Entitlement: "current_composition_scenario", Issues: []string{}, ActualOperationIDs: []string{}}
			historical := !event.EntitlementAt.IsZero() && !event.EntitlementAt.After(asOf)
			cutoff := asOf
			if historical {
				cutoff = event.EntitlementAt
				row.Entitlement = "historical_right"
			}
			if event.EntitlementRule == "unknown" {
				today := asOf.In(loc).Format(time.DateOnly)
				if event.PaymentDate < today || event.RecordDate != "" && event.RecordDate <= today || event.ExDate != "" && event.ExDate <= today {
					row.Issues = append(row.Issues, "past_entitlement_unknown")
				}
			}
			h := getHistory(historyKey{ledger.ID, event.InstrumentKey})
			row.Quantity, row.Issues = forecastQuantityWithIssues(ledger, event, cutoff, historical, startsFromZero[ledger.ID], h.ops, h.legs, row.Issues)
			linked, complete, invalid := false, false, false
			for _, link := range byEvent[linkKey{event.ID, ledger.ID}] {
				if link.Cancelled {
					continue
				}
				if link.EventID != event.ID || link.LedgerID != ledger.ID {
					continue
				}
				linked = true
				o, ok := ops[link.OperationID]
				if !ok || o.LedgerID != ledger.ID || o.Status != "posted" || o.Kind != event.Kind || o.InstrumentKey != event.InstrumentKey || o.Currency != event.Currency || o.OccurredAt.After(asOf) || o.Version != link.OperationVersion || event.Version != link.EventVersion {
					invalid = true
					continue
				}
				if row.ActualNet == nil {
					row.ActualNet = new(big.Rat)
				}
				actual := rational(o.Amount)
				if !ledger.PortfolioTracking {
					actual.Sub(actual, rational(o.Tax))
				}
				row.ActualNet.Add(row.ActualNet, actual)
				row.ActualOperationIDs = append(row.ActualOperationIDs, o.ID)
				complete = complete || link.Complete
			}
			if invalid {
				row.Issues = append(row.Issues, "actual_link_requires_review")
			}
			if linked && !complete {
				row.Issues = append(row.Issues, "partial_actual_payment")
			}
			if row.Quantity != nil && row.Quantity.Sign() == 0 && !linked {
				continue
			}
			if linked && complete && !invalid {
				row.Status = "paid"
			} else {
				if row.Quantity != nil && event.PerShare != "" && len(row.Issues) == 0 {
					row.Gross = new(big.Rat).Mul(row.Quantity, rational(event.PerShare))
				} else {
					row.Issues = append(row.Issues, "forecast_amount_unknown")
				}
				if payment.Format(time.DateOnly) < asOf.In(loc).Format(time.DateOnly) {
					row.Status = "overdue"
				}
				if row.Gross != nil && q.TaxRate != "" {
					row.EstimatedTax = new(big.Rat).Mul(row.Gross, new(big.Rat).Quo(rational(q.TaxRate), big.NewRat(100, 1)))
					row.EstimatedNet = new(big.Rat).Sub(row.Gross, row.EstimatedTax)
				}
				var resolved calc.ResolvedFX
				var ok bool
				if event.Currency == q.Currency {
					resolved = calc.ResolvedFX{Rate: big.NewRat(1, 1)}
					ok = true
				} else if fx != nil {
					resolved, ok = fx.Resolve(event.Currency, q.Currency)
				}
				if ok {
					row.FXLegs = resolved.Legs
					if row.Gross != nil {
						row.ConvertedGross = new(big.Rat).Mul(row.Gross, resolved.Rate)
					}
					if row.EstimatedNet != nil {
						row.ConvertedNet = new(big.Rat).Mul(row.EstimatedNet, resolved.Rate)
					}
				} else {
					row.Issues = append(row.Issues, "current_fx_missing")
				}
			}
			m := months[payment.Format("2006-01")]
			if row.Status == "paid" {
				m.Paid++
			} else {
				if row.Nature == "announced" {
					m.Announced++
				} else {
					m.Projected++
				}
				if row.ConvertedGross == nil {
					m.Unknown++
				} else {
					m.KnownGross.Add(m.KnownGross, row.ConvertedGross)
				}
				if row.ConvertedNet != nil {
					m.KnownEstimatedNet.Add(m.KnownEstimatedNet, row.ConvertedNet)
				}
			}
			if len(out.Rows) >= MaxPayoutOperations {
				return out, d.ErrInvestmentLimit
			}
			out.Rows = append(out.Rows, row)
		}
	}
	out.Gross = new(big.Rat)
	if q.TaxRate != "" {
		out.EstimatedNet = new(big.Rat)
	}
	for month := first; month.Before(end); month = month.AddDate(0, 1, 0) {
		m := months[month.Format("2006-01")]
		if m.Unknown == 0 {
			m.Gross = new(big.Rat).Set(m.KnownGross)
			if q.TaxRate != "" {
				m.EstimatedNet = new(big.Rat).Set(m.KnownEstimatedNet)
			}
		}
		if out.Gross != nil {
			if m.Gross == nil {
				out.Gross = nil
			} else {
				out.Gross.Add(out.Gross, m.Gross)
			}
		}
		if out.EstimatedNet != nil {
			if m.EstimatedNet == nil {
				out.EstimatedNet = nil
			} else {
				out.EstimatedNet.Add(out.EstimatedNet, m.EstimatedNet)
			}
		}
		out.Announced += m.Announced
		out.Projected += m.Projected
		out.Unknown += m.Unknown
		out.Paid += m.Paid
		out.Months = append(out.Months, *m)
	}
	if out.Gross != nil {
		out.MonthlyAverageGross = new(big.Rat).Quo(out.Gross, big.NewRat(12, 1))
	}
	if out.EstimatedNet != nil {
		out.MonthlyAverageNet = new(big.Rat).Quo(out.EstimatedNet, big.NewRat(12, 1))
	}
	sort.Slice(out.Rows, func(i, j int) bool {
		a, b := out.Rows[i], out.Rows[j]
		if a.PaymentDate != b.PaymentDate {
			return a.PaymentDate < b.PaymentDate
		}
		if a.EventID != b.EventID {
			return a.EventID < b.EventID
		}
		return a.LedgerID < b.LedgerID
	})
	return out, nil
}

func forecastQuantityWithIssues(l d.InvestmentLedger, e ForecastEvent, at time.Time, historical bool, startsFromZero bool, ops map[string]d.InvestmentOperation, legs []ForecastSecurityLeg, issues []string) (*big.Rat, []string) {
	q, more := forecastQuantity(l, e, at, historical, startsFromZero, ops, legs)
	if len(issues) > 0 {
		q = nil
	}
	return q, append(issues, more...)
}
