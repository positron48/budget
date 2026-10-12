package investment

import (
	"context"
	d "github.com/positron48/budget/internal/domain"
	"math/big"
	"strings"
	"time"
)

type LedgerUpdate struct {
	ID, Action, Name, Broker, AccountType, Comment, DefaultCurrency, RequestKey, Reason string
	ExpectedMetadataVersion, ExpectedLedgerVersion                                      int64
}
type LedgerRevision struct {
	Version                 int64
	Action, ActorID, Reason string
	RecordedAt              time.Time
	Before, After           *d.InvestmentLedger
}
type LedgerHistory struct {
	Ledger           d.InvestmentLedger
	Revisions        []LedgerRevision
	NextAfterVersion int64
}
type LedgerHistoryQuery struct {
	ID                                    string
	AfterVersion, ExpectedMetadataVersion int64
	Limit                                 int
}
type ledgerLifecycleRepo interface {
	UpdateLedger(context.Context, string, string, LedgerUpdate) (d.InvestmentLedger, error)
	ListLedgersWithArchive(context.Context, string, int, int, bool) ([]d.InvestmentLedger, error)
	LedgerHistory(context.Context, string, string, LedgerHistoryQuery) (LedgerHistory, error)
}

func NormalizeLedgerDetails(l d.InvestmentLedger) (d.InvestmentLedger, error) {
	l.Name = strings.TrimSpace(l.Name)
	l.Broker = strings.TrimSpace(l.Broker)
	l.Comment = strings.TrimSpace(l.Comment)
	if l.AccountType == "" {
		l.AccountType = "other"
	}
	if l.DefaultCurrency == "" {
		l.DefaultCurrency = "RUB"
	}
	if len(l.Name) < 1 || len(l.Name) > 200 || len(l.Broker) > 200 || len(l.Comment) > 2000 || (l.AccountType != "regular" && l.AccountType != "iis" && l.AccountType != "other") || (l.DefaultCurrency != "RUB" && l.DefaultCurrency != "USD" && l.DefaultCurrency != "EUR") {
		return l, d.ErrInvestmentInvalid
	}
	return l, nil
}
func (s *Service) CreateLedgerWithDetails(ctx context.Context, t, u string, l d.InvestmentLedger, key string) (d.InvestmentLedger, error) {
	if e := s.authorize(ctx, t, u, true); e != nil {
		return l, e
	}
	var e error
	l, e = NormalizeLedgerDetails(l)
	if e != nil {
		return l, e
	}
	if l.StartedAt.IsZero() || l.StartedAt.Year() < 1900 || l.StartedAt.Year() > 2200 || l.StartedAt.After(time.Now().UTC()) || len(key) < 1 || len(key) > 128 {
		return l, d.ErrInvestmentInvalid
	}
	l.ID = ""
	l.TenantID = t
	l.Archived = false
	l.MetadataVersion = 0
	l.Version = 0
	l.CreatedAt = time.Time{}
	l.StartedAt = l.StartedAt.UTC().Truncate(time.Microsecond)
	return s.repo.CreateLedger(ctx, l, u, key)
}
func (s *Service) ListWithArchive(ctx context.Context, t, u string, limit, offset int, include bool) ([]d.InvestmentLedger, error) {
	if e := s.authorize(ctx, t, u, false); e != nil {
		return nil, e
	}
	if !validPage(limit, offset) {
		return nil, d.ErrInvestmentInvalid
	}
	repo, ok := s.repo.(ledgerLifecycleRepo)
	if !ok {
		return nil, d.ErrInvestmentPrecondition
	}
	return repo.ListLedgersWithArchive(ctx, t, limit, offset, include)
}
func NormalizeLedgerUpdate(m LedgerUpdate) (LedgerUpdate, error) {
	m.Reason = strings.TrimSpace(m.Reason)
	if !validID(m.ID) || m.ExpectedMetadataVersion < 1 || len(m.RequestKey) < 1 || len(m.RequestKey) > 128 || len(m.Reason) < 1 || len(m.Reason) > 2000 {
		return m, d.ErrInvestmentInvalid
	}
	switch m.Action {
	case "edit":
		l, e := NormalizeLedgerDetails(d.InvestmentLedger{Name: m.Name, Broker: m.Broker, AccountType: m.AccountType, Comment: m.Comment, DefaultCurrency: m.DefaultCurrency})
		if e != nil {
			return m, e
		}
		m.Name = l.Name
		m.Broker = l.Broker
		m.AccountType = l.AccountType
		m.Comment = l.Comment
		m.DefaultCurrency = l.DefaultCurrency
	case "archive":
		if m.ExpectedLedgerVersion < 1 {
			return m, d.ErrInvestmentInvalid
		}
	case "restore":
	default:
		return m, d.ErrInvestmentInvalid
	}
	if m.Action != "edit" {
		m.Name = ""
		m.Broker = ""
		m.AccountType = ""
		m.Comment = ""
		m.DefaultCurrency = ""
	}
	return m, nil
}
func (s *Service) UpdateLedger(ctx context.Context, t, u string, m LedgerUpdate) (d.InvestmentLedger, error) {
	if e := s.authorize(ctx, t, u, true); e != nil {
		return d.InvestmentLedger{}, e
	}
	m, e := NormalizeLedgerUpdate(m)
	if e != nil {
		return d.InvestmentLedger{}, e
	}
	repo, ok := s.repo.(ledgerLifecycleRepo)
	if !ok {
		return d.InvestmentLedger{}, d.ErrInvestmentPrecondition
	}
	return repo.UpdateLedger(ctx, t, u, m)
}
func NormalizeLedgerHistory(q LedgerHistoryQuery) (LedgerHistoryQuery, error) {
	if q.Limit == 0 {
		q.Limit = 50
	}
	if !validID(q.ID) || q.AfterVersion < 0 || q.ExpectedMetadataVersion < 0 || q.AfterVersion > 0 && q.ExpectedMetadataVersion == 0 || q.Limit < 1 || q.Limit > 200 {
		return q, d.ErrInvestmentInvalid
	}
	return q, nil
}
func (s *Service) LedgerHistory(ctx context.Context, t, u string, q LedgerHistoryQuery) (LedgerHistory, error) {
	if e := s.authorize(ctx, t, u, false); e != nil {
		return LedgerHistory{}, e
	}
	q, e := NormalizeLedgerHistory(q)
	if e != nil {
		return LedgerHistory{}, e
	}
	repo, ok := s.repo.(ledgerLifecycleRepo)
	if !ok {
		return LedgerHistory{}, d.ErrInvestmentPrecondition
	}
	return repo.LedgerHistory(ctx, t, u, q)
}

// CheckLedgerClosure requires every native-currency bucket and position to be zero,
// and rejects future facts even if their eventual economic effect would cancel.
func CheckLedgerClosure(s d.InvestmentSnapshot, ops []d.InvestmentOperation, at time.Time) error {
	zero := func(v string) bool { r, ok := new(big.Rat).SetString(v); return ok && r.Sign() == 0 }
	for _, c := range s.Cash {
		if !zero(c.Settled) || !zero(c.Receivable) || !zero(c.Payable) || !zero(c.Economic) {
			return d.ErrInvestmentPrecondition
		}
	}
	for _, p := range s.Positions {
		if !zero(p.Quantity) {
			return d.ErrInvestmentPrecondition
		}
	}
	for _, o := range ops {
		if o.Status == "posted" && (o.OccurredAt.After(at) || o.SettlementAt.After(at)) {
			return d.ErrInvestmentPrecondition
		}
	}
	return nil
}
