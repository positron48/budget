package fximport

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeSource struct {
	quotes []Quote
	err    error
}

func (s fakeSource) Fetch(context.Context, time.Time, time.Time) ([]Quote, error) {
	return s.quotes, s.err
}

type fakeStore struct {
	calls int
	err   error
}

func (s *fakeStore) SaveCBRRates(context.Context, []Quote) error { s.calls++; return s.err }
func TestImport(t *testing.T) {
	at := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, mode := range []string{"ok", "dry", "fetch-error", "empty", "save-error", "invalid-period"} {
		t.Run(mode, func(t *testing.T) {
			source := fakeSource{quotes: []Quote{{Currency: "USD", Rate: "100", Date: at}}}
			store := &fakeStore{}
			end := at
			if mode == "fetch-error" {
				source.err = errors.New("source unavailable")
			}
			if mode == "empty" {
				source.quotes = nil
			}
			if mode == "save-error" {
				store.err = errors.New("storage unavailable")
			}
			if mode == "invalid-period" {
				end = at.AddDate(0, 0, -1)
			}
			n, err := (Service{Source: source, Store: store}).Import(context.Background(), at, end, mode == "dry")
			if mode == "ok" || mode == "dry" {
				if err != nil || n != 1 {
					t.Fatalf("%d %v", n, err)
				}
			} else if err == nil {
				t.Fatal("expected error")
			}
			want := 0
			if mode == "ok" || mode == "save-error" {
				want = 1
			}
			if store.calls != want {
				t.Fatalf("writes: %d want %d", store.calls, want)
			}
		})
	}
}
func TestMoscowDate(t *testing.T) {
	at := time.Date(2026, 10, 2, 21, 30, 0, 0, time.UTC)
	if got := MoscowDate(at).Format(time.RFC3339); got != "2026-10-03T00:00:00Z" {
		t.Fatal(got)
	}
}
