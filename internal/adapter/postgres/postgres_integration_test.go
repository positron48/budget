package postgres

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/positron48/budget/internal/domain"
	txusecase "github.com/positron48/budget/internal/usecase/transaction"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func applyMigrations(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	// locate project root by walking up until migrations dir is found
	dir, _ := os.Getwd()
	var migPath string
	for i := 0; i < 6; i++ { // limit climb
		p := filepath.Join(dir, "migrations")
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			migPath = p
			break
		}
		dir = filepath.Dir(dir)
	}
	if migPath == "" {
		t.Fatalf("migrations dir not found from %s", dir)
	}
	entries, err := os.ReadDir(migPath)
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	var ups []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".up.sql") {
			ups = append(ups, filepath.Join(migPath, name))
		}
	}
	sort.Strings(ups)
	for _, p := range ups {
		sqlb, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		if _, err := db.Exec(ctx, string(sqlb)); err != nil {
			t.Fatalf("apply %s: %v", p, err)
		}
	}
}

func withPg(t *testing.T) (*Pool, func()) {
	if os.Getenv("PG_INTEGRATION") != "1" {
		t.Skip("set PG_INTEGRATION=1 to run postgres integration tests")
	}
	t.Helper()
	ctx := context.Background()
	pgC, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithInitScripts(),
		tcpostgres.WithDatabase("budget"),
		tcpostgres.WithUsername("budget"),
		tcpostgres.WithPassword("budget"),
	)
	if err != nil {
		t.Fatalf("start pg: %v", err)
	}
	t.Cleanup(func() { _ = pgC.Terminate(ctx) })

	connStr, err := pgC.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("conn string: %v", err)
	}
	cfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		t.Fatalf("parse cfg: %v", err)
	}
	cfg.MaxConns = 5
	cfg.MinConns = 1
	cfg.MaxConnIdleTime = time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("pgxpool: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	// wait for DB accepting connections
	var pingErr error
	for i := 0; i < 50; i++ {
		pingErr = pool.Ping(ctx)
		if pingErr == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if pingErr != nil {
		t.Fatalf("db not ready: %v", pingErr)
	}
	applyMigrations(t, ctx, pool)
	return &Pool{DB: pool}, func() {}
}

func TestCategoryRepo_CRUD_PG(t *testing.T) {
	pool, _ := withPg(t)
	// bootstrap tenant row
	ctx := context.Background()
	var tenantID string
	if err := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name, default_currency_code) VALUES ($1,$2) RETURNING id`, "Home", "USD").Scan(&tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	repo := NewCategoryRepo(pool)
	cat, err := repo.Create(ctx, tenantID, "expense", "food", nil, true, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := repo.Update(ctx, cat.ID, "food2", nil, false, nil); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := repo.Get(ctx, cat.ID)
	if err != nil || got.Code != "food2" || got.IsActive {
		t.Fatalf("get: %v %#v", err, got)
	}
	lst, err := repo.List(ctx, tenantID, "expense", true)
	if err != nil || len(lst) != 1 {
		t.Fatalf("list: %v %#v", err, lst)
	}
	if err := repo.Delete(ctx, cat.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestFxRepo_CRUD_PG(t *testing.T) {
	pool, _ := withPg(t)
	ctx := context.Background()
	repo := NewFxRepo(pool)
	asOf := time.Now().AddDate(0, 0, -1)
	// upsert two rates for USD/EUR on two days
	if _, err := repo.UpsertRate(ctx, "USD", "EUR", "1.1000", asOf, "prov"); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if _, err := repo.UpsertRate(ctx, "USD", "EUR", "1.2000", time.Now(), "prov"); err != nil {
		t.Fatalf("upsert2: %v", err)
	}
	// get exact latest
	rate, prov, err := repo.GetRateAsOf(ctx, "USD", "EUR", time.Now())
	if err != nil || !strings.HasPrefix(rate, "1.2") || prov != "prov" {
		t.Fatalf("get: %v %s %s", err, rate, prov)
	}
	// batch
	rows, err := repo.BatchGetRates(ctx, []string{"USD"}, "EUR", time.Now())
	if err != nil || len(rows) != 1 {
		t.Fatalf("batch: %v %#v", err, rows)
	}
}

func TestTransactionRepo_CRUD_PG(t *testing.T) {
	pool, _ := withPg(t)
	ctx := context.Background()
	// seed tenant, user, category
	var tenantID, userID, catID string
	if err := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name, default_currency_code) VALUES ($1,$2) RETURNING id`, "Home", "USD").Scan(&tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	if err := pool.DB.QueryRow(ctx, `INSERT INTO users(email, password_hash) VALUES ($1,$2) RETURNING id`, fmt.Sprintf("u_%d@example.com", time.Now().UnixNano()), "h").Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := pool.DB.QueryRow(ctx, `INSERT INTO categories(tenant_id, kind, code, is_active) VALUES ($1,$2,$3,$4) RETURNING id`, tenantID, "expense", "food", true).Scan(&catID); err != nil {
		t.Fatalf("seed cat: %v", err)
	}
	repo := NewTransactionRepo(pool)
	// create tx in USD with fx=1.0 to exercise fx path and avoid NULL type issues
	now := time.Now()
	tx := domain.Transaction{TenantID: tenantID, UserID: userID, CategoryID: catID, Type: domain.TransactionTypeExpense, Amount: domain.Money{CurrencyCode: "USD", MinorUnits: 1234}, BaseAmount: domain.Money{CurrencyCode: "USD", MinorUnits: 1234}, OccurredAt: now, Fx: &domain.FxInfo{FromCurrency: "USD", ToCurrency: "USD", RateDecimal: "1.0000", Provider: "prov", AsOf: now}}
	created, err := repo.Create(ctx, tx)
	if err != nil || created.ID == "" {
		t.Fatalf("create: %v %#v", err, created)
	}
	// get
	got, err := repo.Get(ctx, created.ID)
	if err != nil || got.Amount.MinorUnits != 1234 {
		t.Fatalf("get: %v %#v", err, got)
	}
	// update amount
	got.Amount.MinorUnits = 1500
	got.BaseAmount.MinorUnits = 1500
	got.Fx = &domain.FxInfo{FromCurrency: "USD", ToCurrency: "USD", RateDecimal: "1.0000", Provider: "prov", AsOf: now}
	up, err := repo.Update(ctx, got)
	if err != nil || up.BaseAmount.MinorUnits != 1500 {
		t.Fatalf("update: %v %#v", err, up)
	}
	// list
	lst, total, err := repo.List(ctx, tenantID, txusecase.ListFilter{Page: 1, PageSize: 10})
	if err != nil || total < 1 || len(lst) < 1 {
		t.Fatalf("list: %v total=%d len=%d", err, total, len(lst))
	}
	// delete
	if err := repo.Delete(ctx, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
}
