package auth

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

type hasherOK struct{}

func (hasherOK) Hash(password string) (string, error) { return "hash:" + password, nil }
func (hasherOK) Verify(hash, password string) bool    { return hash == "hash:"+password }

type hasherBad struct{}

func (hasherBad) Hash(password string) (string, error) { return "hash:" + password, nil }
func (hasherBad) Verify(hash, password string) bool    { return false }

type issuerStub struct{}

func (issuerStub) Issue(ctx context.Context, userID, tenantID string, accessTTL, refreshTTL time.Duration) (TokenPair, error) {
	now := time.Now()
	// add time-based suffix to ensure new refresh token differs from old in tests
	suffix := now.UnixNano()
	return TokenPair{
		AccessToken:           "access-" + userID,
		RefreshToken:          "refresh-" + userID + "-" + fmt.Sprintf("%d", suffix),
		AccessTokenExpiresAt:  now.Add(accessTTL),
		RefreshTokenExpiresAt: now.Add(refreshTTL),
		TokenType:             "Bearer",
	}, nil
}

type userRepoMem struct {
	byEmail map[string]struct {
		U  User
		Ms []TenantMembership
	}
}

func (r *userRepoMem) CreateWithDefaultTenant(ctx context.Context, email, passwordHash, name, locale, tenantName string) (User, Tenant, error) {
	u := User{ID: "u1", Email: email, Name: name, Locale: locale, PasswordHash: passwordHash}
	t := Tenant{ID: "t1", Name: tenantName, DefaultCurrencyCode: "USD"}
	if r.byEmail == nil {
		r.byEmail = map[string]struct {
			U  User
			Ms []TenantMembership
		}{}
	}
	r.byEmail[email] = struct {
		U  User
		Ms []TenantMembership
	}{U: u, Ms: []TenantMembership{{TenantID: t.ID, Role: "owner", IsDefault: true}}}
	return u, t, nil
}

func (r *userRepoMem) GetByEmail(ctx context.Context, email string) (User, []TenantMembership, error) {
	if r.byEmail == nil {
		return User{}, nil, errors.New("not found")
	}
	row, ok := r.byEmail[email]
	if !ok {
		return User{}, nil, errors.New("not found")
	}
	return row.U, row.Ms, nil
}

type tokensMem struct {
	rows map[string]struct {
		UserID    string
		ExpiresAt time.Time
		RevokedAt *time.Time
	}
}

func (t *tokensMem) Store(ctx context.Context, userID, token string, expiresAt time.Time) error {
	if t.rows == nil {
		t.rows = map[string]struct {
			UserID    string
			ExpiresAt time.Time
			RevokedAt *time.Time
		}{}
	}
	t.rows[token] = struct {
		UserID    string
		ExpiresAt time.Time
		RevokedAt *time.Time
	}{UserID: userID, ExpiresAt: expiresAt}
	return nil
}

func (t *tokensMem) Rotate(ctx context.Context, oldToken, newToken string, newExpiresAt time.Time) error {
	if t.rows == nil {
		return errors.New("no rows")
	}
	row := t.rows[oldToken]
	now := time.Now()
	row.RevokedAt = &now
	t.rows[oldToken] = row
	t.rows[newToken] = struct {
		UserID    string
		ExpiresAt time.Time
		RevokedAt *time.Time
	}{UserID: row.UserID, ExpiresAt: newExpiresAt}
	return nil
}

func (t *tokensMem) GetByToken(ctx context.Context, token string) (struct {
	UserID    string
	ExpiresAt time.Time
	RevokedAt *time.Time
}, error,
) {
	if t.rows == nil {
		return struct {
			UserID    string
			ExpiresAt time.Time
			RevokedAt *time.Time
		}{}, errors.New("not found")
	}
	row, ok := t.rows[token]
	if !ok {
		return struct {
			UserID    string
			ExpiresAt time.Time
			RevokedAt *time.Time
		}{}, errors.New("not found")
	}
	return row, nil
}

func TestService_Register_Success(t *testing.T) {
	ur := &userRepoMem{}
	tr := &tokensMem{}
	svc := NewService(ur, tr, hasherOK{}, issuerStub{}, time.Minute, time.Hour)
	u, tenant, tp, err := svc.Register(context.Background(), "e@x", "Passw0rd!", "User", "ru", "Дом")
	if err != nil || u.ID == "" || tenant.ID == "" || tp.AccessToken == "" || tp.RefreshToken == "" {
		t.Fatalf("register: %v %#v %#v %#v", err, u, tenant, tp)
	}
	if _, ok := tr.rows[tp.RefreshToken]; !ok {
		t.Fatal("refresh token not stored")
	}
}

