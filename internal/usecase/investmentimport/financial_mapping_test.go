package investmentimport

import (
	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	"slices"
	"strings"
	"testing"
	"time"
)

const financialHeader = "Event,Date,Symbol,Price,Quantity,Currency,FeeTax,Exchange,NKD,FeeCurrency,DoNotAdjustCash,Note\n"

func financialConfig(t *testing.T, csv string, rules ...FinancialRule) FinancialMapping {
	t.Helper()
	p, e := PreviewSnowball(strings.NewReader(csv), true)
	if e != nil {
		t.Fatal(e)
	}
	m := FinancialMapping{SourceSHA256: p.SourceSHA256, Timezone: "Europe/Moscow", SettlementPolicy: "assume_trade_time", LedgerByRecord: map[int]string{}, Rules: rules}
	ledger := uuid.NewString()
	for _, row := range p.Rows {
		m.LedgerByRecord[row.Record] = ledger
	}
	return m
}
func tradeRule() FinancialRule {
	return FinancialRule{Event: "BUY", Symbol: "TEST", Exchange: "X", Currency: "RUB", InstrumentKey: "isin:TEST", Kind: "buy", AmountBasis: "quantity_price", QuantityFactor: "10", AmountFactor: "1", AccruedBasis: "total", FeePolicy: "same_currency", Reason: "Confirmed source units from broker statement"}
}
func TestFinancialMappingExactAndExplicit(t *testing.T) {
	csv := financialHeader + "BUY,2024-01-01 12:00:00,TEST,9007199254740.123456789012,2,RUB,0.000000000001,X,1e-12,,False,\n"
	m := financialConfig(t, csv, tradeRule())
	report, e := MapSnowballFinancial(strings.NewReader(csv), m)
	if e != nil || report.Ready != 1 || report.Unresolved != 0 {
		t.Fatal(report, e)
	}
	op := report.Rows[0].Operation
	if op.Amount != "18014398509480.246913578024" || op.Quantity != "20" || op.Accrued != "0.000000000001" || op.Fee != "0.000000000001" || !op.SettlementAssumed || op.OccurredAt.Hour() != 9 {
		t.Fatal(op)
	}
	again, e := MapSnowballFinancial(strings.NewReader(csv), m)
	if e != nil || again.Hash != report.Hash {
		t.Fatal("unstable hash", e)
	}
	m.Rules[0].Reason = "Different reviewed evidence"
	changed, e := MapSnowballFinancial(strings.NewReader(csv), m)
	if e != nil || changed.Hash == report.Hash {
		t.Fatal("mapping reason not bound", e)
	}
	m.SourceSHA256 = strings.Repeat("0", 64)
	if _, e = MapSnowballFinancial(strings.NewReader(csv), m); e == nil {
		t.Fatal("different source accepted")
	}
}
func TestFinancialMappingNeverSilentlyDropsRows(t *testing.T) {
	row := "BUY,2024-01-01 12:00:00,TEST,10,2,RUB,0,X,0,,False,\n"
	csv := financialHeader + row + row + "REPAYMENT,2024-01-02 12:00:00,TEST,10,2,RUB,0,X,0,,False,\n"
	m := financialConfig(t, csv, tradeRule())
	report, e := MapSnowballFinancial(strings.NewReader(csv), m)
	if e != nil || len(report.Rows) != 3 || report.Unresolved != 3 {
		t.Fatal(report, e)
	}
	m.DuplicateReview = map[int]string{1: "Distinct execution A", 2: "Distinct execution B"}
	report, e = MapSnowballFinancial(strings.NewReader(csv), m)
	if e != nil || report.Ready != 2 || report.Unresolved != 1 || report.Rows[2].Operation != nil {
		t.Fatal(report, e)
	}
	m.Rules = append(m.Rules, tradeRule())
	report, e = MapSnowballFinancial(strings.NewReader(csv), m)
	if e != nil || report.Ready != 0 {
		t.Fatal("ambiguous rules accepted", report, e)
	}
}
func TestFinancialMappingRejectsUnrepresentedMoney(t *testing.T) {
	for _, test := range []struct {
		name, row string
		change    func(*FinancialMapping)
	}{
		{"cross currency fee", "BUY,2024-01-01 12:00:00,TEST,10,2,RUB,1,X,0,USD,False,\n", nil},
		{"suppressed cash", "BUY,2024-01-01 12:00:00,TEST,10,2,RUB,0,X,0,,True,\n", nil},
		{"precision loss", "BUY,2024-01-01 12:00:00,TEST,0.0000000000001,2,RUB,0,X,0,,False,\n", nil},
		{"negative", "BUY,2024-01-01 12:00:00,TEST,-10,2,RUB,0,X,0,,False,\n", nil},
		{"missing settlement", "BUY,2024-01-01 12:00:00,TEST,10,2,RUB,0,X,0,,False,\n", func(m *FinancialMapping) { m.SettlementPolicy = "explicit" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			csv := financialHeader + test.row
			m := financialConfig(t, csv, tradeRule())
			if test.change != nil {
				test.change(&m)
			}
			report, e := MapSnowballFinancial(strings.NewReader(csv), m)
			if e != nil || report.Ready != 0 || report.Rows[0].Operation != nil {
				t.Fatal(report, e)
			}
		})
	}
}
func TestFinancialMappingIncomeAndDST(t *testing.T) {
	csv := financialHeader + "DIVIDEND,2024-01-01 12:00:00,TEST,87,1,RUB,13,X,0,,False,\n"
	rule := FinancialRule{Event: "DIVIDEND", Symbol: "TEST", Exchange: "X", Currency: "RUB", InstrumentKey: "isin:TEST", Kind: "dividend", AmountBasis: "price", AmountFactor: "1", FeePolicy: "same_currency", IncomeBasis: "net", Reason: "Confirmed net distribution"}
	m := financialConfig(t, csv, rule)
	report, e := MapSnowballFinancial(strings.NewReader(csv), m)
	if e != nil || report.Ready != 1 || report.Rows[0].Operation.Amount != "100" || report.Rows[0].Operation.Tax != "13" {
		t.Fatal(report, e)
	}
	loc, _ := time.LoadLocation("America/New_York")
	for _, date := range []string{"2024-11-03 01:30:00", "2024-03-10 02:30:00"} {
		if _, e := parseUnambiguousWallTime(date, loc); e == nil {
			t.Fatal("ambiguous/nonexistent time accepted", date)
		}
	}
	if _, e := parseUnambiguousWallTime("2024-11-03 03:30:00", loc); e != nil {
		t.Fatal(e)
	}
}

