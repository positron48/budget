package marketdata

import (
	"context"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// BondFacts is provider evidence, not confirmed historical pricing terms or
// the owner's entitlement to a payout. Missing amounts stay nil; known zero is explicit.
type BondAmount struct{ Amount, Currency string }
type BondTerms struct {
	Nominal, InitialNominal, CurrentAccrued *BondAmount
	Maturity, CallDate, Placement           time.Time
	Floating, Perpetual, Amortizing         *bool
	CouponFrequency                         *int32
}
type BondCoupon struct {
	Number                    int64
	Date, FixDate, Start, End time.Time
	Kind                      string
	Pay                       *BondAmount
	PeriodDays                int32
}
type BondEvent struct {
	Number                                            int32
	Kind                                              string
	Date, FixDate, PayDate, ActualPayDate, Start, End time.Time
	Pay                                               *BondAmount
	Value, AnnualRate                                 *string
	Execution, OperationType, Note, ConvertTo         string
}
type BondAccrued struct {
	Date                    time.Time
	Value, Percent, Nominal *string
}
type BondFacts struct {
	Instrument     Instrument
	From, To, AsOf time.Time
	Terms          BondTerms
	Coupons        []BondCoupon
	Events         []BondEvent
	Accrued        []BondAccrued
}
type SavedBondFacts struct {
	BondFacts
	ID, Hash   string
	RecordedAt time.Time
}

func validBondTime(at time.Time, required bool) bool {
	return !required && at.IsZero() || !at.IsZero() && at.Year() >= 1900 && at.Year() <= 2200 && at.Nanosecond()%1000 == 0
}
func validBondNumber(s string, positive bool) bool {
	if !decimalPattern.MatchString(s) {
		return false
	}
	v, ok := new(big.Rat).SetString(s)
	return ok && (v.Sign() > 0 || !positive && v.Sign() == 0)
}
func validBondAmount(v *BondAmount, positive bool) bool {
	return v == nil || validBondNumber(v.Amount, positive) && len(v.Currency) == 3 && stringsCurrency(v.Currency)
}
func stringsCurrency(c string) bool {
	for _, v := range c {
		if v < 'A' || v > 'Z' {
			return false
		}
	}
	return true
}
func validOptionalBondNumber(v *string) bool { return v == nil || validBondNumber(*v, false) }
func NormalizeBondFacts(f BondFacts) (BondFacts, error) {
	if ValidateInstrument(f.Instrument) != nil || f.Instrument.Kind != "bond" || !f.Instrument.Detailed || ValidateRequest(JobRequest{Kind: "bond_facts", InstrumentID: f.Instrument.ID, From: f.From, To: f.To}) != nil || !validBondTime(f.AsOf, true) || len(f.Coupons) > 10000 || len(f.Events) > 10000 || len(f.Accrued) > 1000 {
		return f, ErrInvalid
	}
	if !validBondAmount(f.Terms.Nominal, false) || !validBondAmount(f.Terms.InitialNominal, true) || !validBondAmount(f.Terms.CurrentAccrued, false) || !validBondTime(f.Terms.Maturity, false) || !validBondTime(f.Terms.CallDate, false) || !validBondTime(f.Terms.Placement, false) || f.Terms.CouponFrequency != nil && (*f.Terms.CouponFrequency < 0 || *f.Terms.CouponFrequency > 366) {
		return f, ErrInvalid
	}
	seen := map[string]bool{}
	for _, p := range f.Coupons {
		key := strconv.FormatInt(p.Number, 10)
		if p.Number < 0 || seen[key] || !validBondTime(p.Date, true) || p.Date.Before(f.From) || p.Date.After(f.To) || !validBondTime(p.FixDate, false) || !validBondTime(p.Start, false) || !validBondTime(p.End, false) || !p.Start.IsZero() && !p.End.IsZero() && p.End.Before(p.Start) || !validBondAmount(p.Pay, false) || p.PeriodDays < 0 || p.PeriodDays > 3660 || !mappingText(p.Kind, 100) {
			return f, ErrInvalid
		}
		seen[key] = true
	}
	seen = map[string]bool{}
	for _, p := range f.Events {
		key := p.Kind + ":" + strconv.Itoa(int(p.Number))
		if p.Number < 0 || seen[key] || !mappingText(p.Kind, 100) || !validBondTime(p.Date, true) || p.Date.Before(f.From) || p.Date.After(f.To) || !validBondTime(p.FixDate, false) || !validBondTime(p.PayDate, false) || !validBondTime(p.ActualPayDate, false) || !validBondTime(p.Start, false) || !validBondTime(p.End, false) || !validBondAmount(p.Pay, false) || !validOptionalBondNumber(p.Value) || !validOptionalBondNumber(p.AnnualRate) || !validBondText(p.Note, 4000) || !validBondText(p.Execution, 100) || !validBondText(p.OperationType, 100) || !validBondText(p.ConvertTo, 200) {
			return f, ErrInvalid
		}
		seen[key] = true
	}
	seen = map[string]bool{}
	for _, p := range f.Accrued {
		key := p.Date.UTC().Format(time.RFC3339Nano)
		if seen[key] || !validBondTime(p.Date, true) || p.Date.Before(f.From) || p.Date.After(f.To) || !validOptionalBondNumber(p.Value) || !validOptionalBondNumber(p.Percent) || !validOptionalBondNumber(p.Nominal) {
			return f, ErrInvalid
		}
		seen[key] = true
	}
	f.Coupons = append([]BondCoupon(nil), f.Coupons...)
	f.Events = append([]BondEvent(nil), f.Events...)
	f.Accrued = append([]BondAccrued(nil), f.Accrued...)
	sort.Slice(f.Coupons, func(i, j int) bool {
		if !f.Coupons[i].Date.Equal(f.Coupons[j].Date) {
			return f.Coupons[i].Date.Before(f.Coupons[j].Date)
		}
		return f.Coupons[i].Number < f.Coupons[j].Number
	})
	sort.Slice(f.Events, func(i, j int) bool {
		if !f.Events[i].Date.Equal(f.Events[j].Date) {
			return f.Events[i].Date.Before(f.Events[j].Date)
		}
		if f.Events[i].Kind != f.Events[j].Kind {
			return f.Events[i].Kind < f.Events[j].Kind
		}
		return f.Events[i].Number < f.Events[j].Number
	})
	sort.Slice(f.Accrued, func(i, j int) bool { return f.Accrued[i].Date.Before(f.Accrued[j].Date) })
	return f, nil
}

func validBondText(s string, max int) bool {
	return len(s) <= max && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}

type BondReadRepo interface {
	BondFacts(context.Context, Scope, JobRequest) (*SavedBondFacts, error)
}

func (s *Service) BondFacts(ctx context.Context, t, u string, scope Scope, q JobRequest) (*SavedBondFacts, error) {
	if err := s.allow(ctx, t, u, scope); err != nil {
		return nil, err
	}
	if q.Kind != "bond_facts" || ValidateRequest(q) != nil {
		return nil, ErrInvalid
	}
	repo, ok := s.repo.(BondReadRepo)
	if !ok {
		return nil, ErrNotFound
	}
	return repo.BondFacts(ctx, scope, q)
}
