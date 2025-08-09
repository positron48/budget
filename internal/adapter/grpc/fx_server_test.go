package grpcadapter

import (
    "context"
    "errors"
    "testing"
    "time"

    budgetv1 "github.com/positron48/budget/gen/go/budget/v1"
)

type fxStub struct{
    rate string
    provider string
    err error
    batch []struct{
        From, To, Rate string
        AsOf time.Time
        Provider string
    }
}

func (s fxStub) GetRateAsOf(ctx context.Context, from, to string, asOf time.Time) (string, string, error) {
    return s.rate, s.provider, s.err
}
func (s fxStub) UpsertRate(ctx context.Context, from, to, rateDecimal string, asOf time.Time, provider string) (struct{ From, To, Rate string; AsOf time.Time; Provider string }, error) {
    if s.err != nil { return struct{ From, To, Rate string; AsOf time.Time; Provider string }{}, s.err }
    return struct{ From, To, Rate string; AsOf time.Time; Provider string }{From: from, To: to, Rate: rateDecimal, AsOf: asOf, Provider: provider}, nil
}
func (s fxStub) BatchGetRates(ctx context.Context, fromCurrencies []string, to string, asOf time.Time) ([]struct{ From, To, Rate string; AsOf time.Time; Provider string }, error) {
    if s.err != nil { return nil, s.err }
    if s.batch != nil { return s.batch, nil }
    return []struct{ From, To, Rate string; AsOf time.Time; Provider string }{{From: "USD", To: to, Rate: "2.0", AsOf: asOf, Provider: "stub"}}, nil
}

func TestFxServer_GetRate_Success(t *testing.T) {
    srv := NewFxServer(fxStub{rate: "1.2345", provider: "prov"})
    out, err := srv.GetRate(context.Background(), &budgetv1.GetRateRequest{FromCurrencyCode: "USD", ToCurrencyCode: "RUB"})
    if err != nil || out.GetRate().GetRateDecimal() != "1.2345" { t.Fatalf("unexpected: %v %#v", err, out) }
}

func TestFxServer_GetRate_Error(t *testing.T) {
    srv := NewFxServer(fxStub{err: errors.New("boom")})
    _, err := srv.GetRate(context.Background(), &budgetv1.GetRateRequest{FromCurrencyCode: "USD", ToCurrencyCode: "RUB"})
    if err == nil { t.Fatal("expected error") }
}

func TestFxServer_Upsert_And_Batch(t *testing.T) {
    srv := NewFxServer(fxStub{})
    u, err := srv.UpsertRate(context.Background(), &budgetv1.UpsertRateRequest{Rate: &budgetv1.FxRate{FromCurrencyCode: "USD", ToCurrencyCode: "RUB", RateDecimal: "2.0"}})
    if err != nil || u.GetRate().GetRateDecimal() != "2.0" { t.Fatalf("upsert: %v %#v", err, u) }
    b, err := srv.BatchGetRates(context.Background(), &budgetv1.BatchGetRatesRequest{FromCurrencyCodes: []string{"USD"}, ToCurrencyCode: "RUB"})
    if err != nil || len(b.GetRates()) == 0 { t.Fatalf("batch: %v %#v", err, b) }
}


