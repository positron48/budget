package investmentvaluation

// Variant retains the source account scope, price rules and registered range.
// Registered means the target actor watch covers that entire source range.
type Variant struct {
	Series     SeriesSummary
	Registered bool
	QueuedDays int
}
