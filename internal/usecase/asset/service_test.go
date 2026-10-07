package asset

import (
	"errors"
	"github.com/positron48/budget/internal/domain"
	"math"
	"testing"
	"time"
)

func TestConvertMinorExact(t *testing.T) {
	for _, tc := range []struct {
		amount int64
		rate   string
		want   int64
	}{{100, "90.125", 9013}, {-100, "90.125", -9013}, {1, "0.5", 1}, {-1, "0.5", -1}, {9007199254740993, "1", 9007199254740993}, {math.MaxInt64, "1", math.MaxInt64}} {
		got, err := ConvertMinor(tc.amount, tc.rate)
		if err != nil || got != tc.want {
			t.Fatalf("%d * %s = %d, %v; want %d", tc.amount, tc.rate, got, err, tc.want)
		}
	}
	for _, rate := range []string{"0", "-1", "1/2", "NaN", "no rate"} {
		if _, err := ConvertMinor(100, rate); !errors.Is(err, domain.ErrAssetInvalid) {
			t.Fatalf("accepted %q: %v", rate, err)
		}
	}
	if _, err := ConvertMinor(math.MaxInt64, "2"); err == nil {
		t.Fatal("overflow accepted")
	}
}
func TestSummarizeMissingRatesAndValuation(t *testing.T) {
	accounts := []domain.AssetAccount{{Kind: "cash", Balances: []domain.AssetBalance{{Amount: domain.Money{CurrencyCode: "RUB", MinorUnits: 10000}}, {Amount: domain.Money{CurrencyCode: "USD", MinorUnits: 100}}, {Amount: domain.Money{CurrencyCode: "EUR", MinorUnits: 200}}}}, {Kind: "investment", Balances: []domain.AssetBalance{{Amount: domain.Money{CurrencyCode: "RUB", MinorUnits: 90000}}}}}
	result, err := Summarize(accounts, []domain.AssetFxRate{{From: "USD", To: "RUB", RateDecimal: "90"}}, "RUB", time.Now())
	if err != nil || result.Total.MinorUnits != 109000 || !result.Incomplete || len(result.MissingCurrencies) != 1 || result.MissingCurrencies[0] != "EUR" {
		t.Fatalf("%+v, %v", result, err)
	}
	accounts[0].Balances[2].Amount.MinorUnits = 0
	result, err = Summarize(accounts, []domain.AssetFxRate{{From: "USD", RateDecimal: "90"}}, "RUB", time.Now())
	if err != nil || result.Incomplete {
		t.Fatalf("zero holding requires missing FX: %+v %v", result, err)
	}
}
func TestAssetValidation(t *testing.T) {
	a := domain.AssetAccount{Name: " Deposit ", Kind: "deposit", FixedCurrencyCode: "rub", DepositRateDecimal: "16.5", DepositOpenedOn: "2026-10-03", DepositMaturesOn: "2027-10-03"}
	if err := ValidateAccount(&a); err != nil || a.Name != "Deposit" || a.FixedCurrencyCode != "RUB" {
		t.Fatalf("%+v %v", a, err)
	}
	a.DepositMaturesOn = "2026-01-01"
	if ValidateAccount(&a) == nil {
		t.Fatal("maturity before opening accepted")
	}
	a = domain.AssetAccount{Name: "cash", Kind: "cash", FixedCurrencyCode: "RUB"}
	if ValidateAccount(&a) == nil {
		t.Fatal("fixed-currency cash accepted")
	}
	snapshot := domain.AssetSnapshot{Amount: domain.Money{CurrencyCode: "rub", MinorUnits: -100}, AsOf: time.Now(), Kind: "reconciliation"}
	if err := ValidateSnapshot(&snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.AsOf = time.Now().Add(time.Hour)
	if ValidateSnapshot(&snapshot) == nil {
		t.Fatal("future confirmation accepted")
	}
	transfer := domain.AssetTransfer{FromAccountID: "one", ToAccountID: "one", FromAmount: domain.Money{CurrencyCode: "RUB", MinorUnits: 100}, ToAmount: domain.Money{CurrencyCode: "RUB", MinorUnits: 100}, OccurredAt: time.Now()}
	if ValidateTransfer(&transfer) == nil {
		t.Fatal("self-transfer accepted")
	}
}

func TestPropertyCurrencyAndConversion(t *testing.T) {
	a := domain.AssetAccount{Name: "Car", Kind: "property"}
	if err := ValidateAccount(&a); !errors.Is(err, domain.ErrAssetInvalid) {
		t.Fatalf("missing property currency: %v", err)
	}
	a.FixedCurrencyCode = "usd"
	if err := ValidateAccount(&a); err != nil {
		t.Fatal(err)
	}
	a.Balances = []domain.AssetBalance{{Amount: domain.Money{CurrencyCode: "USD", MinorUnits: 2500000}}}
	result, err := Summarize([]domain.AssetAccount{a}, []domain.AssetFxRate{{From: "USD", To: "RUB", RateDecimal: "90"}}, "RUB", time.Now())
	if err != nil || result.Total.MinorUnits != 225000000 || result.Incomplete {
		t.Fatalf("property FX: %+v, %v", result, err)
	}
}
