package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
)

func brokerSnapshotFixture(t *testing.T, quantity string, extra bool) []byte {
	t.Helper()
	ops := []map[string]any{{"id": "op", "brokerAccountId": "account", "state": "OPERATION_STATE_EXECUTED", "type": "OPERATION_TYPE_BUY", "date": "2024-02-01T00:00:00Z", "quantityDone": quantity}}
	if extra {
		ops = append(ops, map[string]any{"id": "second", "brokerAccountId": "account", "state": "OPERATION_STATE_CANCELED", "type": "OPERATION_TYPE_BUY", "date": "2024-02-02T00:00:00Z"})
	}
	b, _ := json.Marshal(ops)
	hash := sha256.Sum256(b)
	raw, err := json.Marshal(map[string]any{"from": "2024-01-01T00:00:00Z", "to": "2025-01-01T00:00:00Z", "account": map[string]string{"id": "account", "type": "ACCOUNT_TYPE_TINKOFF", "openedDate": "2024-01-01T00:00:00Z"}, "operations": ops, "history_evidence": map[string]any{"version": 1, "complete": true, "pages": []map[string]any{{"cursor": "", "next_cursor": "", "has_next": false, "count": len(ops), "items_sha256": hex.EncodeToString(hash[:])}}}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func TestBrokerSnapshotPersistenceVersionsAndIsolation(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	tenant, other, actor := uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, id := range []string{tenant, other} {
		if _, err := pool.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Broker','RUB')`, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,$2,'test')`, actor, actor+"@example.test"); err != nil {
		t.Fatal(err)
	}
	repo := NewInvestmentImportRepo(pool)
	raw := brokerSnapshotFixture(t, "1", false)
	id, err := repo.SaveBrokerSnapshot(ctx, tenant, actor, raw)
	if err != nil {
		t.Fatal(err)
	}
	again, err := repo.SaveBrokerSnapshot(ctx, tenant, actor, raw)
	if err != nil || again != id {
		t.Fatal(again, err)
	}
	if _, err = repo.SaveBrokerSnapshot(ctx, tenant, actor, brokerSnapshotFixture(t, "1", true)); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.SaveBrokerSnapshot(ctx, tenant, actor, brokerSnapshotFixture(t, "2", true)); err != nil {
		t.Fatal(err)
	}
	var snapshots, observations, links int
	_ = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_broker_snapshots WHERE tenant_id=$1`, tenant).Scan(&snapshots)
	_ = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_broker_observations WHERE tenant_id=$1`, tenant).Scan(&observations)
	_ = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_broker_snapshot_rows WHERE tenant_id=$1`, tenant).Scan(&links)
	if snapshots != 3 || observations != 3 || links != 5 {
		t.Fatal(snapshots, observations, links)
	}
	saved, err := repo.BrokerSnapshotBytes(ctx, tenant, id)
	if err != nil || !bytes.Equal(raw, saved) {
		t.Fatal(err)
	}
	if _, err = repo.BrokerSnapshotBytes(ctx, other, id); !errors.Is(err, imp.ErrNotFound) {
		t.Fatal(err)
	}
	isolated, err := repo.SaveBrokerSnapshot(ctx, other, actor, raw)
	if err != nil || isolated == id {
		t.Fatal(isolated, err)
	}
	// A failed foreign-key write rolls back the source and every observation.
	badActor := uuid.NewString()
	if _, err = repo.SaveBrokerSnapshot(ctx, tenant, badActor, brokerSnapshotFixture(t, "3", true)); err == nil {
		t.Fatal("invalid actor accepted")
	}
	_ = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_broker_snapshots WHERE tenant_id=$1`, tenant).Scan(&snapshots)
	if snapshots != 3 {
		t.Fatal("partial source persisted", snapshots)
	}
	first, err := repo.ListBrokerSnapshots(ctx, tenant, 2, 0)
	if err != nil || len(first) != 2 {
		t.Fatal(first, err)
	}
	second, err := repo.ListBrokerSnapshots(ctx, tenant, 2, 2)
	if err != nil || len(second) != 1 {
		t.Fatal(second, err)
	}
	seen := map[string]bool{}
	for _, item := range append(first, second...) {
		if seen[item.ID] || item.ID == isolated || item.AccountID != "account" || item.SHA256 == "" {
			t.Fatal(item)
		}
		seen[item.ID] = true
	}
	isolatedList, err := repo.ListBrokerSnapshots(ctx, other, 20, 0)
	if err != nil || len(isolatedList) != 1 || isolatedList[0].ID != isolated {
		t.Fatal(isolatedList, err)
	}

	sourceRows, err := repo.BrokerSnapshotRows(ctx, tenant, id, 20, 0)
	if err != nil || len(sourceRows) != 1 || sourceRows[0].OperationID != "op" || !sourceRows[0].HasOtherVersions || sourceRows[0].Ordinal != 1 {
		t.Fatal(sourceRows, err)
	}
	if _, err = repo.BrokerSnapshotRows(ctx, other, id, 20, 0); !errors.Is(err, imp.ErrNotFound) {
		t.Fatal(err)
	}
	isolatedRows, err := repo.BrokerSnapshotRows(ctx, other, isolated, 20, 0)
	if err != nil || len(isolatedRows) != 1 || isolatedRows[0].HasOtherVersions {
		t.Fatal(isolatedRows, err)
	}

}
