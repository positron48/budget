package investment

import (
	"context"
	"math/big"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	d "github.com/positron48/budget/internal/domain"
)

// DividendHistory is issuer evidence, independent of investor holdings and cash.
// BasisAt fixes the common share unit; the reviewer confirms ALL intervening
// splits since ReviewedFrom, including periods without personal holdings.
type DividendHistory struct {
	ID, InstrumentKey, Currency, Timezone string
	BasisAt, ReviewedFrom                 time.Time
	SourceReference, Evidence, Reason     string
	Years                                 []DividendYearReview
	Dividends                             []DividendDeclaration
	Splits                                []DividendUnitSplit
	Version                               int64
	Cancelled                             bool
}
type DividendYearReview struct {
	Year                      int
	Complete                  bool
	SourceReference, Evidence string
}
type DividendDeclaration struct {
	ID, PaymentDate, Amount, Class string
	UnitBasisAt                    time.Time
	SourceReference, Evidence      string
	Status                         string // paid, announced, unknown, cancelled; date alone never proves payment
}
type DividendUnitSplit struct {
	ID, NewUnits, OldUnits    string
	EffectiveAt               time.Time
	SourceReference, Evidence string
}
type DividendYearGrowth struct {
	Year                                                                        int
	Regular, Special, Total, KnownTotal, PreviousRegular, Change, PercentChange *big.Rat
	SpecialCount, UnknownClassCount                                             int
	Status                                                                      string
}
type DividendHistoryReport struct {
	History DividendHistory
	Years   []DividendYearGrowth
}
type DividendGrowthReport struct {
	AsOf      time.Time
	Histories []DividendHistoryReport
}

type dividendGrowthRepo interface {
	DividendGrowth(context.Context, string, string) (DividendGrowthReport, error)
	SaveDividendHistory(context.Context, string, string, DividendHistory, int64) (DividendHistory, error)
}

func dividendEvidence(s string) bool {
	return strings.TrimSpace(s) != "" && len(s) <= 4000 && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}
