package investmentimport

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

var decimalPattern = regexp.MustCompile(`^(-?)([0-9]+)(?:[.,]([0-9]+))?(?:[eE]([+-]?[0-9]+))?$`)

// NormalizeSourceDecimal accepts Snowball's decimal comma and scientific
// notation, returning exact ASCII decimal without binary floating point.
// Database precision/scale validation belongs to financial mapping, not parsing.
func NormalizeSourceDecimal(raw string) (string, error) {
	if len(raw) > 256 {
		return "", fmt.Errorf("decimal too long")
	}
	m := decimalPattern.FindStringSubmatch(raw)
	if m == nil {
		return "", fmt.Errorf("invalid decimal")
	}
	exponent := 0
	if m[4] != "" {
		var err error
		exponent, err = strconv.Atoi(m[4])
		if err != nil || exponent < -100 || exponent > 100 {
			return "", fmt.Errorf("decimal exponent outside source limit")
		}
	}
	digits := m[2] + m[3]
	point := len(m[2]) + exponent
	if point <= 0 {
		digits = strings.Repeat("0", 1-point) + digits
		point = 1
	}
	if point >= len(digits) {
		digits += strings.Repeat("0", point-len(digits))
	}
	whole := strings.TrimLeft(digits[:point], "0")
	if whole == "" {
		whole = "0"
	}
	fraction := strings.TrimRight(digits[point:], "0")
	value := whole
	if fraction != "" {
		value += "." + fraction
	}
	if m[1] == "-" && value != "0" {
		value = "-" + value
	}
	return value, nil
}

// StagedSource is a lossless local import artifact. It is deliberately not a
// posted financial ledger. Each row has stable provenance for later broker
// matching, including identical rows that must not be silently collapsed.
type StagedSource struct {
	Version     int         `json:"version"`
	State       string      `json:"state"`
	Scope       string      `json:"scope"`
	Audit       SourceAudit `json:"audit"`
	OriginalCSV []byte      `json:"original_csv"`
	Rows        []SourceRow `json:"rows"`
}

type SourceRow struct {
	ID       string            `json:"id"`
	Record   int               `json:"record"`
	Raw      map[string]string `json:"raw"`
	Decimals map[string]string `json:"decimals"`
}

func StageSnowball(input io.Reader, combined bool) (StagedSource, error) {
	var stage StagedSource
	if !combined {
		return stage, fmt.Errorf("source has no account column; explicit combined scope required")
	}
	b, err := io.ReadAll(io.LimitReader(input, MaxSourceBytes+1))
	if err != nil {
		return stage, err
	}
	a, err := AuditSnowball(bytes.NewReader(b))
	if err != nil {
		return stage, err
	}
	if len(a.Issues) > 0 {
		return stage, fmt.Errorf("source contains %d syntax issues; inspect audit", len(a.Issues))
	}
	stage = StagedSource{Version: 1, State: "awaiting_financial_mapping", Scope: "combined_unassigned", Audit: a, OriginalCSV: b, Rows: make([]SourceRow, 0, a.Rows)}
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(b, []byte{0xef, 0xbb, 0xbf})))
	_, _ = r.Read()
	for record := 1; record <= a.Rows; record++ {
		values, err := r.Read()
		if err != nil {
			return StagedSource{}, err
		}
		row := SourceRow{ID: fmt.Sprintf("%s:%d", a.SHA256, record), Record: record, Raw: map[string]string{}, Decimals: map[string]string{}}
		for i, key := range snowballHeader {
			row.Raw[key] = values[i]
		}
		for _, key := range []string{"Price", "Quantity", "FeeTax", "NKD"} {
			if row.Raw[key] == "" {
				continue
			}
			value, err := NormalizeSourceDecimal(row.Raw[key])
			if err != nil {
				return StagedSource{}, err
			}
			row.Decimals[key] = value
		}
		stage.Rows = append(stage.Rows, row)
	}
	return stage, nil
}
