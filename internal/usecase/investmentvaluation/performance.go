package investmentvaluation

import calc "github.com/positron48/budget/internal/usecase/investmentcalc"

// Performance describes the recorded journal, not certification that a broker
// import includes every real-world payment. Generation identifies the snapshot.
type Performance struct {
	calc.PeriodPerformance
	Generation                       int64
	SeriesKey, Currency, FXPolicyKey string
	Flows                            []calc.ConvertedFlow
	Days                             []calc.PerformanceDay
	ResetDates                       map[string]bool
}
