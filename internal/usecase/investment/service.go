package investment

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
)

type Mutation struct {
	Action, RequestKey, Reason                      string
	PreviewHash                                     string `json:",omitempty"`
	Operation                                       d.InvestmentOperation
	ExpectedLedgerVersion, ExpectedOperationVersion int64
	Source                                          *d.InvestmentSource
}
type Repo interface {
	CancelBatch(context.Context, string, string, BatchCancellation, string, bool) (BatchCancellationResult, error)
	SaveBatchDraft(context.Context, string, string, BatchDraftWrite) (BatchDraft, error)
	GetBatchDraft(context.Context, string, string, string) (BatchDraft, error)
	ListBatchDrafts(context.Context, string, string, string) ([]BatchDraftSummary, error)
	RunBatchDraft(context.Context, string, string, string, int64, string, bool) (BatchDraft, error)
	DeleteBatchDraft(context.Context, string, string, string, int64) error
	ImportBatch(context.Context, string, string, ImportBatch, string, bool) (ImportBatchResult, error)
	CreateLedger(context.Context, d.InvestmentLedger, string, string) (d.InvestmentLedger, error)
	ListLedgers(context.Context, string, int, int) ([]d.InvestmentLedger, error)
	Snapshot(context.Context, string, string, time.Time) (d.InvestmentSnapshot, error)
	ListOperations(context.Context, string, string, int, int) ([]d.InvestmentOperation, error)
	MutateTransfer(context.Context, string, string, TransferMutation) (d.InvestmentTransfer, error)
	GetTransfer(context.Context, string, string) (d.InvestmentTransfer, error)
	Mutate(context.Context, string, string, Mutation) (d.InvestmentOperation, error)
}
type Roles interface {
	GetUserRole(context.Context, string, string) (d.TenantRole, error)
}
type Service struct {
	repo  Repo
	roles Roles
}

