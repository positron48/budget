package investmentcalc

import (
	"fmt"
	"math/big"
	"time"
)

// FXLeg retains the observed pair and revision even when used inversely. The
// policy identifier must name one approved source/convention; it is not a
// licence to combine arbitrary providers under the generic word "market".
type FXLeg struct {
	ID                          string
	Revision                    int64
	From, To, Policy            string
	At, AvailableAt, ValidUntil time.Time
	Inverse                     bool
}
type ResolvedFX struct {
	Rate *big.Rat
	Legs []FXLeg
}
type FXResolver struct{ latest map[[2]string]FXFixing }

// NewFXResolver selects eligible, fresh evidence under exactly one policy.
// ValidUntil is exclusive and must be established upstream using a verified
// fixing/session convention. No default age or CBR fallback is introduced.
func NewFXResolver(fixings []FXFixing, policy string, at time.Time) (*FXResolver, error) {
	r := &FXResolver{latest: map[[2]string]FXFixing{}}
	if policy == "" || at.IsZero() {
		return nil, fmt.Errorf("FX policy and cutoff required")
	}
	type identity struct {
		from, to string
		at       time.Time
	}
	seen := map[identity]*big.Rat{}
	for _, f := range fixings {
		if f.Policy != policy {
			continue
		}
		if f.ID == "" || f.Revision < 1 || !validMoneyCurrency(f.From) || !validMoneyCurrency(f.To) || f.From == f.To || f.At.IsZero() || f.AvailableAt.IsZero() || f.AvailableAt.Before(f.At) || !f.ValidUntil.After(f.AvailableAt) {
			return nil, fmt.Errorf("invalid FX evidence")
		}
		rate, err := number(f.Rate)
		if f.Withdrawn {
			if f.Rate != "" {
				return nil, fmt.Errorf("withdrawn FX has a rate")
			}
			rate = nil
		} else if err != nil || rate.Sign() <= 0 {
			return nil, fmt.Errorf("positive FX rate required")
		}
		// Availability and validity precede duplicate checks: later revisions must
		// be selected by the repository at the requested knowledge cutoff.
		if f.At.After(at) || f.AvailableAt.After(at) {
			continue
		}
		key := identity{f.From, f.To, f.At.UTC()}
		if old, ok := seen[key]; ok && ((old == nil) != (rate == nil) || (old != nil && rate != nil && old.Cmp(rate) != 0)) {
			return nil, fmt.Errorf("conflicting FX fixing")
		}
		seen[key] = rate
		pair := [2]string{f.From, f.To}
		old, ok := r.latest[pair]
		if !ok || f.At.After(old.At) || (f.At.Equal(old.At) && (f.ID < old.ID || (f.ID == old.ID && f.Revision > old.Revision))) {
			r.latest[pair] = f
		}
	}
	// Do not revive an older fixing with a longer declared validity after a newer one expires.
	for pair, f := range r.latest {
		if f.Withdrawn || !at.Before(f.ValidUntil) {
			delete(r.latest, pair)
		}
	}
	return r, nil
}
func (r *FXResolver) edge(from, to string) (ResolvedFX, bool) {
	f, ok := r.latest[[2]string{from, to}]
	inverse := false
	if !ok {
		f, ok = r.latest[[2]string{to, from}]
		inverse = true
	}
	if !ok {
		return ResolvedFX{}, false
	}
	rate, _ := number(f.Rate)
	if inverse {
		rate.Inv(rate)
	}
	return ResolvedFX{rate, []FXLeg{{ID: f.ID, Revision: f.Revision, From: f.From, To: f.To, Policy: f.Policy, At: f.At, AvailableAt: f.AvailableAt, ValidUntil: f.ValidUntil, Inverse: inverse}}}, true
}

// Resolve prefers an observed direct rate, then its inverse, then a two-leg
// cross through RUB, USD, EUR in that documented order. Both legs must be valid
// at the same cutoff under the same policy. Paths are exposed with the value.
// No arbitrary multi-hop graph or intermediate rounding is used.
func (r *FXResolver) Resolve(from, to string) (ResolvedFX, bool) {
	if !validMoneyCurrency(from) || !validMoneyCurrency(to) {
		return ResolvedFX{}, false
	}
	if from == to {
		return ResolvedFX{big.NewRat(1, 1), []FXLeg{}}, true
	}
	if v, ok := r.edge(from, to); ok {
		return v, true
	}
	for _, pivot := range []string{"RUB", "USD", "EUR"} {
		if pivot == from || pivot == to {
			continue
		}
		first, ok := r.edge(from, pivot)
		if !ok {
			continue
		}
		second, ok := r.edge(pivot, to)
		if !ok {
			continue
		}
		return ResolvedFX{new(big.Rat).Mul(first.Rate, second.Rate), append(first.Legs, second.Legs...)}, true
	}
	return ResolvedFX{}, false
}
