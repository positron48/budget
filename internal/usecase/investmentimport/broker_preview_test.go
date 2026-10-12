package investmentimport

import (
	"context"
	"errors"
	d "github.com/positron48/budget/internal/domain"
	"testing"
	"time"
)

type brokerPreviewRepo struct {
	brokerServiceRepo
	rows       []BrokerStoredRow
	readTenant string
}

func (r *brokerPreviewRepo) BrokerSnapshotRows(_ context.Context, tenant, _ string, limit, offset int) ([]BrokerStoredRow, error) {
	r.readTenant = tenant
	if offset >= len(r.rows) {
		return nil, nil
	}
	end := offset + limit
	if end > len(r.rows) {
		end = len(r.rows)
	}
	return r.rows[offset:end], nil
}
func TestBrokerPreviewChecksEveryComponentVersion(t *testing.T) {
	const id = "22222222-2222-4222-8222-222222222222"
	snapshot := brokerTradeFixture(t, nil)
	for _, mode := range []string{"valid", "claimed", "trade-conflict", "fee-conflict", "hash-mismatch", "missing"} {
		t.Run(mode, func(t *testing.T) {
			repo := &brokerPreviewRepo{brokerServiceRepo: brokerServiceRepo{raw: snapshot.Original}}
			for _, row := range snapshot.Rows {
				repo.rows = append(repo.rows, BrokerStoredRow{AccountID: row.AccountID, OperationID: row.OperationID, SHA256: row.SHA256})
			}
			switch mode {
			case "claimed":
				repo.rows[1].LinkID = "existing-link"
			case "trade-conflict":
				repo.rows[0].HasOtherVersions = true
			case "fee-conflict":
				repo.rows[1].HasOtherVersions = true
			case "hash-mismatch":
				repo.rows[1].SHA256 = "changed"
			case "missing":
				repo.rows = repo.rows[:1]
			}
			svc := NewImportService(repo, testImportRoles{d.TenantRoleMember})
			candidate, err := svc.PreviewBrokerTrade(context.Background(), "00000000-0000-0000-0000-000000000001", "00000000-0000-0000-0000-000000000002", id, "trade", id)
			if mode == "valid" || mode == "claimed" {
				if candidate.PostingAllowed != (mode == "valid") {
					t.Fatal("posting eligibility", candidate)
				}
				if err != nil || candidate.Operation.Fee != "0.3" || repo.readTenant != "00000000-0000-0000-0000-000000000001" {
					t.Fatal(candidate, err, repo.readTenant)
				}
			} else if !errors.Is(err, ErrInvalid) || candidate.Operation.Kind != "" {
				t.Fatal(candidate, err)
			}
		})
	}
	repo := &brokerPreviewRepo{}
	svc := NewImportService(repo, testImportRoles{})
	if _, err := svc.PreviewBrokerTrade(context.Background(), "00000000-0000-0000-0000-000000000001", "00000000-0000-0000-0000-000000000002", id, "trade", id); !errors.Is(err, ErrForbidden) || repo.calls != 0 {
		t.Fatal(err, repo.calls)
	}
}

func (r *brokerPreviewRepo) BrokerTradeJournalCandidates(_ context.Context, tenant, ledger string, _ time.Time) ([]d.InvestmentOperation, error) {
	return nil, nil
}

func (r *brokerPreviewRepo) BrokerTradeReplay(_ context.Context, _ string, _ d.InvestmentOperation) (BrokerReplayCheck, error) {
	return BrokerReplayCheck{LedgerVersion: 1, Valid: true}, nil
}
