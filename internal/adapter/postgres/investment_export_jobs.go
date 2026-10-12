package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	archive "github.com/positron48/budget/internal/usecase/investmentarchive"
	exp "github.com/positron48/budget/internal/usecase/investmentexport"
)

// Only the worker uses this scope; API methods independently authorize tenants.
func NewInvestmentExportWorker(pool *Pool, tenantIDs []string) exp.Processor {
	return &InvestmentRepo{pool: pool, exportTenantIDs: append([]string(nil), tenantIDs...)}
}

func exportUUID(v string) bool { u, e := uuid.Parse(v); return e == nil && u.String() == v }
func exportPermission(ctx context.Context, db investmentNAVReader, tenant, actor string) error {
	var allowed bool
	if e := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_tenants WHERE tenant_id=$1 AND user_id=$2 AND role IN ('owner','admin'))`, tenant, actor).Scan(&allowed); e != nil {
		return e
	}
	if !allowed {
		return d.ErrInvestmentForbidden
	}
	return nil
}

const exportJobColumns = `id,CASE WHEN expires_at<=clock_timestamp() THEN 'expired' ELSE state END,sha256,byte_size,error_code,created_at,completed_at,expires_at`

func scanExportJob(row pgx.Row) (exp.Job, error) {
	var j exp.Job
	e := row.Scan(&j.ID, &j.State, &j.SHA256, &j.ByteSize, &j.ErrorCode, &j.CreatedAt, &j.CompletedAt, &j.ExpiresAt)
	if errors.Is(e, pgx.ErrNoRows) {
		e = d.ErrInvestmentNotFound
	}
	return j, e
}
func (r *InvestmentRepo) StartExport(ctx context.Context, tenant, actor, key string) (exp.Job, error) {
	if !exportUUID(tenant) || !exportUUID(actor) || !exportUUID(key) {
		return exp.Job{}, d.ErrInvestmentInvalid
	}
	tx, e := r.pool.DB.Begin(ctx)
	if e != nil {
		return exp.Job{}, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if e = exportPermission(ctx, tx, tenant, actor); e != nil {
		return exp.Job{}, e
	}
	// Serialize quota checks for this tenant, including different actors.
	var id string
	if e = tx.QueryRow(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, tenant).Scan(&id); e != nil {
		return exp.Job{}, e
	}
	j, e := scanExportJob(tx.QueryRow(ctx, `SELECT `+exportJobColumns+` FROM investment_export_jobs WHERE tenant_id=$1 AND actor_id=$2 AND request_key=$3`, tenant, actor, key))
	if e == nil {
		return j, tx.Commit(ctx)
	}
	if !errors.Is(e, d.ErrInvestmentNotFound) {
		return j, e
	}
	var count int
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM investment_export_jobs WHERE tenant_id=$1 AND (state IN ('pending','running') OR state='ready' AND expires_at>clock_timestamp())`, tenant).Scan(&count); e != nil {
		return j, e
	}
	if count >= 2 {
		return j, d.ErrInvestmentLimit
	}
	j, e = scanExportJob(tx.QueryRow(ctx, `INSERT INTO investment_export_jobs(tenant_id,actor_id,request_key) VALUES($1,$2,$3) RETURNING `+exportJobColumns, tenant, actor, key))
	if e != nil {
		return j, e
	}
	return j, tx.Commit(ctx)
}
func (r *InvestmentRepo) GetExport(ctx context.Context, tenant, actor, id string) (exp.Job, error) {
	if !exportUUID(tenant) || !exportUUID(actor) || !exportUUID(id) {
		return exp.Job{}, d.ErrInvestmentInvalid
	}
	tx, e := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return exp.Job{}, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if e = exportPermission(ctx, tx, tenant, actor); e != nil {
		return exp.Job{}, e
	}
	j, e := scanExportJob(tx.QueryRow(ctx, `SELECT `+exportJobColumns+` FROM investment_export_jobs WHERE tenant_id=$1 AND actor_id=$2 AND id=$3`, tenant, actor, id))
	if e != nil {
		return j, e
	}
	return j, tx.Commit(ctx)
}
func (r *InvestmentRepo) ReadExportChunk(ctx context.Context, tenant, actor, id string, offset int64) (exp.Chunk, error) {
	var c exp.Chunk
	if !exportUUID(tenant) || !exportUUID(actor) || !exportUUID(id) || offset < 0 || offset > archive.MaxBytes {
		return c, d.ErrInvestmentInvalid
	}
	tx, e := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return c, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if e = exportPermission(ctx, tx, tenant, actor); e != nil {
		return c, e
	}
	j, e := scanExportJob(tx.QueryRow(ctx, `SELECT `+exportJobColumns+` FROM investment_export_jobs WHERE tenant_id=$1 AND actor_id=$2 AND id=$3`, tenant, actor, id))
	if e != nil {
		return c, e
	}
	if j.State != "ready" {
		return c, d.ErrInvestmentPrecondition
	}
	if offset >= j.ByteSize {
		return c, d.ErrInvestmentInvalid
	}
	c.Offset = offset
	c.Total = j.ByteSize
	c.SHA256 = j.SHA256
	e = tx.QueryRow(ctx, `SELECT substring(payload FROM $4::integer+1 FOR $5::integer) FROM investment_export_jobs WHERE tenant_id=$1 AND actor_id=$2 AND id=$3`, tenant, actor, id, offset, exp.ChunkBytes).Scan(&c.Data)
	if e != nil {
		return c, e
	}
	return c, tx.Commit(ctx)
}

