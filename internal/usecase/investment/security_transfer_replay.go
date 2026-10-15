package investment

import (
	"crypto/sha256"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"time"

	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
)

type lotOrigin struct {
	ID, LedgerID string
	Sequence     int64
}
type replayOptions struct {
	origins   map[string]bool
	order     map[string]int
	sequences map[string]int64
	receipt   func(string) (SecurityTransferReceipt, error)
	stop      string
}

// SecurityTransferReceipt is derived from the source FIFO prefix, never from
// client-supplied book values. Lots retain parent slice IDs and original dates.
type SecurityTransferReceipt struct {
	TransferID, FromLedgerID, ToLedgerID, InstrumentKey, Currency, Quantity, Nominal string
	At                                                                               time.Time
	Lots                                                                             []RemainingLot
}

func extractSecurityTransfer(p *position, o d.InvestmentOperation, ledger d.InvestmentLedger, sequences map[string]int64) (SecurityTransferReceipt, error) {
	receipt := SecurityTransferReceipt{TransferID: o.TransferID, FromLedgerID: o.LedgerID, ToLedgerID: o.CounterpartyLedgerID, InstrumentKey: o.InstrumentKey, Currency: o.Currency, Quantity: o.Quantity, At: o.OccurredAt}
	if p == nil || p.currency != o.Currency {
		return receipt, inconsistent("source security position and cost currency required")
	}
	clean, interest := p.clean.Clone(), p.accrued.Clone()
	costs, e := clean.ExtractFIFO(o.Quantity)
	if e != nil {
		return receipt, inconsistent(e.Error())
	}
	accrued, e := interest.ExtractFIFO(o.Quantity)
	if e != nil {
		return receipt, inconsistent(e.Error())
	}
	if len(costs) != len(accrued) {
		return receipt, inconsistent("transfer cost books disagree")
	}
	receipt.Nominal = p.nominal
	for i, lot := range costs {
		if lot.OriginID != accrued[i].OriginID || lot.Quantity.Cmp(accrued[i].Quantity) != 0 {
			return receipt, inconsistent("transfer cost books disagree")
		}
		origin := p.lotOrigins[lot.OriginID]
		if origin.ID == "" {
			origin = lotOrigin{ID: lot.OriginID, LedgerID: ledger.ID, Sequence: sequences[lot.OriginID]}
		}
		v := RemainingLot{LotID: lot.OriginID, OriginID: origin.ID, OriginLedgerID: origin.LedgerID, OriginSequence: origin.Sequence, AcquiredAt: p.lotDates[lot.OriginID], Quantity: lot.Quantity, CleanCost: lot.Cost, AcquiredAccrued: accrued[i].Cost}
		if p.cleanUnknown[lot.OriginID] {
			v.CleanCost = nil
		}
		if p.accruedUnknown[lot.OriginID] {
			v.AcquiredAccrued = nil
		}
		receipt.Lots = append(receipt.Lots, v)
	}
	p.clean, p.accrued = *clean, *interest
	return receipt, nil
}
func securitySliceID(transfer, parent string) string {
	return fmt.Sprintf("lot:%x", sha256.Sum256([]byte(fmt.Sprintf("%d:%s%s", len(transfer), transfer, parent))))
}
func receiveSecurityTransfer(p *position, o d.InvestmentOperation, r SecurityTransferReceipt, localOrder, globalOrder map[string]int) (*position, error) {
	if r.TransferID != o.TransferID || r.FromLedgerID != o.CounterpartyLedgerID || r.ToLedgerID != o.LedgerID || r.InstrumentKey != o.InstrumentKey || r.Currency != o.Currency || r.Quantity != o.Quantity || !r.At.Equal(o.OccurredAt) {
		return p, inconsistent("security transfer receipt mismatch")
	}
	if p == nil {
		p = &position{cleanUnknown: map[string]bool{}, accruedUnknown: map[string]bool{}, lotDates: map[string]time.Time{}, lotOrigins: map[string]lotOrigin{}, currency: o.Currency, realized: new(big.Rat), realizedAccrued: new(big.Rat)}
	}
	if p.nominal == "0" {
		return p, inconsistent("cannot transfer into a redeemed instrument")
	}
	if p.currency != o.Currency {
		return p, inconsistent("destination cost currency mismatch")
	}
	q, _ := p.clean.Totals()
	if q.Sign() > 0 && p.nominal != r.Nominal {
		return p, inconsistent("security transfer nominal history mismatch")
	}
	clean, interest := p.clean.Clone(), p.accrued.Clone()
	incomingClean, incomingInterest := []calc.Lot{}, []calc.Lot{}
	dates := map[string]time.Time{}
	ranks := map[string]int{}
	order := []string{}
	for _, lot := range p.clean.Lots() {
		order = append(order, lot.OriginID)
		dates[lot.OriginID] = p.lotDates[lot.OriginID]
		ranks[lot.OriginID] = localOrder[lot.OriginID]
	}
	total := new(big.Rat)
	for _, lot := range r.Lots {
		if lot.LotID == "" || lot.OriginID == "" || lot.OriginLedgerID == "" || lot.OriginSequence < 1 || lot.Quantity == nil || lot.Quantity.Sign() <= 0 || lot.AcquiredAt.After(o.OccurredAt) {
			return p, inconsistent("invalid transfer lot identity or date")
		}
		id := securitySliceID(o.TransferID, lot.LotID)
		cost, accrued := new(big.Rat), new(big.Rat)
		if lot.CleanCost != nil {
			cost.Set(lot.CleanCost)
		}
		if lot.AcquiredAccrued != nil {
			accrued.Set(lot.AcquiredAccrued)
		}
		incomingClean = append(incomingClean, calc.Lot{OriginID: id, Quantity: lot.Quantity, Cost: cost})
		incomingInterest = append(incomingInterest, calc.Lot{OriginID: id, Quantity: lot.Quantity, Cost: accrued})
		total.Add(total, lot.Quantity)
		order = append(order, id)
		dates[id] = lot.AcquiredAt
		ranks[id] = globalOrder[lot.OriginID]
	}
	if total.Cmp(rational(o.Quantity)) != 0 {
		return p, inconsistent("transfer lot quantity mismatch")
	}
	// Equal original acquisitions keep the existing destination slice before a
	// returned part. Distinct origins use the same stable global acquisition order.
	slices.SortStableFunc(order, func(a, b string) int {
		if c := dates[a].Compare(dates[b]); c != 0 {
			return c
		}
		if ranks[a] < ranks[b] {
			return -1
		}
		if ranks[a] > ranks[b] {
			return 1
		}
		return 0
	})
	if e := clean.ReceiveSlices(incomingClean, order); e != nil {
		return p, inconsistent(e.Error())
	}
	if e := interest.ReceiveSlices(incomingInterest, order); e != nil {
		return p, inconsistent(e.Error())
	}
	if p.lotOrigins == nil {
		p.lotOrigins = map[string]lotOrigin{}
	}
	for _, lot := range r.Lots {
		id := securitySliceID(o.TransferID, lot.LotID)
		p.lotOrigins[id] = lotOrigin{lot.OriginID, lot.OriginLedgerID, lot.OriginSequence}
		p.lotDates[id] = lot.AcquiredAt
		p.cleanUnknown[id] = lot.CleanCost == nil
		p.accruedUnknown[id] = lot.AcquiredAccrued == nil
		localOrder[id] = ranks[id]
		if lot.AcquiredAt.IsZero() {
			p.unknownAcquisitionDate = true
		}
	}
	p.clean, p.accrued = *clean, *interest
	if q.Sign() == 0 {
		p.nominal = r.Nominal
	}
	return p, nil
}

