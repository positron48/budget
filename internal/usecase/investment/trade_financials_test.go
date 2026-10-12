package investment

import (
	d "github.com/positron48/budget/internal/domain"
	"testing"
	"time"
)

func detailedBuy() d.InvestmentOperation {
	o := op("buy", "buy", "0", 4)
	o.OccurredAt = day.Add(time.Hour)
	o.Quantity = "2"
	o.InstrumentKey = "stock"
	o.SettlementAt = day.Add(2 * time.Hour)
	money := func(amount, currency, num, den string, at time.Time) d.InvestmentTradeMoney {
		m := d.InvestmentTradeMoney{Amount: amount, Currency: currency, At: at}
		if num != "" {
			m.FXNumerator = num
			m.FXDenominator = den
			m.FXAt = at
			m.FXSource = "reviewed contract"
			m.FXEvidence = "Synthetic broker rate on charge date"
		}
		return m
	}
	o.TradeFinancials = &d.InvestmentTradeFinancials{Price: "50", PriceUnit: "currency_per_unit", Principal: money("9000", "RUB", "1", "90", o.SettlementAt), Accrued: money("90", "RUB", "1", "90", o.SettlementAt), Fee: money("1", "EUR", "1", "3", day.Add(3*time.Hour)), Tax: money("2", "USD", "", "", o.SettlementAt), ActualSettlement: "9090"}
	return o
}
func TestTradeNativeCashExactFIFOAndDifferentFeeDate(t *testing.T) {
	buy := detailedBuy()
	buy.OccurredAt = day.Add(time.Hour)
	ops := []d.InvestmentOperation{}
	for i, v := range []struct{ ccy, amount string }{{"USD", "10"}, {"RUB", "10000"}, {"EUR", "10"}} {
		o := op(v.ccy, "deposit", v.amount, int64(i+1))
		o.Currency = v.ccy
		o.OccurredAt = day
		ops = append(ops, o)
	}
	ops = append(ops, buy)
	at := day.Add(2 * time.Hour)
	got, e := Replay(testLedger(), ops, at)
	if e != nil {
		t.Fatal(e)
	}
	if got.ExactLots["stock"][0].CleanCost.RatString() != "301/3" || got.ExactLots["stock"][0].AcquiredAccrued.RatString() != "1" {
		t.Fatal(got.ExactLots)
	}
	cash := map[string]d.InvestmentCash{}
	for _, v := range got.Snapshot.Cash {
		cash[v.Currency] = v
	}
	if cash["RUB"].Settled != "910" || cash["USD"].Settled != "8" || cash["EUR"].Settled != "10" || cash["EUR"].Payable != "1" || cash["EUR"].Economic != "9" {
		t.Fatal(cash)
	}
	sell := op("sell", "sell", "0", 5)
	sell.InstrumentKey = "stock"
	sell.Quantity = "1"
	sell.OccurredAt = day.Add(4 * time.Hour)
	sell.SettlementAt = day.Add(5 * time.Hour)
	sell.TradeFinancials = &d.InvestmentTradeFinancials{Price: "60", PriceUnit: "currency_per_unit", Principal: d.InvestmentTradeMoney{Amount: "60", Currency: "USD"}, Accrued: d.InvestmentTradeMoney{Amount: "0", Currency: "USD"}, Fee: d.InvestmentTradeMoney{Amount: "1", Currency: "EUR", FXNumerator: "1", FXDenominator: "3", FXSource: "Reviewed rate", FXEvidence: "Synthetic dated rate", FXAt: sell.SettlementAt}, Tax: d.InvestmentTradeMoney{Amount: "1", Currency: "USD"}, ActualSettlement: "59", ActualSettlementIncludesExpenses: true}
	ops = append(ops, sell)
	got, e = Replay(testLedger(), ops, day.Add(6*time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	if got.ExactLots["stock"][0].CleanCost.RatString() != "301/6" || got.Snapshot.Positions[0].RealizedClean != "9.5" {
		t.Fatal(got.ExactLots, got.Snapshot.Positions)
	}
	taxes, fees := 0, 0
	for _, v := range got.ProfitEvents {
		if v.Kind == "taxes" {
			taxes++
			if v.Currency != "USD" {
				t.Fatal(v)
			}
		}
		if v.Kind == "trade_fees_in_cost" {
			fees++
			if v.Currency != "EUR" || v.Amount.RatString() != "1" {
				t.Fatal(v)
			}
		}
	}
	if taxes != 2 || fees != 2 {
		t.Fatal(got.ProfitEvents)
	}
	if e = ValidateOperation(&buy); e != nil || buy.Amount != "100" || buy.Fee != "0.333333333333" {
		t.Fatal(buy, e)
	}
	row, e := operationRow(buy, "Broker", 0)
	if e != nil || len(row.Cash) != 3 {
		t.Fatal(row, e)
	}
	totals := map[string]string{}
	for _, c := range row.Cash {
		totals[c.Currency] = c.Amount.RatString()
	}
	if totals["RUB"] != "-9090" || totals["USD"] != "-2" || totals["EUR"] != "-1" {
		t.Fatal(totals)
	}
}
func TestTradeStatementDifferencesAndValidation(t *testing.T) {
	o := detailedBuy()
	o.TradeFinancials.ActualSettlement = "9090.000001"
	if ValidateOperation(&o) == nil {
		t.Fatal("unexplained statement discrepancy accepted")
	}
	o.TradeFinancials.DifferenceReason = "Reviewed rounding allocated to principal"
	if e := ValidateOperation(&o); e != nil {
		t.Fatal(e)
	}
	principal, _, _ := tradeBookValues(o)
	if principal.RatString() != "9000000001/90000000" {
		t.Fatal(principal)
	}
	for _, change := range []func(*d.InvestmentOperation){
		func(o *d.InvestmentOperation) { o.TradeFinancials.Fee.FXSource = "" },
		func(o *d.InvestmentOperation) { o.TradeFinancials.Fee.FXAt = day },
		func(o *d.InvestmentOperation) { o.TradeFinancials.Fee.FXDenominator = "0" },
		func(o *d.InvestmentOperation) { o.TradeFinancials.Principal.Currency = "GBP" },
		func(o *d.InvestmentOperation) { o.TradeFinancials.Principal.At = day.Add(7 * time.Hour) },
		func(o *d.InvestmentOperation) { o.Amount = "99" },
		func(o *d.InvestmentOperation) { o.Kind = "deposit" },
	} {
		v := detailedBuy()
		change(&v)
		if ValidateOperation(&v) == nil {
			t.Fatal("invalid financial terms accepted", v)
		}
	}
	o = detailedBuy()
	o.Quantity = "2"
	o.TradeFinancials.Price = "5"
	o.TradeFinancials.PriceUnit = "percent_nominal"
	o.TradeFinancials.Nominal = "1000"
	if e := ValidateOperation(&o); e != nil {
		t.Fatal("percent nominal quote", e)
	}
}

func TestTradeQuoteCurrencyCanDifferFromExistingBook(t *testing.T) {
	o := detailedBuy()
	o.Currency = "RUB"
	f := o.TradeFinancials
	f.PriceCurrency = "USD"
	f.PriceFXNumerator = "90"
	f.PriceFXDenominator = "1"
	f.PriceFXSource = "Execution contract"
	f.PriceFXEvidence = "Original USD quote"
	f.PriceFXAt = o.OccurredAt
	f.Principal.FXNumerator = ""
	f.Principal.FXDenominator = ""
	f.Principal.FXSource = ""
	f.Principal.FXEvidence = ""
	f.Principal.FXAt = time.Time{}
	f.Accrued.FXNumerator = ""
	f.Accrued.FXDenominator = ""
	f.Accrued.FXSource = ""
	f.Accrued.FXEvidence = ""
	f.Accrued.FXAt = time.Time{}
	f.Fee.FXNumerator = "30"
	f.Fee.FXDenominator = "1"
	f.Tax.FXNumerator = "90"
	f.Tax.FXDenominator = "1"
	f.Tax.FXSource = "Settlement report"
	f.Tax.FXEvidence = "USD tax to RUB book"
	f.Tax.FXAt = f.Tax.At
	if e := ValidateOperation(&o); e != nil || o.Amount != "9000" || o.Fee != "30" || o.Tax != "180" || o.TradeFinancials.PrincipalDifferenceNumerator != "0" {
		t.Fatal(o, e)
	}
	row, e := operationRow(o, "Broker", 0)
	if e != nil || row.UnitCleanPrice.RatString() != "4500" {
		t.Fatal(row, e)
	}
	o.TradeFinancials.PriceFXAt = o.SettlementAt
	if ValidateOperation(&o) == nil {
		t.Fatal("quote conversion used a different date")
	}
}

func TestStatementBeforeAndAfterCommissionUsesSameCashAndFIFO(t *testing.T) {
	makeTrade := func(includes bool, actual string) d.InvestmentOperation {
		o := op("buy", "buy", "0", 2)
		o.Quantity = "2"
		o.InstrumentKey = "stock"
		o.TradeFinancials = &d.InvestmentTradeFinancials{Price: "50", PriceUnit: "currency_per_unit", Principal: d.InvestmentTradeMoney{Amount: "100", Currency: "USD"}, Accrued: d.InvestmentTradeMoney{Amount: "0", Currency: "USD"}, Fee: d.InvestmentTradeMoney{Amount: "1", Currency: "USD"}, Tax: d.InvestmentTradeMoney{Amount: "2", Currency: "USD"}, ActualSettlement: actual, ActualSettlementIncludesExpenses: includes}
		return o
	}
	fund := op("fund", "deposit", "1000", 1)
	for _, o := range []d.InvestmentOperation{makeTrade(false, "100"), makeTrade(true, "103")} {
		r, e := Replay(testLedger(), []d.InvestmentOperation{fund, o}, day.Add(5*time.Hour))
		if e != nil || r.Snapshot.Cash[0].Settled != "897" || r.ExactLots["stock"][0].CleanCost.RatString() != "101" {
			t.Fatal(r, e)
		}
	}
}

func TestSaleWithCommissionExceedingCreditPreservesReportedDebit(t *testing.T) {
	fund := op("fund", "deposit", "1000", 1)
	buy := op("buy", "buy", "100", 2)
	buy.InstrumentKey = "stock"
	buy.Quantity = "1"
	sale := op("sell", "sell", "0", 3)
	sale.InstrumentKey = "stock"
	sale.Quantity = "1"
	sale.TradeFinancials = &d.InvestmentTradeFinancials{Price: "10", PriceUnit: "currency_per_unit", Principal: d.InvestmentTradeMoney{Amount: "10", Currency: "USD"}, Accrued: d.InvestmentTradeMoney{Amount: "0", Currency: "USD"}, Fee: d.InvestmentTradeMoney{Amount: "100", Currency: "USD"}, Tax: d.InvestmentTradeMoney{Amount: "0", Currency: "USD"}, ActualSettlement: "-90", ActualSettlementIncludesExpenses: true}
	r, e := Replay(testLedger(), []d.InvestmentOperation{fund, buy, sale}, day.Add(5*time.Hour))
	if e != nil || r.Snapshot.Cash[0].Settled != "810" || r.Snapshot.Positions[0].RealizedClean != "-190" {
		t.Fatal(r, e)
	}
}

func TestTinyNativeFeeIsNotLostInRoundedBookSummary(t *testing.T) {
	o := detailedBuy()
	o.TradeFinancials.Fee.Amount = "0.000000000001"
	if e := ValidateOperation(&o); e != nil || o.Fee != "0" {
		t.Fatal(o, e)
	}
	principal, _, fee := tradeBookValues(o)
	if fee.RatString() != "1/3000000000000" || principal.RatString() != "100" {
		t.Fatal(principal, fee)
	}
	ledger := testLedger()
	ledger.TenantID = "11111111-1111-4111-8111-111111111111"
	o.TenantID = ledger.TenantID
	o.LedgerID = ledger.ID
	report, e := BuildPayoutReport(ledger.TenantID, []d.InvestmentLedger{ledger}, []d.InvestmentOperation{o}, PayoutQuery{FromDate: day.Format(time.DateOnly), ToDate: day.AddDate(0, 0, 1).Format(time.DateOnly), Limit: 100}, day.AddDate(0, 0, 2))
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, v := range report.Entries {
		if v.Kind == "fee" {
			found = true
			if v.Currency != "EUR" || v.Net.RatString() != "-1/1000000000000" {
				t.Fatal(v)
			}
		}
	}
	if !found {
		t.Fatal("native fee omitted because book summary rounded to zero")
	}
}
