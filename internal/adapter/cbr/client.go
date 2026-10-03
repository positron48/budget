// Package cbr reads the Bank of Russia's official daily and historical XML feeds.
package cbr

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/positron48/budget/internal/usecase/fximport"
	"golang.org/x/net/html/charset"
)

var currencies = []string{"USD", "EUR", "GBP", "KZT", "CNY", "TRY", "GEL", "AMD", "RSD"}

type Client struct {
	HTTP    *http.Client
	BaseURL string
}

func New() *Client {
	return &Client{HTTP: &http.Client{Timeout: 30 * time.Second}, BaseURL: "https://www.cbr.ru/scripts/"}
}

type value struct {
	Nominal string
	Value   string
}
type daily struct {
	XMLName xml.Name `xml:"ValCurs"`
	Date    string   `xml:"Date,attr"`
	Values  []struct {
		ID   string `xml:"ID,attr"`
		Code string `xml:"CharCode"`
		value
	} `xml:"Valute"`
}
type history struct {
	XMLName xml.Name `xml:"ValCurs"`
	ID      string   `xml:"ID,attr"`
	Records []struct {
		Date string `xml:"Date,attr"`
		ID   string `xml:"Id,attr"`
		value
	} `xml:"Record"`
}

func (c *Client) get(ctx context.Context, path string, params url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path+"?"+params.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Budget-FX/1.0")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("CBR %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("CBR %s: HTTP %d", path, resp.StatusCode)
	}
	decoder := xml.NewDecoder(io.LimitReader(resp.Body, 8<<20))
	decoder.CharsetReader = charset.NewReaderLabel
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("CBR %s XML: %w", path, err)
	}
	return nil
}

func unitRate(v value) (string, error) {
	parse := func(s string) (*big.Rat, bool) {
		return new(big.Rat).SetString(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(s), " ", ""), ",", "."))
	}
	n, ok := parse(v.Nominal)
	if !ok || n.Sign() <= 0 || !n.IsInt() {
		return "", fmt.Errorf("invalid CBR nominal %q", v.Nominal)
	}
	amount, ok := parse(v.Value)
	if !ok || amount.Sign() <= 0 {
		return "", fmt.Errorf("invalid CBR value %q", v.Value)
	}
	rate := new(big.Rat).Quo(amount, n).FloatString(8)
	if rate == "0.00000000" {
		return "", fmt.Errorf("CBR rate rounds to zero")
	}
	return rate, nil
}

func (c *Client) snapshot(ctx context.Context, at time.Time) (map[string]fximport.Quote, map[string]string, error) {
	var doc daily
	if err := c.get(ctx, "XML_daily.asp", url.Values{"date_req": {at.Format("02/01/2006")}}, &doc); err != nil {
		return nil, nil, err
	}
	date, err := time.Parse("02.01.2006", doc.Date)
	if err != nil || date.After(at) {
		return nil, nil, fmt.Errorf("invalid CBR snapshot date %q", doc.Date)
	}
	quotes := map[string]fximport.Quote{}
	ids := map[string]string{}
	for _, v := range doc.Values {
		for _, code := range currencies {
			if v.Code != code {
				continue
			}
			rate, err := unitRate(v.value)
			if err != nil {
				return nil, nil, err
			}
			if v.ID == "" || ids[code] != "" {
				return nil, nil, fmt.Errorf("invalid or duplicate CBR currency %s", code)
			}
			ids[code] = v.ID
			quotes[code] = fximport.Quote{Currency: code, Rate: rate, Date: date}
		}
	}
	for _, code := range currencies {
		if ids[code] == "" {
			return nil, nil, fmt.Errorf("CBR snapshot missing %s", code)
		}
	}
	return quotes, ids, nil
}

// Fetch includes the effective opening quote, even when its date precedes from.
// The closing snapshot verifies that a successful but truncated history isn't saved.
func (c *Client) Fetch(ctx context.Context, from, to time.Time) ([]fximport.Quote, error) {
	if from.IsZero() || to.Before(from) {
		return nil, fmt.Errorf("invalid CBR period")
	}
	opening, ids, err := c.snapshot(ctx, from)
	if err != nil {
		return nil, err
	}
	closing, _, err := c.snapshot(ctx, to)
	if err != nil {
		return nil, err
	}
	out := []fximport.Quote{}
	for _, code := range currencies {
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
		var doc history
		if err := c.get(ctx, "XML_dynamic.asp", url.Values{"date_req1": {from.Format("02/01/2006")}, "date_req2": {to.Format("02/01/2006")}, "VAL_NM_RQ": {ids[code]}}, &doc); err != nil {
			return nil, err
		}
		if doc.ID != ids[code] {
			return nil, fmt.Errorf("CBR history ID mismatch for %s", code)
		}
		byDate := map[time.Time]fximport.Quote{opening[code].Date: opening[code]}
		latest := opening[code]
		for _, record := range doc.Records {
			date, err := time.Parse("02.01.2006", record.Date)
			if err != nil || date.Before(from) || date.After(to) || record.ID != ids[code] {
				return nil, fmt.Errorf("invalid CBR history record for %s: %q", code, record.Date)
			}
			rate, err := unitRate(record.value)
			if err != nil {
				return nil, err
			}
			q := fximport.Quote{Currency: code, Rate: rate, Date: date}
			if existing, ok := byDate[date]; ok && existing.Rate != rate {
				return nil, fmt.Errorf("conflicting CBR quotes for %s", code)
			}
			byDate[date] = q
			if !date.Before(latest.Date) {
				latest = q
			}
		}
		if latest != closing[code] {
			return nil, fmt.Errorf("CBR history does not match closing snapshot for %s", code)
		}
		for _, q := range byDate {
			out = append(out, q)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Date.Equal(out[j].Date) {
			return out[i].Currency < out[j].Currency
		}
		return out[i].Date.Before(out[j].Date)
	})
	return out, nil
}
