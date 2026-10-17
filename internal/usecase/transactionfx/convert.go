// Package transactionfx values budget operations without changing their journal entries.
package transactionfx

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/positron48/budget/internal/domain"
	"github.com/positron48/budget/internal/usecase/fximport"
)

var ErrInvalidRate = errors.New("invalid fx rate")
var ErrOverflow = errors.New("money overflow")

type Rates interface {
	GetRateAsOf(context.Context, string, string, time.Time) (string, string, error)
}

// Converter caches each source/target/calendar-day quote for the whole request.
// The repository selects the last quote on or before the Moscow CBR date.
type Converter struct {
	rates Rates
	cache map[string]string
}

func New(rates Rates) *Converter { return &Converter{rates: rates, cache: make(map[string]string)} }

// Prepare batches source currencies per date when the repository supports it.
func (c *Converter) Prepare(ctx context.Context, items []domain.Transaction, target string) error {
	batch, ok := c.rates.(interface {
		GetAssetRates(context.Context, []string, string, time.Time) ([]domain.AssetFxRate, error)
	})
	if !ok {
		return nil
	}
	groups := make(map[time.Time]map[string]bool)
	for _, tx := range items {
		amount := tx.Amount
		if amount.CurrencyCode == "" {
			amount = tx.BaseAmount
		}
		if amount.CurrencyCode == target || amount.MinorUnits == 0 || tx.BaseAmount.CurrencyCode == target && tx.Fx != nil {
			continue
		}
		date := fximport.MoscowDate(tx.OccurredAt)
		key := amount.CurrencyCode + "/" + target + "/" + date.Format(time.DateOnly)
		if _, found := c.cache[key]; found {
			continue
		}
		if groups[date] == nil {
			groups[date] = make(map[string]bool)
		}
		groups[date][amount.CurrencyCode] = true
	}
	for date, group := range groups {
		codes := make([]string, 0, len(group))
		for code := range group {
			codes = append(codes, code)
		}
		rates, err := batch.GetAssetRates(ctx, codes, target, date)
		if err != nil {
			return err
		}
		for _, rate := range rates {
			c.cache[rate.From+"/"+target+"/"+date.Format(time.DateOnly)] = rate.RateDecimal
		}
	}
	return nil
}

func (c *Converter) Convert(ctx context.Context, tx domain.Transaction, target string) (domain.Money, error) {
	amount := tx.Amount
	// Older internal callers may only supply the stored base amount.
	if amount.CurrencyCode == "" {
		amount = tx.BaseAmount
	}
	if amount.CurrencyCode == target {
		return amount, nil
	}
	// Preserve the recorded historical conversion to the base currency.
	if tx.BaseAmount.CurrencyCode == target && tx.Fx != nil {
		return tx.BaseAmount, nil
	}
	if amount.MinorUnits == 0 {
		return domain.Money{CurrencyCode: target}, nil
	}
	date := fximport.MoscowDate(tx.OccurredAt)
	key := amount.CurrencyCode + "/" + target + "/" + date.Format(time.DateOnly)
	rate, ok := c.cache[key]
	if !ok {
		var err error
		rate, _, err = c.rates.GetRateAsOf(ctx, amount.CurrencyCode, target, date)
		if err != nil {
			return domain.Money{}, fmt.Errorf("FX %s: %w", key, err)
		}
		c.cache[key] = rate
	}
	minor, err := ConvertMinor(amount.MinorUnits, rate)
	return domain.Money{CurrencyCode: target, MinorUnits: minor}, err
}

// ConvertMinor rounds exactly once, half away from zero, checking int64 bounds.
func ConvertMinor(minor int64, rate string) (int64, error) {
	r, ok := new(big.Rat).SetString(rate)
	if !ok || r.Sign() <= 0 {
		return 0, ErrInvalidRate
	}
	value := new(big.Rat).Mul(new(big.Rat).SetInt64(minor), r)
	p := new(big.Int).Abs(value.Num())
	q := value.Denom()
	p.Add(p, new(big.Int).Quo(new(big.Int).Set(q), big.NewInt(2)))
	p.Quo(p, q)
	if minor < 0 {
		p.Neg(p)
	}
	if !p.IsInt64() {
		return 0, ErrOverflow
	}
	return p.Int64(), nil
}

func Add(a, b int64) (int64, error) {
	sum := new(big.Int).Add(big.NewInt(a), big.NewInt(b))
	if !sum.IsInt64() {
		return 0, ErrOverflow
	}
	return sum.Int64(), nil
}
