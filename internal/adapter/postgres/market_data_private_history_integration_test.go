package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/positron48/budget/internal/adapter/tinvest"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

// Opt-in: replay actual private market checkpoints through the durable queue.
// No credentials, broker calls, financial mappings or invented calendars.
func TestInvestmentPrivateMarketHistoryRehearsal(t *testing.T) {
	dir := os.Getenv("INVESTMENT_REHEARSAL_TINVEST_HISTORY_DIR")
	if dir == "" {
		t.Skip("private exact-UID market checkpoints required")
	}
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant string
	if err := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Disposable private market rehearsal','RUB') RETURNING id`).Scan(&tenant); err != nil {
		t.Fatal("create disposable tenant")
	}
	replayPrivateMarketHistory(t, pool, tenant, dir)
}

func replayPrivateMarketHistory(t *testing.T, pool *Pool, tenant, dir string) {
	t.Helper()
	ctx := context.Background()
	repo := NewMarketDataRepo(pool)
	scope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "personal-rehearsal"}
	if err := repo.ConfigureSource(ctx, scope, "Private checkpoint replay; not financial valuation approval", true); err != nil {
		t.Fatal(err)
	}
	var index struct {
		PlanSHA256  string
		Instruments []struct {
			State             string
			UnavailableSHA256 string
			Instrument        md.Instrument
			MetadataSHA256    string
			CheckpointSHA256  []string
		}
	}
	readPrivateHistory(t, filepath.Join(dir, "coverage.json"), "", &index)
	var plan struct {
		Version      int
		SourceSHA256 string
		Entries      []struct {
			InstrumentID string
			From, To     time.Time
		}
	}
	readPrivateHistory(t, filepath.Join(dir, "manifest.json"), index.PlanSHA256, &plan)
	if plan.Version != 1 || len(plan.Entries) == 0 || len(plan.Entries) != len(index.Instruments) {
		t.Fatal("invalid history universe")
	}
	jobs, empty, points, unavailable := 0, 0, 0, 0
	for i, e := range plan.Entries {
		witness := index.Instruments[i]
		if witness.Instrument.ID != e.InstrumentID {
			t.Fatal("history index listing mismatch")
		}
		if witness.State == "instrument_unavailable" {
			var refusal struct {
				InstrumentID, Stage string
				HTTPStatus          int
				RetrievedAt         time.Time
			}
			readPrivateHistory(t, filepath.Join(dir, e.InstrumentID+"-unavailable.json"), witness.UnavailableSHA256, &refusal)
			if refusal.InstrumentID != e.InstrumentID || refusal.Stage != "metadata" || refusal.HTTPStatus != 404 || refusal.RetrievedAt.IsZero() {
				t.Fatal("invalid unavailable evidence")
			}
			unavailable++
			continue
		}
		if witness.State != "observed" {
			t.Fatal("unknown history coverage state")
		}
		var metadata struct {
			InstrumentID string
			RetrievedAt  time.Time
			Response     json.RawMessage
		}
		readPrivateHistory(t, filepath.Join(dir, e.InstrumentID+"-metadata.json"), witness.MetadataSHA256, &metadata)
		instrument, err := tinvest.DecodeMarketInstrument(e.InstrumentID, metadata.Response)
		if err != nil || metadata.InstrumentID != e.InstrumentID || !reflect.DeepEqual(instrument, witness.Instrument) {
			t.Fatal("metadata mismatch")
		}
		unique := map[time.Time]md.Observation{}
		window := 0
		for from := e.From; from.Before(e.To); {
			to := from.Add(365 * 24 * time.Hour)
			if to.After(e.To) {
				to = e.To
			}
			q := md.JobRequest{InstrumentID: e.InstrumentID, Kind: "daily_candle", From: from, To: to}
			raw, err := json.Marshal(q)
			if err != nil {
				t.Fatal(err)
			}
			h := sha256.Sum256(raw)
			if window >= len(witness.CheckpointSHA256) {
				t.Fatal("missing window witness")
			}
			var saved struct {
				Request     md.JobRequest
				RetrievedAt time.Time
				History     tinvest.DailyHistory
			}
			readPrivateHistory(t, filepath.Join(dir, hex.EncodeToString(h[:])+".json"), witness.CheckpointSHA256[window], &saved)
			if saved.Request != q || saved.RetrievedAt.IsZero() {
				t.Fatal("checkpoint request mismatch")
			}
			observations, err := tinvest.CandleObservations(e.InstrumentID, saved.History)
			if err != nil {
				t.Fatal("normalize actual history", err)
			}
			for _, o := range observations {
				if old, ok := unique[o.At]; ok && old != o {
					t.Fatal("conflicting overlap")
				}
				unique[o.At] = o
			}
			job, err := repo.Enqueue(ctx, scope, q)
			if err != nil {
				t.Fatal("enqueue private evidence", err)
			}
			claimed, err := repo.Claim(ctx, scope, time.Minute)
			if err != nil || claimed.ID != job.ID {
				t.Fatal("claim private evidence", err)
			}
			if err = repo.CompleteWithInstrument(ctx, claimed, instrument, observations); err != nil {
				t.Fatal("publish private evidence", err)
			}
			state, err := repo.JobStatus(ctx, tenant, job.ID)
			if err != nil || state.Status != "ready" || state.ObservationCount != len(observations) {
				t.Fatal("coverage/job disagreement")
			}
			if len(observations) == 0 {
				empty++
				if state.CoverageState != "empty_unexplained" {
					t.Fatal("empty history silently accepted as calendar")
				}
			}
			jobs++
			window++
			from = to
		}
		if window != len(witness.CheckpointSHA256) {
			t.Fatal("unexpected window witnesses")
		}
		stored, err := repo.Observations(ctx, scope, e.InstrumentID, "daily_candle", e.From, e.To)
		if err != nil || len(stored) != len(unique) {
			t.Fatal("history count mismatch")
		}
		for _, o := range stored {
			if o.Revision != 1 || unique[o.At] != o.Observation {
				t.Fatal("persisted observation changed")
			}
		}
		// Replay the same source values. No revision, price substitution or nominal inference.
		for start := 0; start < len(stored); start += 1000 {
			end := start + 1000
			if end > len(stored) {
				end = len(stored)
			}
			batch := make([]md.Observation, end-start)
			for k := range batch {
				batch[k] = stored[start+k].Observation
			}
			replayed, err := repo.SaveObservations(ctx, scope, batch)
			if err != nil {
				t.Fatal("replay private observations")
			}
			for _, o := range replayed {
				if o.Revision != 1 {
					t.Fatal("identical replay created revisions")
				}
			}
		}
		points += len(unique)
	}
	var revisions, rows int
	if err := pool.DB.QueryRow(ctx, `SELECT (SELECT count(*) FROM market_provider_observations WHERE tenant_id=$1),(SELECT count(*) FROM market_provider_observation_revisions WHERE tenant_id=$1)`, tenant).Scan(&rows, &revisions); err != nil || rows != points || revisions != points {
		t.Fatal("final history revisions mismatch")
	}
	t.Logf("Private exact-UID evidence: instruments=%d unavailable=%d windows=%d empty_windows=%d unique_observations=%d; replay revisions unchanged; disposable PostgreSQL", len(plan.Entries), unavailable, jobs, empty, points)
}

func readPrivateHistory(t *testing.T, path, wantHash string, v any) {
	t.Helper()
	i, err := os.Lstat(path)
	if err != nil || !i.Mode().IsRegular() || i.Mode().Perm()&0077 != 0 || i.Size() > 16<<20 {
		t.Fatal("invalid private market checkpoint")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("read private market checkpoint")
	}
	h := sha256.Sum256(raw)
	if wantHash != "" && hex.EncodeToString(h[:]) != wantHash {
		t.Fatal("private market checkpoint digest mismatch")
	}
	if err = json.Unmarshal(raw, v); err != nil {
		t.Fatal("invalid private market checkpoint JSON")
	}
}
