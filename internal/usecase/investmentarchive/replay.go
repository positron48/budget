package investmentarchive

import (
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"time"

	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
)

type JournalVerification struct{ Ledgers, Operations, CashLegs, SecurityLegs int }
type storedLedger struct {
	Broker            string `json:"broker"`
	AccountType       string `json:"account_type"`
	Comment           string `json:"comment"`
	DefaultCurrency   string `json:"default_currency"`
	Archived          bool   `json:"archived"`
	MetadataVersion   int64  `json:"metadata_version"`
	PortfolioTracking *bool  `json:"portfolio_tracking,omitempty"`

	ID      string    `json:"id"`
	Tenant  string    `json:"tenant_id"`
	Name    string    `json:"name"`
	Started time.Time `json:"started_at"`
	Version int64     `json:"version"`
	Created time.Time `json:"created_at"`
}
type storedOperation struct {
	ID         string                `json:"id"`
	Tenant     string                `json:"tenant_id"`
	Ledger     string                `json:"ledger_id"`
	Version    int64                 `json:"version"`
	Sequence   int64                 `json:"sequence"`
	Kind       string                `json:"kind"`
	Status     string                `json:"status"`
	Occurred   time.Time             `json:"occurred_at"`
	Settlement time.Time             `json:"settlement_at"`
	Data       d.InvestmentOperation `json:"data"`
}
type storedLeg struct {
	Ledger     string      `json:"ledger_id"`
	Operation  string      `json:"operation_id"`
	Currency   string      `json:"currency"`
	Bucket     string      `json:"bucket"`
	Component  string      `json:"component"`
	Instrument string      `json:"instrument_key"`
	Amount     json.Number `json:"amount"`
	Quantity   json.Number `json:"quantity"`
	Effective  time.Time   `json:"effective_at"`
}

func legKey(ledger, operation, currency, bucket, component, instrument, amount string, at time.Time) (string, error) {
	value, ok := new(big.Rat).SetString(amount)
	if !ok {
		return "", fmt.Errorf("invalid archived leg amount")
	}
	raw, e := json.Marshal([]string{ledger, operation, currency, bucket, component, instrument, value.RatString(), at.UTC().Format(time.RFC3339Nano)})
	return string(raw), e
}

// VerifyJournal replays current heads and verifies every stored cash/security
// movement, operation revision chain, ledger counters and transfer registry.
// It does not replace a database restore or validate import/configuration audits.
func VerifyJournal(data Dataset) (JournalVerification, error) {
	var out JournalVerification
	if e := Validate(data); e != nil {
		return out, e
	}
	if e := verifyCashDeficitSources(data); e != nil {
		return out, e
	}
	if e := VerifyLotConversions(data); e != nil {
		return out, e
	}
	if e := VerifyJournalAudit(data); e != nil {
		return out, e
	}
	if e := VerifyFXPolicies(data); e != nil {
		return out, e
	}
	if e := VerifyDividendHistories(data); e != nil {
		return out, e
	}
	if e := VerifyForecastEvidence(data); e != nil {
		return out, e
	}
	if e := VerifyLedgerLifecycle(data); e != nil {
		return out, e
	}
	ledgers := map[string]d.InvestmentLedger{}
	operations := map[string][]d.InvestmentOperation{}
	ids := map[string]bool{}
	actual := map[string]int{}
	expected := map[string]int{}
	all := []d.InvestmentOperation{}
	for _, table := range data.Tables {
		for _, raw := range table.Rows {
			switch table.Name {
			case "investment_ledgers":
				var row storedLedger
				if e := json.Unmarshal(raw, &row); e != nil {
					return out, e
				}
				if !id(row.ID) || ledgers[row.ID].ID != "" || row.Tenant != data.TenantID || row.Version < 1 {
					return out, fmt.Errorf("invalid archived ledger")
				}
				ledgers[row.ID] = d.InvestmentLedger{ID: row.ID, TenantID: row.Tenant, Name: row.Name, StartedAt: row.Started, Version: row.Version, CreatedAt: row.Created, Broker: row.Broker, AccountType: row.AccountType, Comment: row.Comment, DefaultCurrency: row.DefaultCurrency, Archived: row.Archived, MetadataVersion: row.MetadataVersion, PortfolioTracking: row.PortfolioTracking != nil && *row.PortfolioTracking}
				out.Ledgers++
			case "investment_operations":
				var row storedOperation
				if e := json.Unmarshal(raw, &row); e != nil {
					return out, e
				}
				op := row.Data
				if !id(row.ID) || ids[row.ID] || row.ID != op.ID || row.Tenant != op.TenantID || row.Tenant != data.TenantID || row.Ledger != op.LedgerID || row.Version != op.Version || row.Sequence != op.Sequence || row.Kind != op.Kind || row.Status != op.Status || !row.Occurred.Equal(op.OccurredAt) || !row.Settlement.Equal(op.SettlementAt) {
					return out, fmt.Errorf("archived operation head mismatch")
				}
				ids[row.ID] = true
				operations[row.Ledger] = append(operations[row.Ledger], op)
				all = append(all, op)
				out.Operations++
			case "investment_cash_legs", "investment_security_legs":
				var row storedLeg
				if e := json.Unmarshal(raw, &row); e != nil {
					return out, e
				}
				amount := row.Amount.String()
				if table.Name == "investment_security_legs" {
					amount = row.Quantity.String()
					out.SecurityLegs++
				} else {
					out.CashLegs++
				}
				key, e := legKey(row.Ledger, row.Operation, row.Currency, row.Bucket, row.Component, row.Instrument, amount, row.Effective)
				if e != nil {
					return out, e
				}
				actual[key]++
			}
		}
	}
	for ledger := range operations {
		if ledgers[ledger].ID == "" {
			return out, fmt.Errorf("orphan archived operations")
		}
	}
	accountList := []d.InvestmentLedger{}
	operationList := []d.InvestmentOperation{}
	for id, ledger := range ledgers {
		accountList = append(accountList, ledger)
		operationList = append(operationList, operations[id]...)
	}
	portfolio := inv.PortfolioReplay{}
	if len(accountList) > 0 {
		var e error
		portfolio, e = inv.ReplayPortfolio(accountList, operationList, data.CreatedAt)
		if e != nil {
			return out, fmt.Errorf("archived journal replay failed")
		}
	}
	keys := []string{}
	for id := range ledgers {
		keys = append(keys, id)
		result := portfolio.Ledgers[id]
		if ledgers[id].Archived {
			if e := inv.CheckLedgerClosure(result.Snapshot, operations[id], data.CreatedAt); e != nil {
				return out, fmt.Errorf("archived ledger is not closed")
			}
		}
		for _, leg := range result.CashLegs {
			key, e := legKey(id, leg.OperationID, leg.Currency, leg.Bucket, leg.Component, "", leg.Amount, leg.EffectiveAt)
			if e != nil {
				return out, e
			}
			expected[key]++
		}
		for _, leg := range result.SecurityLegs {
			key, e := legKey(id, leg.OperationID, "", "", "", leg.InstrumentKey, leg.Quantity, leg.EffectiveAt)
			if e != nil {
				return out, e
			}
			expected[key]++
		}
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		if _, e := calc.ExternalFlows(keys, all); e != nil {
			return out, fmt.Errorf("archived transfer links are inconsistent")
		}
	}
	if len(actual) != len(expected) {
		return out, fmt.Errorf("archived movements do not match replay")
	}
	for key, count := range expected {
		if actual[key] != count {
			return out, fmt.Errorf("archived movements do not match replay")
		}
	}
	return out, nil
}
