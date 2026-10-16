package investment

import (
	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"math/big"
	"sort"
	"strings"
	"time"
)

type PayoutSchedule struct {
	InstrumentKey, Reference, Currency string
	Floating                           bool
	Coupons                            []md.BondCoupon
	Dividends                          []md.Dividend
}

// Derived events are a read-only view. Coupons use provider amounts. Historical
// dividends other than known specials repeat a year later as a labelled scenario.
// They never create journal entries or claim that a future payment is declared.
func MarketForecastEvents(tenant string, saved []ForecastEvent, schedules []PayoutSchedule, ops []d.InvestmentOperation, asOf time.Time, fromMonth string) []ForecastEvent {
	first, _ := time.Parse("2006-01", fromMonth)
	end := first.AddDate(1, 0, 0)
	today := asOf.UTC().Format(time.DateOnly)
	out := append([]ForecastEvent{}, saved...)
	key := func(e ForecastEvent) string { return e.InstrumentKey + "\x00" + e.Kind + "\x00" + e.PaymentDate }
	seen := map[string]bool{}
	for _, e := range saved {
		seen[key(e)] = true
	}
	add := func(e ForecastEvent) {
		pay, err := time.Parse(time.DateOnly, e.PaymentDate)
		if len(out) >= 10000 || err != nil || pay.Before(first) || !pay.Before(end) || e.PaymentDate < today || seen[key(e)] {
			return
		}
		e.ID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(tenant+"/derived-payout/"+key(e))).String()
		e.Version = 1
		e.Timezone = "Europe/Moscow"
		e.EntitlementRule = "unknown"
		e.Reason = "Read-only portfolio payout scenario"
		if _, err = NormalizeForecastEvent(e); err != nil {
			return
		}
		seen[key(e)] = true
		out = append(out, e)
	}
	// Announced schedules take precedence over all historical projections.
	for _, s := range schedules {
		for _, c := range s.Coupons {
			currency, amount := s.Currency, ""
			if c.Pay != nil {
				currency, amount = c.Pay.Currency, c.Pay.Amount
			}
			nature := "announced"
			variable := s.Floating || strings.Contains(c.Kind, "FLOAT") || strings.Contains(c.Kind, "VAR")
			if c.Pay != nil && variable && (c.FixDate.IsZero() || c.FixDate.After(asOf)) {
				nature = "forecast"
			}
			add(ForecastEvent{InstrumentKey: s.InstrumentKey, Kind: "coupon", Nature: nature, Currency: currency, PerShare: amount, PaymentDate: c.Date.UTC().Format(time.DateOnly), Origin: "provider", SourceReference: s.Reference, Evidence: "Provider coupon schedule; unfixed variable coupon amounts are projections; current holdings scenario, no confirmed personal entitlement"})
		}
		for _, div := range s.Dividends {
			if div.PaymentDate.IsZero() || div.DividendNet == nil {
				continue
			}
			add(ForecastEvent{InstrumentKey: s.InstrumentKey, Kind: "dividend", Nature: "announced", Currency: div.DividendNet.Currency, PerShare: div.DividendNet.Amount, PaymentDate: div.PaymentDate.UTC().Format(time.DateOnly), Origin: "provider", RecordDate: func() string {
				if div.RecordDate.IsZero() {
					return ""
				}
				return div.RecordDate.UTC().Format(time.DateOnly)
			}(), SourceReference: s.Reference, Evidence: "Provider reported dividend amount; current holdings scenario, no tax adjustment or confirmed personal entitlement"})
		}
	}
	for _, s := range schedules {
		for _, div := range s.Dividends {
			if div.PaymentDate.IsZero() || div.DividendNet == nil || !div.PaymentDate.Before(asOf) || div.PaymentDate.Before(asOf.AddDate(-1, 0, 0)) {
				continue
			}
			kind := strings.ToLower(div.DividendType + " " + div.Regularity)
			if strings.Contains(kind, "special") || strings.Contains(kind, "extra") || strings.Contains(kind, "разов") {
				continue
			}
			amount, ok := new(big.Rat).SetString(div.DividendNet.Amount)
			if !ok {
				continue
			}
			adjustments := map[string]bool{}
			unsupported := false
			for _, op := range ops {
				if op.Status != "posted" || op.InstrumentKey != s.InstrumentKey || !op.OccurredAt.After(div.PaymentDate) || op.OccurredAt.After(asOf) {
					continue
				}
				if op.Kind == "distribution" || op.Kind == "security_transfer_in" || op.Kind == "security_transfer_out" {
					unsupported = true
				}
				if op.Kind == "split" {
					id := op.OccurredAt.UTC().Format(time.RFC3339Nano) + "/" + op.SplitNew + "/" + op.SplitOld
					if !adjustments[id] {
						old, ok1 := new(big.Rat).SetString(op.SplitOld)
						next, ok2 := new(big.Rat).SetString(op.SplitNew)
						if !ok1 || !ok2 || next.Sign() <= 0 {
							unsupported = true
							continue
						}
						amount.Mul(amount, new(big.Rat).Quo(old, next))
						adjustments[id] = true
					}
				}
			}
			if unsupported {
				continue
			}
			pay := div.PaymentDate.AddDate(1, 0, 0)
			add(ForecastEvent{InstrumentKey: s.InstrumentKey, Kind: "dividend", Nature: "forecast", Currency: div.DividendNet.Currency, PerShare: amount.FloatString(12), PaymentDate: pay.UTC().Format(time.DateOnly), Origin: "historical_projection", SourceReference: s.Reference, Evidence: "Scenario: repeat the previous year's dividend date and reported per-unit amount, adjusted for recorded splits; not an issuer announcement"})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].PaymentDate != out[j].PaymentDate {
			return out[i].PaymentDate < out[j].PaymentDate
		}
		return out[i].ID < out[j].ID
	})
	return out
}
