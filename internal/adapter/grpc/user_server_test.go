package grpcadapter

import (
    "context"
    "testing"

    budgetv1 "github.com/positron48/budget/gen/go/budget/v1"
    "github.com/positron48/budget/internal/domain"
    useuser "github.com/positron48/budget/internal/usecase/user"
)

type userRepoStub struct{ u domain.User }
func (r userRepoStub) GetByID(ctx context.Context, id string) (domain.User, error) { return domain.User{ID: id, Email: "e@x", Name: "N"}, nil }
func (r userRepoStub) UpdateProfile(ctx context.Context, id, name, locale string) (domain.User, error) { return domain.User{ID: id, Email: "e@x", Name: name, Locale: locale}, nil }
func (r userRepoStub) ChangePassword(ctx context.Context, id, newHash string) error { return nil }

type passHasherStub struct{}
func (passHasherStub) Hash(password string) (string, error) { return "h", nil }
func (passHasherStub) Verify(hash, password string) bool { return true }

func TestUserServer_GetAndUpdate(t *testing.T) {
    svc := useuser.NewService(userRepoStub{}, passHasherStub{})
    srv := NewUserServer(svc, func(ctx context.Context, userID string) (string, error) { return "h", nil })
    ctx := context.Background()
    if _, err := srv.GetMe(ctx, &budgetv1.GetMeRequest{}); err != nil { t.Fatalf("get: %v", err) }
    if _, err := srv.UpdateProfile(ctx, &budgetv1.UpdateProfileRequest{Name: "New", Locale: "ru"}); err != nil { t.Fatalf("update: %v", err) }
}


