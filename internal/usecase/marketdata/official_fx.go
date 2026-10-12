package marketdata

import "time"

// OfficialFXPolicy opts into the independent CBR source. From is a calendar
// date encoded as UTC midnight, not the economic timestamp of a fixing.
type OfficialFXPolicy struct {
	From       time.Time
	Convention string
}

func ValidateOfficialFXPolicy(p FXPolicy) error {
	if p.Official == nil {
		return nil
	}
	o := p.Official
	if p.Mode != "cbr" || p.Source != "cbr" || p.AccessScope != "public" || len(p.Series) != 0 || o.Convention != "effective_moscow_date_v1" || o.From.IsZero() || o.From.Year() < 2015 || o.From.Year() > 2200 || !o.From.Equal(o.From.UTC().Truncate(24*time.Hour)) {
		return ErrInvalid
	}
	return nil
}

// ValidateOfficialFXPoints verifies complete daily coverage, not merely a count.
func ValidateOfficialFXPoints(p FXPolicy, points []FXPoint, from, to time.Time) error {
	if p.Official == nil || ValidateFXPolicy(p) != nil || from.Before(p.Official.From) || to.Before(from) || to.Sub(from) > 3660*24*time.Hour || !from.Equal(from.UTC().Truncate(24*time.Hour)) || !to.Equal(to.UTC().Truncate(24*time.Hour)) {
		return ErrInvalid
	}
	if len(points) != int(to.Sub(from)/(24*time.Hour)+1)*2 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, point := range points {
		v, e := NormalizeFXPoint(point)
		day := point.At.Add(3 * time.Hour)
		key := point.From + day.Format(time.DateOnly)
		if e != nil || v.Withdrawn || (v.From != "USD" && v.From != "EUR") || v.To != "RUB" || day.Before(from) || day.After(to) || !day.Equal(day.UTC().Truncate(24*time.Hour)) || !v.AvailableAt.Equal(v.At) || !v.ValidUntil.Equal(v.At.Add(24*time.Hour)) || seen[key] {
			return ErrInvalid
		}
		seen[key] = true
	}
	return nil
}
