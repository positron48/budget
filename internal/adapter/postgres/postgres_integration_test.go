package postgres

import (
    "context"
    "os"
    "path/filepath"
    "sort"
    "strings"
    "testing"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
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
        if err != nil { t.Fatalf("read %s: %v", p, err) }
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
    pgC, err := tcpostgres.RunContainer(ctx,
        tcpostgres.WithDatabase("budget"),
        tcpostgres.WithUsername("budget"),
        tcpostgres.WithPassword("budget"),
    )
    if err != nil { t.Fatalf("start pg: %v", err) }
    t.Cleanup(func() { _ = pgC.Terminate(ctx) })

    connStr, err := pgC.ConnectionString(ctx, "sslmode=disable")
    if err != nil { t.Fatalf("conn string: %v", err) }
    cfg, err := pgxpool.ParseConfig(connStr)
    if err != nil { t.Fatalf("parse cfg: %v", err) }
    cfg.MaxConns = 5
    cfg.MinConns = 1
    cfg.MaxConnIdleTime = time.Minute
    pool, err := pgxpool.NewWithConfig(ctx, cfg)
    if err != nil { t.Fatalf("pgxpool: %v", err) }
    t.Cleanup(func() { pool.Close() })
    // wait for DB accepting connections
    var pingErr error
    for i := 0; i < 50; i++ {
        pingErr = pool.Ping(ctx)
        if pingErr == nil { break }
        time.Sleep(200 * time.Millisecond)
    }
    if pingErr != nil { t.Fatalf("db not ready: %v", pingErr) }
    applyMigrations(t, ctx, pool)
    return &Pool{DB: pool}, func() {}
}
+
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
    if err != nil { t.Fatalf("create: %v", err) }
    if _, err := repo.Update(ctx, cat.ID, "food2", nil, false, nil); err != nil { t.Fatalf("update: %v", err) }
    got, err := repo.Get(ctx, cat.ID)
    if err != nil || got.Code != "food2" || got.IsActive { t.Fatalf("get: %v %#v", err, got) }
    lst, err := repo.List(ctx, tenantID, "expense", true)
    if err != nil || len(lst) != 1 { t.Fatalf("list: %v %#v", err, lst) }
    if err := repo.Delete(ctx, cat.ID); err != nil { t.Fatalf("delete: %v", err) }
}


