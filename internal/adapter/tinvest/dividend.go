package tinvest

import (
	"context"
	"time"

	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func (c *Client) LoadDividendFacts(ctx context.Context, q md.JobRequest) (md.DividendFacts, error) {
	var out md.DividendFacts
	if q.Kind != "dividend_facts" || md.ValidateRequest(q) != nil {
		return out, md.ErrInvalid
	}
	instrument, err := c.LoadMarketInstrument(ctx, q.InstrumentID)
	if err != nil {
		return out, err
	}
	if instrument.Kind != "share" && instrument.Kind != "etf" {
		return out, md.ErrInvalid
	}
	out = md.DividendFacts{Instrument: instrument, From: q.From, To: q.To, AsOf: time.Now().UTC().Truncate(time.Microsecond)}
	var response struct {
		Dividends []struct {
			DividendNet  *bondMoney `json:"dividendNet"`
			ClosePrice   *bondMoney `json:"closePrice"`
			PaymentDate  time.Time  `json:"paymentDate"`
			DeclaredDate time.Time  `json:"declaredDate"`
			LastBuyDate  time.Time  `json:"lastBuyDate"`
			RecordDate   time.Time  `json:"recordDate"`
			CreatedAt    time.Time  `json:"createdAt"`
			DividendType string     `json:"dividendType"`
			Regularity   string     `json:"regularity"`
			YieldValue   *Quotation `json:"yieldValue"`
		} `json:"dividends"`
	}
	// The current API accepts instrumentId (UID); FIGI/ticker inference is never used.
	if err = c.read(ctx, "InstrumentsService/GetDividends", map[string]any{"instrumentId": q.InstrumentID, "from": q.From.UTC().Format(time.RFC3339), "to": q.To.UTC().Format(time.RFC3339)}, &response); err != nil {
		return out, err
	}
	for _, v := range response.Dividends {
		amount, err := bondAmount(v.DividendNet)
		if err != nil {
			return out, err
		}
		price, err := bondAmount(v.ClosePrice)
		if err != nil {
			return out, err
		}
		yield, err := bondNumber(v.YieldValue)
		if err != nil {
			return out, err
		}
		out.Dividends = append(out.Dividends, md.Dividend{DividendNet: amount, ClosePrice: price, PaymentDate: v.PaymentDate, DeclaredDate: v.DeclaredDate, LastBuyDate: v.LastBuyDate, RecordDate: v.RecordDate, CreatedAt: v.CreatedAt, DividendType: v.DividendType, Regularity: v.Regularity, YieldValue: yield})
	}
	return md.NormalizeDividendFacts(out)
}
