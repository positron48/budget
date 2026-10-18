package grpcadapter

import (
	"context"
	"errors"
	budgetv1 "github.com/positron48/budget/gen/go/budget/v1"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	rp "github.com/positron48/budget/internal/usecase/recurringpayment"
	"github.com/positron48/budget/internal/usecase/transaction"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"testing"
)

func TestRecurringPaymentErrorContract(t *testing.T) {
	for _, tc := range []struct {
		err     error
		code    codes.Code
		message string
	}{{rp.ErrInvalid, codes.InvalidArgument, "payment_invalid"}, {rp.ErrNotFound, codes.NotFound, "payment_not_found"}, {rp.ErrConflict, codes.Aborted, "payment_version_conflict"}, {rp.ErrPrecondition, codes.FailedPrecondition, "payment_precondition"}, {transaction.ErrFxRateNotFound, codes.FailedPrecondition, "payment_precondition"}, {errors.New("database private diagnostic"), codes.Internal, "payment_failed"}} {
		e := paymentRPCError(tc.err)
		if status.Code(e) != tc.code || status.Convert(e).Message() != tc.message {
			t.Fatal(e)
		}
	}
}

func TestRecurringPaymentExistingExpenseRequiresSelection(t *testing.T) {
	ctx := ctxutil.WithUserID(ctxutil.WithTenantID(context.Background(), "tenant"), "user")
	server := NewRecurringPaymentServer(nil)
	_, err := server.ConfirmPayment(ctx, &budgetv1.ConfirmPaymentRequest{Expense: &budgetv1.ConfirmPaymentRequest_ExistingTransaction{ExistingTransaction: &budgetv1.PaymentExistingTransaction{}}})
	if status.Code(err) != codes.InvalidArgument || status.Convert(err).Message() != "payment_invalid" {
		t.Fatal(err)
	}
}
