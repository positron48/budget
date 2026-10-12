package tinvest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecretFileAndGateRedaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte("fake-test-credential\n"), 0600); err != nil {
		t.Fatal(err)
	}
	client, err := NewFromSecretFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var key string
	client.SetRequestGate(func(_ context.Context, k string) error { key = k; return errors.New("sensitive backend details") })
	_, err = client.FindInstrument(context.Background(), "SBER")
	if err == nil || strings.Contains(err.Error(), "sensitive") || strings.Contains(key, "fake") || len(key) != 64 {
		t.Fatal("unsafe gate error or fingerprint")
	}
	same, _ := newClient("fake-test-credential")
	var sameKey string
	same.SetRequestGate(func(_ context.Context, k string) error { sameKey = k; return nil })
	_ = same.requestGate(context.Background())
	if sameKey != key {
		t.Fatal("replicas do not share gate")
	}
	for _, value := range []string{"", "fake invalid token", strings.Repeat("x", 8193)} {
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := NewFromSecretFile(path); err == nil || strings.Contains(err.Error(), "fake invalid") {
			t.Fatal("invalid secret accepted or disclosed")
		}
	}
}
