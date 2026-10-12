package investmentvaluation

import calc "github.com/positron48/budget/internal/usecase/investmentcalc"

type ProfitAnalytics struct {
	Performance Performance
	Timeline    calc.ProfitTimeline
	Attribution calc.ProfitAttribution
	Instruments InstrumentContributions
}
