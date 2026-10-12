package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	imp "github.com/positron48/budget/internal/usecase/investmentimport"
)

func TestInvestmentImportPersistence(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, other, user string
	for _, fixture := range []struct {
		query string
		dest  *string
	}{
		{`INSERT INTO tenants(name,default_currency_code) VALUES('Import','RUB') RETURNING id`, &tenant},
		{`INSERT INTO tenants(name,default_currency_code) VALUES('Foreign','RUB') RETURNING id`, &other},
		{fmt.Sprintf(`INSERT INTO users(email,password_hash) VALUES('import-%d@example.com','test') RETURNING id`, time.Now().UnixNano()), &user},
	} {
		if err := pool.DB.QueryRow(ctx, fixture.query).Scan(fixture.dest); err != nil {
			t.Fatal(err)
		}
	}
	csv := []byte("Event,Date,Symbol,Price,Quantity,Currency,FeeTax,Exchange,NKD,FeeCurrency,DoNotAdjustCash,Note\nBUY,2020-01-01 12:00:00,TEST,1,2,USD,0,X,0,,False,\nBUY,2020-01-01 12:00:00,TEST,1,2,USD,0,X,0,,False,\n")
	input := imp.Upload{Filename: "source.csv", RequestKey: "upload", CSV: csv, Accounts: []imp.ImportAccount{{SourceKey: "a", Name: "Broker"}, {SourceKey: "other", Name: "Unassigned"}}, Assignments: []imp.Assignment{{Record: 1, AccountKey: "a"}, {Record: 2, AccountKey: "other"}}}
	prepared, err := imp.PrepareUpload(input)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewInvestmentImportRepo(pool)
	// Concurrent retries create exactly one import and preserve identical rows.
	var wg sync.WaitGroup
	results := make(chan imp.Import, 2)
	failures := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := repo.SaveImport(ctx, tenant, user, prepared)
			if e != nil {
				failures <- e
			} else {
				results <- v
			}
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for e := range failures {
		t.Fatal(e)
	}
	var saved imp.Import
	for v := range results {
		if saved.ID != "" && saved.ID != v.ID {
			t.Fatal("retry duplicated import")
		}
		saved = v
	}
	if len(saved.Accounts) != 2 || saved.Rows != 2 {
		t.Fatal(saved)
	}
	records, err := repo.ListImportRows(ctx, tenant, saved.ID, 50, 0)
	if err != nil || len(records) != 2 {
		t.Fatal(records, err)
	}
	if records[0].SourceRowID == records[1].SourceRowID {
		t.Fatal("identical records collapsed")
	}
	var original []byte
	if err = pool.DB.QueryRow(ctx, `SELECT original_csv FROM investment_imports WHERE id=$1`, saved.ID).Scan(&original); err != nil || string(original) != string(csv) {
		t.Fatal("source changed", err)
	}
	if _, err = repo.GetImport(ctx, other, saved.ID); !errors.Is(err, imp.ErrNotFound) {
		t.Fatal("cross tenant metadata", err)
	}
	if _, err = repo.ListImportRows(ctx, other, saved.ID, 50, 0); !errors.Is(err, imp.ErrNotFound) {
		t.Fatal("cross tenant rows", err)
	}
	target := records[1].AccountID
	moved, err := repo.ReassignImportRow(ctx, tenant, user, saved.ID, 1, target, 1, "broker statement")
	if err != nil || moved.Version != 2 || moved.AccountID != target {
		t.Fatal(moved, err)
	}
	if _, err = repo.ReassignImportRow(ctx, tenant, user, saved.ID, 1, target, 1, "stale"); !errors.Is(err, imp.ErrConflict) {
		t.Fatal("stale mapping accepted", err)
	}
	var revisions int
	if err = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_import_mapping_revisions WHERE import_id=$1`, saved.ID).Scan(&revisions); err != nil || revisions != 1 {
		t.Fatal(revisions, err)
	}
	changed := input
	changed.Filename = "changed.csv"
	different, err := imp.PrepareUpload(changed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.SaveImport(ctx, tenant, user, different); !errors.Is(err, imp.ErrConflict) {
		t.Fatal("key reused for changed payload", err)
	}
	foreign, err := repo.SaveImport(ctx, other, user, prepared)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.ReassignImportRow(ctx, tenant, user, saved.ID, 1, foreign.Accounts[0].ID, 2, "foreign"); !errors.Is(err, imp.ErrNotFound) {
		t.Fatal("foreign assignment accepted", err)
	}
	// Same-tenant but different import accounts must also fail composite FK.
	_, err = pool.DB.Exec(ctx, `UPDATE investment_import_rows SET account_id=$1 WHERE tenant_id=$2 AND import_id=$3 AND record=1`, foreign.Accounts[0].ID, tenant, saved.ID)
	if err == nil {
		t.Fatal("database accepted cross-tenant FK")
	}
	// Reapplying initialization migrations is supported by deployment scripts.
	applyMigrations(t, ctx, pool.DB)
}

// Optional private-data acceptance check. No personal fixtures enter Git and
// all writes go to a fresh test container, never the configured application DB.
func TestInvestmentImportPrivateFixture(t *testing.T) {
	sourcePath := os.Getenv("INVESTMENT_SOURCE_FIXTURE")
	mappingPath := os.Getenv("INVESTMENT_MAPPING_FIXTURE")
	if sourcePath == "" || mappingPath == "" {
		t.Skip("private fixture paths not provided")
	}
	var source imp.StagedSource
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal("read source fixture")
	}
	if err = json.Unmarshal(data, &source); err != nil {
		t.Fatal("decode source fixture")
	}
	var mapping struct {
		SourceSHA256 string `json:"source_sha256"`
		Accounts     []struct{ ID, Name string }
		Rows         []struct {
			Record            int    `json:"source_record"`
			Account           string `json:"account_id"`
			Status            string `json:"status"`
			BrokerOperationID string `json:"broker_operation_id"`
		}
	}
	data, err = os.ReadFile(mappingPath)
	if err != nil {
		t.Fatal("read mapping fixture")
	}
	if err = json.Unmarshal(data, &mapping); err != nil {
		t.Fatal("decode mapping fixture")
	}
	in := imp.Upload{Filename: "private-source.csv", CSV: source.OriginalCSV, RequestKey: "private-fixture", MappingSourceSHA256: mapping.SourceSHA256}
	expected := map[string]int{}
	for _, a := range mapping.Accounts {
		in.Accounts = append(in.Accounts, imp.ImportAccount{SourceKey: a.ID, Name: a.Name})
	}
	for _, r := range mapping.Rows {
		evidence, _ := json.Marshal(r)
		in.Assignments = append(in.Assignments, imp.Assignment{Record: r.Record, AccountKey: r.Account, Evidence: string(evidence)})
		expected[r.Account]++
	}
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, user string
	if err = pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Private fixture','RUB') RETURNING id`).Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	if err = pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('private-fixture@example.test','test') RETURNING id`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, user); err != nil {
		t.Fatal(err)
	}
	svc := imp.NewImportService(NewInvestmentImportRepo(pool), NewTenantRepo(pool))
	saved, err := svc.Upload(ctx, tenant, user, in)
	if err != nil {
		t.Fatal("save private source failed", err)
	}
	if saved.Rows != len(source.Rows) || len(saved.Accounts) != len(mapping.Accounts) {
		t.Fatal("private source count mismatch")
	}
	for _, a := range saved.Accounts {
		if a.Rows != expected[a.SourceKey] {
			t.Fatal("private account mapping count mismatch")
		}
	}
	for offset := 0; offset < len(source.Rows); offset += 200 {
		rows, err := svc.Rows(ctx, tenant, user, saved.ID, 200, offset)
		if err != nil {
			t.Fatal(err)
		}
		for i, row := range rows {
			want := source.Rows[offset+i]
			if row.SourceRowID != want.ID || !reflect.DeepEqual(row.Source.Raw, want.Raw) || !reflect.DeepEqual(row.Source.Decimals, want.Decimals) {
				t.Fatal("private source row changed")
			}
		}
	}
	repeated, err := svc.Upload(ctx, tenant, user, in)
	if err != nil || repeated.ID != saved.ID {
		t.Fatal("private retry failed", err)
	}
}
