package grpcadapter

import (
	"errors"

	"github.com/jackc/pgconn"
	"github.com/jackc/pgx/v5"
	authuse "github.com/positron48/budget/internal/usecase/auth"
	tenuse "github.com/positron48/budget/internal/usecase/tenant"
	txuse "github.com/positron48/budget/internal/usecase/transaction"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func mapError(err error) error {
	if err == nil {
		return nil
	}
	// domain/usecase errors
	switch {
	case errors.Is(err, authuse.ErrInvalidCredentials):
		return status.Error(codes.Unauthenticated, err.Error())
	case errors.Is(err, txuse.ErrFxRateNotFound):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, tenuse.ErrPermissionDenied):
		return status.Error(codes.PermissionDenied, err.Error())
	}
	// postgres specific
	var pgErr *pgconn.PgError
	if errors.Is(err, pgx.ErrNoRows) {
		return status.Error(codes.NotFound, "not found")
	}
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return status.Error(codes.AlreadyExists, pgErr.Message)
		}
	}
	// default
	return status.Error(codes.Internal, err.Error())
}
