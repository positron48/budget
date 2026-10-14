package twelvedata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"time"
)

// EquityListing must come from a confirmed instrument mapping, not ticker search.
// This adapter initially supports USD common stocks on the two primary US venues.
type EquityListing struct {
	Symbol, Exchange, MIC string
}

type EquityMeta struct {
	Symbol   string `json:"symbol"`
	Interval string `json:"interval"`
	Currency string `json:"currency"`
	Exchange string `json:"exchange"`
	MIC      string `json:"mic_code"`
	Timezone string `json:"exchange_timezone"`
	Type     string `json:"type"`
}

// EquityAudit retains exchange session dates, not UTC opening/publication times.
// Adjustment records the request contract; the API does not echo it in metadata.
type EquityAudit struct {
	Source      string     `json:"source"`
	Adjustment  string     `json:"adjustment"`
	From        string     `json:"from"`
	To          string     `json:"to"`
	RetrievedAt time.Time  `json:"retrieved_at"`
	BodySHA256  string     `json:"body_sha256"`
	Meta        EquityMeta `json:"meta"`
	Bars        []Bar      `json:"bars"`
}

var equitySymbol = regexp.MustCompile(`^[A-Z][A-Z0-9.-]{0,19}$`)

func equityWindow(listing EquityListing, from, to string, now time.Time) (time.Time, time.Time, error) {
	a, e1 := time.Parse(time.DateOnly, from)
	b, e2 := time.Parse(time.DateOnly, to)
	venue := listing.Exchange == "NASDAQ" && listing.MIC == "XNGS" || listing.Exchange == "NYSE" && listing.MIC == "XNYS"
	// Only completed exchange dates. Missing end-of-day bars remain gaps;
	// receiving an older bar does not prove a newer session is available.
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		return a, b, errors.New("US exchange timezone unavailable")
	}
	local := now.In(loc)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	if !venue || !equitySymbol.MatchString(listing.Symbol) || e1 != nil || e2 != nil || a.Year() < 1900 || a.After(b) || !b.Before(today) || b.Sub(a) > 3660*24*time.Hour {
		return a, b, errors.New("invalid listing or closed equity date range")
	}
	return a, b, nil
}

// EquityHistory always explicitly disables split and dividend adjustment.
// Daily timezone parameters do not change the exchange session date. The end
// boundary is padded by one day, then filtered, to include the requested day.
// https://twelvedata.com/docs/mcp
func (c *Client) EquityHistory(ctx context.Context, listing EquityListing, from, to string) (EquityAudit, error) {
	now := time.Now().UTC()
	_, last, err := equityWindow(listing, from, to, now)
	if err != nil {
		return EquityAudit{}, err
	}
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return EquityAudit{}, errors.New("invalid equity endpoint")
	}
	u.RawQuery = url.Values{"symbol": {listing.Symbol}, "exchange": {listing.Exchange}, "interval": {"1day"}, "start_date": {from}, "end_date": {last.AddDate(0, 0, 1).Format(time.DateOnly)}, "outputsize": {"5000"}, "dp": {"9"}, "order": {"ASC"}, "adjust": {"none"}, "format": {"JSON"}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return EquityAudit{}, errors.New("cannot build equity request")
	}
	req.Header.Set("Authorization", "apikey "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return EquityAudit{}, errors.New("twelve data equity request failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return EquityAudit{}, fmt.Errorf("twelve data equity HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil || len(body) > maxBody {
		return EquityAudit{}, errors.New("invalid or oversized equity response")
	}
	return parseEquity(body, listing, from, to, now)
}

// parseEquity is intentionally private: arbitrary adjusted JSON cannot be
// presented through the public API as an unadjusted provider response.
func parseEquity(body []byte, listing EquityListing, from, to string, now time.Time) (EquityAudit, error) {
	first, last, err := equityWindow(listing, from, to, now)
	if err != nil {
		return EquityAudit{}, err
	}
	var payload struct {
		Meta   EquityMeta `json:"meta"`
		Values []Bar      `json:"values"`
		Status string     `json:"status"`
	}
	if len(body) > maxBody || json.Unmarshal(body, &payload) != nil || payload.Status != "ok" {
		return EquityAudit{}, errors.New("invalid equity response status or JSON")
	}
	m := payload.Meta
	if m.Symbol != listing.Symbol || m.Exchange != listing.Exchange || m.MIC != listing.MIC || m.Interval != "1day" || m.Currency != "USD" || m.Type != "Common Stock" || m.Timezone != "America/New_York" {
		return EquityAudit{}, errors.New("equity response listing mismatch")
	}
	if len(payload.Values) == 0 || len(payload.Values) > 5000 {
		return EquityAudit{}, errors.New("empty or oversized equity series")
	}
	seen := map[string]bool{}
	bars := make([]Bar, 0, len(payload.Values))
	for _, b := range payload.Values {
		day, e := time.Parse(time.DateOnly, b.Date)
		if e != nil || day.Before(first) || day.After(last.AddDate(0, 0, 1)) || seen[b.Date] {
			return EquityAudit{}, errors.New("duplicate or out-of-range equity date")
		}
		seen[b.Date] = true
		o, e1 := price(b.Open)
		h, e2 := price(b.High)
		l, e3 := price(b.Low)
		closePrice, e4 := price(b.Close)
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
			return EquityAudit{}, errors.New("invalid exact equity price")
		}
		if h.Cmp(l) < 0 || h.Cmp(o) < 0 || h.Cmp(closePrice) < 0 || l.Cmp(o) > 0 || l.Cmp(closePrice) > 0 {
			return EquityAudit{}, errors.New("inconsistent equity OHLC")
		}
		if !day.After(last) {
			bars = append(bars, b)
		}
	}
	if len(bars) == 0 {
		return EquityAudit{}, errors.New("no equity bars within requested range")
	}
	sort.Slice(bars, func(i, j int) bool { return bars[i].Date < bars[j].Date })
	hash := sha256.Sum256(body)
	return EquityAudit{Source: "twelvedata", Adjustment: "none", From: from, To: to, RetrievedAt: now, BodySHA256: hex.EncodeToString(hash[:]), Meta: m, Bars: bars}, nil
}
