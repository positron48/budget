package investmentimport

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/positron48/budget/internal/domain"
)

var ErrInvalid = errors.New("invalid investment import")
var ErrConflict = errors.New("investment import version or request conflict")
var ErrForbidden = errors.New("investment import requires owner or admin")
var ErrNotFound = errors.New("investment import not found")

type ImportAccount struct {
	ID, SourceKey, Name string
	Rows                int
}
type Assignment struct {
	Record               int
	AccountKey, Evidence string
}
type Upload struct {
	Filename, RequestKey, MappingSourceSHA256 string
	CSV                                       []byte
	Accounts                                  []ImportAccount
	Assignments                               []Assignment
}
type Import struct {
	ID, Filename, SourceSHA256, State, PreviewHash string
	Rows                                           int
	Version                                        int64
	CreatedAt                                      time.Time
	Accounts                                       []ImportAccount
}
type StoredRow struct {
	LedgerID, OperationID, OperationStatus  string
	Record                                  int
	SourceRowID, AccountID, MappingEvidence string
	Source                                  SourceRow
	Preview                                 PreviewRow
	Version                                 int64
}
type Prepared struct {
	Input       Upload
	Source      StagedSource
	Preview     Preview
	PayloadHash string
}
type ImportRepo interface {
	PreviewFinancialImport(context.Context, string, string, FinancialReviewRequest) (FinancialReview, error)
	SaveImport(context.Context, string, string, Prepared) (Import, error)
	ListImports(context.Context, string, int, int) ([]Import, error)
	GetImport(context.Context, string, string) (Import, error)
	ListImportRows(context.Context, string, string, int, int) ([]StoredRow, error)
	ReassignImportRow(context.Context, string, string, string, int, string, int64, string) (StoredRow, error)
}
type ImportRoles interface {
	GetUserRole(context.Context, string, string) (domain.TenantRole, error)
}
type ImportService struct {
	repo  ImportRepo
	roles ImportRoles
}

