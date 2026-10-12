package investmentarchive

import (
	"encoding/json"
	"fmt"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"reflect"
)

// Bind captured conversion to the immutable mapping revision, not today's
// schedule. Replay separately verifies exact units, validity interval and legs.
func VerifyLotConversions(data Dataset) error {
	revisions := map[string]md.Mapping{}
	operations := []d.InvestmentOperation{}
	key := func(instrument string, version int64) string { return fmt.Sprintf("%s\x00%d", instrument, version) }
	for _, table := range data.Tables {
		for _, raw := range table.Rows {
			switch table.Name {
			case "investment_market_mapping_revisions":
				var row struct {
					Tenant     string     `json:"tenant_id"`
					Instrument string     `json:"instrument_key"`
					Version    int64      `json:"version"`
					Data       md.Mapping `json:"data"`
				}
				if e := json.Unmarshal(raw, &row); e != nil {
					return e
				}
				if row.Tenant != data.TenantID || row.Instrument != row.Data.InstrumentKey || row.Version != row.Data.Version || row.Version < 1 {
					return fmt.Errorf("invalid mapping revision for lot conversion")
				}
				id := key(row.Instrument, row.Version)
				if _, exists := revisions[id]; exists {
					return fmt.Errorf("duplicate mapping revision")
				}
				revisions[id] = row.Data
			case "investment_operation_revisions":
				var row struct {
					After  d.InvestmentOperation  `json:"after_data"`
					Before *d.InvestmentOperation `json:"before_data"`
				}
				if e := json.Unmarshal(raw, &row); e != nil {
					return e
				}
				if row.After.LotConversion != nil {
					operations = append(operations, row.After)
				}
				if row.Before != nil && row.Before.LotConversion != nil {
					operations = append(operations, *row.Before)
				}
			case "investment_operations":
				var row struct {
					Data d.InvestmentOperation `json:"data"`
				}
				if e := json.Unmarshal(raw, &row); e != nil {
					return e
				}
				if row.Data.LotConversion != nil {
					operations = append(operations, row.Data)
				}
			}
		}
	}
	for _, op := range operations {
		actual := op.LotConversion
		mapping, ok := revisions[key(op.InstrumentKey, actual.MappingVersion)]
		if !ok {
			return fmt.Errorf("lot conversion mapping revision missing")
		}
		matched := false
		for i, p := range mapping.Periods {
			if op.OccurredAt.Before(p.From) || !p.To.IsZero() && !op.OccurredAt.Before(p.To) {
				continue
			}
			candidate := op
			candidate.LotConversion = &d.InvestmentLotConversion{Lots: actual.Lots, LotSize: p.LotSize, TermsID: fmt.Sprintf("%s:%d", op.InstrumentKey, i), MappingVersion: mapping.Version, Provider: p.Provider, AccessScope: p.AccessScope, InstrumentID: p.InstrumentID, Evidence: p.Evidence, From: p.From, To: p.To}
			if e := inv.ValidateOperation(&candidate); e != nil {
				return fmt.Errorf("invalid archived lot conversion")
			}
			if !reflect.DeepEqual(candidate.LotConversion, actual) {
				return fmt.Errorf("lot conversion differs from captured revision")
			}
			matched = true
			break
		}
		if !matched {
			return fmt.Errorf("lot conversion falls in mapping gap")
		}
	}
	return nil
}
