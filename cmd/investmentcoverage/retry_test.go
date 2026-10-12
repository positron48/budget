package main

import (
	"context"
	"errors"
	"github.com/positron48/budget/internal/adapter/tinvest"
	"testing"
	"time"
)

func TestReadRetryPolicy(t *testing.T) {
	for _, tc := range []struct {
		code   int
		after  time.Duration
		calls  int
		delays []time.Duration
	}{
		{429, 7 * time.Second, 3, []time.Duration{7 * time.Second, 7 * time.Second}},
		{503, 0, 3, []time.Duration{2 * time.Second, 4 * time.Second}},
		{401, 0, 1, nil}, {429, time.Hour, 1, nil},
	} {
		calls := 0
		var delays []time.Duration
		err := retryWithSleep(context.Background(), func() error { calls++; return &tinvest.HTTPError{StatusCode: tc.code, RetryAfter: tc.after} }, func(_ context.Context, d time.Duration) error { delays = append(delays, d); return nil })
		if err == nil || calls != tc.calls || len(delays) != len(tc.delays) {
			t.Fatalf("%+v: %v %d %v", tc, err, calls, delays)
		}
		for i := range delays {
			if delays[i] != tc.delays[i] {
				t.Fatal(delays)
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	err := retryRead(ctx, func() error { calls++; return nil })
	if !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatal("cancelled request executed")
	}
}
