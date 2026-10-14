// Package workermetrics exposes private worker diagnostics without identifiers,
// credentials, SQL errors, instrument names or financial amounts.
package workermetrics

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/positron48/budget/internal/usecase/workerhealth"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type ReadSnapshot func(context.Context) (workerhealth.Snapshot, error)

type Monitor struct {
	read                                      ReadSnapshot
	ctx                                       context.Context
	scrape, count, due, age, expired, enabled *prometheus.Desc
	navStates                                 *prometheus.Desc
	fxStates                                  *prometheus.Desc
	quoteStates                               *prometheus.Desc
	iterations                                *prometheus.CounterVec
	jobs                                      *prometheus.CounterVec
	lastSuccess                               prometheus.Gauge
}

func New(ctx context.Context, worker string, read ReadSnapshot) (*Monitor, error) {
	if worker != "marketdata" && worker != "investmentnav" && worker != "investcbr" && worker != "alphafx" {
		return nil, errors.New("invalid worker metrics name")
	}
	labels := prometheus.Labels{"worker": worker}
	desc := func(name, help string, variable []string) *prometheus.Desc {
		return prometheus.NewDesc("budget_investment_worker_"+name, help, variable, labels)
	}
	return &Monitor{ctx: ctx, read: read,
		scrape:      desc("snapshot_success", "Whether the current persisted-state query succeeded.", nil),
		count:       desc("jobs", "Persisted jobs, including terminal history; ready does not prove complete valuation.", []string{"kind", "status"}),
		due:         desc("due_jobs", "Pending jobs past available_at or running jobs past lease_until.", []string{"kind", "status"}),
		age:         desc("oldest_due_seconds", "Age of oldest due work, excluding deliberate backoff and active leases.", []string{"kind", "status"}),
		expired:     desc("expired_leases", "Running jobs whose lease expired.", []string{"kind", "status"}),
		enabled:     desc("source_enabled", "Whether the explicitly bound provider scope is enabled.", nil),
		fxStates:    desc("fx_endpoints", "Freshness of the latest eligible USD/RUB and EUR/RUB retrospective fixings, independent of loop success.", []string{"state"}),
		quoteStates: desc("mapped_quote_endpoints", "Usable exchange quotes for held reviewed mappings; missing mappings are not included. Independent of successful jobs.", []string{"state"}),
		navStates:   desc("nav_endpoints", "Quality of expected last completed daily points for enabled authorized watches, including auto extension.", []string{"state"}),
		iterations:  prometheus.NewCounterVec(prometheus.CounterOpts{Name: "budget_investment_worker_iterations_total", Help: "Loop iterations by transport outcome; success does not imply ready or complete data.", ConstLabels: labels}, []string{"result"}),
		jobs:        prometheus.NewCounterVec(prometheus.CounterOpts{Name: "budget_investment_worker_job_attempts_total", Help: "Job attempts by bounded outcome; ready can still contain valuation gaps.", ConstLabels: labels}, []string{"kind", "result"}),
		lastSuccess: prometheus.NewGauge(prometheus.GaugeOpts{Name: "budget_investment_worker_last_loop_success_timestamp_seconds", Help: "Last successful loop iteration; not the age of quotes, FX or NAV.", ConstLabels: labels}),
	}, nil
}

func (m *Monitor) ObserveJob(kind, result string) {
	if !validQueue(workerhealth.Queue{Kind: kind, Status: "pending"}) {
		return
	}
	switch result {
	case "ready", "retry", "error", "persistence_error", "lease_lost", "cancelled":
		m.jobs.WithLabelValues(kind, result).Inc()
	}
}

func (m *Monitor) Observe(err error) {
	result := "success"
	if err != nil {
		result = "error"
	} else {
		m.lastSuccess.SetToCurrentTime()
	}
	m.iterations.WithLabelValues(result).Inc()
}

func (m *Monitor) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{m.scrape, m.count, m.due, m.age, m.expired, m.enabled, m.navStates, m.fxStates, m.quoteStates} {
		ch <- d
	}
	m.iterations.Describe(ch)
	m.jobs.Describe(ch)
	m.lastSuccess.Describe(ch)
}

func (m *Monitor) Collect(ch chan<- prometheus.Metric) {
	m.iterations.Collect(ch)
	m.jobs.Collect(ch)
	m.lastSuccess.Collect(ch)
	if m.read == nil {
		return
	}
	ctx, cancel := context.WithTimeout(m.ctx, 3*time.Second)
	defer cancel()
	s, err := m.read(ctx)
	if err != nil {
		ch <- prometheus.MustNewConstMetric(m.scrape, prometheus.GaugeValue, 0)
		return // Never report zero backlog after a failed read or expose its error.
	}
	ch <- prometheus.MustNewConstMetric(m.scrape, prometheus.GaugeValue, 1)
	if s.QuoteStates != nil {
		for _, state := range []string{"fresh", "expired", "missing"} {
			n := s.QuoteStates[state]
			if n < 0 {
				n = 0
			}
			ch <- prometheus.MustNewConstMetric(m.quoteStates, prometheus.GaugeValue, float64(n), state)
		}
	}
	if s.FXStates != nil {
		for _, state := range []string{"fresh", "expired", "missing"} {
			n := s.FXStates[state]
			if n < 0 {
				n = 0
			}
			ch <- prometheus.MustNewConstMetric(m.fxStates, prometheus.GaugeValue, float64(n), state)
		}
	}
	if s.NAVStates != nil {
		for _, state := range []string{"ready", "incomplete", "stale", "unavailable"} {
			ch <- prometheus.MustNewConstMetric(m.navStates, prometheus.GaugeValue, float64(s.NAVStates[state]), state)
		}
	}
	if s.SourceEnabled != nil {
		v := 0.0
		if *s.SourceEnabled {
			v = 1
		}
		ch <- prometheus.MustNewConstMetric(m.enabled, prometheus.GaugeValue, v)
	}
	for _, q := range s.Queues {
		if !validQueue(q) {
			continue
		}
		for _, v := range []struct {
			d *prometheus.Desc
			n float64
		}{{m.count, float64(q.Count)}, {m.due, float64(q.Due)}, {m.age, q.OldestDueSeconds}, {m.expired, float64(q.ExpiredLeases)}} {
			ch <- prometheus.MustNewConstMetric(v.d, prometheus.GaugeValue, v.n, q.Kind, q.Status)
		}
	}
}

func validQueue(q workerhealth.Queue) bool {
	switch q.Kind {
	case "last_price", "daily_candle", "bond_facts", "dividend_facts", "catalog_search", "daily_nav":
	default:
		return false
	}
	switch q.Status {
	case "pending", "running", "ready", "error":
		return true
	default:
		return false
	}
}

func (m *Monitor) Handler() http.Handler {
	r := prometheus.NewRegistry()
	r.MustRegister(m)
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(r, promhttp.HandlerOpts{MaxRequestsInFlight: 1, Timeout: 4 * time.Second}))
	return mux
}

// Start is optional. Binding failure is fatal to the caller; shutdown follows
// the worker context. HTTP is intended for an internal scrape network only.
func (m *Monitor) Start(addr string) (func(), error) {
	if addr == "" {
		return func() {}, nil
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, errors.New("cannot bind investment worker metrics")
	}
	server := &http.Server{Handler: m.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	done := make(chan struct{})
	go func() { defer close(done); _ = server.Serve(listener) }()
	go func() {
		select {
		case <-m.ctx.Done():
			_ = server.Close()
		case <-done:
		}
	}()
	return func() { _ = server.Close(); <-done }, nil
}