func TestService_Login_Success_DefaultTenant(t *testing.T) {
	// prepare repo with default membership
	ur := &userRepoMem{byEmail: map[string]struct {
		U  User
		Ms []TenantMembership
	}{
		"e@x": {U: User{ID: "u1", Email: "e@x", PasswordHash: "hash:pass"}, Ms: []TenantMembership{{TenantID: "t1", Role: "owner", IsDefault: true}}},
	}}
	tr := &tokensMem{}
	svc := NewService(ur, tr, hasherOK{}, issuerStub{}, time.Minute, time.Hour)
	_, ms, tp, err := svc.Login(context.Background(), "e@x", "pass")
	if err != nil || len(ms) != 1 || tp.AccessToken == "" || tp.RefreshToken == "" {
		t.Fatalf("login: %v %#v %#v", err, ms, tp)
	}
}

func TestService_Login_InvalidPassword(t *testing.T) {
	ur := &userRepoMem{byEmail: map[string]struct {
		U  User
		Ms []TenantMembership
	}{
		"e@x": {U: User{ID: "u1", Email: "e@x", PasswordHash: "hash:correct"}, Ms: []TenantMembership{{TenantID: "t1", Role: "member", IsDefault: true}}},
	}}
	tr := &tokensMem{}
	svc := NewService(ur, tr, hasherBad{}, issuerStub{}, time.Minute, time.Hour)
	if _, _, _, err := svc.Login(context.Background(), "e@x", "wrong"); err == nil {
		t.Fatal("expected invalid credentials")
	}
}

func TestService_Refresh_Success_And_Invalid(t *testing.T) {
	ur := &userRepoMem{}
	tr := &tokensMem{}
	svc := NewService(ur, tr, hasherOK{}, issuerStub{}, time.Minute, time.Hour)
	u, _, tp, err := svc.Register(context.Background(), "e@x", "pass", "U", "ru", "Дом")
	if err != nil {
		t.Fatalf("reg: %v", err)
	}
	// success: returns new pair and rotates old
	newTP, err := svc.Refresh(context.Background(), tp.RefreshToken)
	if err != nil || newTP.RefreshToken == tp.RefreshToken {
		t.Fatalf("refresh: %v %#v", err, newTP)
	}
	// invalid: expired (use a fresh token from second registration and mark expired)
	_, _, tp2, err := svc.Register(context.Background(), "e2@x", "pass", "U2", "ru", "Дом")
	if err != nil {
		t.Fatalf("reg2: %v", err)
	}
	r2 := tr.rows[tp2.RefreshToken]
	r2.ExpiresAt = time.Now().Add(-time.Minute)
	tr.rows[tp2.RefreshToken] = r2
	if _, err := svc.Refresh(context.Background(), tp2.RefreshToken); err == nil {
		t.Fatal("expected invalid for expired")
	}
	// invalid: revoked (revoke newTP and try to refresh)
	rr := tr.rows[newTP.RefreshToken]
	now := time.Now()
	rr.RevokedAt = &now
	tr.rows[newTP.RefreshToken] = rr
	if _, err := svc.Refresh(context.Background(), newTP.RefreshToken); err == nil {
		t.Fatal("expected invalid for revoked")
	}
	_ = u
}

// --- Additional negative paths to raise coverage ---

type hasherErr struct{}

func (hasherErr) Hash(password string) (string, error) { return "", errors.New("hash err") }
func (hasherErr) Verify(hash, password string) bool    { return false }

type issuerErr struct{}

func (issuerErr) Issue(ctx context.Context, userID, tenantID string, accessTTL, refreshTTL time.Duration) (TokenPair, error) {
	return TokenPair{}, errors.New("issue err")
}

type userRepoErr struct{}

func (userRepoErr) CreateWithDefaultTenant(ctx context.Context, email, passwordHash, name, locale, tenantName string) (User, Tenant, error) {
	return User{}, Tenant{}, errors.New("create err")
}

func (userRepoErr) GetByEmail(ctx context.Context, email string) (User, []TenantMembership, error) {
	return User{}, nil, errors.New("get err")
}

type tokensErr struct {
	storeErr, rotateErr bool
	tokensMem
}

func (t *tokensErr) Store(ctx context.Context, userID, token string, expiresAt time.Time) error {
	if t.storeErr {
		return errors.New("store err")
	}
	return t.tokensMem.Store(ctx, userID, token, expiresAt)
}

