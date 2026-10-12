package main

import (
	"context"
	"errors"
	"time"

	"github.com/positron48/budget/internal/adapter/tinvest"
)

// A bounded read-only retry observes the provider's delay. Long Retry-After
// values remain checkpoint errors for a later run instead of blocking forever.
func retryRead(ctx context.Context, request func() error) error {
	return retryWithSleep(ctx, request, func(ctx context.Context, delay time.Duration) error {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		}
	})
}
func retryWithSleep(ctx context.Context, request func() error, sleep func(context.Context, time.Duration) error) error {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if err = ctx.Err(); err != nil {
			return err
		}
		err = request()
		if err == nil {
			return nil
		}
		var failure *tinvest.HTTPError
		if !errors.As(err, &failure) || !failure.Temporary() || attempt == 2 {
			return err
		}
		delay := time.Second * time.Duration(2<<attempt)
		if failure.RetryAfter > delay {
			delay = failure.RetryAfter
		}
		if delay > time.Minute {
			return err
		}
		if err = sleep(ctx, delay); err != nil {
			return err
		}
	}
	return err
}
