package marketdata

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"
)

type Queue interface {
	CompleteCatalog(context.Context, Job, CatalogResult) error
	Claim(context.Context, Scope, time.Duration) (Job, error)
	Renew(context.Context, Job, time.Duration) error
	CompleteWithInstrument(context.Context, Job, Instrument, []Observation) error
	Fail(context.Context, Job, string, bool, time.Duration) error
}
type BondProvider interface {
	LoadBondFacts(context.Context, JobRequest) (BondFacts, error)
}
type BondQueue interface {
	CompleteBondFacts(context.Context, Job, BondFacts) error
}
type Provider interface {
	SearchMarketCatalog(context.Context, JobRequest) (CatalogResult, error)
	LoadMarketInstrument(context.Context, string) (Instrument, error)
	LoadMarketObservations(context.Context, JobRequest) ([]Observation, error)
}

// Worker binds one credential-bearing provider to one explicit entitlement
// scope. It cannot take another organization's queued jobs.
type Worker struct {
	Queue    Queue
	Provider Provider
	Scope    Scope
	Lease    time.Duration
	// Observe receives bounded outcomes only; no credential or job identity.
	Observe func(kind, outcome string)
}

func (w Worker) RunOne(ctx context.Context) (resultErr error) {
	if w.Queue == nil || w.Provider == nil || w.Lease < 30*time.Second || w.Lease > 10*time.Minute {
		return ErrInvalid
	}
	if err := ValidateScope(w.Scope); err != nil {
		return err
	}
	job, err := w.Queue.Claim(ctx, w.Scope, w.Lease)
	if err != nil {
		return err
	}
	outcome := "persistence_error"
	defer func() {
		if w.Observe == nil {
			return
		}
		if resultErr != nil {
			outcome = "persistence_error"
			if errors.Is(resultErr, ErrLeaseLost) {
				outcome = "lease_lost"
			}
			if ctx.Err() != nil {
				outcome = "cancelled"
			}
		}
		w.Observe(job.Kind, outcome)
	}()
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := make(chan struct{})
	heartbeat := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(w.Lease / 3)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				heartbeat <- nil
				return
			case <-workCtx.Done():
				heartbeat <- workCtx.Err()
				return
			case <-ticker.C:
				if err := w.Queue.Renew(workCtx, job, w.Lease); err != nil {
					cancel()
					heartbeat <- err
					return
				}
			}
		}
	}()

	var instrument Instrument
	var observations []Observation
	var catalog CatalogResult
	var bond BondFacts
	var dividends DividendFacts
	var loadErr error
	switch job.Kind {
	case "dividend_facts":
		provider, ok := w.Provider.(DividendProvider)
		_, queueOK := w.Queue.(DividendQueue)
		if !ok || !queueOK {
			loadErr = ErrInvalid
		} else {
			dividends, loadErr = provider.LoadDividendFacts(workCtx, job.JobRequest)
		}
	case "bond_facts":
		provider, ok := w.Provider.(BondProvider)
		_, queueOK := w.Queue.(BondQueue)
		if !ok || !queueOK {
			loadErr = ErrInvalid
		} else {
			bond, loadErr = provider.LoadBondFacts(workCtx, job.JobRequest)
		}
	case "catalog_search":
		catalog, loadErr = w.Provider.SearchMarketCatalog(workCtx, job.JobRequest)
	default:
		instrument, loadErr = w.Provider.LoadMarketInstrument(workCtx, job.InstrumentID)
		if loadErr == nil {
			observations, loadErr = w.Provider.LoadMarketObservations(workCtx, job.JobRequest)
		}
	}

	close(stop)
	if leaseErr := <-heartbeat; leaseErr != nil {
		return leaseErr
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if loadErr == nil {
		outcome = "ready"
		if job.Kind == "dividend_facts" {
			return w.Queue.(DividendQueue).CompleteDividendFacts(ctx, job, dividends)
		}
		if job.Kind == "bond_facts" {
			return w.Queue.(BondQueue).CompleteBondFacts(ctx, job, bond)
		}
		if job.Kind == "catalog_search" {
			return w.Queue.CompleteCatalog(ctx, job, catalog)
		}
		return w.Queue.CompleteWithInstrument(ctx, job, instrument, observations)
	}
	code, retry, delay := failurePolicy(loadErr, job.Attempts)
	outcome = "error"
	if retry && job.Attempts < 5 {
		outcome = "retry"
	}
	return w.Queue.Fail(ctx, job, code, retry, delay)
}
func failurePolicy(err error, attempt int) (string, bool, time.Duration) {
	if errors.Is(err, ErrInvalid) {
		return "invalid_response", false, 0
	}
	var temporary interface{ Temporary() bool }
	if errors.As(err, &temporary) && !temporary.Temporary() {
		return "provider_rejected", false, 0
	}
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 5 {
		attempt = 5
	}
	delay := time.Duration(1<<attempt)*time.Second + time.Duration(rand.IntN(1000))*time.Millisecond
	var retryAfter interface{ RetryDelay() time.Duration }
	if errors.As(err, &retryAfter) && retryAfter.RetryDelay() > delay {
		delay = retryAfter.RetryDelay()
	}
	if delay > 24*time.Hour {
		delay = 24 * time.Hour
	}
	return "provider_temporary", true, delay
}
