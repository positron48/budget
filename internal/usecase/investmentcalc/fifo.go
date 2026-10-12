// Package investmentcalc implements exact calculations over validated ledger
// facts. Source parsing and assumptions about broker reports belong elsewhere.
package investmentcalc

import (
	"fmt"
	"math/big"
	"regexp"
)

var canonicalDecimal = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?$`)

// Lot owns its rational values; public results are copies to prevent mutation.
// Cost is clean acquisition cost including fees in the book currency. Acquired
// interest and foreign currency fee conversion must be resolved upstream.
type Lot struct {
	OriginID string
	Quantity *big.Rat
	Cost     *big.Rat
}

type Match struct {
	OriginID string
	Quantity *big.Rat
	Cost     *big.Rat
}

// Book represents exactly one account/instrument/cost currency. It does not
// combine accounts or convert currencies. Callers replay confirmed time order.
type Book struct {
	lots    []Lot
	origins map[string]bool
}

func number(v string) (*big.Rat, error) {
	if len(v) > 128 || !canonicalDecimal.MatchString(v) {
		return nil, fmt.Errorf("invalid decimal")
	}
	n, ok := new(big.Rat).SetString(v)
	if !ok {
		return nil, fmt.Errorf("invalid decimal")
	}
	return n, nil
}
func copyRat(v *big.Rat) *big.Rat { return new(big.Rat).Set(v) }

func (b *Book) Buy(origin, quantity, price, fee string) error {
	q, err := number(quantity)
	if err != nil || q.Sign() <= 0 {
		return fmt.Errorf("positive quantity required")
	}
	p, err := number(price)
	if err != nil || p.Sign() < 0 {
		return fmt.Errorf("nonnegative price required")
	}
	f, err := number(fee)
	if err != nil || f.Sign() < 0 {
		return fmt.Errorf("nonnegative fee required")
	}
	return b.buyCost(origin, q, new(big.Rat).Add(new(big.Rat).Mul(q, p), f))
}

// BuyCost uses the actual clean settlement amount, including allocated fees.
func (b *Book) BuyCost(origin, quantity, cost string) error {
	q, err := number(quantity)
	if err != nil || q.Sign() <= 0 {
		return fmt.Errorf("positive quantity required")
	}
	c, err := number(cost)
	if err != nil || c.Sign() < 0 {
		return fmt.Errorf("nonnegative cost required")
	}
	return b.buyCost(origin, q, c)
}
func (b *Book) buyCost(origin string, q, cost *big.Rat) error {
	if origin == "" || b.origins[origin] {
		return fmt.Errorf("unique origin required")
	}
	if b.origins == nil {
		b.origins = map[string]bool{}
	}
	b.origins[origin] = true
	b.lots = append(b.lots, Lot{origin, q, cost})
	return nil
}

// Sell is atomic on validation failure. Partial lot cost remains rational even
// when allocation has a recurring decimal. No intermediate rounding is applied.
func (b *Book) Sell(quantity, price, fee string) ([]Match, *big.Rat, error) {
	q, err := number(quantity)
	if err != nil || q.Sign() <= 0 {
		return nil, nil, fmt.Errorf("positive quantity required")
	}
	p, err := number(price)
	if err != nil || p.Sign() < 0 {
		return nil, nil, fmt.Errorf("nonnegative price required")
	}
	f, err := number(fee)
	if err != nil || f.Sign() < 0 {
		return nil, nil, fmt.Errorf("nonnegative fee required")
	}
	return b.sellProceeds(q, new(big.Rat).Sub(new(big.Rat).Mul(q, p), f))
}

// SellProceeds accepts actual proceeds, avoiding reconstruction from a rounded
// average broker price. Negative net clean proceeds can arise from large fees.
func (b *Book) SellProceeds(quantity, proceeds string) ([]Match, *big.Rat, error) {
	q, err := number(quantity)
	if err != nil || q.Sign() <= 0 {
		return nil, nil, fmt.Errorf("positive quantity required")
	}
	p, err := number(proceeds)
	if err != nil {
		return nil, nil, err
	}
	return b.sellProceeds(q, p)
}
func (b *Book) sellProceeds(q, proceeds *big.Rat) ([]Match, *big.Rat, error) {
	total, _ := b.Totals()
	if total.Cmp(q) < 0 {
		return nil, nil, fmt.Errorf("short sale is not supported")
	}
	remaining := copyRat(q)
	cost := new(big.Rat)
	matches := []Match{}
	for i := range b.lots {
		lot := &b.lots[i]
		if remaining.Sign() == 0 {
			break
		}
		if lot.Quantity.Sign() == 0 {
			continue
		}
		take := copyRat(remaining)
		if take.Cmp(lot.Quantity) > 0 {
			take.Set(lot.Quantity)
		}
		allocated := new(big.Rat).Mul(lot.Cost, new(big.Rat).Quo(take, lot.Quantity))
		matches = append(matches, Match{lot.OriginID, copyRat(take), copyRat(allocated)})
		lot.Quantity.Sub(lot.Quantity, take)
		lot.Cost.Sub(lot.Cost, allocated)
		remaining.Sub(remaining, take)
		cost.Add(cost, allocated)
	}
	return matches, proceeds.Sub(proceeds, cost), nil
}

// Split accepts an explicitly confirmed new:old ratio, including reverse splits.
// Fractional quantities remain exact; cash-in-lieu is a separate ledger event.
func (b *Book) Split(newUnits, oldUnits string) error {
	n, err := number(newUnits)
	if err != nil || n.Sign() <= 0 {
		return fmt.Errorf("positive new units required")
	}
	d, err := number(oldUnits)
	if err != nil || d.Sign() <= 0 {
		return fmt.Errorf("positive old units required")
	}
	ratio := new(big.Rat).Quo(n, d)
	for i := range b.lots {
		b.lots[i].Quantity.Mul(b.lots[i].Quantity, ratio)
	}
	return nil
}

func (b *Book) Totals() (quantity, cost *big.Rat) {
	quantity, cost = new(big.Rat), new(big.Rat)
	for _, lot := range b.lots {
		quantity.Add(quantity, lot.Quantity)
		cost.Add(cost, lot.Cost)
	}
	return
}

func (b *Book) Lots() []Lot {
	result := make([]Lot, 0, len(b.lots))
	for _, lot := range b.lots {
		if lot.Quantity.Sign() > 0 {
			result = append(result, Lot{lot.OriginID, copyRat(lot.Quantity), copyRat(lot.Cost)})
		}
	}
	return result
}

// Amortize releases the same fraction of every remaining lot's cost, retaining
// quantities and acquisition identities. Principal is distinct from income.
func (b *Book) Amortize(before, after, principal string) (*big.Rat, error) {
	old, err := number(before)
	if err != nil || old.Sign() <= 0 {
		return nil, fmt.Errorf("positive prior nominal required")
	}
	next, err := number(after)
	if err != nil || next.Sign() <= 0 || next.Cmp(old) >= 0 {
		return nil, fmt.Errorf("lower positive nominal required")
	}
	cash, err := number(principal)
	if err != nil || cash.Sign() < 0 {
		return nil, fmt.Errorf("nonnegative principal required")
	}
	q, _ := b.Totals()
	if q.Sign() == 0 {
		return nil, fmt.Errorf("amortization requires position")
	}
	retained := new(big.Rat).Quo(next, old)
	released := new(big.Rat)
	for i := range b.lots {
		lot := &b.lots[i]
		remaining := new(big.Rat).Mul(lot.Cost, retained)
		released.Add(released, new(big.Rat).Sub(lot.Cost, remaining))
		lot.Cost = remaining
	}
	return new(big.Rat).Sub(cash, released), nil
}

// RecoverCosts releases explicitly identified lot costs without selling units.
// Validate the complete allocation before mutating any lot.
func (b *Book) RecoverCosts(allocations map[string]string) (*big.Rat, error) {
	amounts := map[int]*big.Rat{}
	total := new(big.Rat)
	for origin, raw := range allocations {
		amount, err := number(raw)
		if raw != "all" && (err != nil || amount.Sign() <= 0) {
			return nil, fmt.Errorf("positive recovery required")
		}
		found := false
		for i, lot := range b.lots {
			if lot.OriginID != origin {
				continue
			}
			found = true
			if raw == "all" {
				amount = copyRat(lot.Cost)
			}
			if lot.Quantity.Sign() == 0 || amount.Cmp(lot.Cost) > 0 {
				return nil, fmt.Errorf("recovery exceeds remaining lot cost")
			}
			amounts[i] = amount
			total.Add(total, amount)
			break
		}
		if !found {
			return nil, fmt.Errorf("recovery lot not found")
		}
	}
	for i, amount := range amounts {
		b.lots[i].Cost.Sub(b.lots[i].Cost, amount)
	}
	return total, nil
}

// Clone owns all values and retains consumed origin reservations.
func (b *Book) Clone() *Book {
	result := &Book{origins: map[string]bool{}}
	for id, used := range b.origins {
		result.origins[id] = used
	}
	for _, lot := range b.lots {
		result.lots = append(result.lots, Lot{lot.OriginID, copyRat(lot.Quantity), copyRat(lot.Cost)})
	}
	return result
}

// BuyCostExact preserves reviewed cross-currency cost without an intermediate
// decimal rounding. Quantity follows the ordinary decimal contract.
func (b *Book) BuyCostExact(origin, quantity string, cost *big.Rat) error {
	q, e := number(quantity)
	if e != nil || q.Sign() <= 0 || cost == nil || cost.Sign() < 0 {
		return fmt.Errorf("invalid exact lot cost")
	}
	return b.buyCost(origin, q, copyRat(cost))
}
func (b *Book) SellProceedsExact(quantity string, proceeds *big.Rat) ([]Match, *big.Rat, error) {
	q, e := number(quantity)
	if e != nil || q.Sign() <= 0 || proceeds == nil {
		return nil, nil, fmt.Errorf("invalid exact proceeds")
	}
	return b.sellProceeds(q, copyRat(proceeds))
}