func TestReceivedPayoutDoesNotCalculateOrRequireTax(t *testing.T) {
	csv := financialHeader + "DIVIDEND,2024-01-01 12:00:00,TEST,0,87,RUB,13,X,0,,False,\n"
	rule := FinancialRule{Event: "DIVIDEND", Symbol: "TEST", Exchange: "X", Currency: "RUB", InstrumentKey: "TEST@X", Kind: "dividend", AmountBasis: "quantity", AmountFactor: "1", FeePolicy: "same_currency", IncomeBasis: "received", Reason: "Payout as supplied in the import"}
	report, err := MapSnowballFinancial(strings.NewReader(csv), financialConfig(t, csv, rule))
	if err != nil || report.Ready != 1 {
		t.Fatal(report, err)
	}
	op := report.Rows[0].Operation
	if op.Amount != "87" || op.Tax != "0" || op.TaxReviewRequired {
		t.Fatal(op)
	}
}

func TestFinancialMappingAccruedUnits(t *testing.T) {
	csv := financialHeader + "BUY,2024-01-01 12:00:00,TEST,10,2,RUB,0,X,1,,False,\n"
	for basis, want := range map[string]string{"total": "1", "per_source_quantity": "2", "per_security_unit": "20"} {
		rule := tradeRule()
		rule.AccruedBasis = basis
		m := financialConfig(t, csv, rule)
		report, e := MapSnowballFinancial(strings.NewReader(csv), m)
		if e != nil || report.Ready != 1 || report.Rows[0].Operation.Accrued != want {
			t.Fatal(basis, report, e)
		}
	}
}