// ProcessExport uses a fenced five-minute lease. A crash is retried at most
// three times; revoked membership cannot publish or download a private artifact.
func (r *InvestmentRepo) ProcessExport(ctx context.Context) (bool, error) {
	if _, e := r.pool.DB.Exec(ctx, `UPDATE investment_export_jobs SET state='expired',payload=NULL WHERE expires_at<=clock_timestamp() AND state='ready' AND payload IS NOT NULL`); e != nil {
		return false, e
	}
	if _, e := r.pool.DB.Exec(ctx, `UPDATE investment_export_jobs SET state='error',error_code='attempts_exhausted',lease_token=NULL,lease_until=NULL WHERE state='running' AND lease_until<clock_timestamp() AND attempts>=3 AND (COALESCE(cardinality($1::uuid[]),0)=0 OR tenant_id=ANY($1::uuid[]))`, r.exportTenantIDs); e != nil {
		return false, e
	}
	token := uuid.NewString()
	var id, tenant, actor string
	e := r.pool.DB.QueryRow(ctx, `WITH candidate AS (SELECT id FROM investment_export_jobs WHERE (state='pending' OR state='running' AND lease_until<clock_timestamp()) AND attempts<3 AND (COALESCE(cardinality($2::uuid[]),0)=0 OR tenant_id=ANY($2::uuid[])) ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1) UPDATE investment_export_jobs j SET state='running',lease_token=$1,lease_until=clock_timestamp()+interval '5 minutes',attempts=attempts+1 FROM candidate c WHERE j.id=c.id RETURNING j.id,j.tenant_id,j.actor_id`, token, r.exportTenantIDs).Scan(&id, &tenant, &actor)
	if errors.Is(e, pgx.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	work, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	bundle, exportErr := r.ExportInvestmentArchive(work, tenant, actor)
	var payload []byte
	if exportErr == nil {
		payload, exportErr = json.Marshal(bundle)
		if len(payload) > archive.MaxBytes {
			exportErr = d.ErrInvestmentLimit
		}
	}
	if ctx.Err() != nil {
		return true, ctx.Err()
	}
	if exportErr != nil {
		code := "export_failed"
		if errors.Is(exportErr, d.ErrInvestmentLimit) {
			code = "limit_exceeded"
		}
		if errors.Is(exportErr, d.ErrInvestmentForbidden) {
			code = "access_revoked"
		}
		_, e = r.pool.DB.Exec(ctx, `UPDATE investment_export_jobs SET state='error',error_code=$3,completed_at=clock_timestamp(),lease_token=NULL,lease_until=NULL WHERE id=$1 AND lease_token=$2 AND state='running'`, id, token, code)
		return true, e
	}
	sum := sha256.Sum256(payload)
	// Recheck permission at publication; the archive snapshot may predate revocation.
	tag, e := r.pool.DB.Exec(ctx, `UPDATE investment_export_jobs SET state='ready',payload=$3,byte_size=$4,sha256=$5,completed_at=clock_timestamp(),expires_at=clock_timestamp()+interval '24 hours',lease_token=NULL,lease_until=NULL WHERE id=$1 AND lease_token=$2 AND state='running' AND lease_until>clock_timestamp() AND EXISTS(SELECT 1 FROM user_tenants WHERE tenant_id=$6 AND user_id=$7 AND role IN ('owner','admin'))`, id, token, payload, len(payload), hex.EncodeToString(sum[:]), tenant, actor)
	if e != nil {
		return true, e
	}
	if tag.RowsAffected() != 1 {
		_, e = r.pool.DB.Exec(ctx, `UPDATE investment_export_jobs SET state='error',error_code='access_revoked',completed_at=clock_timestamp(),lease_token=NULL,lease_until=NULL WHERE id=$1 AND lease_token=$2 AND state='running'`, id, token)
	}
	return true, e
}
