package tinvest

import (
	"context"
	"strings"

	md "github.com/positron48/budget/internal/usecase/marketdata"
)

// PriceObservation deliberately leaves units/currency unresolved: metadata
// normalization and a confirmed provider mapping are required before valuation.
func PriceObservation(p LastPrice) (md.Observation, error) {
	price, err := p.Price.Decimal()
	if err != nil {
		return md.Observation{}, err
	}
	source := ""
	switch p.Type {
	case "LAST_PRICE_EXCHANGE":
		source = "exchange"
	case "LAST_PRICE_DEALER":
		source = "dealer"
	}
	// GetLastPrices describes the last actual transaction at this listing/time,
	// unlike historical candles that can be adjusted retrospectively. Currency
	// and monetary/bond-point units still require explicit instrument terms.
	// https://developer.tbank.ru/invest/services/quotes/marketdata
	return md.Normalize(md.Observation{InstrumentID: p.InstrumentUID, Kind: "last_price", At: p.Time, SourceKind: source, PriceUnit: "provider_unspecified", Adjustment: "unadjusted", Price: price, Complete: true})
}
func CandleObservations(instrumentID string, history DailyHistory) ([]md.Observation, error) {
	currency := strings.ToUpper(history.PriceCurrency)
	// The provider uses "pt." for point-quoted instruments. It is not an
	// ISO currency. Keep the unit unresolved: neither bond nominal nor a
	// futures point multiplier can be inferred from this response marker.
	if history.PriceCurrency == "pt." {
		currency = ""
	}
	observations := make([]md.Observation, 0, len(history.Candles))
	for _, c := range history.Candles {
		close, err := c.Close.Decimal()
		if err != nil {
			return nil, err
		}
		open, err := c.Open.Decimal()
		if err != nil {
			return nil, err
		}
		high, err := c.High.Decimal()
		if err != nil {
			return nil, err
		}
		low, err := c.Low.Decimal()
		if err != nil {
			return nil, err
		}
		source := ""
		switch c.Source {
		case "CANDLE_SOURCE_EXCHANGE":
			source = "exchange"
		case "CANDLE_SOURCE_DEALER_WEEKEND":
			source = "dealer"
		}
		volume := c.Volume.String()
		if volume == "" {
			volume = "0"
		}
		o, err := md.Normalize(md.Observation{InstrumentID: instrumentID, Kind: "daily_candle", At: c.Time, SourceKind: source, Currency: currency, PriceUnit: "provider_unspecified", Adjustment: "provider_unspecified", Price: close, Open: open, High: high, Low: low, Volume: volume, Complete: c.Complete})
		if err != nil {
			return nil, err
		}
		observations = append(observations, o)
	}
	return observations, nil
}

// LoadMarketObservations is a read-only provider for the durable queue worker.
func (c *Client) LoadMarketObservations(ctx context.Context, q md.JobRequest) ([]md.Observation, error) {
	if err := md.ValidateRequest(q); err != nil {
		return nil, err
	}
	if q.Kind == "daily_candle" {
		history, err := c.ExchangeDailyHistory(ctx, q.InstrumentID, q.From, q.To)
		if err != nil {
			return nil, err
		}
		return CandleObservations(q.InstrumentID, history)
	}
	prices, err := c.ExchangeLastPrices(ctx, []string{q.InstrumentID})
	if err != nil {
		return nil, err
	}
	result := make([]md.Observation, 0, len(prices))
	for _, price := range prices {
		o, err := PriceObservation(price)
		if err != nil {
			return nil, err
		}
		result = append(result, o)
	}
	return result, nil
}