func TestFinancialMappingQuantityIncomeReplaysTaxOnce(t *testing.T) {
	for _, kind := range []string{"dividend", "coupon"} {
		for _, basis := range []string{"gross", "net"} {
			t.Run(kind+"/"+basis, func(t *testing.T) {
				sourceAmount := "100.123456789012"
				if basis == "net" {
					sourceAmount = "87.123456789012"
				}
				csv := financialHeader + "DIVIDEND,2024-01-01 12:00:00,TEST,0," + sourceAmount + ",RUB,13,X,0,,False,\n"
				rule := FinancialRule{Event: "DIVIDEND", Symbol: "TEST", Exchange: "X", Currency: "RUB", InstrumentKey: "isin:TEST", Kind: kind, AmountBasis: "quantity", AmountFactor: "1", FeePolicy: "same_currency", IncomeBasis: basis, Reason: "Reviewed broker payout and separate withholding"}
				m := financialConfig(t, csv, rule)
				report, err := MapSnowballFinancial(strings.NewReader(csv), m)
				if err != nil || report.Ready != 1 {
					t.Fatal(report, err)
				}
				operation := *report.Rows[0].Operation
				if operation.Amount != "100.123456789012" || operation.Tax != "13" || operation.Quantity != "0" || operation.CouponAllocationConfirmed {
					t.Fatal(operation)
				}
				operation.ID = uuid.NewString()
				operation.Sequence = 1
				operation.Status = "posted"
				result, err := inv.Replay(d.InvestmentLedger{ID: operation.LedgerID, StartedAt: operation.OccurredAt.Add(-time.Hour)}, []d.InvestmentOperation{operation}, operation.OccurredAt.Add(time.Hour))
				if err != nil || len(result.Snapshot.Cash) != 1 || result.Snapshot.Cash[0].Economic != "87.123456789012" {
					t.Fatal(result, err)
				}
				if slices.Contains(result.Snapshot.Issues, "coupon_allocation_required") != (kind == "coupon") {
					t.Fatal(result.Snapshot.Issues)
				}
			})
		}
	}
}

func TestFinancialMappingQuantityIncomeRequiresExplicitSemantics(t *testing.T) {
	for _, change := range []string{"income_basis", "cross_currency", "accrued", "negative", "precision", "event"} {
		t.Run(change, func(t *testing.T) {
			amount, feeCurrency, accrued, event := "100", "RUB", "0", "DIVIDEND"
			rule := FinancialRule{Event: event, Symbol: "TEST", Exchange: "X", Currency: "RUB", InstrumentKey: "isin:TEST", Kind: "coupon", AmountBasis: "quantity", AmountFactor: "1", FeePolicy: "same_currency", IncomeBasis: "gross", Reason: "Reviewed coupon"}
			switch change {
			case "income_basis":
				rule.IncomeBasis = ""
			case "cross_currency":
				feeCurrency = "USD"
			case "accrued":
				accrued = "1"
			case "negative":
				amount = "-100"
			case "precision":
				amount = "100.0000000000001"
			case "event":
				event = "CASH_GAIN"
				rule.Event = event
			}
			csv := financialHeader + event + ",2024-01-01 12:00:00,TEST,0," + amount + ",RUB,13,X," + accrued + "," + feeCurrency + ",False,\n"
			report, err := MapSnowballFinancial(strings.NewReader(csv), financialConfig(t, csv, rule))
			if err != nil || report.Ready != 0 || report.Unresolved != 1 || report.Rows[0].Operation != nil {
				t.Fatal(report, err)
			}
		})
	}
}

func TestFinancialMappingCashGainIsInterest(t *testing.T) {
	csv := financialHeader + "CASH_GAIN,2024-01-01 12:00:00,,1,2.123456789,RUB,0,,0,,False,\n"
	rule := FinancialRule{Event: "CASH_GAIN", Currency: "RUB", Kind: "cash_interest", AmountBasis: "quantity", AmountFactor: "1", FeePolicy: "zero_only", Reason: "Confirmed overnight income"}
	m := financialConfig(t, csv, rule)
	r, err := MapSnowballFinancial(strings.NewReader(csv), m)
	if err != nil || r.Ready != 1 || r.Rows[0].Operation.Amount != "2.123456789" || r.Rows[0].Operation.Kind != "cash_interest" {
		t.Fatal(r, err)
	}
	m.Rules[0].Kind = "deposit"
	r, err = MapSnowballFinancial(strings.NewReader(csv), m)
	if err != nil || r.Ready != 0 {
		t.Fatal("overnight classified as external contribution", r, err)
	}
}