func NewImportService(repo ImportRepo, roles ImportRoles) *ImportService {
	return &ImportService{repo, roles}
}
func (s *ImportService) authorize(ctx context.Context, tenant, user string, write bool) error {
	if _, err := uuid.Parse(tenant); err != nil {
		return ErrForbidden
	}
	if _, err := uuid.Parse(user); err != nil {
		return ErrForbidden
	}
	role, err := s.roles.GetUserRole(ctx, tenant, user)
	if err != nil {
		return err
	}
	if role != domain.TenantRoleOwner && role != domain.TenantRoleAdmin && (write || role != domain.TenantRoleMember) {
		return ErrForbidden
	}
	return nil
}
func PrepareUpload(in Upload) (Prepared, error) {
	in.Accounts = slices.Clone(in.Accounts)
	in.Assignments = slices.Clone(in.Assignments)
	in.Filename = filepath.Base(strings.TrimSpace(in.Filename))
	if in.Filename == "." || in.Filename == "" || len(in.Filename) > 255 || len(in.RequestKey) < 1 || len(in.RequestKey) > 128 || len(in.Accounts) < 1 || len(in.Accounts) > 100 {
		return Prepared{}, ErrInvalid
	}
	source, err := StageSnowball(bytes.NewReader(in.CSV), true)
	if err != nil {
		return Prepared{}, fmt.Errorf("%w: %s", ErrInvalid, err)
	}
	if in.MappingSourceSHA256 != "" && in.MappingSourceSHA256 != source.Audit.SHA256 {
		return Prepared{}, fmt.Errorf("%w: mapping source checksum mismatch", ErrInvalid)
	}
	if len(in.Assignments) == 0 && len(in.Accounts) == 1 {
		for _, row := range source.Rows {
			in.Assignments = append(in.Assignments, Assignment{Record: row.Record, AccountKey: in.Accounts[0].SourceKey, Evidence: "unassigned"})
		}
	}
	preview, err := PreviewSnowball(bytes.NewReader(in.CSV), true)
	if err != nil {
		return Prepared{}, ErrInvalid
	}
	keys := map[string]bool{}
	for i := range in.Accounts {
		a := &in.Accounts[i]
		a.ID = ""
		a.Rows = 0
		a.Name = strings.TrimSpace(a.Name)
		if len(a.SourceKey) == 0 || len(a.SourceKey) > 200 || keys[a.SourceKey] || len(a.Name) == 0 || len(a.Name) > 200 {
			return Prepared{}, ErrInvalid
		}
		keys[a.SourceKey] = true
	}
	if len(in.Assignments) != len(source.Rows) {
		return Prepared{}, fmt.Errorf("%w: assign every source row exactly once", ErrInvalid)
	}
	seen := map[int]bool{}
	for _, a := range in.Assignments {
		if a.Record < 1 || a.Record > len(source.Rows) || seen[a.Record] || !keys[a.AccountKey] || len(a.Evidence) > 2000 {
			return Prepared{}, ErrInvalid
		}
		seen[a.Record] = true
	}
	slices.SortFunc(in.Accounts, func(a, b ImportAccount) int { return strings.Compare(a.SourceKey, b.SourceKey) })
	slices.SortFunc(in.Assignments, func(a, b Assignment) int { return cmp.Compare(a.Record, b.Record) })
	// Bind all mapping inputs and original bytes. Request identity is not payload.
	hashInput := in
	hashInput.RequestKey = ""
	b, _ := json.Marshal(hashInput)
	h := sha256.Sum256(b)
	return Prepared{in, source, preview, hex.EncodeToString(h[:])}, nil
}
func (s *ImportService) Upload(ctx context.Context, t, u string, in Upload) (Import, error) {
	if err := s.authorize(ctx, t, u, true); err != nil {
		return Import{}, err
	}
	p, err := PrepareUpload(in)
	if err != nil {
		return Import{}, err
	}
	return s.repo.SaveImport(ctx, t, u, p)
}
func page(limit, offset int) error {
	if limit < 1 || limit > 200 || offset < 0 || offset > 100000 {
		return ErrInvalid
	}
	return nil
}
func (s *ImportService) List(ctx context.Context, t, u string, limit, offset int) ([]Import, error) {
	if err := s.authorize(ctx, t, u, false); err != nil {
		return nil, err
	}
	if err := page(limit, offset); err != nil {
		return nil, err
	}
	return s.repo.ListImports(ctx, t, limit, offset)
}
func (s *ImportService) Get(ctx context.Context, t, u, id string) (Import, error) {
	if err := s.authorize(ctx, t, u, false); err != nil {
		return Import{}, err
	}
	if _, err := uuid.Parse(id); err != nil {
		return Import{}, ErrInvalid
	}
	return s.repo.GetImport(ctx, t, id)
}
func (s *ImportService) Rows(ctx context.Context, t, u, id string, limit, offset int) ([]StoredRow, error) {
	if err := s.authorize(ctx, t, u, false); err != nil {
		return nil, err
	}
	if _, err := uuid.Parse(id); err != nil {
		return nil, ErrInvalid
	}
	if err := page(limit, offset); err != nil {
		return nil, err
	}
	return s.repo.ListImportRows(ctx, t, id, limit, offset)
}
func (s *ImportService) Reassign(ctx context.Context, t, u, id string, record int, account string, version int64, reason string) (StoredRow, error) {
	if err := s.authorize(ctx, t, u, true); err != nil {
		return StoredRow{}, err
	}
	for _, v := range []string{id, account} {
		if _, err := uuid.Parse(v); err != nil {
			return StoredRow{}, ErrInvalid
		}
	}
	if record < 1 || version < 1 || len(strings.TrimSpace(reason)) == 0 || len(reason) > 2000 {
		return StoredRow{}, ErrInvalid
	}
	return s.repo.ReassignImportRow(ctx, t, u, id, record, account, version, reason)
}
