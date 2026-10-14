package investment

import (
	"fmt"
	"math/big"
	"regexp"
	"slices"
	"strings"
	"time"

	d "github.com/positron48/budget/internal/domain"
)

const MaxOperations = 10000

var decimal = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?$`)

func invalid(message string) error { return fmt.Errorf("%w: %s", d.ErrInvestmentInvalid, message) }
func rational(v string) *big.Rat   { n, _ := new(big.Rat).SetString(v); return n }
func normalize(v string, precision, scale int) (string, error) {
	if v == "" {
		v = "0"
	}
	if len(v) > precision+2 || !decimal.MatchString(v) {
		return "", invalid("invalid decimal")
	}
	parts := strings.Split(v, ".")
	whole := len(parts[0])
	if parts[0] == "0" {
		whole = 0
	}
	fraction := 0
	if len(parts) == 2 {
		fraction = len(strings.TrimRight(parts[1], "0"))
	}
	if whole > precision-scale || fraction > scale {
		return "", invalid("decimal precision exceeded")
	}
	if len(parts) == 2 {
		v = strings.TrimRight(strings.TrimRight(v, "0"), ".")
	}
	return v, nil
}
func ValidateOperation(o *d.InvestmentOperation) error {
	o.FeeCurrency = strings.ToUpper(strings.TrimSpace(o.FeeCurrency))
	if o.FeeCurrency != "" && (o.Kind != "buy" && o.Kind != "sell" || !tradeCurrency(o.FeeCurrency) || o.TradeFinancials != nil) {
		return invalid("native fee currency only applies to simple portfolio trades")
	}
	if err := normalizeCashDeficitReason(o); err != nil {
		return err
	}
	if e := normalizeOperationTiming(o); e != nil {
		return e
	}
	if o.TaxReviewRequired && o.Kind != "dividend" && o.Kind != "coupon" && o.Kind != "repo_income" && o.Kind != "cash_interest" {
		return invalid("tax review flag only applies to income")
	}
	if o.OccurredAt.IsZero() || o.OccurredAt.Year() < 1900 || o.OccurredAt.Year() > 2200 || len(o.Note) > 5000 {
		return invalid("invalid time or note")
	}
	o.OccurredAt = o.OccurredAt.UTC().Truncate(time.Microsecond)
	if o.SettlementAt.IsZero() {
		o.SettlementAt = o.OccurredAt
		o.SettlementAssumed = o.Kind == "buy" || o.Kind == "sell" || o.Kind == "amortization" || o.Kind == "redemption" || o.Kind == "fx"
	}
	o.SettlementAt = o.SettlementAt.UTC().Truncate(time.Microsecond)
	if o.SettlementAt.Before(o.OccurredAt) || o.SettlementAt.Sub(o.OccurredAt) > 366*24*time.Hour {
		return invalid("invalid settlement time")
	}
	o.Currency = strings.ToUpper(strings.TrimSpace(o.Currency))
	o.ToCurrency = strings.ToUpper(strings.TrimSpace(o.ToCurrency))
	o.InstrumentKey = strings.TrimSpace(o.InstrumentKey)
	if len(o.InstrumentKey) > 200 || strings.ContainsAny(o.InstrumentKey, "\r\n\t") {
		return invalid("invalid instrument key")
	}
	fields := []struct {
		v    *string
		p, s int
	}{{&o.Quantity, 28, 10}, {&o.Amount, 36, 12}, {&o.ToAmount, 36, 12}, {&o.Accrued, 36, 12}, {&o.Fee, 36, 12}, {&o.Tax, 36, 12}, {&o.SplitNew, 28, 10}, {&o.SplitOld, 28, 10}, {&o.NominalBefore, 28, 12}, {&o.NominalAfter, 28, 12}}
	for _, f := range fields {
		v, err := normalize(*f.v, f.p, f.s)
		if err != nil {
			return err
		}
		*f.v = v
	}
	if e := normalizeLotConversion(o); e != nil {
		return e
	}
	if e := normalizeTradeFinancials(o); e != nil {
		return e
	}
	zero := func(vs ...string) bool {
		for _, v := range vs {
			if v != "0" {
				return false
			}
		}
		return true
	}
	if o.Kind != "coupon" && (o.CouponAllocationConfirmed || len(o.AccruedRecoveries) > 0) {
		return invalid("accrued recovery only applies to coupons")
	}
	if len(o.AccruedRecoveries) > MaxOperations || (!o.CouponAllocationConfirmed && len(o.AccruedRecoveries) > 0) {
		return invalid("coupon allocation confirmation required")
	}
	o.AccruedRecoveries = slices.Clone(o.AccruedRecoveries)
	seen := map[string]bool{}
	recovered := new(big.Rat)
	for i := range o.AccruedRecoveries {
		a := &o.AccruedRecoveries[i]
		if a.OriginID == "" || len(a.OriginID) > 200 || seen[a.OriginID] {
			return invalid("unique recovery lot required")
		}
		seen[a.OriginID] = true
		amount, err := normalize(a.Amount, 36, 12)
		if err != nil || (!a.AllRemaining && amount == "0") || (a.AllRemaining && amount != "0") {
			return invalid("choose a positive recovery amount or the full remaining lot cost")
		}
		a.Amount = amount
		recovered.Add(recovered, rational(amount))
	}
	if recovered.Cmp(rational(o.Amount)) > 0 {
		return invalid("recovered accrued interest exceeds gross coupon")
	}
	slices.SortFunc(o.AccruedRecoveries, func(a, b d.InvestmentAccruedRecovery) int { return strings.Compare(a.OriginID, b.OriginID) })
	if o.Kind != "opening_position" && (o.OpeningCostKnown || o.OpeningAccruedKnown || !o.AcquiredAt.IsZero()) {
		return invalid("opening cost metadata only applies to opening positions")
	}
	if !o.AcquiredAt.IsZero() {
		o.AcquiredAt = o.AcquiredAt.UTC().Truncate(time.Microsecond)
		if o.AcquiredAt.After(o.OccurredAt) || o.AcquiredAt.Year() < 1900 {
			return invalid("acquisition date must not follow opening")
		}
	}
	if (o.Kind == "opening_cash" || o.Kind == "opening_position") && strings.TrimSpace(o.Note) == "" {
		return invalid("opening requires an evidence note")
	}
	if o.BudgetTransferID != "" && (!validID(o.BudgetTransferID) || o.Kind != "deposit" && o.Kind != "withdrawal") {
		return invalid("Budget cash linkage requires a deposit or withdrawal")
	}
	trade := o.Kind == "buy" || o.Kind == "sell" || o.Kind == "amortization" || o.Kind == "redemption" || o.Kind == "fx"
	if o.Kind != "split" && o.Currency != "RUB" && o.Currency != "USD" && o.Currency != "EUR" {
		return invalid("supported currencies are RUB, USD and EUR")
	}
	if o.Kind != "amortization" && o.Kind != "redemption" && !zero(o.NominalBefore, o.NominalAfter) {
		return invalid("nominal fields only apply to amortization or redemption")
	}
	if o.Kind != "fx" && (o.ToCurrency != "" || o.ToAmount != "0") {
		return invalid("target currency and amount only apply to FX")
	}
	if o.Kind != "transfer_out" && o.Kind != "transfer_in" && o.Kind != "security_transfer_out" && o.Kind != "security_transfer_in" && (o.TransferID != "" || o.CounterpartyLedgerID != "") {
		return invalid("transfer linkage only applies to transfer legs")
	}
	if o.Kind != "distribution" && o.Distribution != nil {
		return invalid("distribution terms only apply to corporate distributions")
	}
	if o.Kind != "cash_reconciliation" && o.CashReconciliation != nil {
		return invalid("cash reconciliation terms only apply to reconciliation")
	}
	switch o.Kind {
	case "cash_reconciliation":
		if o.CashReconciliation == nil || o.InstrumentKey != "" || strings.TrimSpace(o.Note) == "" || !zero(o.Quantity, o.Accrued, o.Fee, o.Tax, o.SplitNew, o.SplitOld) {
			return invalid("cash reconciliation requires settled balances and evidence note only")
		}
		terms := *o.CashReconciliation
		for _, field := range []*string{&terms.ExpectedSettled, &terms.ObservedSettled} {
			if *field == "" {
				return invalid("explicit reconciliation balances required")
			}
			value, err := normalize(*field, 36, 12)
			if err != nil {
				return err
			}
			*field = value
		}
		delta := new(big.Rat).Sub(rational(terms.ObservedSettled), rational(terms.ExpectedSettled))
		amount := decimalString(new(big.Rat).Abs(delta))
		if o.Amount != "0" && rational(o.Amount).Cmp(rational(amount)) != 0 {
			return invalid("reconciliation amount must match balance difference")
		}
		o.Amount = amount
		o.CashReconciliation = &terms
	case "distribution":
		if o.Distribution == nil || o.InstrumentKey == "" || strings.TrimSpace(o.Note) == "" || !zero(o.Quantity, o.Amount, o.Accrued, o.Fee, o.Tax, o.SplitNew, o.SplitOld) {
			return invalid("distribution requires instruments, explicit terms and evidence note, without cash")
		}
		terms := *o.Distribution
		terms.EventKey = strings.TrimSpace(terms.EventKey)
		terms.TargetInstrumentKey = strings.TrimSpace(terms.TargetInstrumentKey)
		if terms.EventKey == "" || len(terms.EventKey) > 200 || terms.TargetInstrumentKey == "" || len(terms.TargetInstrumentKey) > 200 || terms.TargetInstrumentKey == o.InstrumentKey || strings.ContainsAny(terms.EventKey+terms.TargetInstrumentKey, "\r\n\t") {
			return invalid("distinct target instrument and event identity required")
		}
		for _, field := range []*string{&terms.NewUnits, &terms.OldUnits, &terms.CostNumerator, &terms.CostDenominator} {
			value, err := normalize(*field, 28, 10)
			if err != nil {
				return err
			}
			*field = value
		}
		if terms.NewUnits == "0" || terms.OldUnits == "0" || terms.CostDenominator == "0" || rational(terms.CostNumerator).Cmp(rational(terms.CostDenominator)) > 0 || terms.ConsumeSource && terms.CostNumerator != terms.CostDenominator {
			return invalid("invalid distribution ratio or cost share")
		}
		terms.MarketValue = strings.TrimSpace(terms.MarketValue)
		terms.MarketValueEvidence = strings.TrimSpace(terms.MarketValueEvidence)
		if (terms.MarketValue != "") != (terms.MarketValueEvidence != "") || len(terms.MarketValueEvidence) > 2000 {
			return invalid("distribution market value requires separate evidence")
		}
		if terms.MarketValue != "" {
			value, err := normalize(terms.MarketValue, 28, 12)
			if err != nil {
				return err
			}
			terms.MarketValue = value
		}
		o.Distribution = &terms
	case "opening_position":
		if o.InstrumentKey == "" || o.Quantity == "0" || !zero(o.Fee, o.Tax, o.SplitNew, o.SplitOld) || (!o.OpeningCostKnown && o.Amount != "0") || (!o.OpeningAccruedKnown && o.Accrued != "0") {
			return invalid("opening position requires quantity and explicitly known or unknown costs")
		}
	case "buy", "sell":
		if o.InstrumentKey == "" || o.Quantity == "0" || o.Amount == "0" || (!zero(o.SplitNew, o.SplitOld) || o.TradeFinancials == nil && o.Tax != "0") {
			return invalid("trade requires instrument, positive quantity and clean principal")
		}
		if o.Kind == "sell" && o.TradeFinancials == nil && (o.FeeCurrency == "" || o.FeeCurrency == o.Currency) && new(big.Rat).Add(rational(o.Amount), rational(o.Accrued)).Cmp(rational(o.Fee)) < 0 {
			return invalid("sell fee exceeds proceeds")
		}
	case "amortization", "redemption":
		if o.InstrumentKey == "" || o.Amount == "0" || !zero(o.Accrued, o.Fee, o.Tax, o.SplitNew, o.SplitOld) {
			return invalid("principal event requires instrument and amount; coupon is separate")
		}
		if o.Kind == "amortization" && (o.NominalBefore == "0" || o.Quantity != "0" || o.NominalAfter == "0" || rational(o.NominalAfter).Cmp(rational(o.NominalBefore)) >= 0) {
			return invalid("amortization requires a lower positive nominal and no quantity change")
		}
		if o.Kind == "redemption" && (o.Quantity == "0" || o.NominalAfter != "0") {
			return invalid("full redemption requires closing quantity and zero remaining nominal")
		}
	case "fx":
		if o.Amount == "0" || o.ToAmount == "0" || o.Currency == o.ToCurrency || (o.ToCurrency != "RUB" && o.ToCurrency != "USD" && o.ToCurrency != "EUR") || o.InstrumentKey != "" || !zero(o.Quantity, o.Accrued, o.Tax, o.SplitNew, o.SplitOld) {
			return invalid("FX requires different supported currencies and actual positive amounts")
		}
	case "security_transfer_out", "security_transfer_in":
		if o.TransferID == "" || o.CounterpartyLedgerID == "" || o.CounterpartyLedgerID == o.LedgerID || o.InstrumentKey == "" || o.Quantity == "0" || !zero(o.Amount, o.Accrued, o.Fee, o.Tax, o.SplitNew, o.SplitOld) {
			return invalid("linked security transfer requires instrument and quantity without cash")
		}
	case "transfer_out", "transfer_in":
		if o.TransferID == "" || o.CounterpartyLedgerID == "" || o.CounterpartyLedgerID == o.LedgerID || o.Amount == "0" || o.InstrumentKey != "" || !zero(o.Quantity, o.Accrued, o.Fee, o.Tax, o.SplitNew, o.SplitOld) {
			return invalid("linked transfer requires positive cash and a different counterparty")
		}
	case "deposit", "withdrawal", "fee", "tax", "tax_refund", "opening_cash", "cash_interest":
		if o.Amount == "0" || o.InstrumentKey != "" || !zero(o.Quantity, o.Accrued, o.Fee, o.Tax, o.SplitNew, o.SplitOld) {
			return invalid("cash operation requires only positive amount")
		}
	case "dividend", "coupon", "repo_income":
		if o.InstrumentKey == "" || o.Amount == "0" || !zero(o.Quantity, o.Accrued, o.Fee, o.SplitNew, o.SplitOld) || rational(o.Tax).Cmp(rational(o.Amount)) > 0 {
			return invalid("income requires instrument and gross amount >= tax")
		}
	case "split":
		if o.InstrumentKey == "" || o.Currency != "" || o.SplitNew == "0" || o.SplitOld == "0" || !zero(o.Quantity, o.Amount, o.Accrued, o.Fee, o.Tax) {
			return invalid("split requires only instrument and positive ratio")
		}
	default:
		return invalid("unsupported operation type")
	}
	if !trade && !o.SettlementAt.Equal(o.OccurredAt) {
		return invalid("separate settlement only applies to trades")
	}
	return nil
}
