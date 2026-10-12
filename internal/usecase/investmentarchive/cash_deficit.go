package investmentarchive

import (
	"encoding/json"
	"fmt"
	d "github.com/positron48/budget/internal/domain"
)

// Imported exceptions require their consumed source row, including when an
// exception only survives in an old revision or a voided operation.
func verifyCashDeficitSources(data Dataset) error {
	type sourceLink struct {
		Tenant    string `json:"tenant_id"`
		Import    string `json:"import_id"`
		Ledger    string `json:"ledger_id"`
		Operation string `json:"operation_id"`
		Reason    string `json:"review_reason"`
		Account   string `json:"account_id"`
		Record    int    `json:"record"`
		Version   int64  `json:"source_version"`
	}
	type sourceRow struct {
		Tenant  string `json:"tenant_id"`
		Import  string `json:"import_id"`
		Record  int    `json:"record"`
		Version int64  `json:"version"`
		Account string `json:"account_id"`
	}
	rows := map[string]sourceRow{}
	duplicates := map[string]bool{}
	links := map[string]sourceLink{}
	operations := []d.InvestmentOperation{}
	for _, table := range data.Tables {
		for _, raw := range table.Rows {
			switch table.Name {
			case "investment_source_links":
				var link sourceLink
				if err := json.Unmarshal(raw, &link); err != nil {
					return err
				}
				if _, duplicate := links[link.Operation]; duplicate {
					duplicates[link.Operation] = true
				}
				links[link.Operation] = link
			case "investment_import_rows":
				var row sourceRow
				if err := json.Unmarshal(raw, &row); err != nil {
					return err
				}
				rows[fmt.Sprintf("%s:%d", row.Import, row.Record)] = row
			case "investment_operations":
				var row storedOperation
				if err := json.Unmarshal(raw, &row); err != nil {
					return err
				}
				operations = append(operations, row.Data)
			case "investment_operation_revisions":
				var row operationRevision
				if err := json.Unmarshal(raw, &row); err != nil {
					return err
				}
				operations = append(operations, row.After)
				if row.Before != nil {
					operations = append(operations, *row.Before)
				}
			}
		}
	}
	for _, o := range operations {
		if o.ImportedCashDeficitReason == "" {
			continue
		}
		s := o.Source
		link, ok := links[o.ID]
		if s == nil || !ok || duplicates[o.ID] || link.Tenant != data.TenantID || link.Tenant != o.TenantID || link.Ledger != o.LedgerID || link.Import != s.ImportID || link.Record != s.Record || link.Version != s.ExpectedVersion || link.Reason != s.ReviewReason {
			return fmt.Errorf("archived cash deficit exception has no matching consumed source row")
		}
		row, ok := rows[fmt.Sprintf("%s:%d", link.Import, link.Record)]
		if !ok || row.Tenant != data.TenantID || row.Account != link.Account || row.Version < link.Version {
			return fmt.Errorf("archived cash deficit source row is missing or inconsistent")
		}
	}
	return nil
}
