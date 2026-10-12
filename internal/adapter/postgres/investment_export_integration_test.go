package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	archive "github.com/positron48/budget/internal/usecase/investmentarchive"
	exp "github.com/positron48/budget/internal/usecase/investmentexport"
)

func TestInvestmentAsyncExportIsolationLeaseAndDownload(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	var tenant, actor, member, other string
	must(pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Export','RUB') RETURNING id`).Scan(&tenant))
	must(pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Other','RUB') RETURNING id`).Scan(&other))
	must(pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('export@example.test','test') RETURNING id`).Scan(&actor))
	must(pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('member@example.test','test') RETURNING id`).Scan(&member))
	_, e := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner'),($1,$3,'member'),($4,$2,'owner')`, tenant, actor, member, other)
	must(e)
	repo := NewInvestmentRepo(pool)
	svc := inv.NewService(repo, NewTenantRepo(pool))
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ledger, e := svc.CreateLedger(ctx, tenant, actor, "Broker", at, "ledger")
	must(e)
	_, e = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: "deposit", ExpectedLedgerVersion: 1, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "deposit", Currency: "RUB", Amount: "9007199254740993.123456789012", OccurredAt: at.Add(time.Hour)}})
	must(e)
	for i := 0; i < 35; i++ {
		_, e = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: fmt.Sprintf("fixture-%d", i), ExpectedLedgerVersion: int64(i + 2), Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "deposit", Currency: "RUB", Amount: "1", OccurredAt: at.Add(time.Duration(i+2) * time.Hour), Note: strings.Repeat("a", 4990) + "<>&\u2028"}})
		must(e)
	}
	if _, e = repo.StartExport(ctx, tenant, member, uuid.NewString()); !errors.Is(e, d.ErrInvestmentForbidden) {
		t.Fatal("member export", e)
	}
	key := uuid.NewString()
	job, e := repo.StartExport(ctx, tenant, actor, key)
	must(e)
	assertInvestmentRollbackRefused(t, ctx, pool, "0059_investment_export_jobs")
	again, e := repo.StartExport(ctx, tenant, actor, key)
	must(e)
	if job.ID != again.ID {
		t.Fatal("request retry duplicated job")
	}
	if _, e = repo.ReadExportChunk(ctx, tenant, actor, job.ID, 0); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal("pending downloadable", e)
	}
	if _, e = repo.GetExport(ctx, other, actor, job.ID); !errors.Is(e, d.ErrInvestmentNotFound) {
		t.Fatal("foreign tenant", e)
	}
	scoped := NewInvestmentExportWorker(pool, []string{other})
	if worked, e := scoped.ProcessExport(ctx); e != nil || worked {
		t.Fatal("worker escaped rollout scope", worked, e)
	}
	// Simulate a process crash after claiming. A fresh worker recovers its lease.
	_, e = pool.DB.Exec(ctx, `UPDATE investment_export_jobs SET state='running',lease_token=gen_random_uuid(),lease_until=clock_timestamp()-interval '1 second',attempts=1 WHERE id=$1`, job.ID)
	must(e)
	worked, e := repo.ProcessExport(ctx)
	must(e)
	if !worked {
		t.Fatal("expired lease not recovered")
	}
	ready, e := repo.GetExport(ctx, tenant, actor, job.ID)
	must(e)
	if ready.State != "ready" || ready.ByteSize <= exp.ChunkBytes || len(ready.SHA256) != 64 {
		t.Fatal("not ready", ready.State)
	}
	var data []byte
	for int64(len(data)) < ready.ByteSize {
		c, e := repo.ReadExportChunk(ctx, tenant, actor, job.ID, int64(len(data)))
		must(e)
		if c.Offset != int64(len(data)) || c.Total != ready.ByteSize || c.SHA256 != ready.SHA256 || len(c.Data) == 0 || len(c.Data) > exp.ChunkBytes {
			t.Fatal("invalid chunk envelope")
		}
		data = append(data, c.Data...)
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != ready.SHA256 {
		t.Fatal("download checksum")
	}
	bundle, e := archive.Decode(bytes.NewReader(data))
	must(e)
	_, e = archive.VerifyJournal(bundle.Data)
	must(e)
	must(archive.VerifyJournalAudit(bundle.Data))

	// Restore the bytes obtained through chunk download into a second PostgreSQL.
	target, targetCleanup := withPg(t)
	defer targetCleanup()
	_, e = target.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Restored','RUB')`, tenant)
	must(e)
	_, e = target.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,'restored@example.test','test')`, actor)
	must(e)
	_, e = target.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor)
	must(e)
	targetRepo := NewInvestmentRepo(target)
	restored, e := targetRepo.RestoreInvestmentArchive(ctx, actor, bundle, bundle.SHA256)
	must(e)
	if restored.Operations != 36 {
		t.Fatal("download restore lost operations", restored.Operations)
	}
	before, e := repo.Snapshot(ctx, tenant, ledger.ID, at.AddDate(0, 0, 10))
	must(e)
	after, e := targetRepo.Snapshot(ctx, tenant, ledger.ID, at.AddDate(0, 0, 10))
	must(e)
	if !reflect.DeepEqual(before.Cash, after.Cash) || !reflect.DeepEqual(before.Positions, after.Positions) {
		t.Fatal("download round trip changed snapshot")
	}
	targetBundle, e := targetRepo.ExportInvestmentArchive(ctx, tenant, actor)
	must(e)
	_, e = archive.VerifyJournal(targetBundle.Data)
	must(e)
	must(archive.VerifyJournalAudit(targetBundle.Data))
	var artifacts int
	must(target.DB.QueryRow(ctx, `SELECT count(*) FROM investment_export_jobs`).Scan(&artifacts))
	if artifacts != 0 {
		t.Fatal("ephemeral exports copied into restored domain")
	}
	if bundle.Data.TenantID != tenant || bundle.Data.Excluded["investment_export_jobs"] == "" {
		t.Fatal("export scope/classification")
	}
	if _, e = repo.ReadExportChunk(ctx, tenant, actor, job.ID, ready.ByteSize); !errors.Is(e, d.ErrInvestmentInvalid) {
		t.Fatal("past EOF", e)
	}
	if _, e = repo.ReadExportChunk(ctx, tenant, member, job.ID, 0); !errors.Is(e, d.ErrInvestmentForbidden) {
		t.Fatal("member download", e)
	}
	// Download rechecks current role, including after successful generation.
	_, e = pool.DB.Exec(ctx, `UPDATE user_tenants SET role='member' WHERE tenant_id=$1 AND user_id=$2`, tenant, actor)
	must(e)
	if _, e = repo.ReadExportChunk(ctx, tenant, actor, job.ID, 0); !errors.Is(e, d.ErrInvestmentForbidden) {
		t.Fatal("revoked role download", e)
	}
	_, e = pool.DB.Exec(ctx, `UPDATE user_tenants SET role='owner' WHERE tenant_id=$1 AND user_id=$2`, tenant, actor)
	must(e)
	second, e := repo.StartExport(ctx, tenant, actor, uuid.NewString())
	must(e)
	if _, e = repo.StartExport(ctx, tenant, actor, uuid.NewString()); !errors.Is(e, d.ErrInvestmentLimit) {
		t.Fatal("tenant quota", e)
	}
	_, e = pool.DB.Exec(ctx, `UPDATE investment_export_jobs SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, job.ID)
	must(e)
	expired, e := repo.GetExport(ctx, tenant, actor, job.ID)
	must(e)
	if expired.State != "expired" {
		t.Fatal("expiry not visible")
	}
	if _, e = repo.ReadExportChunk(ctx, tenant, actor, job.ID, 0); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal("expired download", e)
	}
	worked, e = repo.ProcessExport(ctx)
	must(e)
	if !worked {
		t.Fatal("second job not processed")
	}
	var cleared bool
	must(pool.DB.QueryRow(ctx, `SELECT payload IS NULL FROM investment_export_jobs WHERE id=$1`, job.ID).Scan(&cleared))
	if !cleared {
		t.Fatal("expired private payload retained")
	}
	// Membership revoked while pending causes a safe terminal error, no artifact.
	_, e = pool.DB.Exec(ctx, `UPDATE investment_export_jobs SET state='pending',payload=NULL,byte_size=0,sha256='',expires_at=NULL WHERE id=$1`, second.ID)
	must(e)
	_, e = pool.DB.Exec(ctx, `UPDATE user_tenants SET role='member' WHERE tenant_id=$1 AND user_id=$2`, tenant, actor)
	must(e)
	worked, e = repo.ProcessExport(ctx)
	must(e)
	if !worked {
		t.Fatal("revoked job not claimed")
	}
	var state, code string
	must(pool.DB.QueryRow(ctx, `SELECT state,error_code,payload IS NULL FROM investment_export_jobs WHERE id=$1`, second.ID).Scan(&state, &code, &cleared))
	if state != "error" || code != "access_revoked" || !cleared {
		t.Fatal("revoked job published", state, code)
	}
	_, e = pool.DB.Exec(ctx, `UPDATE investment_export_jobs SET state='running',attempts=3,lease_token=gen_random_uuid(),lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, second.ID)
	must(e)
	worked, e = repo.ProcessExport(ctx)
	must(e)
	if worked {
		t.Fatal("exhausted job reclaimed")
	}
	must(pool.DB.QueryRow(ctx, `SELECT state,error_code,payload IS NULL FROM investment_export_jobs WHERE id=$1`, second.ID).Scan(&state, &code, &cleared))
	if state != "error" || code != "attempts_exhausted" || !cleared {
		t.Fatal("unbounded crash retries")
	}

}
