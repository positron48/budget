package marketdata

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
)

var ErrForbidden = errors.New("market data access denied")
var ErrNotFound = errors.New("market data not found")
var ErrLimit = errors.New("market data request limit")

type Source struct {
	Scope
	Title string
}
type JobStatus struct {
	InstrumentCount int
	Job
	ErrorCode        string
	CoverageState    string
	ObservationCount int
	AvailableAt      time.Time
}
type ReadRepo interface {
	SearchCatalog(context.Context, Scope, string, int, int) ([]SavedInstrument, error)
	Sources(context.Context, string) ([]Source, error)
	SourceEnabled(context.Context, Scope) (bool, error)
	Observations(context.Context, Scope, string, string, time.Time, time.Time) ([]SavedObservation, error)
	RequestRefresh(context.Context, Scope, JobRequest) (Job, error)
	JobStatus(context.Context, string, string) (JobStatus, error)
}
type Roles interface {
	GetUserRole(context.Context, string, string) (d.TenantRole, error)
}
type Service struct {
	repo  ReadRepo
	roles Roles
}

func NewService(repo ReadRepo, roles Roles) *Service { return &Service{repo: repo, roles: roles} }
func (s *Service) authorize(ctx context.Context, tenant, user string) error {
	for _, v := range []string{tenant, user} {
		if id, err := uuid.Parse(v); err != nil || id.String() != v {
			return ErrForbidden
		}
	}
	role, err := s.roles.GetUserRole(ctx, tenant, user)
	if err != nil {
		return ErrForbidden
	}
	if role != d.TenantRoleOwner && role != d.TenantRoleAdmin && role != d.TenantRoleMember {
		return ErrForbidden
	}
	return nil
}
func (s *Service) Sources(ctx context.Context, t, u string) ([]Source, error) {
	if err := s.authorize(ctx, t, u); err != nil {
		return nil, err
	}
	return s.repo.Sources(ctx, t)
}
func (s *Service) allow(ctx context.Context, t, u string, scope Scope) error {
	if scope.TenantID != t {
		return ErrForbidden
	}
	if err := s.authorize(ctx, t, u); err != nil {
		return err
	}
	if err := ValidateScope(scope); err != nil {
		return err
	}
	enabled, err := s.repo.SourceEnabled(ctx, scope)
	if err != nil {
		return err
	}
	if !enabled {
		return ErrNotFound
	}
	return nil
}
func (s *Service) History(ctx context.Context, t, u string, scope Scope, id, kind string, from, to time.Time) ([]SavedObservation, error) {
	if err := s.allow(ctx, t, u, scope); err != nil {
		return nil, err
	}
	return s.repo.Observations(ctx, scope, id, kind, from, to)
}
func (s *Service) Refresh(ctx context.Context, t, u string, scope Scope, q JobRequest) (Job, error) {
	if err := s.allow(ctx, t, u, scope); err != nil {
		return Job{}, err
	}
	if err := ValidateRequest(q); err != nil {
		return Job{}, err
	}
	horizon := time.Now().UTC()
	if q.Kind == "bond_facts" || q.Kind == "dividend_facts" {
		horizon = horizon.Add(365 * 24 * time.Hour)
	}
	if !q.To.IsZero() && q.To.After(horizon) {
		return Job{}, ErrInvalid
	}
	return s.repo.RequestRefresh(ctx, scope, q)
}

// RequestHistory is an atomic batch, not a relaxation of a worker job's limit.
func (s *Service) RequestHistory(ctx context.Context, t, u string, scope Scope, q JobRequest) ([]Job, error) {
	if err := s.allow(ctx, t, u, scope); err != nil {
		return nil, err
	}
	if _, err := PlanRequestedHistory(q); err != nil || q.To.After(time.Now().UTC()) {
		return nil, ErrInvalid
	}
	repo, ok := s.repo.(interface {
		RequestHistory(context.Context, Scope, JobRequest) ([]Job, error)
	})
	if !ok {
		return nil, ErrNotFound
	}
	return repo.RequestHistory(ctx, scope, q)
}
func (s *Service) Job(ctx context.Context, t, u, id string) (JobStatus, error) {
	if err := s.authorize(ctx, t, u); err != nil {
		return JobStatus{}, err
	}
	if _, err := uuid.Parse(id); err != nil {
		return JobStatus{}, ErrInvalid
	}
	return s.repo.JobStatus(ctx, t, id)
}

func (s *Service) Search(ctx context.Context, t, u string, scope Scope, query string, limit, offset int) ([]SavedInstrument, error) {
	if err := s.allow(ctx, t, u, scope); err != nil {
		return nil, err
	}
	return s.repo.SearchCatalog(ctx, scope, query, limit, offset)
}
