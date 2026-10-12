package alphavantage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"
)

// NewFromSecretFile supports projected Kubernetes secret symlinks. The opened
// target must be a bounded private regular file; neither path nor data is logged.
func NewFromSecretFile(path string) (*Client, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("alpha secret file unavailable")
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0027 != 0 || info.Size() > 256 {
		return nil, errors.New("invalid Alpha secret file")
	}
	raw, err := io.ReadAll(io.LimitReader(f, 257))
	defer clear(raw)
	if err != nil || len(raw) > 256 {
		return nil, errors.New("invalid Alpha secret file")
	}
	return newClient(strings.TrimSpace(string(raw)))
}

// SetBatchGate binds a shared persistent quota using an irreversible, provider-
// specific credential fingerprint. It must be set before concurrent use.
func (c *Client) SetBatchGate(gate func(context.Context, string) (bool, error)) {
	digest := sha256.Sum256([]byte("alphavantage-fx-batch:" + c.token))
	key := hex.EncodeToString(digest[:])
	c.batchGate = func(ctx context.Context) (bool, error) { return gate(ctx, key) }
}
func (c *Client) BeginBatch(ctx context.Context) (bool, error) {
	if c.batchGate == nil {
		return false, errors.New("persistent Alpha batch gate required")
	}
	return c.batchGate(ctx)
}
