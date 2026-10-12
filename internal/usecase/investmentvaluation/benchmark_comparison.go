package investmentvaluation

import calc "github.com/positron48/budget/internal/usecase/investmentcalc"

type BenchmarkComparison struct {
	Profile             SavedBenchmarkProfile
	PriceProfileVersion int64
	Portfolio           Performance
	Result              calc.BenchmarkResult
	Evidence            []BenchmarkDailyProvenance
}