func TestFinancialMappingExplicitDatePrecision(t *testing.T) {
	csv := financialHeader + "BUY,2024-01-01 00:00:00,TEST,10,1,RUB,0,X,0,,False,\n"
	m := financialConfig(t, csv, tradeRule())
	m.DateOnlyByRecord = map[int]bool{1: true}
	m.SettlementPolicy = "explicit"
	m.SettlementDateByRecord = map[int]string{1: "2024-01-02"}
	report, e := MapSnowballFinancial(strings.NewReader(csv), m)
	if e != nil || report.Ready != 1 {
		t.Fatal(report, e)
	}
	o := report.Rows[0].Operation
	if o.Timing.Timezone != "Europe/Moscow" || o.Timing.OccurredDate != "2024-01-01" || o.SettlementAt.Format(time.RFC3339Nano) != "2024-01-02T20:59:59.999999Z" {
		t.Fatal(o)
	}
	// Midnight alone is not evidence of date-only precision.
	m.DateOnlyByRecord = nil
	report, e = MapSnowballFinancial(strings.NewReader(csv), m)
	if e != nil || report.Ready != 1 || report.Rows[0].Operation.Timing.OccurredDate != "" {
		t.Fatal(report, e)
	}
}

func TestTrackerMappingSplitAndRepoPayout(t *testing.T) {
	for _, tc := range []struct{ event, kind, price, quantity string }{
		{"SPLIT", "split", "4", "0"},
		{"CASH_GAIN", "repo_income", "0", "17.25"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			csv := financialHeader + tc.event + ",2024-01-01 12:00:00,TEST," + tc.price + "," + tc.quantity + ",RUB,0,X,0,,False,\n"
			rule := FinancialRule{Event: tc.event, Symbol: "TEST", Exchange: "X", Currency: "RUB", InstrumentKey: "TEST@X", Kind: tc.kind, AmountBasis: "quantity", AmountFactor: "1", FeePolicy: "zero_only", IncomeBasis: "received", Reason: "Source supplied portfolio fact"}
			report, err := MapSnowballFinancial(strings.NewReader(csv), financialConfig(t, csv, rule))
			if err != nil || report.Ready != 1 || report.Rows[0].Operation == nil {
				t.Fatal(report, err)
			}
			op := report.Rows[0].Operation
			if tc.kind == "split" && (op.SplitNew != "4" || op.SplitOld != "1" || op.Currency != "" || op.Amount != "0") {
				t.Fatal(op)
			}
			if tc.kind == "repo_income" && (op.Amount != "17.25" || op.Tax != "0") {
				t.Fatal(op)
			}
		})
	}
}

func TestTrackerImportRoundsMoneyAfterArithmeticAndKeepsQuantityExact(t *testing.T) {
	csv := financialHeader + "BUY,2024-01-01 12:00:00,TEST,10.00000000000025,2,RUB,0,X,4.1614599393756935,,False,\n"
	rule := tradeRule()
	rule.QuantityFactor, rule.MoneyRounding = "1", "12dp_half_away"
	mapping := financialConfig(t, csv, rule)
	report, err := MapSnowballFinancial(strings.NewReader(csv), mapping)
	if err != nil || report.Ready != 1 {
		t.Fatal(report, err)
	}
	op := report.Rows[0].Operation
	if op.Amount != "20.000000000001" || op.Accrued != "4.161459939376" || op.Quantity != "2" {
		t.Fatal(op)
	}
	// The policy must be explicit and cannot make unsupported quantities valid.
	mapping.Rules[0].MoneyRounding = ""
	strict, err := MapSnowballFinancial(strings.NewReader(csv), mapping)
	if err != nil || strict.Ready != 0 || strict.Hash == report.Hash {
		t.Fatal(strict, err)
	}
	csv = strings.Replace(csv, ",2,RUB,", ",0.00000000001,RUB,", 1)
	mapping = financialConfig(t, csv, rule)
	invalid, err := MapSnowballFinancial(strings.NewReader(csv), mapping)
	if err != nil || invalid.Ready != 0 {
		t.Fatal(invalid, err)
	}
}

func TestTrackerImportRoundingDoesNotDiscardOversizedFees(t *testing.T) {
	csv := financialHeader + "BUY,2024-01-01 12:00:00,TEST,10,2,RUB,1e100,X,0,,False,\n"
	rule := tradeRule()
	rule.MoneyRounding = "12dp_half_away"
	report, err := MapSnowballFinancial(strings.NewReader(csv), financialConfig(t, csv, rule))
	if err != nil || report.Ready != 0 {
		t.Fatal(report, err)
	}
}
