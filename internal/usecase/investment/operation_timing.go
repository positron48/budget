package investment

import (
	d "github.com/positron48/budget/internal/domain"
	"strings"
	"time"
)

// Day-only trades/openings/corporate actions use civil-day start; cash uses
// civil-day end after trades. Explicit day-only settlement also uses
// civil-day end. These are calculation conventions, never inferred executions.
// Stable sequence resolves ties and remains visible in the audit.
func normalizeOperationTiming(o *d.InvestmentOperation) error {
	if o.Timing == nil {
		return nil
	}
	terms := *o.Timing
	if terms.Timezone == "" || terms.Timezone == "Local" || len(terms.Timezone) > 100 || strings.TrimSpace(terms.Timezone) != terms.Timezone {
		return invalid("explicit IANA timing timezone required")
	}
	location, e := time.LoadLocation(terms.Timezone)
	if e != nil {
		return invalid("invalid timing timezone")
	}
	date := func(value string) (time.Time, time.Time, error) { return civilDateInterval(value, location) }
	if terms.OccurredDate != "" {
		at, end, err := date(terms.OccurredDate)
		if err != nil {
			return err
		}
		switch o.Kind {
		case "buy", "sell", "amortization", "redemption", "fx", "opening_cash", "opening_position", "split", "distribution", "security_transfer_in", "security_transfer_out":
		default:
			at = end.Add(-time.Microsecond)
		}
		if !o.OccurredAt.IsZero() && !o.OccurredAt.Equal(at) {
			return invalid("timestamp contradicts date-only occurrence")
		}
		o.OccurredAt = at.UTC()
	}
	if terms.SettlementDate != "" {
		if o.Kind != "buy" && o.Kind != "sell" && o.Kind != "amortization" && o.Kind != "redemption" && o.Kind != "fx" {
			return invalid("explicit settlement date only applies to trades")
		}
		_, end, err := date(terms.SettlementDate)
		if err != nil {
			return err
		}
		at := end.Add(-time.Microsecond)
		if !o.SettlementAt.IsZero() && !o.SettlementAt.Equal(at) {
			return invalid("timestamp contradicts date-only settlement")
		}
		o.SettlementAt = at.UTC()
		o.SettlementAssumed = false
	}
	if terms.SourceDate != "" {
		if _, _, err := date(terms.SourceDate); err != nil {
			return err
		}
		if o.OccurredAt.IsZero() || o.OccurredAt.In(location).Format(time.DateOnly) != terms.SourceDate {
			return invalid("source date contradicts occurrence")
		}
	}
	o.Timing = &terms
	return nil
}

// Find civil-day boundaries across constant-offset intervals. Midnight may be
// skipped by DST, and an entire civil date may be skipped at the date line.
// ZoneBounds avoids treating either case as an ordinary 24-hour UTC day.
func civilDateInterval(value string, location *time.Location) (time.Time, time.Time, error) {
	civil, e := time.Parse(time.DateOnly, value)
	if e != nil || len(value) != 10 || civil.Format(time.DateOnly) != value || civil.Year() < 1900 || civil.Year() > 2200 {
		return time.Time{}, time.Time{}, invalid("invalid civil date")
	}
	windowStart, windowEnd := civil.Add(-48*time.Hour), civil.Add(72*time.Hour)
	var first, last time.Time
	for segment := windowStart; segment.Before(windowEnd); {
		local := segment.In(location)
		_, offset := local.Zone()
		_, zoneEnd := local.ZoneBounds()
		end := zoneEnd
		if end.IsZero() || end.After(windowEnd) {
			end = windowEnd
		}
		if !end.After(segment) {
			return time.Time{}, time.Time{}, invalid("invalid timezone interval")
		}
		begin := civil.Add(-time.Duration(offset) * time.Second)
		finish := begin.Add(24 * time.Hour)
		if begin.Before(segment) {
			begin = segment
		}
		if finish.After(end) {
			finish = end
		}
		if begin.Before(finish) && begin.In(location).Format(time.DateOnly) == value {
			if first.IsZero() || begin.Before(first) {
				first = begin
			}
			if last.IsZero() || finish.After(last) {
				last = finish
			}
		}
		segment = end
	}
	if first.IsZero() {
		return time.Time{}, time.Time{}, invalid("civil date does not exist in timezone")
	}
	return first.UTC(), last.UTC(), nil
}

func operationTimingPriority(o d.InvestmentOperation) int {
	if o.Kind == "opening_cash" || o.Kind == "opening_position" {
		return 0
	}
	if o.Timing != nil && o.Timing.OccurredDate != "" && (o.Kind == "split" || o.Kind == "distribution") {
		return 1
	}
	return 2
}

// A confirmed civil date may be today even when its calculation boundary is
// day-end. The conditional boundary is not evidence of a future real event.
func operationIsFuture(o d.InvestmentOperation, now time.Time) bool {
	if o.Timing != nil && o.Timing.OccurredDate != "" {
		location, e := time.LoadLocation(o.Timing.Timezone)
		if e != nil {
			return true
		}
		return o.Timing.OccurredDate > now.In(location).Format(time.DateOnly)
	}
	return o.OccurredAt.After(now.Add(5 * time.Minute))
}
