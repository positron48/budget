// Package investmentarchive defines a versioned, exact investment-data bundle.
// It is distinct from a complete PostgreSQL backup and contains no auth secrets.
package investmentarchive

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const Format = "budget.investments.postgres.v1"
const MaxBytes = 256 * 1024 * 1024
const MaxRows = 100000

// Explicit classification prevents new domain tables being silently omitted.
var Included = []string{
	"investment_imports", "investment_import_accounts", "investment_import_rows", "investment_import_mapping_revisions",
	"investment_ledgers", "investment_ledger_revisions", "investment_operations", "investment_operation_revisions", "investment_security_legs", "investment_cash_legs", "investment_transfers", "investment_source_links", "investment_import_ledger_bindings",
	"investment_market_mappings", "investment_market_mapping_revisions",
	"investment_fx_policies", "investment_fx_policy_revisions",
	"investment_price_conventions", "investment_price_convention_revisions",
	"investment_benchmarks", "investment_benchmark_revisions",
	"investment_portfolios", "investment_portfolio_revisions", "investment_portfolio_requests", "financial_write_requests",
	"investment_archive_restores", "investment_batch_drafts", "investment_batch_cancellations",
	"investment_broker_snapshots", "investment_broker_observations", "investment_broker_snapshot_rows",
	"investment_broker_links", "investment_broker_claims",
	"investment_groups", "investment_group_revisions", "investment_group_requests",
	"investment_payout_events", "investment_payout_event_revisions", "investment_payout_links", "investment_payout_link_revisions",
	"investment_dividend_histories", "investment_dividend_history_revisions",
}
var Excluded = map[string]string{
	"investment_export_jobs":            "Ephemeral private downloads; regenerated from canonical domain facts",
	"investment_cash_links":             "budget_integration_requires_full_backup",
	"investment_asset_activations":      "budget_integration_requires_full_backup",
	"investment_recalculation_jobs":     "derived_work_queue",
	"investment_market_mapping_changes": "derived_change_feed",
	"investment_fx_points":              "licensed_market_cache", "investment_fx_point_revisions": "licensed_market_cache", "investment_fx_changes": "derived_change_feed",
	"investment_valuation_generations": "derived_generation", "investment_nav_invalidations": "derived_change_feed",
	"investment_nav_series": "derived_projection", "investment_daily_nav": "derived_projection", "investment_daily_nav_revisions": "derived_projection",
	"investment_nav_jobs": "derived_work_queue", "investment_nav_watches": "derived_work_queue",
}

type Column struct {
	Name, Type string
	NotNull    bool
}
type Table struct {
	Name    string
	Columns []Column
	Rows    []json.RawMessage
}
type Dataset struct {
	TenantID  string
	CreatedAt time.Time
	Tables    []Table
	Actors    []string // referenced identities; no credentials or auth records
	Excluded  map[string]string
}
type Bundle struct {
	Format string
	SHA256 string
	Data   Dataset
}

func id(s string) bool { u, e := uuid.Parse(s); return e == nil && u.String() == s }
func Validate(data Dataset) error {
	if !id(data.TenantID) || data.CreatedAt.IsZero() || len(data.Tables) != len(Included) || len(data.Excluded) < len(Excluded)-3 || len(data.Excluded) > len(Excluded) {
		return fmt.Errorf("invalid archive manifest")
	}
	for key, reason := range Excluded {
		if (key == "investment_asset_activations" || key == "investment_cash_links" || key == "investment_export_jobs") && data.Excluded[key] == "" {
			continue
		} // older v1 bundles predate Budget integration or ephemeral export jobs
		if data.Excluded[key] != reason {
			return fmt.Errorf("archive exclusion manifest mismatch")
		}
	}
	for key, reason := range data.Excluded {
		if expected, ok := Excluded[key]; !ok || expected != reason {
			return fmt.Errorf("unknown archive exclusion")
		}
	}
	required := map[string]bool{}
	for _, name := range Included {
		required[name] = true
	}
	actors := map[string]bool{}
	for _, actor := range data.Actors {
		if !id(actor) || actors[actor] {
			return fmt.Errorf("invalid actor manifest")
		}
		actors[actor] = true
	}
	total := 0
	for _, table := range data.Tables {
		if !required[table.Name] {
			return fmt.Errorf("unknown or duplicate archive table")
		}
		delete(required, table.Name)
		columns := map[string]bool{}
		for _, column := range table.Columns {
			if column.Name == "" || column.Type == "" || columns[column.Name] {
				return fmt.Errorf("invalid column manifest")
			}
			columns[column.Name] = true
		}
		if !columns["tenant_id"] {
			return fmt.Errorf("tenant column required")
		}
		for _, raw := range table.Rows {
			total++
			if total > MaxRows {
				return fmt.Errorf("archive row limit exceeded")
			}
			var values map[string]json.RawMessage
			if json.Unmarshal(raw, &values) != nil || values == nil || len(values) != len(columns) {
				return fmt.Errorf("invalid archive row")
			}
			for column := range values {
				if !columns[column] {
					return fmt.Errorf("unknown archive column")
				}
			}
			var tenant string
			if json.Unmarshal(values["tenant_id"], &tenant) != nil || tenant != data.TenantID {
				return fmt.Errorf("cross-tenant archive row")
			}
			if table.Name == "financial_write_requests" {
				var operation string
				if json.Unmarshal(values["operation"], &operation) != nil || !strings.HasPrefix(operation, "investment.") {
					return fmt.Errorf("non-investment request in archive")
				}
			}
			for _, field := range []string{"actor_id", "created_by"} {
				if value, ok := values[field]; ok && string(value) != "null" {
					var actor string
					if json.Unmarshal(value, &actor) != nil || !actors[actor] {
						return fmt.Errorf("undeclared archive actor")
					}
				}
			}
		}
	}
	return nil
}
func Seal(data Dataset) (Bundle, error) {
	if e := Validate(data); e != nil {
		return Bundle{}, e
	}
	raw, e := json.Marshal(data)
	if e != nil {
		return Bundle{}, e
	}
	if len(raw) > MaxBytes {
		return Bundle{}, fmt.Errorf("archive byte limit exceeded")
	}
	sum := sha256.Sum256(raw)
	out := Bundle{Format: Format, SHA256: hex.EncodeToString(sum[:]), Data: data}
	encoded, e := json.Marshal(out)
	if e != nil || len(encoded) > MaxBytes {
		return Bundle{}, fmt.Errorf("archive byte limit exceeded")
	}
	return out, nil
}
func Decode(reader io.Reader) (Bundle, error) {
	var out Bundle
	raw, e := io.ReadAll(io.LimitReader(reader, MaxBytes+1))
	if e != nil || len(raw) > MaxBytes {
		return out, fmt.Errorf("archive unreadable or too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&out); e != nil {
		return out, fmt.Errorf("invalid archive schema")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return out, fmt.Errorf("trailing archive document")
	}
	if out.Format != Format {
		return out, fmt.Errorf("unsupported archive format")
	}
	expected, e := Seal(out.Data)
	if e != nil {
		return out, e
	}
	if out.SHA256 != expected.SHA256 {
		return out, fmt.Errorf("archive checksum mismatch")
	}
	return out, nil
}
func Canonicalize(data *Dataset) {
	sort.Slice(data.Tables, func(i, j int) bool { return data.Tables[i].Name < data.Tables[j].Name })
	sort.Strings(data.Actors)
	for i := range data.Tables {
		sort.Slice(data.Tables[i].Rows, func(a, b int) bool { return bytes.Compare(data.Tables[i].Rows[a], data.Tables[i].Rows[b]) < 0 })
	}
}