func dividendInstant(t time.Time) bool {
	return !t.IsZero() && t.Year() >= 1900 && t.Year() <= 2200 && t.Nanosecond()%1000 == 0
}
func NormalizeDividendHistory(h DividendHistory) (DividendHistory, error) {
	bad := func() (DividendHistory, error) { return h, d.ErrInvestmentInvalid }
	if !validID(h.ID) || h.Version < 0 || len(h.InstrumentKey) < 1 || len(h.InstrumentKey) > 200 || strings.TrimSpace(h.InstrumentKey) != h.InstrumentKey || !utf8.ValidString(h.InstrumentKey) || strings.ContainsRune(h.InstrumentKey, 0) || (h.Currency != "RUB" && h.Currency != "USD" && h.Currency != "EUR") || !dividendEvidence(h.SourceReference) || !dividendEvidence(h.Evidence) || !dividendEvidence(h.Reason) || !dividendInstant(h.BasisAt) || !dividendInstant(h.ReviewedFrom) || h.ReviewedFrom.After(h.BasisAt) || len(h.Years) > 201 || len(h.Dividends) > 2000 || len(h.Splits) > 200 {
		return bad()
	}
	h.Years = append([]DividendYearReview(nil), h.Years...)
	h.Dividends = append([]DividendDeclaration(nil), h.Dividends...)
	h.Splits = append([]DividendUnitSplit(nil), h.Splits...)
	loc, e := time.LoadLocation(h.Timezone)
	if e != nil || h.Timezone == "" || h.Timezone == "Local" {
		return bad()
	}
	h.BasisAt = h.BasisAt.UTC()
	h.ReviewedFrom = h.ReviewedFrom.UTC()
	years := map[int]bool{}
	ids := map[string]bool{}
	instants := map[time.Time]bool{}
	for _, y := range h.Years {
		if y.Year < 1900 || y.Year > 2200 || years[y.Year] || !dividendEvidence(y.SourceReference) || !dividendEvidence(y.Evidence) {
			return bad()
		}
		years[y.Year] = true
	}
	for i, v := range h.Dividends {
		date, err := forecastDate(v.PaymentDate, loc)
		if err != nil || !years[date.Year()] || !validID(v.ID) || ids[v.ID] || (v.Class != "regular" && v.Class != "special" && v.Class != "unknown") || (v.Status != "paid" && v.Status != "announced" && v.Status != "unknown" && v.Status != "cancelled") || (v.Status == "paid" && date.After(h.BasisAt)) || !dividendInstant(v.UnitBasisAt) || v.UnitBasisAt.Before(h.ReviewedFrom) || v.UnitBasisAt.After(h.BasisAt) || !dividendEvidence(v.SourceReference) || !dividendEvidence(v.Evidence) {
			return bad()
		}
		if v.Amount != "" {
			normalized, er := normalize(v.Amount, 36, 12)
			if er != nil || rational(normalized).Sign() < 0 {
				return bad()
			}
			h.Dividends[i].Amount = normalized
		}
		ids[v.ID] = true
		h.Dividends[i].UnitBasisAt = v.UnitBasisAt.UTC()
	}
	for i, v := range h.Splits {
		if !validID(v.ID) || ids[v.ID] || !dividendInstant(v.EffectiveAt) || v.EffectiveAt.Before(h.ReviewedFrom) || v.EffectiveAt.After(h.BasisAt) || instants[v.EffectiveAt.UTC()] || !dividendEvidence(v.SourceReference) || !dividendEvidence(v.Evidence) {
			return bad()
		}
		n, err := exactNonNegative(v.NewUnits)
		if err != nil || n.Sign() == 0 {
			return bad()
		}
		o, err := exactNonNegative(v.OldUnits)
		if err != nil || o.Sign() == 0 {
			return bad()
		}
		h.Splits[i].NewUnits, _ = normalize(v.NewUnits, 36, 12)
		h.Splits[i].OldUnits, _ = normalize(v.OldUnits, 36, 12)
		ids[v.ID] = true
		instants[v.EffectiveAt.UTC()] = true
		h.Splits[i].EffectiveAt = v.EffectiveAt.UTC()
	}
	sort.Slice(h.Years, func(i, j int) bool { return h.Years[i].Year < h.Years[j].Year })
	sort.Slice(h.Dividends, func(i, j int) bool { return h.Dividends[i].ID < h.Dividends[j].ID })
	sort.Slice(h.Splits, func(i, j int) bool { return h.Splits[i].EffectiveAt.Before(h.Splits[j].EffectiveAt) })
	return h, nil
}

