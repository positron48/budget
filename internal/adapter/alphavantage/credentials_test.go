package alphavantage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecretFileAndPersistentBatchGate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	secret := "syntheticALPHA12345"
	if err := os.WriteFile(path, []byte(secret+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	client, err := NewFromSecretFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprintf("%#v", client), secret) {
		t.Fatal("credential formatting leak")
	}
	if _, err = client.BeginBatch(context.Background()); err == nil {
		t.Fatal("ungated worker allowed")
	}
	calls := 0
	client.SetBatchGate(func(_ context.Context, key string) (bool, error) {
		calls++
		if len(key) != 64 || strings.Contains(key, secret) {
			t.Fatal("invalid gate fingerprint")
		}
		return calls == 1, nil
	})
	first, err := client.BeginBatch(context.Background())
	if err != nil || !first {
		t.Fatal(err)
	}
	second, err := client.BeginBatch(context.Background())
	if err != nil || second {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "projected-token")
	if err = os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err = NewFromSecretFile(link); err != nil {
		t.Fatal("projected Kubernetes secret", err)
	}
	for _, mode := range []os.FileMode{0644, 0660} {
		if err = os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if _, err = NewFromSecretFile(path); err == nil {
			t.Fatal("unsafe credential permissions accepted")
		}
	}
	if err = os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte(strings.Repeat("x", 257)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = NewFromSecretFile(path); err == nil || strings.Contains(err.Error(), "xxx") {
		t.Fatal("invalid credential was accepted or exposed")
	}
}
