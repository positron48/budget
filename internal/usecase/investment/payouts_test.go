package investment

import (
	"errors"
	d "github.com/positron48/budget/internal/domain"
	"math/big"
	"testing"
	"time"
)

const payoutLedgerID = "00000000-0000-0000-0000-000000000001"
const payoutOtherID = "00000000-0000-0000-0000-000000000002"

func payoutFixture() ([]d.InvestmentLedger, []d.InvestmentOperation, PayoutQuery, time.Time) {
	ledgers := []d.InvestmentLedger{{ID: payoutLedgerID, TenantID: "tenant", Name: "Broker", Version: 10, StartedAt: day}, {ID: payoutOtherID, TenantID: "tenant", Name: "Other", Version: 1, StartedAt: day}}
	makeOp := func(id, kind, amount string, at time.Time) d.InvestmentOperation {
		o := op(id, kind, amount, 1)
		o.LedgerID = payoutLedgerID
		o.OccurredAt = at
		o.Version = 1
		return o
	}
	// Jan 31 UTC is already Feb 1 in Moscow.
	at := time.Date(2024, 1, 31, 22, 0, 0, 0, time.UTC)
	dividend := makeOp("dividend", "dividend", "9007199254740993.1234567891", at)
	dividend.InstrumentKey = "STOCK"
	dividend.Tax = "0.1234567891"
	coupon := makeOp("coupon", "coupon", "10", at)
	coupon.InstrumentKey = "BOND"
	coupon.TaxReviewRequired = true
	principal := makeOp("amortization", "amortization", "20", at)
	principal.InstrumentKey = "BOND"
	principal.NominalBefore = "100"
	principal.NominalAfter = "80"
	principal.SettlementAt = at.AddDate(0, 0, 2)
	buy := makeOp("buy", "buy", "100", at)
	buy.InstrumentKey = "STOCK"
	buy.Quantity = "1"
	buy.Fee = "0.1"
	buy.SettlementAt = at.AddDate(0, 1, 0)
	buy.SettlementAssumed = true
	fee := makeOp("fee", "fee", "1.1", at)
	tax := makeOp("tax", "tax", "2", at)
	refund := makeOp("refund", "tax_refund", "3", at)
	euro := makeOp("euro", "cash_interest", "5", at)
	euro.LedgerID = payoutOtherID
	euro.Currency = "EUR"
	voided := dividend
	voided.ID = "voided"
	voided.Status = "voided"
	future := principal
	future.ID = "future"
	future.SettlementAt = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	pendingFee := buy
	pendingFee.ID = "pending_fee"
	pendingFee.SettlementAt = time.Date(2024, 4, 1, 0, 30, 0, 0, time.UTC)
	deposit := makeOp("deposit", "deposit", "10000", at)
	return ledgers, []d.InvestmentOperation{dividend, coupon, principal, buy, fee, tax, refund, euro, voided, future, deposit, pendingFee}, PayoutQuery{FromDate: "2024-01-01", ToDate: "2024-12-31", Limit: 2}, time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC)
}
func expectPayoutRat(t *testing.T, v *big.Rat, want string) {
	t.Helper()
	if v == nil || v.Cmp(rational(want)) != 0 {
		t.Fatalf("got %v want %s", v, want)
	}
}
func TestPayoutFactsExactTotalsDatesAndPaging(t *testing.T) {
	ledgers, ops, q, now := payoutFixture()
	out, e := BuildPayoutReport("tenant", ledgers, ops, q, now)
	if e != nil {
		t.Fatal(e)
	}
	if out.TotalCount != 8 || len(out.Entries) != 2 || out.NextCursor == "" || len(out.Totals) != 2 || len(out.Months) != 3 {
		t.Fatal("scope", out.TotalCount, len(out.Months), e)
	}
	usd := out.Totals[1]
	if usd.Currency != "USD" || usd.Gross != nil || usd.Tax != nil {
		t.Fatal("unknown withholding must poison full gross/tax, not net")
	}
	expectPayoutRat(t, usd.IncomeNet, "9007199254741003")
	expectPayoutRat(t, usd.Fees, "1.2")
	expectPayoutRat(t, usd.StandaloneTax, "2")
	expectPayoutRat(t, usd.TaxRefund, "3")
	expectPayoutRat(t, usd.PrincipalReturn, "20")
	expectPayoutRat(t, usd.NetCash, "9007199254741022.8")
	expectPayoutRat(t, out.Totals[0].NetCash, "5")
	if out.Months[0].Month != "2024-02" || out.Months[2].Month != "2024-03" {
		t.Fatal("Moscow month/settlement month", out.Months)
	}
	seen := map[string]bool{}
	for {
		for _, p := range out.Entries {
			if seen[p.ID] {
				t.Fatal("duplicate page", p.ID)
			}
			seen[p.ID] = true
			if p.ID == "buy:fee" && !p.SettlementAssumed {
				t.Fatal("assumed fee date falsely confirmed")
			}
			if p.ID == "coupon" && (p.Gross != nil || p.Tax != nil || !p.TaxReviewRequired) {
				t.Fatal("provisional income falsely confirmed")
			}
		}
		if out.NextCursor == "" {
			break
		}
		q.Cursor = out.NextCursor
		out, e = BuildPayoutReport("tenant", ledgers, ops, q, now.Add(time.Hour))
		if e != nil {
			t.Fatal(e)
		}
		if !out.AsOf.Equal(now) || out.TotalCount != 8 {
			t.Fatal("cutoff drift")
		}
		expectPayoutRat(t, out.Totals[1].NetCash, "9007199254741022.8")
	}
	if len(seen) != 8 || seen["voided"] || seen["future"] || seen["deposit"] || seen["pending_fee:fee"] || !seen["buy:fee"] {
		t.Fatal("unexpected facts", seen)
	}
}
func TestPayoutFiltersAndAmendmentFence(t *testing.T) {
	ledgers, ops, q, now := payoutFixture()
	out, e := BuildPayoutReport("tenant", ledgers, ops, q, now)
	if e != nil {
		t.Fatal(e)
	}
	q.Cursor = out.NextCursor
	ledgers[0].Version++
	if _, e = BuildPayoutReport("tenant", ledgers, ops, q, now); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("stale financial snapshot", e)
	}
	ledgers[0].Version--
	q.Kinds = []string{"coupon"}
	if _, e = BuildPayoutReport("tenant", ledgers, ops, q, now); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("scope change", e)
	}
	q.Cursor = ""
	q.Kinds = []string{"dividend"}
	q.LedgerIDs = []string{payoutLedgerID}
	q.InstrumentKey = "STOCK"
	out, e = BuildPayoutReport("tenant", ledgers, ops, q, now)
	if e != nil || out.TotalCount != 1 {
		t.Fatal(e, out)
	}
	expectPayoutRat(t, out.Totals[0].Gross, "9007199254740993.1234567891")
	expectPayoutRat(t, out.Totals[0].Tax, "0.1234567891")
	q.FromDate = "2024-02-02"
	out, e = BuildPayoutReport("tenant", ledgers, ops, q, now)
	if e != nil || out.TotalCount != 0 || len(out.Totals) != 0 {
		t.Fatal("empty filtered report", e)
	}
	q.LedgerIDs = []string{"00000000-0000-0000-0000-000000000003"}
	if _, e = BuildPayoutReport("tenant", ledgers, ops, q, now); !errors.Is(e, d.ErrInvestmentNotFound) {
		t.Fatal("foreign/absent ledger", e)
	}
	q.LedgerIDs = nil
	q.Cursor = "invalid"
	if _, e = BuildPayoutReport("tenant", ledgers, ops, q, now); !errors.Is(e, d.ErrInvestmentInvalid) {
		t.Fatal(e)
	}
	q.Cursor = ""
	ledgers[0].TenantID = "foreign"
	if _, e = BuildPayoutReport("tenant", ledgers, ops, q, now); !errors.Is(e, d.ErrInvestmentForbidden) {
		t.Fatal(e)
	}
}
func TestTaxRefundReplayIsCashAndTaxReversal(t *testing.T) {
	refund := op("refund", "tax_refund", "2.1234567891", 1)
	replay, e := Replay(testLedger(), []d.InvestmentOperation{refund}, day.Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	if len(replay.Snapshot.Cash) != 1 || replay.Snapshot.Cash[0].Settled != "2.1234567891" || len(replay.ProfitEvents) != 1 || replay.ProfitEvents[0].Kind != "taxes" {
		t.Fatal(replay)
	}
	expectPayoutRat(t, replay.ProfitEvents[0].Amount, "2.1234567891")
}
