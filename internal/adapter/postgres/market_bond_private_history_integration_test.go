package postgres

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	md "github.com/positron48/budget/internal/usecase/marketdata"
)

// Actual provider snapshots stay private; this rehearsal approves no financial
// terms, coupon entitlements, historical currency or completeness of schedules.
func TestInvestmentPrivateBondHistoryRehearsal(t *testing.T) {
	dir := os.Getenv("INVESTMENT_REHEARSAL_TINVEST_BONDS_DIR")
	if dir == "" {
		t.Skip("private bond evidence required")
	}
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant string
	if err := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Private bond evidence rehearsal','RUB') RETURNING id`).Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	replayPrivateBondHistory(t, pool, tenant, dir)
}

func replayPrivateBondHistory(t *testing.T, pool *Pool, tenant, dir string) {
	t.Helper()
	ctx := context.Background()
	var index struct {
		ManifestSHA256 string
		Windows        []struct {
			Request                  md.JobRequest
			SHA256                   string
			Accrued, Coupons, Events int
		}
	}
	readPrivateHistory(t, filepath.Join(dir, "coverage.json"), "", &index)
	var manifest struct {
		SourceCoverageSHA256 string
		Plan                 struct {
			Version      int
			SourceSHA256 string
			Entries      []struct {
				InstrumentID string
				From, To     time.Time
			}
		}
	}
	readPrivateHistory(t, filepath.Join(dir, "manifest.json"), index.ManifestSHA256, &manifest)
	if manifest.Plan.Version != 1 || len(manifest.Plan.Entries) == 0 || len(index.Windows) == 0 {
		t.Fatal("invalid evidence universe")
	}
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal-rehearsal"}
	repo := NewMarketDataRepo(pool)
	if err := repo.ConfigureSource(ctx, scope, "Private facts, not approved financial terms", true); err != nil {
		t.Fatal(err)
	}
	accrued, coupons, events, empty := 0, 0, 0, 0
	for _, w := range index.Windows {
		if w.Request.Kind != "bond_facts" {
			t.Fatal("unexpected job kind")
		}
		var facts md.BondFacts
		readPrivateHistory(t, filepath.Join(dir, investmentHash(w.Request)+".json"), w.SHA256, &facts)
		normalized, err := md.NormalizeBondFacts(facts)
		if err != nil || facts.Instrument.ID != w.Request.InstrumentID || !facts.From.Equal(w.Request.From) || !facts.To.Equal(w.Request.To) {
			t.Fatal("source identity/range mismatch", err)
		}
		if len(facts.Accrued) != w.Accrued || len(facts.Coupons) != w.Coupons || len(facts.Events) != w.Events {
			t.Fatal("coverage counts changed")
		}
		job, err := repo.Enqueue(ctx, scope, w.Request)
		if err != nil {
			t.Fatal(err)
		}
		claimed, err := repo.Claim(ctx, scope, time.Minute)
		if err != nil || claimed.ID != job.ID {
			t.Fatal("claim", err)
		}
		if err = repo.CompleteBondFacts(ctx, claimed, facts); err != nil {
			t.Fatal("atomic publication", err)
		}
		state, err := repo.JobStatus(ctx, tenant, job.ID)
		if err != nil || state.Status != "ready" || state.ObservationCount != w.Accrued+w.Coupons+w.Events {
			t.Fatal("job coverage", err)
		}
		saved, err := repo.BondFacts(ctx, scope, w.Request)
		if err != nil || saved == nil || saved.Hash != investmentHash(normalized) || saved.ID != job.ID || !reflect.DeepEqual(saved.BondFacts, normalized) {
			t.Fatal("saved facts differ from provider evidence", err)
		}
		// The persisted JSON is immutable and scoped, including explicit nulls/zero.
		var raw []byte
		if err = pool.DB.QueryRow(ctx, `SELECT data FROM market_bond_snapshots WHERE tenant_id=$1 AND job_id=$2`, tenant, job.ID).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var roundtrip md.BondFacts
		if json.Unmarshal(raw, &roundtrip) != nil || !reflect.DeepEqual(roundtrip, normalized) {
			t.Fatal("financial fields changed during persistence")
		}
		accrued += w.Accrued
		coupons += w.Coupons
		events += w.Events
		if w.Accrued == 0 {
			empty++
		}
	}
	t.Logf("private bond snapshots=%d accrued_rows=%d coupon_rows=%d event_rows=%d empty_accrued_windows=%d financial_terms_approved=false", len(index.Windows), accrued, coupons, events, empty)
}
