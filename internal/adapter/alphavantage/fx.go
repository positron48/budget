// Package alphavantage probes historical FX without writing financial data.
package alphavantage

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
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

const KeychainService = "budget.alphavantage.readonly"
const maxBody = 4 << 20

type Client struct {
	token     string
	http      *http.Client
	endpoint  string
	batchGate func(context.Context) (bool, error)
}

func (c *Client) String() string   { return "Alpha Vantage FX client (credential redacted)" }
func (c *Client) GoString() string { return c.String() }
func NewFromKeychain(ctx context.Context, account string) (*Client, error) {
	if runtime.GOOS != "darwin" || strings.TrimSpace(account) == "" {
		return nil, errors.New("macOS Keychain account required")
	}
	raw, err := exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password", "-a", account, "-s", KeychainService, "-w").Output()
	defer clear(raw)
	if err != nil {
		return nil, errors.New("alpha vantage Keychain credential unavailable")
	}
	return newClient(strings.TrimSpace(string(raw)))
}

func newClient(token string) (*Client, error) {
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`).MatchString(token) {
		return nil, errors.New("invalid Alpha credential format")
	}
	return &Client{token: token, endpoint: "https://www.alphavantage.co/query", http: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect refused") }, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}}}, nil
}

type Bar struct{ Date, Open, High, Low, Close string }
type Gap struct {
	From, To string
	Days     int
}
type Audit struct {
	Source, Pair, From, To, Timezone, LastRefreshed, BodySHA256 string
	RetrievedAt                                                 time.Time
	Bars                                                        []Bar
	ExcludedOutsideWindow                                       int
}
type Coverage struct {
	Count                                               int
	First, Last                                         string
	LeadingDays, TrailingDays                           int
	GapsOver7Days                                       []Gap
	FinanciallyApproved, PublicationAndValidityVerified bool
	WeekdayDatesWithoutBar, WeekendBars                 int
	MissingWeekdayExamples                              []string
	TradingCalendarVerified                             bool
}

func window(pair, from, to string, now time.Time) (time.Time, time.Time, error) {
	a, e1 := time.Parse(time.DateOnly, from)
	b, e2 := time.Parse(time.DateOnly, to)
	if (pair != "USD/RUB" && pair != "EUR/RUB" && pair != "EUR/USD") || e1 != nil || e2 != nil || a.After(b) || !b.Before(now.UTC().Truncate(24*time.Hour)) || b.Sub(a) > 3660*24*time.Hour {
		return a, b, errors.New("require a supported pair and closed date range of at most 3661 days")
	}
	return a, b, nil
}
func (c *Client) History(ctx context.Context, pair, from, to string) (Audit, error) {
	now := time.Now().UTC()
	if _, _, err := window(pair, from, to, now); err != nil {
		return Audit{}, err
	}
	u, err := url.Parse(c.endpoint)
	if err != nil {
		return Audit{}, errors.New("invalid FX endpoint")
	}
	parts := strings.Split(pair, "/")
	// The provider documents authentication in the query. It remains exclusively
	// inside this HTTP process; URL-bearing transport/provider errors never escape.
	u.RawQuery = url.Values{"function": {"FX_DAILY"}, "from_symbol": {parts[0]}, "to_symbol": {parts[1]}, "outputsize": {"full"}, "datatype": {"json"}, "apikey": {c.token}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Audit{}, errors.New("cannot build FX request")
	}
	response, err := c.http.Do(req)
	if err != nil {
		return Audit{}, errors.New("alpha vantage request failed; diagnostics suppressed")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != 200 {
		return Audit{}, errors.New("alpha vantage returned non-success HTTP status")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxBody+1))
	defer clear(raw)
	if err != nil || len(raw) > maxBody {
		return Audit{}, errors.New("invalid or oversized Alpha Vantage response")
	}
	return Parse(raw, pair, from, to, now)
}

// Reject duplicate JSON keys before decoding the date-keyed time series. A map
// alone would silently replace a conflicting duplicate historical observation.
func uniqueJSON(dec *json.Decoder) error { return uniqueJSONDepth(dec, 0) }

func uniqueJSONDepth(dec *json.Decoder, depth int) error {
	if depth > 32 {
		return errors.New("FX JSON nesting limit")
	}
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for dec.More() {
			key, err := dec.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return errors.New("duplicate FX JSON key")
			}
			seen[name] = true
			if err = uniqueJSONDepth(dec, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for dec.More() {
			if err = uniqueJSONDepth(dec, depth+1); err != nil {
				return err
			}
		}
	default:
		return errors.New("unexpected JSON delimiter")
	}
	_, err = dec.Token()
	return err
}

func Parse(raw []byte, pair, from, to string, now time.Time) (Audit, error) {
	first, last, err := window(pair, from, to, now)
	if err != nil {
		return Audit{}, err
	}
	if len(raw) > maxBody {
		return Audit{}, errors.New("oversized FX response")
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	if err = uniqueJSON(dec); err != nil {
		return Audit{}, errors.New("invalid or duplicate FX JSON")
	}
	if _, err = dec.Token(); err != io.EOF {
		return Audit{}, errors.New("trailing FX JSON")
	}
	var payload struct {
		Meta   map[string]string `json:"Meta Data"`
		Series map[string]struct {
			Open  string `json:"1. open"`
			High  string `json:"2. high"`
			Low   string `json:"3. low"`
			Close string `json:"4. close"`
		} `json:"Time Series FX (Daily)"`
		Note, Information string
		Error             string `json:"Error Message"`
	}
	if json.Unmarshal(raw, &payload) != nil || payload.Note != "" || payload.Information != "" || payload.Error != "" {
		return Audit{}, errors.New("alpha vantage denied FX data or returned invalid JSON; diagnostics suppressed")
	}
	parts := strings.Split(pair, "/")
	if metadataField(payload.Meta, "From Symbol") != parts[0] || metadataField(payload.Meta, "To Symbol") != parts[1] {
		return Audit{}, errors.New("unverified FX currency pair identity")
	}
	if metadataField(payload.Meta, "Output Size") != "Full size" {
		return Audit{}, errors.New("FX response does not confirm full history")
	}
	if metadataField(payload.Meta, "Time Zone") != "UTC" {
		return Audit{}, errors.New("FX response does not confirm UTC timezone")
	}
	if !validRefresh(metadataField(payload.Meta, "Last Refreshed")) {
		return Audit{}, errors.New("invalid FX refresh metadata")
	}
	sum := sha256.Sum256(raw)
	a := Audit{Source: "alphavantage", Pair: pair, From: from, To: to, Timezone: "UTC", LastRefreshed: metadataField(payload.Meta, "Last Refreshed"), BodySHA256: hex.EncodeToString(sum[:]), RetrievedAt: now.UTC()}
	for date, values := range payload.Series {
		at, err := time.Parse(time.DateOnly, date)
		if err != nil {
			return Audit{}, errors.New("invalid FX date")
		}
		if at.Before(first) || at.After(last) {
			a.ExcludedOutsideWindow++
			continue
		}
		a.Bars = append(a.Bars, Bar{date, values.Open, values.High, values.Low, values.Close})
	}
	sort.Slice(a.Bars, func(i, j int) bool { return a.Bars[i].Date < a.Bars[j].Date })
	if _, err = a.Validate(); err != nil {
		return Audit{}, err
	}
	return a, nil
}

// A daily refresh date is metadata, not an instant of publication. Preserve its
// original precision and never manufacture midnight for a date-only value.
func validRefresh(value string) bool {
	for _, layout := range []string{time.DateOnly, "2006-01-02 15:04:05"} {
		if _, err := time.Parse(layout, value); err == nil {
			return true
		}
	}
	return false
}

// Metadata ordinals are presentation labels, not field identities. Require one
// explicitly named value; never guess a missing field or accept two aliases.
func metadataField(meta map[string]string, label string) string {
	value := ""
	found := false
	for i := byte('1'); i <= '9'; i++ {
		if current, ok := meta[string(i)+". "+label]; ok {
			if found {
				return ""
			}
			found = true
			value = current
		}
	}
	return value
}

var decimal = regexp.MustCompile(`^(0|[1-9][0-9]{0,17})(\.[0-9]{1,11})?$`)

func exact(s string) (*big.Rat, error) {
	if !decimal.MatchString(s) {
		return nil, errors.New("invalid exact FX value")
	}
	n, ok := new(big.Rat).SetString(s)
	if !ok || n.Sign() <= 0 {
		return nil, errors.New("nonpositive FX value")
	}
	return n, nil
}
func (a Audit) Validate() (Coverage, error) {
	first, last, err := window(a.Pair, a.From, a.To, a.RetrievedAt)
	if err != nil {
		return Coverage{}, err
	}
	hash, err := hex.DecodeString(a.BodySHA256)
	if a.Source != "alphavantage" || a.Timezone != "UTC" || err != nil || len(hash) != 32 || len(a.Bars) == 0 || len(a.Bars) > 3661 || a.ExcludedOutsideWindow < 0 {
		return Coverage{}, errors.New("invalid FX audit metadata or empty history")
	}
	if !validRefresh(a.LastRefreshed) {
		return Coverage{}, errors.New("invalid FX refresh metadata")
	}
	coverage := Coverage{Count: len(a.Bars), First: a.Bars[0].Date, Last: a.Bars[len(a.Bars)-1].Date}
	var previous time.Time
	seen := map[string]bool{}
	for _, bar := range a.Bars {
		at, err := time.Parse(time.DateOnly, bar.Date)
		if err != nil || at.Before(first) || at.After(last) || (!previous.IsZero() && !at.After(previous)) {
			return Coverage{}, errors.New("duplicate, unordered or out-of-range FX date")
		}
		o, e1 := exact(bar.Open)
		h, e2 := exact(bar.High)
		l, e3 := exact(bar.Low)
		c, e4 := exact(bar.Close)
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
			return Coverage{}, errors.New("invalid exact FX OHLC")
		}
		if h.Cmp(l) < 0 || h.Cmp(o) < 0 || h.Cmp(c) < 0 || l.Cmp(o) > 0 || l.Cmp(c) > 0 {
			return Coverage{}, errors.New("inconsistent FX OHLC")
		}
		if previous.IsZero() {
			coverage.LeadingDays = int(at.Sub(first) / (24 * time.Hour))
		} else if days := int(at.Sub(previous) / (24 * time.Hour)); days > 7 {
			coverage.GapsOver7Days = append(coverage.GapsOver7Days, Gap{previous.Format(time.DateOnly), bar.Date, days})
		}
		previous = at
		seen[bar.Date] = true
		if at.Weekday() == time.Saturday || at.Weekday() == time.Sunday {
			coverage.WeekendBars++
		}
	}
	coverage.TrailingDays = int(last.Sub(previous) / (24 * time.Hour))
	// These are calendar weekdays, not a claim that the provider traded on
	// holidays. Report short missing dates as well as long gaps.
	for at := first; !at.After(last); at = at.AddDate(0, 0, 1) {
		if at.Weekday() != time.Saturday && at.Weekday() != time.Sunday && !seen[at.Format(time.DateOnly)] {
			coverage.WeekdayDatesWithoutBar++
			if len(coverage.MissingWeekdayExamples) < 20 {
				coverage.MissingWeekdayExamples = append(coverage.MissingWeekdayExamples, at.Format(time.DateOnly))
			}
		}
	}
	return coverage, nil
}
