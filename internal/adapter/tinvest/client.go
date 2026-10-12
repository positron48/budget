// Package tinvest provides read-only access to T-Invest market data.
package tinvest

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const KeychainService = "budget.tinvest.readonly"
const apiURL = "https://invest-public-api.tbank.ru/rest/"

//go:embed certs/russian_trusted_root_ca.pem
var russianTrustedRoot []byte

// Client keeps credentials private. Never log requests, headers or raw responses.
type Client struct {
	requestGate    func(context.Context) error
	requestBackoff func(context.Context, time.Duration) error
	token          string
	http           *http.Client
	baseURL        string
}

func (c *Client) String() string   { return "T-Invest client (credential redacted)" }
func (c *Client) GoString() string { return c.String() }

// NewFromKeychain reads the credential directly into process memory, without
// placing it in command arguments, environment variables, files or tool output.
func NewFromKeychain(ctx context.Context, account string) (*Client, error) {
	if runtime.GOOS != "darwin" {
		return nil, errors.New("macOS Keychain is required")
	}
	if strings.TrimSpace(account) == "" {
		return nil, errors.New("keychain account is required")
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password", "-a", account, "-s", KeychainService, "-w")
	value, err := cmd.Output()
	defer clear(value)
	if err != nil {
		return nil, errors.New("keychain credential unavailable (check access permission outside sandbox)")
	}
	return newClient(strings.TrimSpace(string(value)))
}

func newClient(token string) (*Client, error) {
	if token == "" || strings.ContainsAny(token, "\r\n\t ") {
		return nil, errors.New("invalid Keychain credential format")
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		return nil, errors.New("cannot load system certificate roots")
	}
	if !roots.AppendCertsFromPEM(russianTrustedRoot) {
		return nil, errors.New("cannot load T-Invest certificate root")
	}
	// This private transport augments trust only for this API client. It does
	// not modify macOS Keychain, DefaultTransport or certificate verification.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	return &Client{token: token, baseURL: apiURL, http: &http.Client{
		Transport: transport,
		Timeout:   20 * time.Second,
		// Do not forward credentials to any redirected destination.
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

type Instrument struct {
	UID            string `json:"uid"`
	FIGI           string `json:"figi"`
	ISIN           string `json:"isin"`
	Ticker         string `json:"ticker"`
	ClassCode      string `json:"classCode"`
	Name           string `json:"name"`
	InstrumentType string `json:"instrumentType"`
}

// FindInstrument only reads public instrument metadata. Matching a ticker is
// not proof of a unique listing, historical coverage or trading entitlement.
func (c *Client) FindInstrument(ctx context.Context, query string) ([]Instrument, error) {
	return c.FindInstrumentOfKind(ctx, query, "")
}

// FindInstrumentOfKind bounds broad searches by an explicit P0 asset kind.
// It does not require present-day trading availability, preserving delistings.
func (c *Client) FindInstrumentOfKind(ctx context.Context, query, kind string) ([]Instrument, error) {
	if kind != "" && kind != "INSTRUMENT_TYPE_SHARE" && kind != "INSTRUMENT_TYPE_BOND" && kind != "INSTRUMENT_TYPE_ETF" && kind != "INSTRUMENT_TYPE_CURRENCY" {
		return nil, errors.New("unsupported instrument search kind")
	}
	if strings.TrimSpace(query) == "" || len(query) > 200 {
		return nil, errors.New("instrument query must contain 1-200 bytes")
	}
	var result struct {
		Instruments []Instrument `json:"instruments"`
	}
	request := map[string]any{"query": query}
	if kind != "" {
		request["instrumentKind"] = kind
	}
	err := c.read(ctx, "InstrumentsService/FindInstrument", request, &result)
	return result.Instruments, err
}

// HTTPError contains only safe retry metadata, never provider response bodies.
type HTTPError struct {
	StatusCode int
	RetryAfter time.Duration
}

func (e *HTTPError) Error() string             { return fmt.Sprintf("T-Invest HTTP %d", e.StatusCode) }
func (e *HTTPError) RetryDelay() time.Duration { return e.RetryAfter }
func (e *HTTPError) Temporary() bool {
	return e.StatusCode == 429 || e.StatusCode == 408 || e.StatusCode >= 500 && e.StatusCode <= 599
}

func retryAfter(value string, now time.Time) time.Duration {
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds <= 0 {
			return 0
		}
		// Bound untrusted remote delays before multiplying to avoid overflow.
		if seconds > 86400 {
			return 24 * time.Hour
		}
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil && at.After(now) {
		delay := at.Sub(now)
		if delay > 24*time.Hour {
			return 24 * time.Hour
		}
		return delay
	}
	return 0
}

// read is private: public methods below expose only audited read endpoints.
func (c *Client) read(ctx context.Context, method string, input, output any) error {
	body, err := json.Marshal(input)
	if err != nil {
		return errors.New("invalid request payload")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"tinkoff.public.invest.api.contract.v1."+method, bytes.NewReader(body))
	if err != nil {
		return errors.New("cannot build T-Invest request")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	if c.requestGate != nil {
		if err := c.requestGate(ctx); err != nil {
			return errors.New("T-Invest request gate unavailable")
		}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		var certificateError *tls.CertificateVerificationError
		if errors.As(err, &certificateError) {
			return errors.New("T-Invest TLS certificate verification failed; credential was not sent over an unverified connection")
		}
		return errors.New("T-Invest request failed (network, TLS or timeout)")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		failure := &HTTPError{StatusCode: resp.StatusCode, RetryAfter: retryAfter(resp.Header.Get("Retry-After"), time.Now())}
		if resp.StatusCode == http.StatusTooManyRequests && c.requestBackoff != nil {
			delay := failure.RetryAfter
			if delay <= 0 {
				delay = time.Minute
			}
			if err := c.requestBackoff(ctx, delay); err != nil {
				return errors.New("T-Invest shared backoff unavailable")
			}
		}
		return failure
	}
	limit := int64(2 << 20)
	// Short catalog searches (for example C) can return many unrelated
	// listings. Keep this endpoint bounded separately from market/account data.
	if method == "InstrumentsService/FindInstrument" {
		limit = 16 << 20
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return errors.New("T-Invest response read failed")
	}
	if int64(len(data)) > limit {
		return errors.New("T-Invest response exceeds endpoint size limit")
	}
	if err := json.Unmarshal(data, output); err != nil {
		return errors.New("invalid T-Invest JSON response")
	}
	return nil
}
