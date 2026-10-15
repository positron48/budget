package investmentvaluation

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Signed pages let the backend calculate returns from the same read snapshot
// already used for the chart. They contain no auth credential, confer no access,
// expire promptly, and are never persisted. Every summary rechecks authorization
// and the current journal/market basis. Shared signing keys support replicas.
type TrackerSnapshotSigner struct{ key []byte }
type TrackerSnapshotDay struct {
	Date, Value, Flow string
	Complete, Reset   bool
	FlowReason        string
}
type TrackerSnapshot struct {
	Tenant, Actor, Basis, FXMode string
	Request                      TrackerHistoryRequest
	FXVersion                    int64
	Expires                      int64
	Days                         []TrackerSnapshotDay
}

func NewTrackerSnapshotSigner(root string) *TrackerSnapshotSigner {
	if root == "" {
		return nil
	}
	h := hmac.New(sha256.New, []byte(root))
	_, _ = h.Write([]byte("budget:securities-performance:pages:v1"))
	return &TrackerSnapshotSigner{key: h.Sum(nil)}
}
func (*TrackerSnapshotSigner) String() string     { return "tracker snapshot signer (key redacted)" }
func (s *TrackerSnapshotSigner) GoString() string { return s.String() }

func (s *TrackerSnapshotSigner) Seal(tenant, actor string, q TrackerHistoryRequest, data TrackerHistory) (string, error) {
	if s == nil || len(data.Points) == 0 || len(data.Points) > 31 || q.PerformanceFrom == "" {
		return "", errors.New("invalid performance page")
	}
	claim := TrackerSnapshot{Tenant: tenant, Actor: actor, Request: q, Basis: data.BasisKey, FXVersion: data.FXPolicyVersion, FXMode: data.FXMode, Expires: data.ReadAt.Add(15 * time.Minute).Unix()}
	appendPoint := func(p TrackerHistoryPoint) {
		d := TrackerSnapshotDay{Date: p.Date, Complete: p.State == "ready", Reset: p.Reset, FlowReason: p.FlowReason}
		if p.KnownValue != nil {
			d.Value = p.KnownValue.RatString()
		}
		if p.StrategyFlow != nil {
			d.Flow = p.StrategyFlow.RatString()
		}
		claim.Days = append(claim.Days, d)
	}
	if data.Baseline != nil {
		appendPoint(*data.Baseline)
	}
	for _, p := range data.Points {
		appendPoint(p)
	}
	raw, e := json.Marshal(claim)
	if e != nil {
		return "", e
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	h := hmac.New(sha256.New, s.key)
	_, _ = h.Write([]byte(payload))
	token := "v1." + payload + "." + hex.EncodeToString(h.Sum(nil))
	if len(token) > 32768 {
		return "", errors.New("performance page limit")
	}
	return token, nil
}

func (s *TrackerSnapshotSigner) Open(token string, now time.Time) (TrackerSnapshot, error) {
	var claim TrackerSnapshot
	fail := func() (TrackerSnapshot, error) { return TrackerSnapshot{}, errors.New("invalid performance snapshot") }
	if s == nil || len(token) > 32768 {
		return fail()
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != "v1" {
		return fail()
	}
	sig, e := hex.DecodeString(parts[2])
	if e != nil {
		return fail()
	}
	h := hmac.New(sha256.New, s.key)
	_, _ = h.Write([]byte(parts[1]))
	if !hmac.Equal(sig, h.Sum(nil)) {
		return fail()
	}
	raw, e := base64.RawURLEncoding.DecodeString(parts[1])
	if e != nil || json.Unmarshal(raw, &claim) != nil || claim.Expires <= now.Unix() || claim.Expires > now.Add(16*time.Minute).Unix() || len(claim.Days) < 1 || len(claim.Days) > 32 {
		return fail()
	}
	return claim, nil
}