func NewService(repo Repo, roles Roles) *Service { return &Service{repo, roles} }
func (s *Service) authorize(ctx context.Context, t, u string, admin bool) error {
	if _, err := uuid.Parse(t); err != nil {
		return d.ErrInvestmentForbidden
	}
	if _, err := uuid.Parse(u); err != nil {
		return d.ErrInvestmentForbidden
	}
	r, err := s.roles.GetUserRole(ctx, t, u)
	if err != nil {
		return err
	}
	if r != d.TenantRoleOwner && r != d.TenantRoleAdmin && (admin || r != d.TenantRoleMember) {
		return d.ErrInvestmentForbidden
	}
	return nil
}
func validID(v string) bool { _, err := uuid.Parse(v); return err == nil }
func validPage(limit, offset int) bool {
	return limit > 0 && limit <= 200 && offset >= 0 && offset <= MaxOperations
}
func (s *Service) CreateLedger(ctx context.Context, t, u, name string, started time.Time, key string) (d.InvestmentLedger, error) {
	return s.CreateLedgerWithDetails(ctx, t, u, d.InvestmentLedger{Name: name, StartedAt: started}, key)
}
func (s *Service) List(ctx context.Context, t, u string, limit, offset int) ([]d.InvestmentLedger, error) {
	if err := s.authorize(ctx, t, u, false); err != nil {
		return nil, err
	}
	if !validPage(limit, offset) {
		return nil, d.ErrInvestmentInvalid
	}
	return s.repo.ListLedgers(ctx, t, limit, offset)
}
func (s *Service) Snapshot(ctx context.Context, t, u, id string, at time.Time) (d.InvestmentSnapshot, error) {
	if err := s.authorize(ctx, t, u, false); err != nil {
		return d.InvestmentSnapshot{}, err
	}
	if !validID(id) || at.IsZero() {
		return d.InvestmentSnapshot{}, d.ErrInvestmentInvalid
	}
	return s.repo.Snapshot(ctx, t, id, at)
}
func (s *Service) Operations(ctx context.Context, t, u, id string, limit, offset int) ([]d.InvestmentOperation, error) {
	if err := s.authorize(ctx, t, u, false); err != nil {
		return nil, err
	}
	if !validID(id) || !validPage(limit, offset) {
		return nil, d.ErrInvestmentInvalid
	}
	return s.repo.ListOperations(ctx, t, id, limit, offset)
}
func (s *Service) Mutate(ctx context.Context, t, u string, m Mutation) (d.InvestmentOperation, error) {
	if e := s.authorize(ctx, t, u, m.Source != nil); e != nil {
		return d.InvestmentOperation{}, e
	}
	m, e := NormalizeMutation(t, u, m, time.Now().UTC())
	if e != nil {
		return d.InvestmentOperation{}, e
	}
	return s.repo.Mutate(ctx, t, u, m)
}
func NormalizeMutation(t, u string, m Mutation, now time.Time) (Mutation, error) {
	if m.Source != nil && (m.Action != "create" || !validID(m.Source.ImportID) || m.Source.Record < 1 || m.Source.Record > 100000 || m.Source.ExpectedVersion < 1 || len(strings.TrimSpace(m.Source.ReviewReason)) == 0 || len(m.Source.ReviewReason) > 2000) {
		return Mutation{}, d.ErrInvestmentInvalid
	}
	if m.Operation.TransferID != "" || m.Operation.CounterpartyLedgerID != "" || m.Operation.Kind == "transfer_out" || m.Operation.Kind == "transfer_in" || m.Operation.Kind == "security_transfer_in" || m.Operation.Kind == "security_transfer_out" {
		return Mutation{}, invalid("use the linked transfer API")
	}
	if m.Operation.BudgetTransferID != "" {
		return Mutation{}, d.ErrInvestmentPrecondition
	}
	if m.Action == "create" && strings.TrimSpace(m.Operation.ImportedCashDeficitReason) != "" && m.Source == nil {
		return Mutation{}, invalid("cash deficit exception requires a verified import source")
	}
	m.Operation.Source = nil // only the repository can attach verified provenance
	if !validID(m.Operation.LedgerID) || len(m.RequestKey) < 1 || len(m.RequestKey) > 128 || m.ExpectedLedgerVersion < 1 {
		return Mutation{}, d.ErrInvestmentInvalid
	}
	if m.Action != "create" && m.Action != "amend" && m.Action != "void" {
		return Mutation{}, d.ErrInvestmentInvalid
	}
	if m.Action != "create" && (!validID(m.Operation.ID) || m.ExpectedOperationVersion < 1 || len(strings.TrimSpace(m.Reason)) == 0 || len(m.Reason) > 2000) {
		return Mutation{}, d.ErrInvestmentInvalid
	}
	if m.Action == "create" {
		m.Operation.ID = ""
		m.ExpectedOperationVersion = 0
		m.Reason = ""
	}
	if m.Action != "void" {
		if err := ValidateOperation(&m.Operation); err != nil {
			return Mutation{}, err
		}
		if operationIsFuture(m.Operation, now) {
			return Mutation{}, invalid("confirmed operation cannot be future-dated")
		}
	}
	m.Operation.TenantID = t
	m.Operation.ActorID = u
	// Client cannot assign versions, status, order or server timestamps.
	m.Operation.Version = 0
	m.Operation.Sequence = 0
	m.Operation.Status = ""
	m.Operation.CreatedAt = time.Time{}
	if m.Action == "void" {
		m.Operation = d.InvestmentOperation{ID: m.Operation.ID, LedgerID: m.Operation.LedgerID, TenantID: t, ActorID: u}
	}

	return m, nil
}

type TransferMutation struct {
	Action, RequestKey, Reason                              string
	PreviewHash                                             string `json:",omitempty"`
	Transfer                                                d.InvestmentTransfer
	ExpectedVersion, ExpectedFromVersion, ExpectedToVersion int64
}

