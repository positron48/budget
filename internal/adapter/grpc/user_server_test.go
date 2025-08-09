package grpcadapter

import (
	"context"
	"testing"

	budgetv1 "github.com/positron48/budget/gen/go/budget/v1"
	"github.com/positron48/budget/internal/domain"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	useuser "github.com/positron48/budget/internal/usecase/user"
)

type userRepoStub struct{}

func (r userRepoStub) GetByID(ctx context.Context, id string) (domain.User, error) {
	return domain.User{ID: id, Email: "e@x", Name: "N"}, nil
}

func (r userRepoStub) UpdateProfile(ctx context.Context, id, name, locale string) (domain.User, error) {
	return domain.User{ID: id, Email: "e@x", Name: name, Locale: locale}, nil
}
func (r userRepoStub) ChangePassword(ctx context.Context, id, newHash string) error { return nil }

type passHasherStub struct{}

func (passHasherStub) Hash(password string) (string, error) { return "h", nil }
func (passHasherStub) Verify(hash, password string) bool    { return true }

func TestUserServer_GetAndUpdate(t *testing.T) {
	svc := useuser.NewService(userRepoStub{}, passHasherStub{})
	srv := NewUserServer(svc, func(ctx context.Context, userID string) (string, error) { return "h", nil })
	ctx := ctxutil.WithUserID(context.Background(), "u1")
	me, err := srv.GetMe(ctx, &budgetv1.GetMeRequest{})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if me.GetUser().GetId() != "u1" {
		t.Fatalf("expected user id u1, got %q", me.GetUser().GetId())
	}
	if _, err := srv.UpdateProfile(ctx, &budgetv1.UpdateProfileRequest{Name: "New", Locale: "ru"}); err != nil {
		t.Fatalf("update: %v", err)
	}
}

func TestUserServer_ChangePassword(t *testing.T) {
	svc := useuser.NewService(userRepoStub{}, passHasherStub{})
	srv := NewUserServer(svc, func(ctx context.Context, userID string) (string, error) { return "h", nil })
	ctx := context.Background()
	if _, err := srv.ChangePassword(ctx, &budgetv1.ChangePasswordRequest{CurrentPassword: "old", NewPassword: "new"}); err != nil {
		t.Fatalf("change: %v", err)
	}
}

type badHasher struct{}

func (badHasher) Hash(password string) (string, error) { return "h", nil }
func (badHasher) Verify(hash, password string) bool    { return false }

func TestUserServer_ChangePassword_InvalidCurrent(t *testing.T) {
	svc := useuser.NewService(userRepoStub{}, badHasher{})
	srv := NewUserServer(svc, func(ctx context.Context, userID string) (string, error) { return "h", nil })
	if _, err := srv.ChangePassword(context.Background(), &budgetv1.ChangePasswordRequest{CurrentPassword: "bad", NewPassword: "new"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestUserServer_UpdateProfile_Invalid(t *testing.T) {
	svc := useuser.NewService(userRepoStub{}, passHasherStub{})
	srv := NewUserServer(svc, func(ctx context.Context, userID string) (string, error) { return "h", nil })
	if _, err := srv.UpdateProfile(context.Background(), &budgetv1.UpdateProfileRequest{}); err == nil {
		t.Fatal("expected invalid argument")
	}
}
