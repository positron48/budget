//go:build !ignore
// +build !ignore

package grpcadapter

import (
    "context"

    budgetv1 "github.com/positron48/budget/gen/go/budget/v1"
    "github.com/positron48/budget/internal/domain"
    "github.com/positron48/budget/internal/pkg/ctxutil"
    useuser "github.com/positron48/budget/internal/usecase/user"
    "google.golang.org/protobuf/types/known/timestamppb"
)

type UserServer struct {
    budgetv1.UnimplementedUserServiceServer
    svc       *useuser.Service
    forGetPwd func(ctx context.Context, userID string) (string, error)
}

func NewUserServer(svc *useuser.Service, getHash func(ctx context.Context, userID string) (string, error)) *UserServer {
    return &UserServer{svc: svc, forGetPwd: getHash}
}

func (s *UserServer) GetMe(ctx context.Context, _ *budgetv1.GetMeRequest) (*budgetv1.GetMeResponse, error) {
    userID, _ := ctxutil.UserIDFromContext(ctx)
    u, err := s.svc.GetMe(ctx, userID)
    if err != nil {
        return nil, mapError(err)
    }
    return &budgetv1.GetMeResponse{User: toProtoUser(u)}, nil
}

func (s *UserServer) UpdateProfile(ctx context.Context, req *budgetv1.UpdateProfileRequest) (*budgetv1.UpdateProfileResponse, error) {
    userID, _ := ctxutil.UserIDFromContext(ctx)
    u, err := s.svc.UpdateProfile(ctx, userID, req.GetName(), req.GetLocale())
    if err != nil {
        return nil, mapError(err)
    }
    return &budgetv1.UpdateProfileResponse{User: toProtoUser(u)}, nil
}

func (s *UserServer) ChangePassword(ctx context.Context, req *budgetv1.ChangePasswordRequest) (*budgetv1.ChangePasswordResponse, error) {
    userID, _ := ctxutil.UserIDFromContext(ctx)
    if err := s.svc.ChangePassword(ctx, userID, req.GetCurrentPassword(), req.GetNewPassword(), s.forGetPwd); err != nil {
        return nil, mapError(err)
    }
    return &budgetv1.ChangePasswordResponse{}, nil
}

func toProtoUser(u domain.User) *budgetv1.User {
    var updated *timestamppb.Timestamp
    if u.UpdatedAt != nil {
        t := *u.UpdatedAt
        updated = timestamppb.New(t)
    }
    return &budgetv1.User{
        Id:            u.ID,
        Email:         u.Email,
        Name:          u.Name,
        Locale:        u.Locale,
        EmailVerified: u.EmailVerified,
        CreatedAt:     timestamppb.New(u.CreatedAt),
        UpdatedAt:     updated,
    }
}