func (s *Service) Transfer(ctx context.Context, t, u string, m TransferMutation) (d.InvestmentTransfer, error) {
	if err := s.authorize(ctx, t, u, false); err != nil {
		return d.InvestmentTransfer{}, err
	}
	m, err := NormalizeTransferMutation(t, u, m, time.Now().UTC())
	if err != nil {
		return d.InvestmentTransfer{}, err
	}
	return s.repo.MutateTransfer(ctx, t, u, m)
}
func (s *Service) GetTransfer(ctx context.Context, t, u, id string) (d.InvestmentTransfer, error) {
	if err := s.authorize(ctx, t, u, false); err != nil {
		return d.InvestmentTransfer{}, err
	}
	if !validID(id) {
		return d.InvestmentTransfer{}, d.ErrInvestmentInvalid
	}
	return s.repo.GetTransfer(ctx, t, id)
}

func NormalizeTransferMutation(t, u string, m TransferMutation, now time.Time) (TransferMutation, error) {
	if !validID(m.Transfer.FromLedgerID) || !validID(m.Transfer.ToLedgerID) || m.Transfer.FromLedgerID == m.Transfer.ToLedgerID || m.ExpectedFromVersion < 1 || m.ExpectedToVersion < 1 || len(m.RequestKey) < 1 || len(m.RequestKey) > 128 {
		return TransferMutation{}, d.ErrInvestmentInvalid
	}
	if m.Action != "create" && m.Action != "amend" && m.Action != "void" {
		return TransferMutation{}, d.ErrInvestmentInvalid
	}
	if m.Action != "create" && (!validID(m.Transfer.ID) || m.ExpectedVersion < 1 || len(strings.TrimSpace(m.Reason)) == 0 || len(m.Reason) > 2000) {
		return TransferMutation{}, d.ErrInvestmentInvalid
	}
	if m.Action == "create" {
		m.Transfer.ID = ""
		m.ExpectedVersion = 0
		m.Reason = ""
	}
	if m.Action != "void" {
		o := d.InvestmentOperation{Kind: "transfer_out", InstrumentKey: m.Transfer.InstrumentKey, Quantity: m.Transfer.Quantity, TransferID: "validation", LedgerID: m.Transfer.FromLedgerID, CounterpartyLedgerID: m.Transfer.ToLedgerID, Currency: m.Transfer.Currency, Amount: m.Transfer.Amount, Timing: m.Transfer.Timing, Note: m.Transfer.Note, OccurredAt: m.Transfer.OccurredAt}
		if m.Transfer.InstrumentKey != "" || m.Transfer.Quantity != "" && m.Transfer.Quantity != "0" {
			o.Kind = "security_transfer_out"
		}
		if m.Transfer.Timing != nil && m.Transfer.Timing.SettlementDate != "" {
			return TransferMutation{}, invalid("transfer has no separate settlement date")
		}
		if err := ValidateOperation(&o); err != nil {
			return TransferMutation{}, err
		}
		m.Transfer.InstrumentKey = o.InstrumentKey
		m.Transfer.Quantity = o.Quantity
		if o.Kind == "transfer_out" {
			m.Transfer.Quantity = ""
		}
		m.Transfer.Currency = o.Currency
		m.Transfer.Amount = o.Amount
		if operationIsFuture(o, now) {
			return TransferMutation{}, invalid("confirmed transfer cannot be future-dated")
		}
		m.Transfer.Timing = o.Timing
		m.Transfer.OccurredAt = o.OccurredAt
	} else {
		m.Transfer = d.InvestmentTransfer{ID: m.Transfer.ID, FromLedgerID: m.Transfer.FromLedgerID, ToLedgerID: m.Transfer.ToLedgerID}
	}
	m.Transfer.TenantID = t
	m.Transfer.Version = 0
	m.Transfer.Status = ""
	m.Transfer.FromOperationID = ""
	m.Transfer.ToOperationID = ""
	return m, nil
}
