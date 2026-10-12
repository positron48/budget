package investment

import (
	"context"
	d "github.com/positron48/budget/internal/domain"
	"math/big"
	"strings"
	"time"
)

func normalizeLotConversion(o *d.InvestmentOperation) error {
	if o.LotConversion == nil {
		return nil
	}
	if o.Kind != "buy" && o.Kind != "sell" {
		return invalid("lot conversion only applies to buy/sell")
	}
	terms := *o.LotConversion
	var e error
	terms.Lots, e = normalize(terms.Lots, 28, 10)
	if e != nil || terms.Lots == "0" {
		return invalid("positive lot count required")
	}
	terms.LotSize, e = normalize(terms.LotSize, 28, 10)
	if e != nil || terms.LotSize == "0" {
		return invalid("positive historical lot size required")
	}
	if terms.MappingVersion < 1 || terms.TermsID == "" || len(terms.TermsID) > 250 || terms.Provider == "" || len(terms.Provider) > 100 || terms.AccessScope == "" || len(terms.AccessScope) > 100 || !validID(terms.InstrumentID) || strings.TrimSpace(terms.Evidence) == "" || len(terms.Evidence) > 2000 || terms.From.IsZero() || terms.From.Nanosecond()%1000 != 0 || (!terms.To.IsZero() && (!terms.To.After(terms.From) || terms.To.Nanosecond()%1000 != 0)) {
		return invalid("confirmed historical lot terms required")
	}
	if o.OccurredAt.Before(terms.From) || (!terms.To.IsZero() && !o.OccurredAt.Before(terms.To)) {
		return invalid("lot terms do not cover trade time")
	}
	product := new(big.Rat).Mul(rational(terms.Lots), rational(terms.LotSize))
	quantity := decimalString(product)
	if rational(quantity).Cmp(product) != 0 {
		return invalid("lot conversion exceeds quantity precision")
	}
	quantity, e = normalize(quantity, 28, 10)
	if e != nil || quantity == "0" {
		return invalid("lot conversion exceeds quantity precision")
	}
	if o.Quantity != "0" && rational(o.Quantity).Cmp(product) != 0 {
		return invalid("quantity contradicts lot conversion")
	}
	o.Quantity = quantity
	terms.From = terms.From.UTC()
	terms.To = terms.To.UTC()
	o.LotConversion = &terms
	return nil
}

type LotQuantityRequest struct {
	InstrumentKey, Lots string
	OccurredAt          time.Time
	Timing              *d.InvestmentOperationTiming
}
type LotQuantityPreview struct {
	Quantity    string
	Conversion  *d.InvestmentLotConversion
	EffectiveAt time.Time
}
type lotQuantityRepo interface {
	PreviewLotQuantity(context.Context, string, string, LotQuantityRequest) (LotQuantityPreview, error)
}

func (s *Service) PreviewLotQuantity(ctx context.Context, t, u string, q LotQuantityRequest) (LotQuantityPreview, error) {
	if e := s.authorize(ctx, t, u, false); e != nil {
		return LotQuantityPreview{}, e
	}
	if len(strings.TrimSpace(q.InstrumentKey)) == 0 || len(q.InstrumentKey) > 200 {
		return LotQuantityPreview{}, d.ErrInvestmentInvalid
	}
	lots, e := normalize(q.Lots, 28, 10)
	if e != nil || lots == "0" {
		return LotQuantityPreview{}, d.ErrInvestmentInvalid
	}
	q.Lots = lots
	if q.Timing != nil {
		terms := *q.Timing
		terms.SettlementDate = ""
		terms.SourceDate = ""
		q.Timing = &terms
	}
	op := d.InvestmentOperation{Kind: "buy", OccurredAt: q.OccurredAt, Timing: q.Timing}
	if e = normalizeOperationTiming(&op); e != nil {
		return LotQuantityPreview{}, e
	}
	if op.OccurredAt.IsZero() || op.OccurredAt.Year() < 1900 || op.OccurredAt.Year() > 2200 {
		return LotQuantityPreview{}, d.ErrInvestmentInvalid
	}
	q.OccurredAt = op.OccurredAt.UTC().Truncate(time.Microsecond)
	repo, ok := s.repo.(lotQuantityRepo)
	if !ok {
		return LotQuantityPreview{}, d.ErrInvestmentPrecondition
	}
	return repo.PreviewLotQuantity(ctx, t, u, q)
}
