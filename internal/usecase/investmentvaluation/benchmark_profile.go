package investmentvaluation

import "time"

// BenchmarkProfile labels a reviewed price series without conflating a price
// index with an ETF proxy. The price profile owns listing periods and calendars.
type BenchmarkProfile struct {
	Key, Title, Kind, Currency, Evidence string
	PriceProfileKey, InstrumentKey       string
	Enabled                              bool
}
type SavedBenchmarkProfile struct {
	BenchmarkProfile
	Version    int64
	ActorID    string
	RecordedAt time.Time
}

func ValidateBenchmarkProfile(p BenchmarkProfile) bool {
	return ValidConventionKey(p.Key) && ValidConventionKey(p.PriceProfileKey) && conventionText(p.Title, 200) && conventionText(p.Evidence, 2000) && conventionText(p.InstrumentKey, 200) && (p.Kind == "price_index" || p.Kind == "etf_price_proxy") && (p.Currency == "RUB" || p.Currency == "USD" || p.Currency == "EUR")
}
