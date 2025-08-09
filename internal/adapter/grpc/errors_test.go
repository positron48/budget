package grpcadapter

import (
    "errors"
    "testing"

    "github.com/jackc/pgconn"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
)

func TestMapError_DomainAndPg(t *testing.T) {
    // pg unique violation
    pgerr := &pgconn.PgError{Code: "23505", Message: "duplicate"}
    if status.Code(mapError(pgerr)) != codes.AlreadyExists { t.Fatal("pg unique should map to AlreadyExists") }
    if status.Code(mapError(errors.New("boom"))) != codes.Internal { t.Fatal("unknown -> Internal") }
}


