package investmentimport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
)

// FinancialMapping is an explicit interpretation of one immutable source.
// It produces review candidates only; no journal or source links are written.
type FinancialMapping struct {
	SplitEvidence          map[int]SplitEvidence         `json:"split_evidence,omitempty"`
	DateOnlyByRecord       map[int]bool                  `json:"date_only_by_record,omitempty"`
	SettlementDateByRecord map[int]string                `json:"settlement_date_by_record,omitempty"`
	InstrumentIdentities   map[string]InstrumentIdentity `json:"instrument_identities,omitempty"`
	CashEvidence           map[int]CashEvidence          `json:"cash_evidence,omitempty"`
	IncomeEvidence         map[int]IncomeEvidence        `json:"income_evidence,omitempty"`
	TradeEvidence          map[int]TradeEvidence         `json:"trade_evidence,omitempty"`
	SourceSHA256           string                        `json:"source_sha256"`
	Timezone               string                        `json:"timezone"`
	SettlementPolicy       string                        `json:"settlement_policy"` // assume_trade_time, or explicit
	LedgerByRecord         map[int]string                `json:"ledger_by_record"`
	SettlementByRecord     map[int]time.Time             `json:"settlement_by_record"`
	DuplicateReview        map[int]string                `json:"duplicate_review"`
	Rules                  []FinancialRule               `json:"rules"`
}
type FinancialRule struct {
	Event, Symbol, Exchange, Currency string
	InstrumentKey, Kind               string
	AmountBasis                       string // price, quantity, quantity_price, fee_tax
	QuantityFactor                    string // required for trades; source lots to security units
	AmountFactor                      string // explicit multiplier after AmountBasis, e.g. nominal/100
	AccruedBasis                      string // zero_only, total, per_source_quantity, per_security_unit
	FeePolicy                         string // zero_only, same_currency, native_currency; missing FeeCurrency follows trade currency
	IncomeBasis                       string // received preserves the supplied payout, without tax accounting
	MoneyRounding                     string // empty: exact; 12dp_half_away: explicit tracker import policy
	Reason                            string
}
type FinancialCandidate struct {
	Record       int
	SourceRowID  string
	Operation    *d.InvestmentOperation
	ReviewReason string
	Issues       []string
}
type FinancialMappingReport struct {
	Version                   int
	SourceSHA256, Hash, State string
	Mapping                   FinancialMapping
	Rows                      []FinancialCandidate
	Ready, Unresolved         int
}

