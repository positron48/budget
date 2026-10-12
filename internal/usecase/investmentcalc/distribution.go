package investmentcalc

import (
	"fmt"
	"math/big"
)

// Distribute creates a new instrument's book from explicitly confirmed terms.
// It preserves lot origins and exact fractional entitlements. The caller must
// carry acquisition dates and unknown-cost flags by OriginID, and must not treat
// this calculation as evidence of an actual corporate action. CostShare is an
// explicit allocation, not inferred from market prices or share quantities.
//
// consumeSource distinguishes replacement from distribution alongside the old
// security. Replacement must transfer all remaining cost; otherwise cost would
// remain attached to zero units. The returned book must be integrated into the
// destination's acquisition-time ordering by the journal, not blindly appended.
func (b *Book) Distribute(newUnits, oldUnits, costNumerator, costDenominator string, consumeSource bool) (*Book, error) {
	n, err := number(newUnits)
	if err != nil || n.Sign() <= 0 {
		return nil, fmt.Errorf("positive new units required")
	}
	d, err := number(oldUnits)
	if err != nil || d.Sign() <= 0 {
		return nil, fmt.Errorf("positive old units required")
	}
	cn, err := number(costNumerator)
	if err != nil || cn.Sign() < 0 {
		return nil, fmt.Errorf("nonnegative cost numerator required")
	}
	cd, err := number(costDenominator)
	if err != nil || cd.Sign() <= 0 || cn.Cmp(cd) > 0 {
		return nil, fmt.Errorf("cost share must be between zero and one")
	}
	if consumeSource && cn.Cmp(cd) != 0 {
		return nil, fmt.Errorf("replacement must transfer all cost")
	}
	quantity, _ := b.Totals()
	if quantity.Sign() <= 0 {
		return nil, fmt.Errorf("distribution requires position")
	}
	ratio := new(big.Rat).Quo(n, d)
	share := new(big.Rat).Quo(cn, cd)
	result := &Book{origins: map[string]bool{}}
	// Build the result before changing the source. Every public value is owned
	// independently; later sales or splits cannot mutate the other instrument.
	for _, lot := range b.lots {
		if lot.Quantity.Sign() == 0 {
			continue
		}
		result.lots = append(result.lots, Lot{lot.OriginID,
			new(big.Rat).Mul(lot.Quantity, ratio), new(big.Rat).Mul(lot.Cost, share)})
		result.origins[lot.OriginID] = true
	}
	for i := range b.lots {
		lot := &b.lots[i]
		lot.Cost.Sub(lot.Cost, new(big.Rat).Mul(lot.Cost, share))
		if consumeSource {
			lot.Quantity.SetInt64(0)
		}
	}
	return result, nil
}

// Absorb moves all remaining lots from another book in an explicit FIFO order.
// The journal supplies order from acquisition dates and stable sequence IDs;
// neither event arrival time nor map iteration is a valid ordering substitute.
// Both books are unchanged on failure. Consumed origin IDs stay reserved.
func (b *Book) Absorb(other *Book, order []string) error {
	if other == nil || other == b {
		return fmt.Errorf("distinct source book required")
	}
	lots := map[string]Lot{}
	for _, lot := range b.Lots() {
		lots[lot.OriginID] = lot
	}
	incoming := other.Lots()
	if len(incoming) == 0 {
		return fmt.Errorf("source position required")
	}
	for _, lot := range incoming {
		if b.origins[lot.OriginID] {
			return fmt.Errorf("duplicate acquisition origin")
		}
		lots[lot.OriginID] = lot
	}
	if len(order) != len(lots) {
		return fmt.Errorf("complete acquisition order required")
	}
	ordered := make([]Lot, 0, len(order))
	seen := map[string]bool{}
	for _, origin := range order {
		lot, ok := lots[origin]
		if !ok || seen[origin] {
			return fmt.Errorf("invalid acquisition order")
		}
		seen[origin] = true
		ordered = append(ordered, lot)
	}
	if b.origins == nil {
		b.origins = map[string]bool{}
	}
	for _, lot := range incoming {
		b.origins[lot.OriginID] = true
	}
	b.lots = ordered
	for i := range other.lots {
		other.lots[i].Quantity.SetInt64(0)
		other.lots[i].Cost.SetInt64(0)
	}
	return nil
}
