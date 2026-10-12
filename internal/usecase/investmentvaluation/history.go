package investmentvaluation

import "time"

// DailyNAV distinguishes missing/stale values from an incomplete known subtotal.
type DailyNAV struct {
	Date       string
	Generation int64
	State      string
	ComputedAt time.Time
	Result     *Result
}

type SeriesSummary struct {
	ExtensionLimited                bool
	Key                             string
	LedgerIDs                       []string
	Currency, FXPolicyKey, From, To string
}
