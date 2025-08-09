package domain

type Money struct {
	CurrencyCode string
	MinorUnits   int64 // cents/kopecks (assumed 2 decimals for now)
}
