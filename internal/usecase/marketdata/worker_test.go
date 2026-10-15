package marketdata

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeQueue struct {
	job               Job
	completed, failed bool
	code              string
	retry             bool
	delay             time.Duration
	observations      []Observation
	persistErr        error
}

func (q *fakeQueue) Claim(context.Context, Scope, time.Duration) (Job, error) { return q.job, nil }
func (q *fakeQueue) Renew(context.Context, Job, time.Duration) error          { return nil }
func (q *fakeQueue) CompleteWithInstrument(_ context.Context, _ Job, _ Instrument, o []Observation) error {
	q.completed = true
	q.observations = o
	return q.persistErr
}
func (q *fakeQueue) Fail(_ context.Context, _ Job, c string, r bool, d time.Duration) error {
	q.failed = true
	q.code = c
	q.retry = r
	q.delay = d
	return q.persistErr
}

type fakeProvider struct{ err error }

func (p fakeProvider) LoadMarketObservations(context.Context, JobRequest) ([]Observation, error) {
	return nil, p.err
}

type failure struct {
	temporary bool
	after     time.Duration
}

func (f failure) Error() string             { return "must not be stored" }
func (f failure) Temporary() bool           { return f.temporary }
func (f failure) RetryDelay() time.Duration { return f.after }
func TestWorkerOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		code  string
		retry bool
	}{
		{"empty", nil, "", false}, {"invalid", ErrInvalid, "invalid_response", false}, {"unauthorized", failure{}, "provider_rejected", false}, {"rate limit", failure{true, time.Minute}, "provider_temporary", true}, {"network", errors.New("network"), "provider_temporary", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := &fakeQueue{job: Job{Attempts: 1, JobRequest: JobRequest{Kind: "last_price"}}}
			worker := Worker{Queue: q, Provider: fakeProvider{tc.err}, Scope: Scope{TenantID: "11111111-1111-4111-8111-111111111111", Provider: "tinvest", AccessScope: "personal"}, Lease: time.Minute}
			calls := 0
			outcome := ""
			worker.Observe = func(kind, result string) {
				if kind != "last_price" {
					t.Error(kind)
				}
				calls++
				outcome = result
			}
			if err := worker.RunOne(context.Background()); err != nil {
				t.Fatal(err)
			}
			if q.completed != (tc.err == nil) || q.failed != (tc.err != nil) || q.code != tc.code || q.retry != tc.retry {
				t.Fatal(q)
			}
			if tc.name == "rate limit" && q.delay != time.Minute {
				t.Fatal(q.delay)
			}
			want := "empty"
			if tc.err != nil {
				want = "error"
				if tc.retry {
					want = "retry"
				}
			}
			if calls != 1 || outcome != want {
				t.Fatal("provider failure mislabeled", calls, outcome, want)
			}
		})
	}
}

func TestWorkerOutcomeLastAttemptAndPersistenceFailure(t *testing.T) {
	for _, tc := range []struct {
		name    string
		persist error
		want    string
	}{
		{"exhausted", nil, "error"},
		{"write failure", errors.New("private SQL error"), "persistence_error"},
		{"lease changed", ErrLeaseLost, "lease_lost"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := &fakeQueue{job: Job{Attempts: 5, JobRequest: JobRequest{Kind: "last_price"}}, persistErr: tc.persist}
			outcome := ""
			w := Worker{Queue: q, Provider: fakeProvider{errors.New("temporary provider failure")}, Scope: Scope{TenantID: "11111111-1111-4111-8111-111111111111", Provider: "tinvest", AccessScope: "personal"}, Lease: time.Minute, Observe: func(_ string, result string) { outcome = result }}
			err := w.RunOne(context.Background())
			if !errors.Is(err, tc.persist) || outcome != tc.want {
				t.Fatal(err, outcome)
			}
		})
	}
}

func (p fakeProvider) LoadMarketInstrument(context.Context, string) (Instrument, error) {
	return Instrument{}, nil
}

func (q *fakeQueue) CompleteCatalog(context.Context, Job, CatalogResult) error {
	q.completed = true
	return nil
}
func (p fakeProvider) SearchMarketCatalog(context.Context, JobRequest) (CatalogResult, error) {
	return CatalogResult{}, p.err
}
