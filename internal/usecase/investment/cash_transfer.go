package investment

import (
	"context"
	d "github.com/positron48/budget/internal/domain"
	"strings"
	"time"
)

// The Budget document version is also the investment operation version.
type CashTransfer struct {
	Timing                        *d.InvestmentOperationTiming `json:",omitempty"`
	Transfer                      d.AssetTransfer
	LedgerID, OperationID, Status string
}
type CashTransferMutation struct {
	Timing                                                                                       *d.InvestmentOperationTiming `json:",omitempty"`
	PreviewHash                                                                                  string                       `json:",omitempty"`
	Action, RequestKey, Reason                                                                   string
	Transfer                                                                                     d.AssetTransfer
	ExpectedVersion, ExpectedLedgerVersion, ExpectedFromAccountVersion, ExpectedToAccountVersion int64
}
type cashTransferRepo interface {
	MutateCashTransfer(context.Context, string, string, CashTransferMutation) (CashTransfer, error)
	GetCashTransfer(context.Context, string, string) (CashTransfer, error)
}

func (s *Service) CashTransfer(ctx context.Context, t, u string, m CashTransferMutation) (CashTransfer, error) {
	var zero CashTransfer
	if e := s.authorize(ctx, t, u, false); e != nil {
		return zero, e
	}
	m, e := NormalizeCashTransferMutation(t, u, m, time.Now().UTC())
	if e != nil {
		return zero, e
	}
	repo, ok := s.repo.(cashTransferRepo)
	if !ok {
		return zero, d.ErrInvestmentPrecondition
	}
	return repo.MutateCashTransfer(ctx, t, u, m)
}
func (s *Service) ReadCashTransfer(ctx context.Context, t, u, id string) (CashTransfer, error) {
	if e := s.authorize(ctx, t, u, false); e != nil {
		return CashTransfer{}, e
	}
	if !validID(id) {
		return CashTransfer{}, d.ErrInvestmentInvalid
	}
	repo, ok := s.repo.(cashTransferRepo)
	if !ok {
		return CashTransfer{}, d.ErrInvestmentPrecondition
	}
	return repo.GetCashTransfer(ctx, t, id)
}

func NormalizeCashTransferMutation(t, u string, m CashTransferMutation, now time.Time) (CashTransferMutation, error) {
	a := &m.Transfer
	if !validID(a.FromAccountID) || !validID(a.ToAccountID) || a.FromAccountID == a.ToAccountID || m.ExpectedLedgerVersion < 1 || m.ExpectedFromAccountVersion < 1 || m.ExpectedToAccountVersion < 1 || len(m.RequestKey) < 1 || len(m.RequestKey) > 128 {
		return CashTransferMutation{}, d.ErrInvestmentInvalid
	}
	if m.Action != "create" && m.Action != "amend" && m.Action != "void" {
		return CashTransferMutation{}, d.ErrInvestmentInvalid
	}
	if m.Action != "create" && (!validID(a.ID) || m.ExpectedVersion < 1 || len(strings.TrimSpace(m.Reason)) == 0 || len(m.Reason) > 2000) {
		return CashTransferMutation{}, d.ErrInvestmentInvalid
	}
	if m.Action != "void" {
		o := d.InvestmentOperation{Kind: "deposit", OccurredAt: a.OccurredAt, Timing: m.Timing}
		if m.Timing != nil && m.Timing.SettlementDate != "" {
			return CashTransferMutation{}, invalid("cash transfer has no separate settlement date")
		}
		if err := normalizeOperationTiming(&o); err != nil {
			return CashTransferMutation{}, err
		}
		a.OccurredAt = o.OccurredAt
		m.Timing = o.Timing
		if a.OccurredAt.IsZero() || a.OccurredAt.Year() < 1900 || operationIsFuture(o, now) || len(a.Note) > 5000 {
			return CashTransferMutation{}, d.ErrInvestmentInvalid
		}
		for _, v := range []d.Money{a.FromAmount, a.ToAmount} {
			if v.MinorUnits <= 0 || (v.CurrencyCode != "RUB" && v.CurrencyCode != "USD" && v.CurrencyCode != "EUR") {
				return CashTransferMutation{}, d.ErrInvestmentInvalid
			}
		}
		if a.FromAmount.CurrencyCode == a.ToAmount.CurrencyCode && a.FromAmount.MinorUnits != a.ToAmount.MinorUnits {
			return CashTransferMutation{}, d.ErrInvestmentInvalid
		}
	}
	a.TenantID = t
	a.UserID = u
	a.RequestKey = ""
	a.Version = 0
	a.CreatedAt = time.Time{}
	a.OccurredAt = a.OccurredAt.UTC().Truncate(time.Microsecond)
	if m.Action == "create" {
		a.ID = ""
		m.ExpectedVersion = 0
		m.Reason = ""
	}
	return m, nil
}
