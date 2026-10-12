// Package investmentimport inspects source exports before financial mapping.
package investmentimport

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxSourceBytes = 20 << 20
const MaxSourceRows = 100000

var snowballHeader = []string{"Event", "Date", "Symbol", "Price", "Quantity", "Currency", "FeeTax", "Exchange", "NKD", "FeeCurrency", "DoNotAdjustCash", "Note"}

// SourceAudit reports syntax and coverage only, never import readiness or NAV.
// Dates retain their original wall time: the export does not specify a timezone.
type SourceAudit struct {
	Format        string             `json:"format"`
	SHA256        string             `json:"sha256"`
	Rows          int                `json:"rows"`
	FirstDate     string             `json:"first_date"`
	LastDate      string             `json:"last_date"`
	Events        map[string]int     `json:"events"`
	Currencies    []string           `json:"currencies"`
	Instruments   []SourceInstrument `json:"instruments"`
	IdenticalRows int                `json:"identical_rows"`
	Issues        []SourceIssue      `json:"issues"`
	Limitations   []string           `json:"limitations"`
}

type SourceInstrument struct {
	Symbol   string `json:"symbol"`
	Exchange string `json:"exchange"`
}

type SourceIssue struct {
	Row   int    `json:"row"`
	Field string `json:"field"`
	Code  string `json:"code"`
}

// AuditSnowball preserves duplicates and unknown event names. Mapping their
// financial meaning requires a separate preview; no source values are executed.
func AuditSnowball(input io.Reader) (SourceAudit, error) {
	a := SourceAudit{Format: "snowball-csv-12-v1", Events: map[string]int{}, Issues: []SourceIssue{}, Instruments: []SourceInstrument{}, Currencies: []string{}, Limitations: []string{
		"account_not_in_file: explicit combined scope or one export per source account required",
		"operation_id_not_in_file: identical rows are candidates, not duplicates to discard",
		"timezone_and_settlement_not_in_file",
		"cash_and_corporate_action_coverage_requires_reconciliation",
	}}
	b, err := io.ReadAll(io.LimitReader(input, MaxSourceBytes+1))
	if err != nil {
		return a, fmt.Errorf("read source: %w", err)
	}
	if len(b) > MaxSourceBytes {
		return a, fmt.Errorf("source exceeds 20 MiB")
	}
	if !utf8.Valid(b) {
		return a, fmt.Errorf("source must be UTF-8")
	}
	hash := sha256.Sum256(b)
	a.SHA256 = hex.EncodeToString(hash[:])
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(b, []byte{0xef, 0xbb, 0xbf})))
	header, err := r.Read()
	if err != nil || !slices.Equal(header, snowballHeader) {
		return a, fmt.Errorf("unsupported Snowball header; expected exact 12-column export")
	}
	seen := map[[32]byte]bool{}
	instruments := map[SourceInstrument]bool{}
	currencies := map[string]bool{}
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return a, fmt.Errorf("invalid CSV record %d", a.Rows+2)
		}
		a.Rows++
		if a.Rows > MaxSourceRows {
			return a, fmt.Errorf("source exceeds 100000 rows")
		}
		issue := func(field, code string) { a.Issues = append(a.Issues, SourceIssue{a.Rows + 1, field, code}) }
		a.Events[row[0]]++
		if row[0] == "" {
			issue("Event", "required")
		}
		if _, err := time.Parse("2006-01-02 15:04:05", row[1]); err != nil {
			issue("Date", "invalid_wall_time")
		} else {
			if a.FirstDate == "" || row[1] < a.FirstDate {
				a.FirstDate = row[1]
			}
			if row[1] > a.LastDate {
				a.LastDate = row[1]
			}
		}
		for _, index := range []int{3, 4, 6, 8} {
			if index == 8 && row[index] == "" {
				continue
			}
			if _, err := NormalizeSourceDecimal(row[index]); err != nil {
				issue(snowballHeader[index], "invalid_decimal")
			}
		}
		if row[5] == "" {
			issue("Currency", "required")
		} else {
			currencies[row[5]] = true
		}
		if row[9] != "" {
			currencies[row[9]] = true
		}
		if row[10] != "True" && row[10] != "False" && (row[0] != "SPLIT" || row[10] != "") {
			issue("DoNotAdjustCash", "invalid_boolean")
		}
		if row[2] != "" {
			instruments[SourceInstrument{row[2], row[7]}] = true
		}
		// Canonical CSV encoding avoids ambiguous separator-based fingerprints.
		var encoded strings.Builder
		w := csv.NewWriter(&encoded)
		_ = w.Write(row)
		w.Flush()
		fingerprint := sha256.Sum256([]byte(encoded.String()))
		if seen[fingerprint] {
			a.IdenticalRows++
		}
		seen[fingerprint] = true
	}
	if a.Rows == 0 {
		return a, fmt.Errorf("source has no records")
	}
	for currency := range currencies {
		a.Currencies = append(a.Currencies, currency)
	}
	slices.Sort(a.Currencies)
	for instrument := range instruments {
		a.Instruments = append(a.Instruments, instrument)
	}
	slices.SortFunc(a.Instruments, func(x, y SourceInstrument) int {
		if c := strings.Compare(x.Symbol, y.Symbol); c != 0 {
			return c
		}
		return strings.Compare(x.Exchange, y.Exchange)
	})
	return a, nil
}
