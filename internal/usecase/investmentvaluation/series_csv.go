package investmentvaluation

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	"math/big"
	"strconv"
	"time"
)

const MaxSeriesCSVBytes = 32 << 20
const MaxSeriesCompressedBytes = 3 << 20

type SeriesCSV struct {
	Filename, SHA256       string
	Data                   []byte
	Rows, UncompressedSize int
	Generation             int64
}
type csvLimitWriter struct {
	bytes.Buffer
	limit int
}

func (w *csvLimitWriter) Write(p []byte) (int, error) {
	if w.Len()+len(p) > w.limit {
		return 0, d.ErrInvestmentLimit
	}
	return w.Buffer.Write(p)
}

// EncodeSeriesCSV exports every requested reporting day. Unknown financial
// values are empty cells, not zero. Exact fractions accompany display decimals.
// Gzip is transport only; SHA256 describes the original deterministic CSV.
func EncodeSeriesCSV(p Performance, history []DailyNAV, mode string) (SeriesCSV, error) {
	out := SeriesCSV{Generation: p.Generation}
	from, e := time.Parse(time.DateOnly, p.From)
	if e != nil {
		return out, d.ErrInvestmentInvalid
	}
	to, e := time.Parse(time.DateOnly, p.To)
	if e != nil || from.After(to) || p.SeriesKey == "" {
		return out, d.ErrInvestmentInvalid
	}
	count := int(to.Sub(from)/(24*time.Hour)) + 1
	if count > 3660 || len(history) != count || len(p.Days) != count+1 {
		return out, d.ErrInvestmentPrecondition
	}
	timeline, e := calc.CalculateProfitTimeline(calc.PeriodPerformanceInput{Days: p.Days, FlowCoverage: true, ResetDates: p.ResetDates})
	if e != nil {
		return out, e
	}
	profit := map[string]calc.ProfitPoint{}
	for _, point := range timeline.Points {
		profit[point.Date] = point
	}
	twr := map[string]calc.TWRPoint{}
	for _, point := range p.TWR.Points {
		twr[point.Date] = point
	}
	buf := &csvLimitWriter{limit: MaxSeriesCSVBytes}
	writer := csv.NewWriter(buf)
	header := []string{"schema_version", "series_key", "from_date", "to_date", "baseline_date", "date", "reporting_timezone", "currency", "fx_policy_key", "fx_policy_mode", "fx_policy_ref", "read_generation", "nav_generation", "computed_at", "as_of", "nav_state"}
	amountHeaders := func(name string) { header = append(header, name+"_numerator", name+"_denominator", name+"_decimal") }
	for _, name := range []string{"baseline_nav", "nav", "securities_known", "cash_known", "external_flow", "daily_profit", "cumulative_profit", "daily_return", "segment_return", "twr_index"} {
		amountHeaders(name)
	}
	header = append(header, "securities_complete", "cash_complete", "external_flow_known", "daily_profit_reason", "cumulative_profit_reason", "twr_state", "twr_reason", "twr_segment", "nav_issues_json", "nav_warnings_json", "breakdown_issues_json", "fx_paths_json", "scope", "baseline_complete", "ledger_versions_json")
	if e = writer.Write(header); e != nil {
		return out, e
	}
	stamp := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.UTC().Format(time.RFC3339Nano)
	}
	number := func(row []string, value *big.Rat) []string {
		if value == nil {
			return append(row, "", "", "")
		}
		return append(row, value.Num().String(), value.Denom().String(), value.FloatString(12))
	}
	for i, day := range history {
		expected := from.AddDate(0, 0, i).Format(time.DateOnly)
		if day.Date != expected || p.Days[i+1].Date != expected || p.Days[0].Date != from.AddDate(0, 0, -1).Format(time.DateOnly) {
			return out, d.ErrInvestmentPrecondition
		}
		perfDay := p.Days[i+1]
		point := profit[day.Date]
		returnPoint := twr[day.Date]
		row := []string{"investment_series_v1", p.SeriesKey, p.From, p.To, p.Days[0].Date, day.Date, "Europe/Moscow", p.Currency, p.FXPolicyKey, mode, "", strconv.FormatInt(p.Generation, 10), strconv.FormatInt(day.Generation, 10), stamp(day.ComputedAt), "", day.State}
		var value, securities, cash *big.Rat
		var composition Composition
		var issues, warnings []calc.QualityIssue
		var paths map[string][]calc.FXLeg
		if (day.State == "ready" || day.State == "incomplete") && day.Result != nil {
			r := day.Result
			value = r.KnownValue
			row[10] = r.FXPolicy
			row[14] = stamp(r.At)
			issues = r.Issues
			warnings = r.Warnings
			paths = r.FXPaths
			composition, e = ComposeComposition(*r, InstrumentGroups{}, nil, "account")
			if e != nil {
				return out, e
			}
			securities, cash = composition.SecuritiesKnown, composition.CashKnown
		}
		baseline := p.Days[0].NAV
		if !p.Days[0].Complete {
			baseline = nil
		}
		daily, cumulative := point.Daily, point.Cumulative
		if point.DailyReason != "" {
			daily = nil
		}
		if point.CumulativeReason != "" {
			cumulative = nil
		}
		dailyReturn, segmentReturn := returnPoint.DailyReturn, returnPoint.SegmentReturn
		if returnPoint.State != "calculated" {
			dailyReturn = nil
			segmentReturn = nil
		}
		for _, v := range []*big.Rat{baseline, value, securities, cash, perfDay.ExternalFlow, daily, cumulative, dailyReturn, segmentReturn, calc.TWRIndex(returnPoint)} {
			row = number(row, v)
		}
		row = append(row, strconv.FormatBool(composition.SecuritiesComplete), strconv.FormatBool(composition.CashComplete), strconv.FormatBool(perfDay.ExternalFlow != nil), point.DailyReason, point.CumulativeReason, returnPoint.State, returnPoint.Reason, strconv.Itoa(returnPoint.Segment))
		for _, v := range []any{issues, warnings, composition.Issues, paths} {
			raw, err := json.Marshal(v)
			if err != nil {
				return out, err
			}
			row = append(row, string(raw))
		}
		versions := map[string]int64(nil)
		if (day.State == "ready" || day.State == "incomplete") && day.Result != nil {
			versions = day.Result.LedgerVersions
		}
		rawVersions, err := json.Marshal(versions)
		if err != nil {
			return out, err
		}
		row = append(row, "full_registered_accounts", strconv.FormatBool(p.Days[0].Complete), string(rawVersions))
		if e = writer.Write(row); e != nil {
			return out, e
		}
	}
	writer.Flush()
	if e = writer.Error(); e != nil {
		return out, e
	}
	raw := buf.Bytes()
	sum := sha256.Sum256(raw)
	out.SHA256 = hex.EncodeToString(sum[:])
	out.UncompressedSize = len(raw)
	compressed := &csvLimitWriter{limit: MaxSeriesCompressedBytes}
	zip := gzip.NewWriter(compressed)
	if _, e = zip.Write(raw); e != nil {
		return out, e
	}
	if e = zip.Close(); e != nil {
		return out, e
	}
	out.Data = compressed.Bytes()
	out.Rows = count
	out.Filename = "investments-" + p.Currency + "-" + p.From + "-" + p.To + ".csv"
	return out, nil
}