type PortfolioReplay struct {
	Ledgers   map[string]ReplayResult
	Transfers map[string]SecurityTransferReceipt
}

// ReplayPortfolio derives transfer receipts lazily from source prefixes. Local
// sequence order determines same-timestamp dependencies; a circular dependency
// is rejected rather than resolved by account/map order. Final account replays
// validate complete histories, including future settlements and linked legs.
func ReplayPortfolio(ledgers []d.InvestmentLedger, operations []d.InvestmentOperation, asOf time.Time) (PortfolioReplay, error) {
	return replayPortfolio(ledgers, operations, asOf, nil)
}

// ReplayPortfolioLotHistory follows selected original acquisitions across all
// accounts in the connected scope, retaining distinct before/after slice IDs.
func ReplayPortfolioLotHistory(ledgers []d.InvestmentLedger, operations []d.InvestmentOperation, asOf time.Time, originIDs []string) (PortfolioReplay, error) {
	if len(originIDs) > 50 {
		return PortfolioReplay{}, d.ErrInvestmentLimit
	}
	origins := map[string]bool{}
	for _, id := range originIDs {
		if id == "" {
			return PortfolioReplay{}, d.ErrInvestmentInvalid
		}
		origins[id] = true
	}
	return replayPortfolio(ledgers, operations, asOf, origins)
}
func replayPortfolio(ledgers []d.InvestmentLedger, operations []d.InvestmentOperation, asOf time.Time, origins map[string]bool) (PortfolioReplay, error) {
	out := PortfolioReplay{Ledgers: map[string]ReplayResult{}, Transfers: map[string]SecurityTransferReceipt{}}
	if asOf.IsZero() || len(ledgers) == 0 {
		return out, invalid("portfolio ledgers and cutoff required")
	}
	if len(ledgers) > 100 || len(operations) > 100000 {
		return out, d.ErrInvestmentLimit
	}
	accounts := map[string]d.InvestmentLedger{}
	inputs := map[string][]d.InvestmentOperation{}
	tenant := ""
	for _, l := range ledgers {
		if l.ID == "" || l.TenantID == "" || accounts[l.ID].ID != "" || l.StartedAt.IsZero() {
			return out, invalid("unique scoped ledger required")
		}
		if tenant != "" && tenant != l.TenantID {
			return out, invalid("portfolio tenant mismatch")
		}
		tenant = l.TenantID
		accounts[l.ID] = l
	}
	transfers := map[string][]d.InvestmentOperation{}
	seen := map[string]bool{}
	sequences := map[string]int64{}
	ordered := slices.Clone(operations)
	for i := range ordered {
		op := &ordered[i]
		if op.Status == "posted" || op.Kind == "security_transfer_in" || op.Kind == "security_transfer_out" {
			if e := ValidateOperation(op); e != nil {
				return out, e
			}
		}
	}
	slices.SortFunc(ordered, func(a, b d.InvestmentOperation) int {
		if c := a.OccurredAt.Compare(b.OccurredAt); c != 0 {
			return c
		}
		if c := strings.Compare(a.LedgerID, b.LedgerID); c != 0 {
			return c
		}
		if aPriority, bPriority := operationTimingPriority(a), operationTimingPriority(b); aPriority != bPriority {
			if aPriority < bPriority {
				return -1
			}
			return 1
		}
		if a.Sequence < b.Sequence {
			return -1
		}
		if a.Sequence > b.Sequence {
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})
	acquisitionOrder := map[string]int{}
	for i, op := range ordered {
		if accounts[op.LedgerID].ID == "" || op.TenantID != tenant || op.ID == "" || seen[op.ID] {
			return out, invalid("unique scoped portfolio operation required")
		}
		seen[op.ID] = true
		sequences[op.ID] = op.Sequence
		acquisitionOrder[op.ID] = i
		inputs[op.LedgerID] = append(inputs[op.LedgerID], op)
		if op.Kind == "security_transfer_in" || op.Kind == "security_transfer_out" {
			if op.Status != "posted" && op.Status != "voided" {
				return out, invalid("transfer status required")
			}
			transfers[op.TransferID] = append(transfers[op.TransferID], op)
		}
	}
	sources := map[string]d.InvestmentOperation{}
	if len(transfers) > 2000 {
		return out, d.ErrInvestmentLimit
	}
	for id, pair := range transfers {
		if len(pair) != 2 {
			return out, inconsistent("security transfer requires both legs")
		}
		a, b := pair[0], pair[1]
		if a.Kind == "security_transfer_in" {
			a, b = b, a
		}
		aq, bq := rational(a.Quantity), rational(b.Quantity)
		if a.Kind != "security_transfer_out" || b.Kind != "security_transfer_in" || a.LedgerID != b.CounterpartyLedgerID || a.CounterpartyLedgerID != b.LedgerID || a.InstrumentKey != b.InstrumentKey || a.Currency != b.Currency || aq.Cmp(bq) != 0 || !a.OccurredAt.Equal(b.OccurredAt) || a.Status != b.Status {
			return out, inconsistent("security transfer legs disagree")
		}
		if a.Status == "posted" {
			sources[id] = a
		}
	}
	visiting := map[string]bool{}
	work := 0
	var resolve func(string) (SecurityTransferReceipt, error)
	resolve = func(id string) (SecurityTransferReceipt, error) {
		if r, ok := out.Transfers[id]; ok {
			return r, nil
		}
		source, ok := sources[id]
		if !ok {
			return SecurityTransferReceipt{}, inconsistent("active security transfer source required")
		}
		if visiting[id] {
			return SecurityTransferReceipt{}, inconsistent("circular same-time security transfers")
		}
		visiting[id] = true
		defer delete(visiting, id)
		work += len(inputs[source.LedgerID])
		if work > 10000000 {
			return SecurityTransferReceipt{}, d.ErrInvestmentLimit
		}
		result, e := replay(accounts[source.LedgerID], inputs[source.LedgerID], source.OccurredAt, replayOptions{receipt: resolve, stop: source.ID, order: acquisitionOrder, sequences: sequences})
		if e != nil {
			return SecurityTransferReceipt{}, e
		}
		r, ok := result.SecurityTransfers[id]
		if !ok {
			return SecurityTransferReceipt{}, inconsistent("security transfer source not replayed")
		}
		out.Transfers[id] = r
		return r, nil
	}
	// Deterministic traversal also makes the first rejected account repeatable.
	ids := make([]string, 0, len(accounts))
	for id := range accounts {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	traceCount := 0
	for _, id := range ids {
		l := accounts[id]
		startsFromZero := RecordedTrackerStartsFromZero(l, inputs[id])
		at := asOf
		if !startsFromZero && at.Before(l.StartedAt) {
			at = l.StartedAt
		}
		result, e := replay(l, inputs[id], at, replayOptions{receipt: resolve, order: acquisitionOrder, sequences: sequences, origins: origins})
		if e != nil {
			return out, e
		}
		// Out-only sources still need receipts in the portfolio result.
		for transfer := range result.SecurityTransfers {
			if _, e = resolve(transfer); e != nil {
				return out, e
			}
		}
		if !startsFromZero && asOf.Before(l.StartedAt) {
			result.LotHistory = nil
			result.ExactLots = map[string][]RemainingLot{}
			result.ExactPositions = map[string]PositionBookValues{}
			result.ProfitEvents = nil
			result.Snapshot = d.InvestmentSnapshot{Ledger: l, AsOf: asOf, Cash: []d.InvestmentCash{}, Positions: []d.InvestmentPosition{}, ValuationStatus: "market_data_required"}
		}
		traceCount += len(result.LotHistory)
		if traceCount > 10000 {
			return out, d.ErrInvestmentLimit
		}
		out.Ledgers[id] = result
	}
	return out, nil
}
