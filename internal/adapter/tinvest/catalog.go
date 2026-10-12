package tinvest

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/google/uuid"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func (c *Client) LoadMarketInstrument(ctx context.Context, id string) (md.Instrument, error) {
	if parsed, err := uuid.Parse(id); err != nil || parsed.String() != id {
		return md.Instrument{}, md.ErrInvalid
	}
	raw, err := c.InstrumentByUID(ctx, id)
	if err != nil {
		return md.Instrument{}, err
	}
	return DecodeMarketInstrument(id, raw)
}

// DecodeMarketInstrument validates exact listing identity in saved provider evidence.
// Current metadata does not establish historical nominal or trading calendars.
func DecodeMarketInstrument(id string, data json.RawMessage) (md.Instrument, error) {
	if parsed, err := uuid.Parse(id); err != nil || parsed.String() != id {
		return md.Instrument{}, md.ErrInvalid
	}
	var response struct {
		Instrument struct {
			Instrument
			Currency string `json:"currency"`
		} `json:"instrument"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return md.Instrument{}, md.ErrInvalid
	}
	raw := response.Instrument
	item := md.Instrument{Detailed: true, ID: raw.UID, FIGI: raw.FIGI, ISIN: raw.ISIN, Ticker: raw.Ticker, ClassCode: raw.ClassCode, Name: raw.Name, Kind: raw.InstrumentType, Currency: strings.ToUpper(raw.Currency)}
	if item.ID != id {
		return md.Instrument{}, md.ErrInvalid
	}
	return item, md.ValidateInstrument(item)
}

// SearchMarketCatalog returns bounded candidates, never confirmed import links.
func (c *Client) SearchMarketCatalog(ctx context.Context, q md.JobRequest) (md.CatalogResult, error) {
	q = md.NormalizeRequest(q)
	if err := md.ValidateRequest(q); err != nil || q.Kind != "catalog_search" {
		return md.CatalogResult{}, md.ErrInvalid
	}
	kinds := map[string]string{"share": "INSTRUMENT_TYPE_SHARE", "bond": "INSTRUMENT_TYPE_BOND", "etf": "INSTRUMENT_TYPE_ETF", "currency": "INSTRUMENT_TYPE_CURRENCY"}
	found, err := c.FindInstrumentOfKind(ctx, q.SearchQuery, kinds[q.InstrumentKind])
	if err != nil {
		return md.CatalogResult{}, err
	}
	rank := func(i Instrument) int {
		for _, v := range []string{i.UID, i.FIGI, i.ISIN, i.Ticker} {
			if strings.EqualFold(v, q.SearchQuery) {
				return 0
			}
		}
		if strings.HasPrefix(strings.ToLower(i.Ticker), q.SearchQuery) {
			return 1
		}
		return 2
	}
	sort.Slice(found, func(i, j int) bool {
		a, b := rank(found[i]), rank(found[j])
		if a != b {
			return a < b
		}
		if found[i].Ticker != found[j].Ticker {
			return found[i].Ticker < found[j].Ticker
		}
		if found[i].ClassCode != found[j].ClassCode {
			return found[i].ClassCode < found[j].ClassCode
		}
		return found[i].UID < found[j].UID
	})
	result := md.CatalogResult{Instruments: []md.Instrument{}}
	seen := map[string]bool{}
	for _, raw := range found {
		if seen[raw.UID] {
			continue
		}
		seen[raw.UID] = true
		if len(result.Instruments) == 1000 {
			result.Truncated = true
			break
		}
		item := md.Instrument{ID: raw.UID, FIGI: raw.FIGI, ISIN: raw.ISIN, Ticker: raw.Ticker, ClassCode: raw.ClassCode, Name: raw.Name, Kind: raw.InstrumentType}
		if item.Kind != q.InstrumentKind {
			return md.CatalogResult{}, md.ErrInvalid
		}
		if err := md.ValidateInstrument(item); err != nil {
			return md.CatalogResult{}, err
		}
		result.Instruments = append(result.Instruments, item)
	}
	return result, nil
}
