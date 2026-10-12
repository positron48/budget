package marketdata

import "time"

// AlphaFXPolicy explicitly opts into retrospective valuation. The provider's
// daily date is not evidence of historical publication time. Availability here
// is an application convention, never a claim of point-in-time market access.
type AlphaFXPolicy struct {
	From       time.Time
	Convention string
}

const AlphaRetrospectiveConvention = "retrospective_close_next_utc_day_rub_hub_v1"
const AlphaTimeConvention = "Retrospective valuation: UTC daily close becomes eligible at the next UTC midnight by application convention; historical publication time is unknown. Not suitable for point-in-time backtesting."
const AlphaValueConvention = "FX_DAILY close, quote currency per one base unit; USD/RUB and EUR/RUB only. EUR/USD is derived through RUB, never merged with the provider direct EUR/USD series."
const AlphaValidityConvention = "Eligible for at most 96 hours from next UTC midnight; latest eligible observation wins, including weekends and short gaps. No fabricated observations, indefinite carry, averaging or CBR fallback."

func ValidateAlphaFXPolicy(p FXPolicy) error {
	if p.Alpha == nil {
		return nil
	}
	a := p.Alpha
	if p.Mode != "market" || p.Source != "alphavantage" || p.AccessScope != "personal" || p.Official != nil || len(p.Series) != 0 || a.Convention != AlphaRetrospectiveConvention || a.From.IsZero() || a.From.Year() < 1900 || a.From.Year() > 2199 || !a.From.Equal(a.From.UTC().Truncate(24*time.Hour)) || p.TimeConvention != AlphaTimeConvention || p.ValueConvention != AlphaValueConvention || p.ValidityConvention != AlphaValidityConvention {
		return ErrInvalid
	}
	return nil
}
