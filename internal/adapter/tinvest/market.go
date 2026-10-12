package tinvest

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"time"

	"github.com/google/uuid"
)

// Quotation retains the provider's integer representation without float rounding.
type Quotation struct {
	Units json.Number `json:"units"`
	Nano  int32       `json:"nano"`
}

func (q Quotation) Decimal() (string, error) {
	units := q.Units
	if units == "" {
		units = "0"
	}
	n, err := units.Int64()
	if err != nil || q.Nano <= -1000000000 || q.Nano >= 1000000000 || (n > 0 && q.Nano < 0) || (n < 0 && q.Nano > 0) {
		return "", errors.New("invalid T-Invest quotation")
	}
	value := new(big.Int).Mul(big.NewInt(n), big.NewInt(1000000000))
	value.Add(value, big.NewInt(int64(q.Nano)))
	return new(big.Rat).SetFrac(value, big.NewInt(1000000000)).FloatString(9), nil
}

type LastPrice struct {
	InstrumentUID string    `json:"instrumentUid"`
	FIGI          string    `json:"figi"`
	Price         Quotation `json:"price"`
	Time          time.Time `json:"time"`
	Type          string    `json:"lastPriceType"`
}

// ExchangeLastPrices requests exchange prices only. Missing instruments remain
// missing; prices retain provider units (bond points are not currency amounts).
func (c *Client) ExchangeLastPrices(ctx context.Context, ids []string) ([]LastPrice, error) {
	if len(ids) == 0 || len(ids) > 100 {
		return nil, errors.New("provide 1-100 instrument UIDs")
	}
	requested := make(map[string]bool, len(ids))
	for _, id := range ids {
		if _, err := uuid.Parse(id); err != nil || requested[id] {
			return nil, errors.New("invalid or duplicate instrument UID")
		}
		requested[id] = true
	}
	var result struct {
		Prices []LastPrice `json:"lastPrices"`
	}
	if err := c.read(ctx, "MarketDataService/GetLastPrices", map[string]any{"instrumentId": ids, "lastPriceType": "LAST_PRICE_EXCHANGE"}, &result); err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	prices := make([]LastPrice, 0, len(result.Prices))
	for _, p := range result.Prices {
		if !requested[p.InstrumentUID] || seen[p.InstrumentUID] {
			return nil, errors.New("invalid T-Invest exchange price identity")
		}
		seen[p.InstrumentUID] = true
		// The API returns an identity-only placeholder when no price exists.
		if p.Type == "LAST_PRICE_UNSPECIFIED" && p.Time.IsZero() && p.Price.Units == "" && p.Price.Nano == 0 {
			continue
		}
		if p.Time.IsZero() || p.Type != "LAST_PRICE_EXCHANGE" {
			return nil, errors.New("invalid T-Invest exchange price metadata")
		}
		if _, err := p.Price.Decimal(); err != nil {
			return nil, err
		}
		prices = append(prices, p)
	}
	return prices, nil
}

type Candle struct {
	Open     Quotation   `json:"open"`
	High     Quotation   `json:"high"`
	Low      Quotation   `json:"low"`
	Close    Quotation   `json:"close"`
	Volume   json.Number `json:"volume"`
	Time     time.Time   `json:"time"`
	Complete bool        `json:"isComplete"`
	Source   string      `json:"candleSource"`
}

// ExchangeDailyCandles returns raw daily observations. Time is the candle start,
// not a promise that its close was available at that instant. Incomplete candles
// are retained explicitly; gaps never receive fabricated prices.
func (c *Client) ExchangeDailyCandles(ctx context.Context, id string, from, to time.Time) ([]Candle, error) {
	history, err := c.ExchangeDailyHistory(ctx, id, from, to)
	return history.Candles, err
}

// DailyHistory retains the response currency alongside its candle observations.
type DailyHistory struct {
	Candles       []Candle `json:"candles"`
	PriceCurrency string   `json:"priceCurrency"`
}

func (c *Client) ExchangeDailyHistory(ctx context.Context, id string, from, to time.Time) (DailyHistory, error) {
	if _, err := uuid.Parse(id); err != nil || from.IsZero() || !to.After(from) || to.Sub(from) > 365*24*time.Hour {
		return DailyHistory{}, errors.New("provide an instrument UID and a range of at most 365 days")
	}
	var result DailyHistory
	if err := c.read(ctx, "MarketDataService/GetCandles", map[string]any{"instrumentId": id, "from": from.UTC(), "to": to.UTC(), "interval": "CANDLE_INTERVAL_DAY", "candleSourceType": "CANDLE_SOURCE_EXCHANGE"}, &result); err != nil {
		return DailyHistory{}, err
	}
	seen := make(map[time.Time]bool)
	for _, candle := range result.Candles {
		if candle.Time.Before(from) || candle.Time.After(to) || seen[candle.Time] || candle.Source != "CANDLE_SOURCE_EXCHANGE" {
			return DailyHistory{}, errors.New("invalid T-Invest daily candle metadata")
		}
		values := make([]*big.Rat, 4)
		for i, q := range []Quotation{candle.Open, candle.High, candle.Low, candle.Close} {
			decimal, err := q.Decimal()
			if err != nil {
				return DailyHistory{}, err
			}
			values[i], _ = new(big.Rat).SetString(decimal)
		}
		if values[1].Cmp(values[2]) < 0 || values[0].Cmp(values[2]) < 0 || values[0].Cmp(values[1]) > 0 || values[3].Cmp(values[2]) < 0 || values[3].Cmp(values[1]) > 0 {
			return DailyHistory{}, errors.New("invalid T-Invest OHLC range")
		}
		volume := candle.Volume
		if volume == "" {
			volume = "0"
		}
		n, err := volume.Int64()
		if err != nil || n < 0 {
			return DailyHistory{}, errors.New("invalid T-Invest candle volume")
		}
		seen[candle.Time] = true
	}
	return result, nil
}
