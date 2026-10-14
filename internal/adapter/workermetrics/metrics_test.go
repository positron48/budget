package workermetrics

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/positron48/budget/internal/usecase/workerhealth"
)

func TestMetricsPersistedStateFailureRedactionAndRecovery(t *testing.T) {
	fail := false
	enabled := true
	m, err := New(context.Background(), "marketdata", func(ctx context.Context) (workerhealth.Snapshot, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 3*time.Second {
			t.Error("unbounded query")
		}
		if fail {
			return workerhealth.Snapshot{}, errors.New("secret-account SQL password")
		}
		return workerhealth.Snapshot{SourceEnabled: &enabled, QuoteStates: map[string]int64{"fresh": 7, "missing": 1, "private-instrument": 9}, NAVStates: map[string]int64{"incomplete": 2, "private-instrument": 9}, Queues: []workerhealth.Queue{
			{Kind: "daily_candle", Status: "pending", Count: 4, Due: 2, OldestDueSeconds: 120},
			{Kind: "dividend_facts", Status: "pending", Count: 1, Due: 1},
			{Kind: "private-instrument-id", Status: "error", Count: 17},
		}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	h := m.Handler()
	scrape := func() string {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", "/metrics", nil))
		if r.Code != 200 {
			t.Fatal(r.Code, r.Body.String())
		}
		return r.Body.String()
	}
	m.Observe(nil)
	m.ObserveJob("daily_candle", "retry")
	m.ObserveJob("dividend_facts", "ready")
	m.ObserveJob("private-account", "error")
	body := scrape()
	for _, want := range []string{
		`budget_investment_worker_snapshot_success{worker="marketdata"} 1`,
		`budget_investment_worker_mapped_quote_endpoints{state="fresh",worker="marketdata"} 7`,
		`budget_investment_worker_mapped_quote_endpoints{state="missing",worker="marketdata"} 1`,
		`budget_investment_worker_due_jobs{kind="daily_candle",status="pending",worker="marketdata"} 2`,
		`budget_investment_worker_due_jobs{kind="dividend_facts",status="pending",worker="marketdata"} 1`,
		`budget_investment_worker_job_attempts_total{kind="dividend_facts",result="ready",worker="marketdata"} 1`,
		`budget_investment_worker_job_attempts_total{kind="daily_candle",result="retry",worker="marketdata"} 1`,
		`budget_investment_worker_nav_endpoints{state="incomplete",worker="marketdata"} 2`,
	} {
		if !strings.Contains(body, want) {
			t.Fatal("missing metric", want, body)
		}
	}
	if strings.Contains(body, "private-") {
		t.Fatal("unbounded private label", body)
	}
	fail = true
	body = scrape()
	if !strings.Contains(body, `budget_investment_worker_snapshot_success{worker="marketdata"} 0`) || strings.Contains(body, "budget_investment_worker_due_jobs{") || strings.Contains(body, "secret-account") {
		t.Fatal("failed query must not invent healthy queue", body)
	}
	fail = false
	if !strings.Contains(scrape(), `budget_investment_worker_snapshot_success{worker="marketdata"} 1`) {
		t.Fatal("recovery failed")
	}
}

func TestMetricsListenerLifecycle(t *testing.T) {
	if os.Getenv("WORKER_METRICS_HTTP_TEST") != "1" {
		t.Skip("set WORKER_METRICS_HTTP_TEST=1 for local TCP listener test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m, err := New(ctx, "investmentnav", func(context.Context) (workerhealth.Snapshot, error) { return workerhealth.Snapshot{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := reservation.Addr().String()
	if _, err = m.Start(addr); err == nil {
		t.Fatal("occupied metrics listener ignored")
	}
	if err = reservation.Close(); err != nil {
		t.Fatal(err)
	}
	stop, err := m.Start(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	client := &http.Client{Timeout: time.Second}
	response, err := client.Get("http://" + addr + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	if closeErr := response.Body.Close(); err == nil {
		err = closeErr
	}
	if err != nil || response.StatusCode != 200 || !strings.Contains(string(body), `budget_investment_worker_snapshot_success{worker="investmentnav"} 1`) {
		t.Fatal(string(body), err)
	}
	cancel()
	deadline := time.Now().Add(time.Second)
	for {
		listener, err := net.Listen("tcp", addr)
		if err == nil {
			if err = listener.Close(); err != nil {
				t.Fatal(err)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("worker cancellation did not close listener")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestMetricsCancellationAndDisabledListener(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m, err := New(ctx, "investmentnav", func(ctx context.Context) (workerhealth.Snapshot, error) { return workerhealth.Snapshot{}, ctx.Err() })
	if err != nil {
		t.Fatal(err)
	}
	closeMetrics, err := m.Start("")
	if err != nil {
		t.Fatal(err)
	}
	closeMetrics()
	r := httptest.NewRecorder()
	m.Handler().ServeHTTP(r, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.Contains(r.Body.String(), `budget_investment_worker_snapshot_success{worker="investmentnav"} 0`) {
		t.Fatal(r.Body.String())
	}
	if _, err = New(context.Background(), "private-tenant", nil); err == nil {
		t.Fatal("arbitrary worker label accepted")
	}
	if _, err = m.Start("invalid-address"); err == nil {
		t.Fatal("invalid listener ignored")
	}
}

func TestAlphaFXFreshnessIndependentOfSuccessfulLoop(t *testing.T) {
	m, err := New(context.Background(), "alphafx", func(context.Context) (workerhealth.Snapshot, error) {
		return workerhealth.Snapshot{FXStates: map[string]int64{"fresh": 1, "expired": 1, "missing": 0, "secret-pair": 99}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	m.Observe(nil)
	recorder := httptest.NewRecorder()
	m.Handler().ServeHTTP(recorder, httptest.NewRequest("GET", "/metrics", nil))
	body := recorder.Body.String()
	if !strings.Contains(body, `budget_investment_worker_fx_endpoints{state="expired",worker="alphafx"} 1`) || strings.Contains(body, "secret-pair") {
		t.Fatal("FX freshness lost or unbounded label exposed")
	}
}
