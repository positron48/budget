package investmentcalc

import (
	"math"
	"math/big"
	"sort"
	"time"
)

type DatedCashFlow struct {
	Date   string
	Amount *big.Rat
}
type XIRRResult struct {
	Rate       *float64 // annual decimal return, e.g. 0.1 = 10%; nil means undefined
	Reason     string
	Days       int
	Annualized bool
}
type irrTerm struct{ t, c float64 }

// XIRR uses investor signs (contributions negative, withdrawals/terminal NAV
// positive) and ACT/365 calendar dates. It isolates all simple roots using the
// stationary points of a sparse exponential sum, rather than selecting whichever
// root Newton iteration happens to reach. Tangencies and unstable ranges fail
// explicitly. Conventional cash flows use a unique-root fast path.
func XIRR(flows []DatedCashFlow) XIRRResult {
	out := XIRRResult{Annualized: true}
	fail := func(reason string) XIRRResult { out.Reason = reason; return out }
	grouped := map[string]*big.Rat{}
	dates := map[string]time.Time{}
	for _, f := range flows {
		date, e := time.Parse(time.DateOnly, f.Date)
		if e != nil || date.Format(time.DateOnly) != f.Date {
			return fail("invalid_date")
		}
		if f.Amount == nil {
			return fail("cash_flow_unknown")
		}
		if grouped[f.Date] == nil {
			grouped[f.Date] = new(big.Rat)
		}
		grouped[f.Date].Add(grouped[f.Date], f.Amount)
		dates[f.Date] = date
	}
	keys := []string{}
	for date, amount := range grouped {
		if amount.Sign() != 0 {
			keys = append(keys, date)
		}
	}
	sort.Strings(keys)
	if len(keys) < 2 {
		return fail("insufficient_dates")
	}
	out.Days = int((dates[keys[len(keys)-1]].Unix() - dates[keys[0]].Unix()) / 86400)
	terms := []irrTerm{}
	for _, date := range keys {
		value, _ := grouped[date].Float64()
		if value == 0 || math.IsInf(value, 0) || math.IsNaN(value) {
			return fail("numerical_range")
		}
		terms = append(terms, irrTerm{float64((dates[date].Unix()-dates[keys[0]].Unix())/86400) / 365, value})
	}
	roots, reason := irrRoots(terms)
	if reason != "" {
		return fail(reason)
	}
	if len(roots) == 0 {
		return fail("no_root")
	}
	if len(roots) > 1 {
		return fail("multiple_roots")
	}
	// A zero produced by rounded coefficients is not evidence of exact break-even.
	if roots[0] == 0 {
		total := new(big.Rat)
		for _, amount := range grouped {
			total.Add(total, amount)
		}
		if total.Sign() != 0 {
			return fail("numerical_precision")
		}
	}
	if slope := math.Abs(irrSlope(terms, roots[0])); slope == 0 || float64(len(terms))*64*2.220446049250313e-16/slope > 1e-8*math.Max(1, math.Abs(roots[0])) {
		return fail("ill_conditioned_root")
	}
	rate := math.Expm1(roots[0])
	if rate <= -1 || math.IsInf(rate, 0) || math.IsNaN(rate) {
		return fail("rate_precision_limit")
	}
	out.Rate = &rate
	return out
}

func irrSign(value float64) int {
	if value < 0 {
		return -1
	}
	if value > 0 {
		return 1
	}
	return 0
}

// Normalize by the largest coefficient, factoring out the first exponential.
func irrNormalize(input []irrTerm) ([]irrTerm, bool) {
	max := 0.0
	for _, v := range input {
		max = math.Max(max, math.Abs(v.c))
	}
	if max == 0 || math.IsInf(max, 0) {
		return nil, false
	}
	out := make([]irrTerm, len(input))
	for i, v := range input {
		out[i] = irrTerm{v.t - input[0].t, v.c / max}
		if out[i].c == 0 {
			return nil, false
		}
	}
	return out, true
}

