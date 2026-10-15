package investmentvaluation

import (
	"math/big"
	"time"
)

type TrackerBenchmarkPoint struct {
	Date, Reason string
	Index        *big.Rat
}
type TrackerBenchmark struct {
	Key, Title, Kind, NativeCurrency, Provider, SourceURL, BaseDate, Reason string
	ReadAt                                                                  time.Time
	Points                                                                  []TrackerBenchmarkPoint
}
