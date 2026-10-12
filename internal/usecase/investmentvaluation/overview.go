package investmentvaluation

import (
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	"math/big"
	"time"
)

// Overview is one authorized read snapshot. Displaying its fields does not
// certify completeness of the underlying imported journal.
type Overview struct {
	Performance Performance
	NAV         DailyNAV
	Composition *Composition
	Daily       calc.ProfitPoint
	FX          []OverviewFX
	ReadAt      time.Time
}
type OverviewFX struct {
	From, To, Policy, Source, Mode, State string
	PolicyVersion, Revision               int64
	Rate, Change                          *big.Rat
	At, AvailableAt, ValidUntil           time.Time
}
