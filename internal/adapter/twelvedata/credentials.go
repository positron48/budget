package twelvedata

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

func NewFromSecretFile(path string) (*Client, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, errors.New("twelve data secret file unavailable")
	}
	defer func() { _ = f.Close() }()
	b, e := io.ReadAll(io.LimitReader(f, 8193))
	defer clear(b)
	if e != nil || len(b) > 8192 {
		return nil, errors.New("invalid Twelve Data secret file")
	}
	return newClient(strings.TrimSpace(string(b)))
}

// SetRequestGate shares only an irreversible, provider-namespaced fingerprint.
// Call before using the client concurrently. No credential enters queue data.
func (c *Client) SetRequestGate(gate func(context.Context, string) error) {
	h := sha256.Sum256([]byte("twelvedata:" + c.token))
	key := hex.EncodeToString(h[:])
	c.requestGate = func(ctx context.Context) error { return gate(ctx, key) }
}

func (c *Client) SetRequestBackoff(backoff func(context.Context, string, time.Duration) error) {
	h := sha256.Sum256([]byte("twelvedata:" + c.token))
	key := hex.EncodeToString(h[:])
	c.requestBackoff = func(ctx context.Context, delay time.Duration) error { return backoff(ctx, key, delay) }
}