// Signed residual divided by sum(abs(terms)); log scaling prevents overflow.
func irrValue(terms []irrTerm, y float64) float64 {
	largest := math.Inf(-1)
	for _, v := range terms {
		largest = math.Max(largest, math.Log(math.Abs(v.c))-v.t*y)
	}
	sum, total, correction := 0.0, 0.0, 0.0
	for _, v := range terms {
		magnitude := math.Exp(math.Log(math.Abs(v.c)) - v.t*y - largest)
		value := math.Copysign(magnitude, v.c) - correction
		next := sum + value
		correction = (next - sum) - value
		sum = next
		total += magnitude
	}
	return sum / total
}
func irrRoots(input []irrTerm) ([]float64, string) {
	terms, ok := irrNormalize(input)
	if !ok {
		return nil, "numerical_range"
	}
	changes := 0
	for i := 1; i < len(terms); i++ {
		if irrSign(terms[i].c) != irrSign(terms[i-1].c) {
			changes++
		}
	}
	if changes == 0 {
		return nil, ""
	}
	if changes == 1 {
		root, ok := irrBisect(terms, math.Inf(-1), math.Inf(1), irrSign(terms[len(terms)-1].c), irrSign(terms[0].c))
		if !ok {
			return nil, "numerical_range"
		}
		return []float64{root}, ""
	}
	if len(terms) > 256 {
		return nil, "complexity_limit"
	}
	derivative := make([]irrTerm, 0, len(terms)-1)
	for _, v := range terms[1:] {
		derivative = append(derivative, irrTerm{v.t, -v.t * v.c})
	}
	critical, reason := irrRoots(derivative)
	if reason == "non_simple_root" {
		reason = "root_isolation_unreliable"
	}
	if reason != "" {
		return nil, reason
	}
	boundaries := append([]float64{math.Inf(-1)}, critical...)
	boundaries = append(boundaries, math.Inf(1))
	signs := make([]int, len(boundaries))
	signs[0] = irrSign(terms[len(terms)-1].c)
	signs[len(signs)-1] = irrSign(terms[0].c)
	for i, y := range critical {
		residual := irrValue(terms, y)
		// A double root or nearly coalescing pair cannot be identified reliably as
		// a unique economic return. Do not turn numerical proximity into certainty.
		if math.Abs(residual) < 1e-10 {
			return nil, "non_simple_root"
		}
		signs[i+1] = irrSign(residual)
	}
	roots := []float64{}
	for i := 0; i < len(boundaries)-1; i++ {
		if signs[i] == signs[i+1] {
			continue
		}
		root, ok := irrBisect(terms, boundaries[i], boundaries[i+1], signs[i], signs[i+1])
		if !ok {
			return nil, "numerical_range"
		}
		roots = append(roots, root)
	}
	return roots, ""
}
func irrBisect(terms []irrTerm, lo, hi float64, loSign, hiSign int) (float64, bool) {
	if math.IsInf(lo, -1) {
		base := hi
		if math.IsInf(base, 1) {
			base = 0
		}
		step := 1.0
		lo = base - step
		for irrSign(irrValue(terms, lo)) != loSign {
			step *= 2
			lo = base - step
			if step > 1e6 {
				return 0, false
			}
		}
	}
	if math.IsInf(hi, 1) {
		step := 1.0
		hi = lo + step
		for irrSign(irrValue(terms, hi)) != hiSign {
			step *= 2
			hi = lo + step
			if step > 1e6 {
				return 0, false
			}
		}
	}
	for i := 0; i < 256; i++ {
		mid := lo + (hi-lo)/2
		value := irrValue(terms, mid)
		if value == 0 {
			return mid, true
		}
		if irrSign(value) == loSign {
			lo = mid
		} else {
			hi = mid
		}
		if hi-lo < 1e-13*math.Max(1, math.Abs(mid)) && math.Abs(value) < 1e-10 {
			return mid, true
		}
	}
	return 0, false
}

// Derivative on the same log-scaled basis estimates root sensitivity to floating
// point error. Numerically flat roots are not presented as reliable percentages.
func irrSlope(terms []irrTerm, y float64) float64 {
	largest := math.Inf(-1)
	for _, v := range terms {
		largest = math.Max(largest, math.Log(math.Abs(v.c))-v.t*y)
	}
	derivative, total := 0.0, 0.0
	for _, v := range terms {
		weight := math.Exp(math.Log(math.Abs(v.c)) - v.t*y - largest)
		derivative -= v.t * math.Copysign(weight, v.c)
		total += weight
	}
	return derivative / total
}
