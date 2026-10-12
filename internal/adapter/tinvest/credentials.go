package tinvest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"
	"time"
)

// NewFromSecretFile supports a mounted Kubernetes secret. Neither content nor
// path is included in errors, logs, command arguments, or environment values.
func NewFromSecretFile(path string) (*Client, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("T-Invest secret file unavailable")
	}
	defer func() { _ = file.Close() }()
	value, err := io.ReadAll(io.LimitReader(file, 8193))
	defer clear(value)
	if err != nil || len(value) > 8192 {
		return nil, errors.New("invalid T-Invest secret file")
	}
	return newClient(strings.TrimSpace(string(value)))
}

// SetRequestGate must be called before using the client concurrently. The
// callback receives only an irreversible credential fingerprint for quota
// coordination; it never receives the credential itself.
func (c *Client) SetRequestGate(gate func(context.Context, string) error) {
	digest := sha256.Sum256([]byte("tinvest:" + c.token))
	key := hex.EncodeToString(digest[:])
	c.requestGate = func(ctx context.Context) error { return gate(ctx, key) }
}

// SetRequestBackoff shares a provider's 429 delay across worker replicas.
func (c *Client) SetRequestBackoff(backoff func(context.Context, string, time.Duration) error) {
	digest := sha256.Sum256([]byte("tinvest:" + c.token))
	key := hex.EncodeToString(digest[:])
	c.requestBackoff = func(ctx context.Context, delay time.Duration) error { return backoff(ctx, key, delay) }
}
