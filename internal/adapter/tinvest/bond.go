package tinvest

import (
	"context"
	"encoding/json"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"strings"
	"time"
)

type bondMoney struct {
	Quotation
	Currency string `json:"currency"`
}

func bondAmount(v *bondMoney) (*md.BondAmount, error) {
	if v == nil {
		return nil, nil
	}
	n, e := v.Decimal()
	if e != nil {
		return nil, md.ErrInvalid
	}
	return &md.BondAmount{Amount: n, Currency: strings.ToUpper(v.Currency)}, nil
}
func bondNumber(v *Quotation) (*string, error) {
	if v == nil {
		return nil, nil
	}
	n, e := v.Decimal()
	if e != nil {
		return nil, md.ErrInvalid
	}
	return &n, nil
}
func (c *Client) LoadBondFacts(ctx context.Context, q md.JobRequest) (md.BondFacts, error) {
	var out md.BondFacts
	if md.ValidateRequest(q) != nil || q.Kind != "bond_facts" {
		return out, md.ErrInvalid
	}
	out.From = q.From
	out.To = q.To
	out.AsOf = time.Now().UTC().Truncate(time.Microsecond)
	var response struct {
		Instrument struct {
			Instrument
			Currency       string     `json:"currency"`
			Nominal        *bondMoney `json:"nominal"`
			InitialNominal *bondMoney `json:"initialNominal"`
			ACI            *bondMoney `json:"aciValue"`
			Maturity       time.Time  `json:"maturityDate"`
			Call           time.Time  `json:"callDate"`
			Placement      time.Time  `json:"placementDate"`
			Floating       *bool      `json:"floatingCouponFlag"`
			Perpetual      *bool      `json:"perpetualFlag"`
			Amortizing     *bool      `json:"amortizationFlag"`
			Frequency      *int32     `json:"couponQuantityPerYear"`
		} `json:"instrument"`
	}
	if e := c.read(ctx, "InstrumentsService/BondBy", map[string]any{"idType": "INSTRUMENT_ID_TYPE_UID", "id": q.InstrumentID}, &response); e != nil {
		return out, e
	}
	raw := response.Instrument
	if raw.UID != q.InstrumentID {
		return out, md.ErrInvalid
	}
	out.Instrument = md.Instrument{Detailed: true, ID: raw.UID, FIGI: raw.FIGI, ISIN: raw.ISIN, Ticker: raw.Ticker, ClassCode: raw.ClassCode, Name: raw.Name, Kind: "bond", Currency: strings.ToUpper(raw.Currency)}
	var e error
	if out.Terms.Nominal, e = bondAmount(raw.Nominal); e != nil {
		return out, e
	}
	if out.Terms.InitialNominal, e = bondAmount(raw.InitialNominal); e != nil {
		return out, e
	}
	if out.Terms.CurrentAccrued, e = bondAmount(raw.ACI); e != nil {
		return out, e
	}
	out.Terms.Maturity = raw.Maturity
	out.Terms.CallDate = raw.Call
	out.Terms.Placement = raw.Placement
	out.Terms.Floating = raw.Floating
	out.Terms.Perpetual = raw.Perpetual
	out.Terms.Amortizing = raw.Amortizing
	out.Terms.CouponFrequency = raw.Frequency
	request := map[string]any{"instrumentId": q.InstrumentID, "from": q.From.UTC().Format(time.RFC3339), "to": q.To.UTC().Format(time.RFC3339)}
	var coupons struct {
		Events []struct {
			FIGI    string      `json:"figi"`
			Date    time.Time   `json:"couponDate"`
			Number  json.Number `json:"couponNumber"`
			FixDate time.Time   `json:"fixDate"`
			Pay     *bondMoney  `json:"payOneBond"`
			Kind    string      `json:"couponType"`
			Start   time.Time   `json:"couponStartDate"`
			End     time.Time   `json:"couponEndDate"`
			Days    int32       `json:"couponPeriod"`
		} `json:"events"`
	}
	if e = c.read(ctx, "InstrumentsService/GetBondCoupons", request, &coupons); e != nil {
		return out, e
	}
	for _, p := range coupons.Events {
		if p.FIGI != "" && p.FIGI != raw.FIGI {
			return out, md.ErrInvalid
		}
		number, e := p.Number.Int64()
		if e != nil {
			return out, md.ErrInvalid
		}
		pay, e := bondAmount(p.Pay)
		if e != nil {
			return out, e
		}
		out.Coupons = append(out.Coupons, md.BondCoupon{Number: number, Date: p.Date, FixDate: p.FixDate, Pay: pay, Kind: p.Kind, Start: p.Start, End: p.End, PeriodDays: p.Days})
	}
	var events struct {
		Events []struct {
			InstrumentID  string     `json:"instrumentId"`
			Number        int32      `json:"eventNumber"`
			Date          time.Time  `json:"eventDate"`
			Kind          string     `json:"eventType"`
			FixDate       time.Time  `json:"fixDate"`
			PayDate       time.Time  `json:"payDate"`
			ActualPayDate time.Time  `json:"realPayDate"`
			Pay           *bondMoney `json:"payOneBond"`
			Value         *Quotation `json:"value"`
			Rate          *Quotation `json:"couponInterestRate"`
			Execution     string     `json:"execution"`
			Operation     string     `json:"operationType"`
			Note          string     `json:"note"`
			ConvertTo     string     `json:"convertToFinToolId"`
			Start         time.Time  `json:"couponStartDate"`
			End           time.Time  `json:"couponEndDate"`
		} `json:"events"`
	}
	if e = c.read(ctx, "InstrumentsService/GetBondEvents", request, &events); e != nil {
		return out, e
	}
	for _, p := range events.Events {
		if p.InstrumentID != "" && p.InstrumentID != q.InstrumentID && p.InstrumentID != raw.FIGI {
			return out, md.ErrInvalid
		}
		pay, e := bondAmount(p.Pay)
		if e != nil {
			return out, e
		}
		value, e := bondNumber(p.Value)
		if e != nil {
			return out, e
		}
		rate, e := bondNumber(p.Rate)
		if e != nil {
			return out, e
		}
		out.Events = append(out.Events, md.BondEvent{Number: p.Number, Kind: p.Kind, Date: p.Date, FixDate: p.FixDate, PayDate: p.PayDate, ActualPayDate: p.ActualPayDate, Pay: pay, Value: value, AnnualRate: rate, Execution: p.Execution, OperationType: p.Operation, Note: p.Note, ConvertTo: p.ConvertTo, Start: p.Start, End: p.End})
	}
	var accrued struct {
		Points []struct {
			Date    time.Time  `json:"date"`
			Value   *Quotation `json:"value"`
			Percent *Quotation `json:"valuePercent"`
			Nominal *Quotation `json:"nominal"`
		} `json:"accruedInterests"`
	}
	// NКD is historical evidence; future schedules do not require future accrued data.
	accruedTo := q.To
	if accruedTo.After(out.AsOf) {
		accruedTo = out.AsOf.Truncate(time.Second)
	}
	accruedRequest := map[string]any{"instrumentId": q.InstrumentID, "from": q.From.UTC().Format(time.RFC3339), "to": accruedTo.UTC().Format(time.RFC3339)}
	if !accruedTo.Before(q.From) {
		if e = c.read(ctx, "InstrumentsService/GetAccruedInterests", accruedRequest, &accrued); e != nil {
			return out, e
		}
	}
	for _, p := range accrued.Points {
		value, e := bondNumber(p.Value)
		if e != nil {
			return out, e
		}
		percent, e := bondNumber(p.Percent)
		if e != nil {
			return out, e
		}
		nominal, e := bondNumber(p.Nominal)
		if e != nil {
			return out, e
		}
		out.Accrued = append(out.Accrued, md.BondAccrued{Date: p.Date, Value: value, Percent: percent, Nominal: nominal})
	}
	return md.NormalizeBondFacts(out)
}
