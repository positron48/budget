// Package twelvedata audits candidate FX history without activating financial policies.
package twelvedata

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

const KeychainService = "budget.twelvedata.readonly"
const maxBody = 4 << 20

// Client exposes only the historical forex endpoint. Credentials never enter URLs.
type Client struct {
	token, baseURL string
	http           *http.Client
}

func (c *Client) String() string   { return "Twelve Data client (credential redacted)" }
func (c *Client) GoString() string { return c.String() }

func NewFromKeychain(ctx context.Context, account string) (*Client, error) {
	if runtime.GOOS != "darwin" || strings.TrimSpace(account) == "" {
		return nil, errors.New("macOS Keychain account required")
	}
	value, err := exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password", "-a", account, "-s", KeychainService, "-w").Output()
	defer clear(value)
	if err != nil {
		return nil, errors.New("twelve data Keychain credential unavailable")
	}
	return newClient(strings.TrimSpace(string(value)))
}

func newClient(token string) (*Client, error) {
	if token == "" || strings.ContainsAny(token, "\r\n\t ") {
		return nil, errors.New("invalid Keychain credential format")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	return &Client{token: token, baseURL: "https://api.twelvedata.com/time_series", http: &http.Client{Transport: transport, Timeout: 20 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

type Meta struct {
	Symbol           string `json:"symbol"`
	Interval         string `json:"interval"`
	CurrencyBase     string `json:"currency_base"`
	CurrencyQuote    string `json:"currency_quote"`
	ExchangeTimezone string `json:"exchange_timezone"`
	Type             string `json:"type"`
}
type Bar struct {
	Date  string `json:"datetime"`
	Open  string `json:"open"`
	High  string `json:"high"`
	Low   string `json:"low"`
	Close string `json:"close"`
}

// Audit stores exact provider strings and a hash, never a raw body or provider error text.
// Daily bar dates are not publication instants and do not define FX validity.
type Audit struct {
	Source      string    `json:"source"`
	From        string    `json:"from"`
	To          string    `json:"to"`
	RetrievedAt time.Time `json:"retrieved_at"`
	BodySHA256  string    `json:"body_sha256"`
	Meta        Meta      `json:"meta"`
	Bars        []Bar     `json:"bars"`
}
type Gap struct {
	From, To string
	Days     int
}
type Coverage struct {
	SourceTimezone                 string `json:"source_timezone"`
	PublicationAndValidityVerified bool   `json:"publication_and_validity_verified"`
	Count                          int    `json:"count"`
	First, Last                    string
	LeadingDays, TrailingDays      int
	Gaps                           []Gap `json:"gaps_over_7_days"`
	FinanciallyApproved            bool  `json:"financially_approved"`
}

func window(symbol, from, to string, now time.Time) (time.Time, time.Time, error) {
	if symbol != "USD/RUB" && symbol != "EUR/RUB" && symbol != "EUR/USD" {
		return time.Time{}, time.Time{}, errors.New("unsupported FX pair")
	}
	a, e1 := time.Parse(time.DateOnly, from)
	b, e2 := time.Parse(time.DateOnly, to)
	today := now.UTC().Truncate(24 * time.Hour)
	if e1 != nil || e2 != nil || a.After(b) || !b.Before(today) || b.Sub(a) > 3660*24*time.Hour {
		return a, b, errors.New("require a closed UTC date range of at most 3661 days")
	}
	return a, b, nil
}

func (c *Client) History(ctx context.Context, symbol, from, to string) (Audit, error) {
	now := time.Now().UTC()
	if _, _, err := window(symbol, from, to, now); err != nil {
		return Audit{}, err
	}
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return Audit{}, errors.New("invalid provider endpoint")
	}
	u.RawQuery = url.Values{"symbol": {symbol}, "interval": {"1day"}, "start_date": {from}, "end_date": {to}, "outputsize": {"5000"}, "dp": {"11"}, "order": {"ASC"}, "adjust": {"none"}, "timezone": {"UTC"}, "format": {"JSON"}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Audit{}, errors.New("cannot build FX request")
	}
	req.Header.Set("Authorization", "apikey "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return Audit{}, errors.New("twelve data request failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Audit{}, fmt.Errorf("twelve data HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil || len(body) > maxBody {
		return Audit{}, errors.New("invalid or oversized FX response")
	}
	return Parse(body, symbol, from, to, now)
}

// Parse requires explicit forex identity and a supported native bar timezone.
// The requested UTC timezone does not override daily bars. No publication or
// validity instants are inferred. Values remain in provider precision.
func Parse(body []byte, symbol, from, to string, now time.Time) (Audit, error) {
	var payload struct {
		Meta   Meta   `json:"meta"`
		Values []Bar  `json:"values"`
		Status string `json:"status"`
	}
	if len(body) > maxBody || json.Unmarshal(body, &payload) != nil || payload.Status != "ok" {
		return Audit{}, errors.New("invalid FX response status or JSON")
	}
	hash := sha256.Sum256(body)
	a := Audit{Source: "twelvedata", From: from, To: to, RetrievedAt: now.UTC(), BodySHA256: hex.EncodeToString(hash[:]), Meta: payload.Meta, Bars: payload.Values}
	if a.Meta.Symbol != symbol {
		return Audit{}, errors.New("FX response symbol mismatch")
	}
	if _, err := a.Validate(); err != nil {
		return Audit{}, err
	}
	return a, nil
}

var decimal = regexp.MustCompile(`^(0|[1-9][0-9]{0,17})(\.[0-9]{1,11})?$`)

func price(s string) (*big.Rat, error) {
	if !decimal.MatchString(s) {
		return nil, errors.New("invalid exact FX price")
	}
	n, ok := new(big.Rat).SetString(s)
	if !ok || n.Sign() <= 0 {
		return nil, errors.New("nonpositive FX price")
	}
	return n, nil
}

// Validate rechecks checkpoint contents offline. Coverage reports calendar gaps;
// it does not invent a trading calendar or certify market origin/licensing.
func (a Audit) Validate() (Coverage, error) {
	first, last, err := window(a.Meta.Symbol, a.From, a.To, a.RetrievedAt)
	if err != nil {
		return Coverage{}, err
	}
	if a.Source != "twelvedata" || a.Meta.Interval != "1day" || a.Meta.Type != "Physical Currency" {
		return Coverage{}, errors.New("unverified FX identity")
	}
	if a.Meta.ExchangeTimezone == "" {
		return Coverage{}, errors.New("FX response omits daily bar timezone; requested timezone is not evidence")
	}
	if a.Meta.ExchangeTimezone != "UTC" && a.Meta.ExchangeTimezone != "Australia/Sydney" {
		return Coverage{}, errors.New("unsupported FX daily bar timezone")
	}
	hash, err := hex.DecodeString(a.BodySHA256)
	if err != nil || len(hash) != sha256.Size {
		return Coverage{}, errors.New("invalid response hash")
	}
	if len(a.Bars) == 0 || len(a.Bars) > int(last.Sub(first)/(24*time.Hour))+1 {
		return Coverage{}, errors.New("empty or overfull FX history")
	}
	seen := map[string]bool{}
	dates := make([]time.Time, 0, len(a.Bars))
	for _, b := range a.Bars {
		at, e := time.Parse(time.DateOnly, b.Date)
		if e != nil || at.Before(first) || at.After(last) || seen[b.Date] {
			return Coverage{}, errors.New("duplicate or out-of-range FX date")
		}
		seen[b.Date] = true
		dates = append(dates, at)
		o, e1 := price(b.Open)
		h, e2 := price(b.High)
		l, e3 := price(b.Low)
		c, e4 := price(b.Close)
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
			return Coverage{}, errors.New("invalid exact FX price")
		}
		if h.Cmp(l) < 0 || h.Cmp(o) < 0 || h.Cmp(c) < 0 || l.Cmp(o) > 0 || l.Cmp(c) > 0 {
			return Coverage{}, errors.New("inconsistent FX OHLC")
		}
	}
	sort.Slice(dates, func(i, j int) bool { return dates[i].Before(dates[j]) })
	result := Coverage{SourceTimezone: a.Meta.ExchangeTimezone, Count: len(dates), First: dates[0].Format(time.DateOnly), Last: dates[len(dates)-1].Format(time.DateOnly), LeadingDays: int(dates[0].Sub(first) / (24 * time.Hour)), TrailingDays: int(last.Sub(dates[len(dates)-1]) / (24 * time.Hour)), Gaps: []Gap{}}
	for i := 1; i < len(dates); i++ {
		days := int(dates[i].Sub(dates[i-1]) / (24 * time.Hour))
		if days > 7 {
			result.Gaps = append(result.Gaps, Gap{From: dates[i-1].Format(time.DateOnly), To: dates[i].Format(time.DateOnly), Days: days})
		}
	}
	return result, nil
}
