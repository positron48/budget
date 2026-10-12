package investmentcalc

import "fmt"

// ExtractFIFO moves units and their exact remaining cost without realizing a
// profit. It is atomic on failure; returned lots own their rational values.
func (b *Book) ExtractFIFO(quantity string) ([]Lot, error) {
	copy := b.Clone()
	matches, _, err := copy.SellProceeds(quantity, "0")
	if err != nil {
		return nil, err
	}
	lots := make([]Lot, 0, len(matches))
	for _, m := range matches {
		lots = append(lots, Lot{OriginID: m.OriginID, Quantity: copyRat(m.Quantity), Cost: copyRat(m.Cost)})
	}
	*b = *copy
	return lots, nil
}

// ReceiveSlices keeps separately transferred parts distinct. Combining their
// costs under the acquisition ID would average differently recovered interest
// and change later FIFO allocations. The caller supplies deterministic slice IDs
// and an acquisition order, never an order based on map iteration.
func (b *Book) ReceiveSlices(incoming []Lot, order []string) error {
	all := map[string]Lot{}
	for _, lot := range b.Lots() {
		all[lot.OriginID] = lot
	}
	seen := map[string]bool{}
	for _, lot := range incoming {
		if lot.OriginID == "" || seen[lot.OriginID] || b.origins[lot.OriginID] || lot.Quantity == nil || lot.Quantity.Sign() <= 0 || lot.Cost == nil || lot.Cost.Sign() < 0 {
			return fmt.Errorf("unique positive incoming slice with nonnegative cost required")
		}
		seen[lot.OriginID] = true
		all[lot.OriginID] = Lot{lot.OriginID, copyRat(lot.Quantity), copyRat(lot.Cost)}
	}
	if len(incoming) == 0 || len(order) != len(all) {
		return fmt.Errorf("complete acquisition order required")
	}
	next := make([]Lot, 0, len(order))
	seen = map[string]bool{}
	for _, id := range order {
		lot, ok := all[id]
		if !ok || seen[id] {
			return fmt.Errorf("invalid acquisition order")
		}
		seen[id] = true
		next = append(next, lot)
	}
	if b.origins == nil {
		b.origins = map[string]bool{}
	}
	for _, lot := range incoming {
		b.origins[lot.OriginID] = true
	}
	b.lots = next
	return nil
}