func BuildDividendHistory(h DividendHistory, asOf time.Time) (DividendHistoryReport, error) {
	h, e := NormalizeDividendHistory(h)
	if e != nil {
		return DividendHistoryReport{}, e
	}
	out := DividendHistoryReport{History: h}
	loc, _ := time.LoadLocation(h.Timezone)
	type sums struct {
		regular, special, total        *big.Rat
		unknownAmount, unknownClass    bool
		unknownRegular, unknownSpecial bool
		unconfirmedRegular             bool
		specialCount, unknownCount     int
	}
	annual := map[int]*sums{}
	reviews := map[int]DividendYearReview{}
	for _, y := range h.Years {
		annual[y.Year] = &sums{regular: new(big.Rat), special: new(big.Rat), total: new(big.Rat)}
		reviews[y.Year] = y
	}
	adjustments := make([]*big.Rat, len(h.Splits)+1)
	adjustments[len(h.Splits)] = big.NewRat(1, 1)
	for i := len(h.Splits) - 1; i >= 0; i-- {
		n, _ := exactNonNegative(h.Splits[i].NewUnits)
		o, _ := exactNonNegative(h.Splits[i].OldUnits)
		adjustments[i] = new(big.Rat).Mul(adjustments[i+1], new(big.Rat).Quo(o, n))
	}
	for _, v := range h.Dividends {
		at, _ := time.ParseInLocation(time.DateOnly, v.PaymentDate, loc)
		a := annual[at.Year()]
		if v.Status == "cancelled" {
			continue
		}
		if v.Class == "special" {
			a.specialCount++
		}
		if v.Class == "unknown" {
			a.unknownClass = true
			a.unknownCount++
		}
		if v.Amount == "" || v.Status != "paid" {
			a.unknownAmount = true
			if v.Class == "regular" {
				a.unknownRegular = true
				if v.Status != "paid" {
					a.unconfirmedRegular = true
				}
			}
			if v.Class == "special" {
				a.unknownSpecial = true
			}
			continue
		}
		amount, _ := exactNonNegative(v.Amount)
		// At an exact split timestamp, UnitBasisAt denotes the NEW unit. Only later
		// splits adjust this declaration. No settlement calendar is inferred.
		index := sort.Search(len(h.Splits), func(i int) bool { return h.Splits[i].EffectiveAt.After(v.UnitBasisAt) })
		amount.Mul(amount, adjustments[index])
		a.total.Add(a.total, amount)
		switch v.Class {
		case "regular":
			a.regular.Add(a.regular, amount)
		case "special":
			a.special.Add(a.special, amount)
		}
	}
	complete := func(year int) bool {
		y, ok := reviews[year]
		end := time.Date(year+1, 1, 1, 0, 0, 0, 0, loc)
		return ok && y.Complete && !end.After(asOf) && !end.After(h.BasisAt) && !h.BasisAt.After(asOf)
	}
	for _, y := range h.Years {
		a := annual[y.Year]
		g := DividendYearGrowth{Year: y.Year, KnownTotal: new(big.Rat).Set(a.total), SpecialCount: a.specialCount, UnknownClassCount: a.unknownCount, Status: "available"}
		if complete(y.Year) {
			if !a.unknownAmount {
				g.Total = new(big.Rat).Set(a.total)
			}
			if !a.unknownClass && !a.unknownRegular {
				g.Regular = new(big.Rat).Set(a.regular)
			}
			if !a.unknownClass && !a.unknownSpecial {
				g.Special = new(big.Rat).Set(a.special)
			}
		}
		end := time.Date(y.Year+1, 1, 1, 0, 0, 0, 0, loc)
		switch {
		case h.Cancelled:
			g.Status = "cancelled"
		case end.After(asOf) || end.After(h.BasisAt) || h.BasisAt.After(asOf):
			g.Status = "partial_year"
		case !complete(y.Year) || !complete(y.Year-1):
			g.Status = "incomplete_history"
		case a.unconfirmedRegular || annual[y.Year-1].unconfirmedRegular:
			g.Status = "unconfirmed_payment"
		case a.unknownRegular || a.unknownClass || annual[y.Year-1].unknownRegular || annual[y.Year-1].unknownClass:
			g.Status = "unknown_amount_or_class"
		default:
			g.PreviousRegular = new(big.Rat).Set(annual[y.Year-1].regular)
			g.Change = new(big.Rat).Sub(a.regular, g.PreviousRegular)
			if g.PreviousRegular.Sign() == 0 {
				g.Status = "zero_baseline"
			} else {
				g.PercentChange = new(big.Rat).Mul(new(big.Rat).Quo(g.Change, g.PreviousRegular), big.NewRat(100, 1))
			}
		}
		if h.Cancelled {
			g.Regular = nil
			g.Special = nil
			g.Total = nil
		}
		out.Years = append(out.Years, g)
	}
	return out, nil
}
func (s *Service) DividendGrowth(ctx context.Context, t, u string) (DividendGrowthReport, error) {
	if e := s.authorize(ctx, t, u, false); e != nil {
		return DividendGrowthReport{}, e
	}
	r, ok := s.repo.(dividendGrowthRepo)
	if !ok {
		return DividendGrowthReport{}, d.ErrInvestmentPrecondition
	}
	return r.DividendGrowth(ctx, t, u)
}
func (s *Service) SaveDividendHistory(ctx context.Context, t, u string, h DividendHistory, expected int64) (DividendHistory, error) {
	if e := s.authorize(ctx, t, u, true); e != nil {
		return DividendHistory{}, e
	}
	h, e := NormalizeDividendHistory(h)
	if e != nil || expected < 0 || expected == 1<<63-1 {
		return DividendHistory{}, d.ErrInvestmentInvalid
	}
	r, ok := s.repo.(dividendGrowthRepo)
	if !ok {
		return DividendHistory{}, d.ErrInvestmentPrecondition
	}
	return r.SaveDividendHistory(ctx, t, u, h, expected)
}

func exactNonNegative(s string) (*big.Rat, error) {
	v, e := normalize(s, 36, 12)
	if e != nil {
		return nil, e
	}
	r := rational(v)
	if r.Sign() < 0 {
		return nil, d.ErrInvestmentInvalid
	}
	return r, nil
}
