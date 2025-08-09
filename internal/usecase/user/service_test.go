package user

import (
	"context"
	"testing"

	"github.com/positron48/budget/internal/domain"
)

type repoStub struct{}

func (r repoStub) GetByID(ctx context.Context, id string) (domain.User, error) {
	return domain.User{ID: id, Email: "e@x", Name: "N"}, nil
}

func (r repoStub) UpdateProfile(ctx context.Context, id, name, locale string) (domain.User, error) {
	return domain.User{ID: id, Email: "e@x", Name: name, Locale: locale}, nil
}
func (r repoStub) ChangePassword(ctx context.Context, id, newHash string) error { return nil }

type hasherOK struct{}

func (hasherOK) Hash(password string) (string, error) { return "h", nil }
func (hasherOK) Verify(hash, password string) bool    { return true }

type hasherBad struct{}

func (hasherBad) Hash(password string) (string, error) { return "h", nil }
func (hasherBad) Verify(hash, password string) bool    { return false }

func TestService_GetMe_UpdateProfile(t *testing.T) {
	svc := NewService(repoStub{}, hasherOK{})
	if _, err := svc.GetMe(context.Background(), "u1"); err != nil {
		t.Fatalf("get: %v", err)
	}
	u, err := svc.UpdateProfile(context.Background(), "u1", "New", "ru")
	if err != nil || u.Name != "New" || u.Locale != "ru" {
		t.Fatalf("update: %v %#v", err, u)
	}
}

func TestService_ChangePassword_SuccessAndInvalid(t *testing.T) {
	// success
	svc := NewService(repoStub{}, hasherOK{})
	if err := svc.ChangePassword(context.Background(), "u1", "old", "new", func(ctx context.Context, userID string) (string, error) { return "h", nil }); err != nil {
		t.Fatalf("change ok: %v", err)
	}
	// invalid current
	svc2 := NewService(repoStub{}, hasherBad{})
	if err := svc2.ChangePassword(context.Background(), "u1", "bad", "new", func(ctx context.Context, userID string) (string, error) { return "h", nil }); err == nil {
		t.Fatal("expected error on invalid current password")
	}
}