func MapSnowballFinancial(input io.Reader, mapping FinancialMapping) (FinancialMappingReport, error) {
	p, err := PreviewSnowball(input, true)
	if err != nil {
		return FinancialMappingReport{}, err
	}
	out := FinancialMappingReport{Version: 1, SourceSHA256: p.SourceSHA256, State: "review_required", Mapping: mapping, Rows: []FinancialCandidate{}}
	if mapping.SourceSHA256 != p.SourceSHA256 {
		return out, fmt.Errorf("mapping source checksum does not match")
	}
	if mapping.Timezone == "" || mapping.Timezone == "Local" {
		return out, fmt.Errorf("explicit IANA timezone required")
	}
	location, err := time.LoadLocation(mapping.Timezone)
	if err != nil {
		return out, fmt.Errorf("invalid mapping timezone")
	}
	if mapping.SettlementPolicy != "assume_trade_time" && mapping.SettlementPolicy != "explicit" {
		return out, fmt.Errorf("explicit settlement policy required")
	}
	if len(mapping.SplitEvidence) > MaxSourceRows || len(mapping.DateOnlyByRecord) > MaxSourceRows || len(mapping.SettlementDateByRecord) > MaxSourceRows || len(mapping.InstrumentIdentities) > MaxSourceRows || len(mapping.CashEvidence) > MaxSourceRows || len(mapping.IncomeEvidence) > MaxSourceRows || len(mapping.TradeEvidence) > MaxSourceRows || len(mapping.Rules) > 2000 || len(mapping.LedgerByRecord) > MaxSourceRows || len(mapping.SettlementByRecord) > MaxSourceRows || len(mapping.DuplicateReview) > MaxSourceRows {
		return out, fmt.Errorf("financial mapping exceeds limits")
	}
	for record := range mapping.SplitEvidence {
		if record < 1 || record > len(p.Rows) {
			return out, fmt.Errorf("split evidence references missing source row")
		}
	}
	positions := map[string]string{}
	for uid, identity := range mapping.InstrumentIdentities {
		parsed, err := uuid.Parse(uid)
		if err != nil || parsed.String() != uid {
			return out, fmt.Errorf("invalid instrument identity UID")
		}
		if _, err := mapping.instrumentKey(uid, identity.FIGI, identity.PositionUID); err != nil {
			return out, err
		}
		if figi, ok := positions[identity.PositionUID]; ok && figi != identity.FIGI {
			return out, fmt.Errorf("conflicting FIGI for instrument position")
		}
		positions[identity.PositionUID] = identity.FIGI
	}
	for record := range mapping.LedgerByRecord {
		if record < 1 || record > len(p.Rows) {
			return out, fmt.Errorf("ledger mapping references missing source row")
		}
	}
	for record := range mapping.SettlementByRecord {
		if record < 1 || record > len(p.Rows) {
			return out, fmt.Errorf("settlement mapping references missing source row")
		}
	}
	for record, reason := range mapping.DuplicateReview {
		if record < 1 || record > len(p.Rows) || len(reason) > 2000 {
			return out, fmt.Errorf("invalid duplicate review")
		}
	}
	seenEvidence := map[string]bool{}
	for record, evidence := range mapping.TradeEvidence {
		identity := evidence.BrokerAccountID + "\x00" + evidence.BrokerOperationID
		if record < 1 || record > len(p.Rows) || seenEvidence[identity] {
			return out, fmt.Errorf("duplicate or invalid broker evidence reference")
		}
		seenEvidence[identity] = true
	}
	for record, evidence := range mapping.IncomeEvidence {
		if record < 1 || record > len(p.Rows) {
			return out, fmt.Errorf("invalid income evidence reference")
		}
		if _, exists := mapping.TradeEvidence[record]; exists {
			return out, fmt.Errorf("conflicting broker evidence for source row")
		}
		for _, op := range []IncomeBrokerOperation{evidence.Credit, evidence.Tax} {
			if evidence.TaxReviewRequired && op == (IncomeBrokerOperation{}) {
				continue
			}
			identity := op.AccountID + "\x00" + op.ID
			if seenEvidence[identity] {
				return out, fmt.Errorf("duplicate broker evidence reference")
			}
			seenEvidence[identity] = true
		}
	}
	for record, evidence := range mapping.CashEvidence {
		_, trade := mapping.TradeEvidence[record]
		_, income := mapping.IncomeEvidence[record]
		identity := evidence.Operation.AccountID + "\x00" + evidence.Operation.ID
		if record < 1 || record > len(p.Rows) || trade || income || seenEvidence[identity] {
			return out, fmt.Errorf("duplicate or conflicting cash evidence")
		}
		seenEvidence[identity] = true
	}
	for _, rule := range mapping.Rules {
		if rule.MoneyRounding != "" && rule.MoneyRounding != "12dp_half_away" {
			return out, fmt.Errorf("unsupported money rounding policy")
		}
		if strings.TrimSpace(rule.Reason) == "" || len(rule.Reason) > 2000 {
			return out, fmt.Errorf("every financial rule requires a review reason")
		}
		factor, err := NormalizeSourceDecimal(rule.AmountFactor)
		if err != nil || decimalSign(factor) <= 0 {
			return out, fmt.Errorf("positive exact amount factor required")
		}

	}
	for _, row := range p.Rows {
		item := FinancialCandidate{Record: row.Record, SourceRowID: row.SourceRowID, Issues: []string{}}
		issue := func(code string) { item.Issues = append(item.Issues, code) }
		ledger := mapping.LedgerByRecord[row.Record]
		id, e := uuid.Parse(ledger)
		if e != nil || id.String() != ledger {
			issue("ledger_mapping_required")
		}
		rules := []FinancialRule{}
		for _, rule := range mapping.Rules {
			if rule.Event == row.Event && rule.Symbol == row.Instrument.Symbol && rule.Exchange == row.Instrument.Exchange && rule.Currency == row.Currency {
				rules = append(rules, rule)
			}
		}
		evidence, hasEvidence := mapping.TradeEvidence[row.Record]
		incomeEvidence, hasIncomeEvidence := mapping.IncomeEvidence[row.Record]
		cashEvidence, hasCashEvidence := mapping.CashEvidence[row.Record]
		splitEvidence, hasSplitEvidence := mapping.SplitEvidence[row.Record]
		if !hasSplitEvidence && !hasEvidence && !hasIncomeEvidence && !hasCashEvidence && len(rules) == 0 {
			issue("financial_rule_required")
		} else if !hasSplitEvidence && !hasEvidence && !hasIncomeEvidence && !hasCashEvidence && len(rules) > 1 {
			issue("ambiguous_financial_rules")
		}
		if row.SuppressCash {
			issue("cash_suppression_requires_manual_operation")
		}
		if len(row.IdenticalRecords) > 0 && strings.TrimSpace(mapping.DuplicateReview[row.Record]) == "" {
			issue("identical_rows_require_review")
		}
		var occurred time.Time
		// An explicitly reviewed split date does not claim an execution clock time.
		if !hasSplitEvidence || !mapping.DateOnlyByRecord[row.Record] {
			occurred, e = parseUnambiguousWallTime(row.WallTime, location)
			if e != nil {
				issue("ambiguous_or_invalid_wall_time")
			}
		}
		if hasSplitEvidence {
			item.ReviewReason = splitEvidence.Reason
			if hasEvidence || hasIncomeEvidence || hasCashEvidence || len(rules) > 0 {
				issue("ambiguous_financial_evidence")
			}
			op, err := evidenceSplit(row, splitEvidence, ledger, mapping)
			if err != nil {
				issue(err.Error())
			} else if len(item.Issues) == 0 {
				item.Operation = &op
				out.Ready++
			}
		} else if hasCashEvidence {
			item.ReviewReason = cashEvidence.Reason
			op, err := evidenceCash(row, cashEvidence, ledger, occurred, location, mapping)
			if err != nil {
				issue(err.Error())
			} else if len(item.Issues) == 0 {
				item.Operation = &op
				out.Ready++
			}
		} else if hasIncomeEvidence {
			item.ReviewReason = incomeEvidence.Reason
			op, err := evidenceIncome(row, incomeEvidence, ledger, occurred, location, mapping)
			if err != nil {
				issue(err.Error())
			} else if len(item.Issues) == 0 {
				item.Operation = &op
				out.Ready++
			}
		} else if hasEvidence {
			item.ReviewReason = evidence.Reason
			op, err := evidenceTrade(row, evidence, ledger, occurred, location, mapping)
			if err != nil {
				issue(err.Error())
			} else if len(item.Issues) == 0 {
				item.Operation = &op
				out.Ready++
			}
		} else if len(rules) == 1 {
			rule := rules[0]
			item.ReviewReason = rule.Reason
			op := d.InvestmentOperation{Timing: mappedTiming(row, mapping), LedgerID: ledger, Kind: rule.Kind, InstrumentKey: rule.InstrumentKey, Currency: row.Currency, OccurredAt: occurred}
			if mapping.SettlementPolicy == "explicit" {
				op.SettlementAt = mapping.SettlementByRecord[row.Record]
				if op.SettlementAt.IsZero() && mapping.SettlementDateByRecord[row.Record] == "" {
					issue("settlement_time_required")
				}
			}
			value := func(key string) string {
				v := row.Values[key]
				if v == "" {
					return "0"
				}
				return v
			}
			for _, key := range []string{"Price", "Quantity", "FeeTax", "NKD"} {
				if key == "FeeTax" && rule.IncomeBasis == "received" && (rule.Kind == "dividend" || rule.Kind == "coupon" || rule.Kind == "repo_income") {
					continue
				}
				if decimalSign(value(key)) < 0 {
					issue("negative_" + key)
				}
			}
			switch rule.AmountBasis {
			case "price":
				op.Amount = value("Price")
			case "quantity":
				op.Amount = value("Quantity")
			case "quantity_price":
				op.Amount = exactProduct(value("Quantity"), value("Price"))
			case "fee_tax":
				op.Amount = value("FeeTax")
			default:
				issue("amount_basis_required")
			}
			factor, _ := NormalizeSourceDecimal(rule.AmountFactor)
			op.Amount = exactProduct(op.Amount, factor)
			switch rule.Kind {
			case "buy", "sell", "redemption":
				if rule.AmountBasis == "fee_tax" {
					issue("trade_principal_basis_required")
				}
				if rule.Kind == "redemption" && rule.Event != "REPAYMENT" || rule.Kind != "redemption" && (rule.Event != "BUY" && rule.Event != "SELL" || rule.Kind != strings.ToLower(rule.Event)) {
					issue("event_kind_mismatch")
				}
				q, e := NormalizeSourceDecimal(rule.QuantityFactor)
				if e != nil || decimalSign(q) <= 0 {
					issue("quantity_factor_required")
				} else {
					op.Quantity = exactProduct(value("Quantity"), q)
				}
				switch rule.AccruedBasis {
				case "zero_only":
					if decimalSign(value("NKD")) != 0 {
						issue("accrued_basis_required")
					}
				case "total":
					op.Accrued = value("NKD")
				case "per_security_unit":
					op.Accrued = exactProduct(value("NKD"), op.Quantity)
				case "per_source_quantity":
					op.Accrued = exactProduct(value("NKD"), value("Quantity"))
				default:
					issue("accrued_basis_required")
				}
				op.Fee = value("FeeTax")
			case "dividend", "coupon", "repo_income":
				if rule.AmountBasis == "fee_tax" {
					issue("income_amount_basis_required")
				}
				allowedIncome := rule.Event == "DIVIDEND" && rule.Kind != "repo_income" || rule.Event == "CASH_GAIN" && rule.Kind == "repo_income"
				if !allowedIncome {
					issue("event_kind_mismatch")
				}
				op.Tax = value("FeeTax")
				if rule.IncomeBasis == "received" {
					op.Tax = "0"
				} else if rule.IncomeBasis == "net" {
					op.Amount = exactSum(op.Amount, op.Tax)
				} else if rule.IncomeBasis != "gross" {
					issue("income_basis_required")
				}
				if decimalSign(value("NKD")) != 0 {
					issue("unexpected_accrued_interest")
				}
			case "split":
				if rule.Event != "SPLIT" || decimalSign(value("Quantity")) != 0 || decimalSign(value("FeeTax")) != 0 || decimalSign(value("NKD")) != 0 {
					issue("event_kind_mismatch")
				}
				op.Currency, op.Amount, op.SplitNew, op.SplitOld = "", "0", value("Price"), "1"
			case "deposit", "withdrawal", "fee", "tax", "cash_interest":
				allowed := rule.Event == "CASH_GAIN" && rule.Kind == "cash_interest" || rule.Event == "CASH_IN" && rule.Kind == "deposit" || rule.Event == "FEE" && rule.Kind == "fee" || rule.Event == "TAX" && rule.Kind == "tax"
				if !allowed {
					issue("event_kind_requires_manual_review")
				}
				if rule.InstrumentKey != "" || row.Instrument.Symbol != "" {
					issue("cash_instrument_requires_review")
				}
				if decimalSign(value("NKD")) != 0 {
					issue("unexpected_accrued_interest")
				}
				if rule.AmountBasis != "fee_tax" && decimalSign(value("FeeTax")) != 0 {
					issue("unallocated_fee_or_tax")
				}
				if rule.AmountBasis == "fee_tax" && decimalSign(value("Price")) != 0 {
					issue("unallocated_price_amount")
				}
			default:
				issue("event_kind_requires_manual_review")
			}
			if rule.FeePolicy != "same_currency" && rule.FeePolicy != "native_currency" && rule.FeePolicy != "zero_only" {
				issue("fee_policy_required")
			}
			receivedPayout := rule.IncomeBasis == "received" && (rule.Kind == "dividend" || rule.Kind == "coupon" || rule.Kind == "repo_income")
			if decimalSign(value("FeeTax")) != 0 && !receivedPayout {
				if rule.FeePolicy == "zero_only" {
					issue("unexpected_fee_or_tax")
				}
				if row.FeeCurrency != "" && row.FeeCurrency != row.Currency {
					if rule.FeePolicy == "native_currency" && (rule.Kind == "buy" || rule.Kind == "sell") {
						op.FeeCurrency = row.FeeCurrency
					} else {
						issue("cross_currency_fee_requires_manual_operation")
					}
				}
			}
			if rule.MoneyRounding == "12dp_half_away" {
				// Quantities and split ratios remain exact. Round monetary totals
				// only after arithmetic; the immutable CSV retains every digit.
				for _, value := range []*string{&op.Amount, &op.Accrued, &op.Fee, &op.Tax} {
					if number, ok := new(big.Rat).SetString(*value); ok {
						*value = strings.TrimRight(strings.TrimRight(number.FloatString(12), "0"), ".")
					}
				}
			}
			applyMappedDateOnly(&op, row)
			if e := inv.ValidateOperation(&op); e != nil {
				issue("financial_operation_invalid")
			}
			if len(item.Issues) == 0 {
				item.Operation = &op
				out.Ready++
			}
		}
		if item.Operation == nil {
			out.Unresolved++
		}
		out.Rows = append(out.Rows, item)
	}
	// Retain source order, including identical rows. Journal preview is a separate
	// required step and will reject an incomplete or inconsistent cash history.
	raw, err := json.Marshal(out)
	if err != nil {
		return out, err
	}
	hash := sha256.Sum256(raw)
	out.Hash = hex.EncodeToString(hash[:])
	return out, nil
}
func exactSum(a, b string) string {
	// Both inputs are terminating decimals. Reuse exact multiplication's scale
	// handling by selecting the larger input scale for rational serialization.
	x, ok := new(big.Rat).SetString(a)
	if !ok {
		return ""
	}
	y, ok := new(big.Rat).SetString(b)
	if !ok {
		return ""
	}
	scale := 0
	for _, v := range []string{a, b} {
		if i := strings.IndexByte(v, '.'); i >= 0 && len(v)-i-1 > scale {
			scale = len(v) - i - 1
		}
	}
	out, _ := NormalizeSourceDecimal(x.Add(x, y).FloatString(scale))
	return out
}
func parseUnambiguousWallTime(value string, location *time.Location) (time.Time, error) {
	const layout = "2006-01-02 15:04:05"
	parsed, err := time.ParseInLocation(layout, value, location)
	if err != nil || parsed.Format(layout) != value {
		return time.Time{}, fmt.Errorf("invalid wall time")
	}
	offsets := map[int]bool{}
	for _, delta := range []time.Duration{-48 * time.Hour, -24 * time.Hour, 0, 24 * time.Hour, 48 * time.Hour} {
		_, offset := parsed.Add(delta).Zone()
		offsets[offset] = true
	}
	_, selected := parsed.Zone()
	matches := []time.Time{}
	for offset := range offsets {
		candidate := parsed.Add(time.Duration(selected-offset) * time.Second)
		if candidate.In(location).Format(layout) == value {
			matches = append(matches, candidate)
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].Before(matches[j]) })
	if len(matches) != 1 {
		return time.Time{}, fmt.Errorf("ambiguous wall time")
	}
	return matches[0].UTC(), nil
}
