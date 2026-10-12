package investmentimport

import (
	"context"
	"errors"
	"testing"

	"github.com/positron48/budget/internal/domain"
)

type testImportRoles struct{ role domain.TenantRole }

func (r testImportRoles) GetUserRole(context.Context, string, string) (domain.TenantRole, error) {
	return r.role, nil
}
func TestImportPermissionsBeforeSourceParsing(t *testing.T) {
	const tenant = "00000000-0000-0000-0000-000000000001"
	const user = "00000000-0000-0000-0000-000000000002"
	for _, role := range []domain.TenantRole{"", domain.TenantRoleMember} {
		svc := NewImportService(nil, testImportRoles{role})
		if _, err := svc.Upload(context.Background(), tenant, user, Upload{}); !errors.Is(err, ErrForbidden) {
			t.Fatal(role, err)
		}
		if _, err := svc.Reassign(context.Background(), tenant, user, "", 1, "", 1, "reason"); !errors.Is(err, ErrForbidden) {
			t.Fatal(role, err)
		}
	}
	for _, role := range []domain.TenantRole{domain.TenantRoleOwner, domain.TenantRoleAdmin} {
		svc := NewImportService(nil, testImportRoles{role})
		if _, err := svc.Upload(context.Background(), tenant, user, Upload{}); !errors.Is(err, ErrInvalid) {
			t.Fatal(role, err)
		}
	}
}
func TestPrepareUploadRejectsMissingDuplicateUnknownAssignments(t *testing.T) {
	csv := []byte("Event,Date,Symbol,Price,Quantity,Currency,FeeTax,Exchange,NKD,FeeCurrency,DoNotAdjustCash,Note\nBUY,2020-01-01 12:00:00,X,1,2,USD,0,X,0,,False,\nBUY,2020-01-01 12:00:00,X,1,2,USD,0,X,0,,False,\n")
	for _, assignments := range [][]Assignment{{{Record: 1, AccountKey: "a"}}, {{Record: 1, AccountKey: "a"}, {Record: 1, AccountKey: "a"}}, {{Record: 1, AccountKey: "a"}, {Record: 2, AccountKey: "other"}}} {
		_, err := PrepareUpload(Upload{Filename: "f.csv", RequestKey: "key", CSV: csv, Accounts: []ImportAccount{{SourceKey: "a", Name: "A"}}, Assignments: assignments})
		if !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
}

func TestPrepareUploadChecksSourceBindingAndNormalizesMappingOrder(t *testing.T) {
	csv := []byte("Event,Date,Symbol,Price,Quantity,Currency,FeeTax,Exchange,NKD,FeeCurrency,DoNotAdjustCash,Note\nBUY,2020-01-01 12:00:00,X,1,2,USD,0,X,0,,False,\n")
	in := Upload{Filename: "f.csv", RequestKey: "key", CSV: csv, Accounts: []ImportAccount{{SourceKey: "a", Name: "A"}}}
	p, err := PrepareUpload(in)
	if err != nil || len(p.Input.Assignments) != 1 {
		t.Fatal(p, err)
	}
	in.MappingSourceSHA256 = "wrong"
	if _, err := PrepareUpload(in); !errors.Is(err, ErrInvalid) {
		t.Fatal("source mapping binding ignored")
	}
}
