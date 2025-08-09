package user

import (
    "context"

    "github.com/positron48/budget/internal/domain"
    authuse "github.com/positron48/budget/internal/usecase/auth"
)

type Repo interface {
    GetByID(ctx context.Context, id string) (domain.User, error)
    UpdateProfile(ctx context.Context, id, name, locale string) (domain.User, error)
    ChangePassword(ctx context.Context, id, newHash string) error
}

type PasswordHasher interface {
    Hash(password string) (string, error)
    Verify(hash, password string) bool
}

type Service struct {
    users  Repo
    hasher PasswordHasher
}

func NewService(users Repo, hasher PasswordHasher) *Service { return &Service{users: users, hasher: hasher} }

func (s *Service) GetMe(ctx context.Context, userID string) (domain.User, error) {
    return s.users.GetByID(ctx, userID)
}

func (s *Service) UpdateProfile(ctx context.Context, userID, name, locale string) (domain.User, error) {
    return s.users.UpdateProfile(ctx, userID, name, locale)
}

func (s *Service) ChangePassword(ctx context.Context, userID, currentPassword, newPassword string, getCurrentHash func(ctx context.Context, userID string) (string, error)) error {
    // Support verifying current password using repo or external provider
    currentHash, err := getCurrentHash(ctx, userID)
    if err != nil {
        return err
    }
    if !s.hasher.Verify(currentHash, currentPassword) {
        return authuse.ErrInvalidCredentials
    }
    newHash, err := s.hasher.Hash(newPassword)
    if err != nil {
        return err
    }
    return s.users.ChangePassword(ctx, userID, newHash)
}


