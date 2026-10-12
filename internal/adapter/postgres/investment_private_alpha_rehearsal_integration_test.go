package postgres

import (
	"context"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	av "github.com/positron48/budget/internal/adapter/alphavantage"
	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

type privateAlphaNAV struct {
	Ledger, Date, Currency, State, KnownValue string
	Complete                                  bool
	Issues                                    []calc.QualityIssue
	Warnings                                  []calc.QualityIssue
}
type privateAlphaReport struct {
	Prices                                               *privatePriceReport `json:",omitempty"`
	Policy, Convention                                   string
	Points, FXLegChecks                                  int
	NAV                                                  []privateAlphaNAV
	CompleteNAV, IncompleteNAV                           int
	HistoricalPublicationVerified, RestoredNAVVerified   bool
	CacheAbsentAfterRestore, ExternalCheckpointsReloaded bool
}

func privateAlphaRehearsal(t *testing.T, pool *Pool, tenant, actor string, checkpoints []d.InvestmentSnapshot) *privateAlphaReport {
	t.Helper()
	ctx := context.Background()
	audits := privateAlphaAudits(t)
	from, err := time.Parse(time.DateOnly, audits[0].From)
	if err != nil {
		t.Fatal("invalid Alpha date")
	}
	policy := md.FXPolicy{Key: "alpha-retrospective", Mode: "market", Source: "alphavantage", AccessScope: "personal", SourceReference: "User-approved personal FX_DAILY retrospective valuation; audited checkpoints 161; pair-specific upstream provenance and historical publication unknown", TimeConvention: md.AlphaTimeConvention, ValueConvention: md.AlphaValueConvention, ValidityConvention: md.AlphaValidityConvention, Alpha: &md.AlphaFXPolicy{From: from, Convention: md.AlphaRetrospectiveConvention}}
	repo := NewMarketDataRepo(pool)
	saved, err := repo.RegisterFXPolicy(ctx, tenant, actor, policy, 0)
	if err != nil {
		t.Fatal("register private Alpha policy")
	}
	var total int
	for i := 0; i < 2; i++ {
		total, err = repo.SaveAlphaFX(ctx, tenant, policy.Key, saved.Version, audits)
		if err != nil {
			t.Fatal("publish private Alpha evidence")
		}
	}
	var revisions int
	if err = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_fx_point_revisions WHERE tenant_id=$1 AND point_id IN (SELECT id FROM investment_fx_points WHERE tenant_id=$1 AND policy_key=$2)`, tenant, policy.Key).Scan(&revisions); err != nil || revisions != total {
		t.Fatal("private FX retry changed revision count")
	}
	report := &privateAlphaReport{Policy: md.FXPolicyRef(policy.Key, saved.Version), Convention: md.AlphaRetrospectiveConvention, Points: total}
	report.Prices = privatePriceRehearsal(t, pool, tenant, actor, checkpoints)
	for _, snapshot := range checkpoints {
		date := snapshot.AsOf.In(time.FixedZone("Moscow", 3*3600)).Format(time.DateOnly)
		at, e := nav.DayCutoff(date)
		if e != nil {
			t.Fatal(e)
		}
		_, points, e := repo.ReadFXSnapshot(ctx, tenant, policy.Key, at, time.Time{})
		if e != nil {
			t.Fatal("read private FX snapshot")
		}
		fixings := make([]calc.FXFixing, len(points))
		for i, p := range points {
			fixings[i] = p.Fixing()
		}
		resolver, e := calc.NewFXResolver(fixings, report.Policy, at)
		if e != nil {
			t.Fatal("resolve private FX")
		}
		// Compare each leg with the exact latest eligible original checkpoint close.
		// Expiry is explicit. No NAV completeness claim follows from source coverage.
		for _, audit := range audits {
			var latest *av.Bar
			var effective time.Time
			for i := range audit.Bars {
				day, _ := time.Parse(time.DateOnly, audit.Bars[i].Date)
				eligible := day.Add(24 * time.Hour)
				if !eligible.After(at) {
					latest = &audit.Bars[i]
					effective = eligible
				}
			}
			resolved, ok := resolver.Resolve(audit.Pair[:3], "RUB")
			want := latest != nil && at.Before(effective.Add(96*time.Hour))
			if ok != want {
				t.Fatal("private FX coverage mismatch")
			}
			if want {
				rate, valid := new(big.Rat).SetString(latest.Close)
				if !valid || resolved.Rate.Cmp(rate) != 0 {
					t.Fatal("private FX close mismatch")
				}
				report.FXLegChecks++
			}
		}
		for _, currency := range []string{"RUB", "USD", "EUR"} {
			q := nav.Request{TenantID: tenant, UserID: actor, LedgerIDs: []string{snapshot.Ledger.ID}, Currency: currency, FXPolicyKey: policy.Key}
			if report.Prices != nil {
				q.ConventionKey = report.Prices.ConventionKey
			}
			value, e := repo.BuildDailyNAV(ctx, q, date)
			if e != nil || value.Result == nil {
				t.Fatal("build private Alpha NAV")
			}
			if report.Prices != nil {
				verifyPrivatePriceFacts(t, report.Prices, value.Result, date, false)
			}
			if value.Result.Complete {
				report.CompleteNAV++
			} else {
				report.IncompleteNAV++
			}
			report.NAV = append(report.NAV, privateAlphaNAV{Ledger: snapshot.Ledger.ID, Date: date, Currency: currency, State: value.State, KnownValue: value.Result.KnownValue.RatString(), Complete: value.Result.Complete, Issues: value.Result.Issues, Warnings: value.Result.Warnings})
		}
	}
	if len(report.NAV) == 0 || report.FXLegChecks == 0 {
		t.Fatal("private FX rehearsal exercised no historical evidence")
	}
	return report
}
func verifyPrivateAlphaRestore(t *testing.T, pool *Pool, tenant, actor string, report *privateAlphaReport) {
	t.Helper()
	repo := NewMarketDataRepo(pool)
	ctx := context.Background()
	policy, points, err := repo.ReadFXSnapshot(ctx, tenant, "alpha-retrospective", time.Now(), time.Time{})
	if err != nil || policy.Alpha == nil || len(points) != 0 {
		t.Fatal("canonical restore must retain Alpha convention and exclude licensed cache")
	}
	report.CacheAbsentAfterRestore = true
	n, err := repo.SaveAlphaFX(ctx, tenant, policy.Key, policy.Version, privateAlphaAudits(t))
	if err != nil || n != report.Points {
		t.Fatal("reload external Alpha checkpoints after restore")
	}
	if report.Prices != nil {
		reloadPrivatePriceCache(t, pool, tenant)
	}
	report.ExternalCheckpointsReloaded = true
	for _, want := range report.NAV {
		q := nav.Request{TenantID: tenant, UserID: actor, LedgerIDs: []string{want.Ledger}, Currency: want.Currency, FXPolicyKey: "alpha-retrospective"}
		if report.Prices != nil {
			q.ConventionKey = report.Prices.ConventionKey
		}
		got, err := repo.BuildDailyNAV(ctx, q, want.Date)
		if err != nil || got.Result == nil || got.State != want.State || got.Result.Complete != want.Complete || got.Result.KnownValue.RatString() != want.KnownValue || !reflect.DeepEqual(got.Result.Issues, want.Issues) || !reflect.DeepEqual(got.Result.Warnings, want.Warnings) {
			t.Fatal("restored private Alpha NAV differs")
		}
		if report.Prices != nil {
			verifyPrivatePriceFacts(t, report.Prices, got.Result, want.Date, true)
		}
	}
	report.RestoredNAVVerified = true
}

func privateAlphaAudits(t *testing.T) []av.Audit {
	t.Helper()
	dir := os.Getenv("INVESTMENT_REHEARSAL_ALPHA_DIR")
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		t.Fatal("invalid private Alpha evidence directory")
	}
	var audits []av.Audit
	for _, pair := range []string{"USD-RUB", "EUR-RUB"} {
		path := filepath.Join(dir, pair+".json")
		info, err = os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 8<<20 {
			t.Fatal("invalid private Alpha checkpoint")
		}
		raw, e := os.ReadFile(path)
		if e != nil {
			t.Fatal("read private Alpha checkpoint")
		}
		var a av.Audit
		if json.Unmarshal(raw, &a) != nil || a.Pair != pair[:3]+"/"+pair[4:] {
			t.Fatal("decode private Alpha checkpoint")
		}
		audits = append(audits, a)
	}

	return audits
}
