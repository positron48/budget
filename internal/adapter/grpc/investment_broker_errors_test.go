package grpcadapter

import (
	"fmt"
	d "github.com/positron48/budget/internal/domain"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"testing"
)

func TestBrokerPostingErrorCodes(t *testing.T) {
	for _, test := range []struct {
		err  error
		code codes.Code
	}{
		{imp.ErrConflict, codes.Aborted}, {d.ErrInvestmentConflict, codes.Aborted}, {d.ErrInvestmentPrecondition, codes.FailedPrecondition}, {imp.ErrInvalid, codes.InvalidArgument}, {d.ErrInvestmentLimit, codes.ResourceExhausted}, {d.ErrInvestmentNotFound, codes.NotFound}, {imp.ErrForbidden, codes.PermissionDenied},
	} {
		if got := status.Code(investmentImportError(fmt.Errorf("wrapped: %w", test.err))); got != test.code {
			t.Fatal(test.err, got)
		}
	}
	if got := status.Convert(investmentImportError(fmt.Errorf("private storage details"))); got.Code() != codes.Internal || got.Message() == "private storage details" {
		t.Fatal(got)
	}
}