func (t *tokensErr) Rotate(ctx context.Context, oldToken, newToken string, newExpiresAt time.Time) error {
	if t.rotateErr {
		return errors.New("rotate err")
	}
	return t.tokensMem.Rotate(ctx, oldToken, newToken, newExpiresAt)
}

func TestService_Register_Errors(t *testing.T) {
	// hasher error
	if _, _, _, err := NewService(&userRepoMem{}, &tokensMem{}, hasherErr{}, issuerStub{}, time.Minute, time.Hour).Register(context.Background(), "e@x", "p", "n", "ru", "T"); err == nil {
		t.Fatal("expected hasher error")
	}
	// user repo error
	if _, _, _, err := NewService(userRepoErr{}, &tokensMem{}, hasherOK{}, issuerStub{}, time.Minute, time.Hour).Register(context.Background(), "e@x", "p", "n", "ru", "T"); err == nil {
		t.Fatal("expected repo error")
	}
	// issuer error
	if _, _, _, err := NewService(&userRepoMem{}, &tokensMem{}, hasherOK{}, issuerErr{}, time.Minute, time.Hour).Register(context.Background(), "e@x", "p", "n", "ru", "T"); err == nil {
		t.Fatal("expected issuer error")
	}
	// tokens store error
	if _, _, _, err := NewService(&userRepoMem{}, &tokensErr{storeErr: true}, hasherOK{}, issuerStub{}, time.Minute, time.Hour).Register(context.Background(), "e@x", "p", "n", "ru", "T"); err == nil {
		t.Fatal("expected store error")
	}
}

func TestService_Login_Errors_And_NoDefault(t *testing.T) {
	// getByEmail error
	if _, _, _, err := NewService(userRepoErr{}, &tokensMem{}, hasherOK{}, issuerStub{}, time.Minute, time.Hour).Login(context.Background(), "e@x", "p"); err == nil {
		t.Fatal("expected getByEmail error")
	}
	// issuer error on login
	ur := &userRepoMem{byEmail: map[string]struct {
		U  User
		Ms []TenantMembership
	}{
		"e@x": {U: User{ID: "u1", Email: "e@x", PasswordHash: "hash:p"}, Ms: []TenantMembership{{TenantID: "t1", Role: "member"}}},
	}}
	if _, _, _, err := NewService(ur, &tokensMem{}, hasherOK{}, issuerErr{}, time.Minute, time.Hour).Login(context.Background(), "e@x", "p"); err == nil {
		t.Fatal("expected issuer error on login")
	}
	// tokens store error on login
	if _, _, _, err := NewService(ur, &tokensErr{storeErr: true}, hasherOK{}, issuerStub{}, time.Minute, time.Hour).Login(context.Background(), "e@x", "p"); err == nil {
		t.Fatal("expected store error on login")
	}
	// no default membership → select first
	ur2 := &userRepoMem{byEmail: map[string]struct {
		U  User
		Ms []TenantMembership
	}{
		"e@x": {U: User{ID: "u1", Email: "e@x", PasswordHash: "hash:p"}, Ms: []TenantMembership{{TenantID: "t2", Role: "member"}, {TenantID: "t3", Role: "member"}}},
	}}
	if _, ms, _, err := NewService(ur2, &tokensMem{}, hasherOK{}, issuerStub{}, time.Minute, time.Hour).Login(context.Background(), "e@x", "p"); err != nil || len(ms) != 2 {
		t.Fatalf("login no-default: %v %#v", err, ms)
	}
}

func TestService_Refresh_IssueAndRotateErrors(t *testing.T) {
	ur := &userRepoMem{}
	tr := &tokensErr{}
	svc := NewService(ur, tr, hasherOK{}, issuerStub{}, time.Minute, time.Hour)
	_, _, tp, err := svc.Register(context.Background(), "e@x", "p", "n", "ru", "T")
	if err != nil {
		t.Fatalf("reg: %v", err)
	}
	// issue error
	svcIssueErr := NewService(ur, tr, hasherOK{}, issuerErr{}, time.Minute, time.Hour)
	if _, err := svcIssueErr.Refresh(context.Background(), tp.RefreshToken); err == nil {
		t.Fatal("expected issue error")
	}
	// rotate error
	tr.rotateErr = true
	if _, err := svc.Refresh(context.Background(), tp.RefreshToken); err == nil {
		t.Fatal("expected rotate error")
	}
}
