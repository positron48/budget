package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	d "github.com/positron48/budget/internal/domain"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func TestPortfolioExchangeRatesAuthorizeAndTranslateHistoricalDates(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, foreign, actor string
	for _, id := range []*string{&tenant, &foreign} {
		if e := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Rates report','RUB') RETURNING id`).Scan(id); e != nil {
			t.Fatal(e)
		}
	}
	if e := pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('rates-report@example.test','test') RETURNING id`).Scan(&actor); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'member')`, tenant, actor); e != nil {
		t.Fatal(e)
	}
	// Register policy as owner, then verify the report remains available to members.
	if _, e := pool.DB.Exec(ctx, `UPDATE user_tenants SET role='owner' WHERE tenant_id=$1 AND user_id=$2`, tenant, actor); e != nil {
		t.Fatal(e)
	}
	repo := NewMarketDataRepo(pool)
	p := md.FXPolicy{Key: "report-market", Mode: "market", Source: "synthetic", AccessScope: "personal", SourceReference: "Synthetic evidence", TimeConvention: "Explicit timestamps", ValueConvention: "One unit", ValidityConvention: "Explicit validity"}
	if _, e := repo.RegisterFXPolicy(ctx, tenant, actor, p, 0); e != nil {
		t.Fatal(e)
	}
	at := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	point := md.FXPoint{From: "USD", To: "RUB", Rate: "80", At: at, AvailableAt: at, ValidUntil: at.Add(24 * time.Hour), EvidenceSHA256: strings.Repeat("a", 64), RecordReference: "synthetic/2020-01-01"}
	if _, e := repo.SaveFXPoints(ctx, tenant, p.Key, 1, []md.FXPoint{point}); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.DB.Exec(ctx, `UPDATE user_tenants SET role='member' WHERE tenant_id=$1 AND user_id=$2`, tenant, actor); e != nil {
		t.Fatal(e)
	}
	report, e := repo.InvestmentExchangeRates(ctx, tenant, actor, p.Key, "RUB", []string{"USD"}, []string{"2020-01-01"})
	if e != nil || len(report.Rates) != 2 || report.Mode != "market" || report.PolicyVersion != 1 {
		t.Fatal(report, e)
	}
	if report.Rates[0].Rate != nil || report.Rates[1].Rate == nil || report.Rates[1].Rate.RatString() != "80" {
		t.Fatal(report.Rates)
	}
	if _, e = repo.InvestmentExchangeRates(ctx, foreign, actor, p.Key, "RUB", []string{"USD"}, nil); !errors.Is(e, d.ErrInvestmentForbidden) {
		t.Fatal("cross-tenant rates", e)
	}
	if _, e = repo.InvestmentExchangeRates(ctx, tenant, actor, p.Key, "GBP", nil, nil); !errors.Is(e, d.ErrInvestmentInvalid) {
		t.Fatal("bad currency", e)
	}
}
