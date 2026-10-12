package cbr

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"math/big"
	"net/url"
	"sort"
	"strings"
	"time"
)

type officialRecord struct{ Date, ID, Nominal, Value string }
type officialQuote struct {
	date                  time.Time
	rate, hash, reference string
}

func officialValue(r officialRecord, code string) (officialQuote, error) {
	if len(r.ID) > 100 || len(r.Nominal) > 48 || len(r.Value) > 64 {
		return officialQuote{}, fmt.Errorf("oversized official FX record")
	}
	day, e := time.Parse("02.01.2006", r.Date)
	if e != nil || r.ID == "" {
		return officialQuote{}, fmt.Errorf("invalid official FX identity")
	}
	n, ok := new(big.Rat).SetString(strings.ReplaceAll(strings.TrimSpace(r.Nominal), ",", "."))
	if !ok || !n.IsInt() || n.Sign() <= 0 {
		return officialQuote{}, fmt.Errorf("invalid official FX nominal")
	}
	v, ok := new(big.Rat).SetString(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(r.Value), " ", ""), ",", "."))
	if !ok || v.Sign() <= 0 {
		return officialQuote{}, fmt.Errorf("invalid official FX value")
	}
	rate := new(big.Rat).Quo(v, n)
	text := rate.FloatString(18)
	check, _ := new(big.Rat).SetString(text)
	if check.Cmp(rate) != 0 {
		return officialQuote{}, fmt.Errorf("inexact official FX nominal")
	}
	// Hash the exact source record fields, before decimal/nominal normalization.
	// The locator identifies the authoritative CBR currency and effective date.
	raw, _ := json.Marshal(r)
	sum := sha256.Sum256(raw)
	ref := "https://www.cbr.ru/scripts/XML_daily.asp?date_req=" + day.Format("02/01/2006") + "#Valute[ID=" + r.ID + ";CharCode=" + code + "]"
	return officialQuote{day, text, hex.EncodeToString(sum[:]), ref}, nil
}
func (c *Client) officialSnapshot(ctx context.Context, at time.Time) (map[string]officialQuote, map[string]string, error) {
	var doc daily
	if e := c.get(ctx, "XML_daily.asp", url.Values{"date_req": {at.Format("02/01/2006")}}, &doc); e != nil {
		return nil, nil, e
	}
	date, e := time.Parse("02.01.2006", doc.Date)
	if e != nil || date.After(at) || at.Sub(date) > 14*24*time.Hour {
		return nil, nil, fmt.Errorf("invalid official snapshot date")
	}
	result := map[string]officialQuote{}
	ids := map[string]string{}
	for _, v := range doc.Values {
		if v.Code != "USD" && v.Code != "EUR" {
			continue
		}
		if ids[v.Code] != "" {
			return nil, nil, fmt.Errorf("duplicate official FX currency")
		}
		q, e := officialValue(officialRecord{doc.Date, v.ID, v.Nominal, v.Value}, v.Code)
		if e != nil {
			return nil, nil, e
		}
		result[v.Code] = q
		ids[v.Code] = v.ID
	}
	if len(result) != 2 {
		return nil, nil, fmt.Errorf("official snapshot missing USD/EUR")
	}
	return result, ids, nil
}

// FetchOfficialFX expands effective CBR dates only through the requested closing
// snapshot. Each calendar-day fixing expires at the next Moscow midnight;
// neither future publication nor indefinite last-known validity is invented.
func (c *Client) FetchOfficialFX(ctx context.Context, from, to time.Time) ([]md.FXPoint, error) {
	if from.IsZero() || from.Year() < 2015 || to.Before(from) || to.Sub(from) > 3660*24*time.Hour || !from.Equal(from.UTC().Truncate(24*time.Hour)) || !to.Equal(to.UTC().Truncate(24*time.Hour)) {
		return nil, fmt.Errorf("invalid official FX range")
	}
	opening, ids, e := c.officialSnapshot(ctx, from)
	if e != nil {
		return nil, e
	}
	closing, closingIDs, e := c.officialSnapshot(ctx, to)
	if e != nil {
		return nil, e
	}
	var result []md.FXPoint
	for _, code := range []string{"USD", "EUR"} {
		if closingIDs[code] != ids[code] {
			return nil, fmt.Errorf("official currency identity changed")
		}
		var doc history
		if e = c.get(ctx, "XML_dynamic.asp", url.Values{"date_req1": {from.Format("02/01/2006")}, "date_req2": {to.Format("02/01/2006")}, "VAL_NM_RQ": {ids[code]}}, &doc); e != nil {
			return nil, e
		}
		if doc.ID != ids[code] {
			return nil, fmt.Errorf("official history identity mismatch")
		}
		byDate := map[time.Time]officialQuote{opening[code].date: opening[code]}
		for _, r := range doc.Records {
			if r.ID != ids[code] {
				return nil, fmt.Errorf("official record identity mismatch")
			}
			q, e := officialValue(officialRecord{r.Date, r.ID, r.Nominal, r.Value}, code)
			if e != nil {
				return nil, e
			}
			if q.date.Before(from) || q.date.After(to) {
				return nil, fmt.Errorf("official record outside range")
			}
			if old, ok := byDate[q.date]; ok && old.rate != q.rate {
				return nil, fmt.Errorf("conflicting official FX record")
			}
			byDate[q.date] = q
		}
		dates := make([]time.Time, 0, len(byDate))
		for d := range byDate {
			dates = append(dates, d)
		}
		sort.Slice(dates, func(i, j int) bool { return dates[i].Before(dates[j]) })
		last := byDate[dates[len(dates)-1]]
		if last.date != closing[code].date || last.rate != closing[code].rate {
			return nil, fmt.Errorf("official history does not match closing snapshot")
		}
		index := 0
		for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
			for index+1 < len(dates) && !dates[index+1].After(day) {
				index++
			}
			q := byDate[dates[index]]
			// Calendar dates from XML are effective Moscow dates (UTC+3).
			at := day.Add(-3 * time.Hour)
			p, e := md.NormalizeFXPoint(md.FXPoint{From: code, To: "RUB", Rate: q.rate, At: at, AvailableAt: at, ValidUntil: at.Add(24 * time.Hour), EvidenceSHA256: q.hash, RecordReference: q.reference})
			if e != nil {
				return nil, e
			}
			result = append(result, p)
		}
	}
	return result, nil
}
