package postgres

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/positron48/budget/internal/domain"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	"github.com/positron48/budget/internal/usecase/asset"
	"github.com/positron48/budget/migrations"
	"sync"
	"testing"
	"time"
)

func TestAssetsAccounting_PG(t *testing.T) {
	pool, _ := withPg(t)
	ctx := context.Background()
	repo := NewAssetRepo(pool)
	txRepo := NewTransactionRepo(pool)
	exRepo := NewCurrencyExchangeRepo(pool)
	var tenant, user, cat, foreign string
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Assets','RUB') RETURNING id`).Scan(&tenant))
	must(pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Foreign','RUB') RETURNING id`).Scan(&foreign))
	must(pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES($1,'test') RETURNING id`, fmt.Sprintf("assets-%d@example.com", time.Now().UnixNano())).Scan(&user))
	must(pool.DB.QueryRow(ctx, `INSERT INTO categories(tenant_id,kind,code) VALUES($1,'expense','asset-test') RETURNING id`, tenant).Scan(&cat))
	ctx = ctxutil.WithUserID(ctxutil.WithTenantID(ctx, tenant), user)
	svc := asset.NewService(repo, NewFxRepo(pool), NewTenantRepo(pool))
	opening := time.Now().Add(-48 * time.Hour).Truncate(time.Microsecond)
	create := func(name, kind string, amount int64) domain.AssetAccount {
		t.Helper()
		fixed := ""
		if kind == "investment" || kind == "deposit" {
			fixed = "RUB"
		}
		a, err := svc.CreateAccount(ctx, domain.AssetAccount{TenantID: tenant, Name: name, Kind: kind, FixedCurrencyCode: fixed}, []domain.AssetSnapshot{{TenantID: tenant, UserID: user, Amount: domain.Money{CurrencyCode: "RUB", MinorUnits: amount}, AsOf: opening}}, "")
		must(err)
		return a
	}
	bank := create("Bank", "bank", 100000)
	cash := create("Cash", "cash", 10000)
	investment := create("Broker", "investment", 900000)
	balance := func(id, code string) int64 {
		t.Helper()
		a, err := repo.GetAccount(ctx, tenant, id, time.Now())
		must(err)
		for _, b := range a.Balances {
			if b.Amount.CurrencyCode == code {
				return b.Amount.MinorUnits
			}
		}
		t.Fatalf("missing currency %s", code)
		return 0
	}
	assertBalance := func(id string, want int64) {
		t.Helper()
		if got := balance(id, "RUB"); got != want {
			t.Fatalf("balance %s = %d; want %d", id, got, want)
		}
	}
	expense := func(id string, amount int64, at time.Time, key string) (domain.Transaction, error) {
		return txRepo.Create(ctx, domain.Transaction{TenantID: tenant, UserID: user, CategoryID: cat, Type: domain.TransactionTypeExpense, Amount: domain.Money{CurrencyCode: "RUB", MinorUnits: amount}, BaseAmount: domain.Money{CurrencyCode: "RUB", MinorUnits: amount}, OccurredAt: at, AssetAccountID: id, RequestKey: key})
	}
	t.Run("transaction lifecycle and replay", func(t *testing.T) {
		at := opening.Add(time.Hour)
		first, err := expense(bank.ID, 1000, at, "expense-key")
		must(err)
		second, err := expense(bank.ID, 1000, at, "expense-key")
		must(err)
		if first.ID != second.ID {
			t.Fatal("idempotency created a second expense")
		}
		assertBalance(bank.ID, 99000)
		if _, err = expense(bank.ID, 2000, at, "expense-key"); !errors.Is(err, domain.ErrIdempotencyConflict) {
			t.Fatalf("reused key: %v", err)
		}
		first.Amount.MinorUnits = 2000
		first.AssetAccountID = cash.ID
		first, err = txRepo.Update(ctx, first)
		must(err)
		assertBalance(bank.ID, 100000)
		assertBalance(cash.ID, 8000)
		bad := first
		bad.Amount.CurrencyCode = "USD"
		if _, err = txRepo.Update(ctx, bad); !errors.Is(err, domain.ErrAssetPrecondition) {
			t.Fatalf("uninitialized currency: %v", err)
		}
		assertBalance(cash.ID, 8000)
		must(txRepo.Delete(ctx, first.ID))
		assertBalance(cash.ID, 10000)
		_, err = expense("", 1234, at, "")
		must(err)
		assertBalance(bank.ID, 100000)
	})
	t.Run("checkpoints and time boundaries", func(t *testing.T) {
		checkpoint := time.Now().Add(-12 * time.Hour).Truncate(time.Microsecond)
		snap, err := svc.CreateSnapshot(ctx, domain.AssetSnapshot{TenantID: tenant, AccountID: bank.ID, UserID: user, Amount: domain.Money{CurrencyCode: "RUB", MinorUnits: 50000}, Kind: "reconciliation", AsOf: checkpoint}, "")
		must(err)
		old, err := expense(bank.ID, 9999, checkpoint.Add(-time.Hour), "")
		must(err)
		assertBalance(bank.ID, 50000)
		current, err := expense(bank.ID, 2000, checkpoint.Add(time.Hour), "")
		must(err)
		assertBalance(bank.ID, 48000)
		same, err := expense(bank.ID, 777, checkpoint, "")
		must(err)
		history, _, err := repo.ListAccountHistory(ctx, tenant, bank.ID, 1, 25)
		must(err)
		found := false
		for _, h := range history {
			if h.SourceID == snap.ID {
				found = true
				if h.CalculatedAmount == nil || h.CalculatedAmount.MinorUnits != 89224 {
					t.Fatalf("calculated checkpoint = %+v", h.CalculatedAmount)
				}
			}
		}
		if !found {
			t.Fatal("checkpoint missing in history")
		}
		assertBalance(bank.ID, 48000)
		future, err := expense(bank.ID, 1000, time.Now().Add(time.Hour), "")
		must(err)
		assertBalance(bank.ID, 48000)
		old.OccurredAt = checkpoint.Add(2 * time.Hour)
		_, err = txRepo.Update(ctx, old)
		must(err)
		assertBalance(bank.ID, 38001)
		must(txRepo.Delete(ctx, old.ID))
		assertBalance(bank.ID, 48000)
		snap.Amount.MinorUnits = 40000
		updated, err := svc.UpdateSnapshot(ctx, snap)
		must(err)
		assertBalance(bank.ID, 38000)
		if _, err = svc.UpdateSnapshot(ctx, snap); !errors.Is(err, domain.ErrAssetConflict) {
			t.Fatalf("stale snapshot: %v", err)
		}
		must(svc.DeleteSnapshot(ctx, tenant, updated.ID, updated.Version))
		assertBalance(bank.ID, 97223)
		must(txRepo.Delete(ctx, current.ID))
		must(txRepo.Delete(ctx, same.ID))
		must(txRepo.Delete(ctx, future.ID))
		assertBalance(bank.ID, 100000)
	})
	t.Run("transfers valuation exchanges and atomic rollback", func(t *testing.T) {
		transfer, err := svc.CreateTransfer(ctx, domain.AssetTransfer{TenantID: tenant, UserID: user, FromAccountID: bank.ID, ToAccountID: investment.ID, FromAmount: domain.Money{CurrencyCode: "RUB", MinorUnits: 10000}, ToAmount: domain.Money{CurrencyCode: "RUB", MinorUnits: 10000}, OccurredAt: time.Now().Add(-time.Hour), RequestKey: "transfer-key"})
		must(err)
		assertBalance(bank.ID, 90000)
		assertBalance(investment.ID, 910000)
		replay := transfer
		replay.ID = ""
		replay.Version = 0
		replay.CreatedAt = time.Time{}
		replay.RequestKey = "transfer-key"
		_, err = svc.CreateTransfer(ctx, replay)
		must(err)
		assertBalance(bank.ID, 90000)
		valuation, err := svc.CreateSnapshot(ctx, domain.AssetSnapshot{TenantID: tenant, AccountID: investment.ID, UserID: user, Kind: "valuation", Amount: domain.Money{CurrencyCode: "RUB", MinorUnits: 930000}, AsOf: time.Now().Add(-30 * time.Minute)}, "")
		must(err)
		assertBalance(investment.ID, 930000)
		must(svc.DeleteSnapshot(ctx, tenant, valuation.ID, valuation.Version))
		assertBalance(investment.ID, 910000)
		transfer.FromAmount.MinorUnits = 20000
		transfer.ToAmount.MinorUnits = 20000
		transfer, err = svc.UpdateTransfer(ctx, transfer)
		must(err)
		assertBalance(bank.ID, 80000)
		assertBalance(investment.ID, 920000)
		must(svc.DeleteTransfer(ctx, tenant, transfer.ID, transfer.Version))
		assertBalance(bank.ID, 100000)
		assertBalance(investment.ID, 900000)
		_, err = svc.CreateSnapshot(ctx, domain.AssetSnapshot{TenantID: tenant, AccountID: cash.ID, UserID: user, Kind: "opening", Amount: domain.Money{CurrencyCode: "USD"}, AsOf: opening}, "")
		must(err)
		exchange, err := exRepo.Create(ctx, domain.CurrencyExchange{TenantID: tenant, UserID: user, FromAmount: domain.Money{CurrencyCode: "RUB", MinorUnits: 9000}, ToAmount: domain.Money{CurrencyCode: "USD", MinorUnits: 100}, FromAssetAccountID: cash.ID, ToAssetAccountID: cash.ID, OccurredAt: opening.Add(time.Hour)})
		must(err)
		assertBalance(cash.ID, 1000)
		if balance(cash.ID, "USD") != 100 {
			t.Fatal("exchange credit missing")
		}
		exchange.FromAmount.MinorUnits = 4500
		exchange.ToAmount.MinorUnits = 50
		exchange.PreserveAssetAccounts = true
		exchange.FromAssetAccountID = ""
		exchange.ToAssetAccountID = ""
		exchange, err = exRepo.Update(ctx, exchange)
		must(err)
		assertBalance(cash.ID, 5500)
		must(exRepo.Delete(ctx, tenant, exchange.ID))
		assertBalance(cash.ID, 10000)
		if balance(cash.ID, "USD") != 0 {
			t.Fatal("exchange delete left credit")
		}
		// Fail the second ledger insert after the first one has succeeded.
		_, err = pool.DB.Exec(ctx, `CREATE FUNCTION fail_asset_credit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.leg='to' THEN RAISE EXCEPTION 'injected failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER fail_asset_credit BEFORE INSERT ON asset_movements FOR EACH ROW EXECUTE FUNCTION fail_asset_credit()`)
		must(err)
		_, err = svc.CreateTransfer(ctx, domain.AssetTransfer{TenantID: tenant, UserID: user, FromAccountID: bank.ID, ToAccountID: cash.ID, FromAmount: domain.Money{CurrencyCode: "RUB", MinorUnits: 1000}, ToAmount: domain.Money{CurrencyCode: "RUB", MinorUnits: 1000}, OccurredAt: time.Now()})
		if err == nil {
			t.Fatal("injected failure ignored")
		}
		assertBalance(bank.ID, 100000)
		assertBalance(cash.ID, 10000)
		_, err = pool.DB.Exec(ctx, `DROP TRIGGER fail_asset_credit ON asset_movements; DROP FUNCTION fail_asset_credit()`)
		must(err)
		history, total, err := repo.ListAccountHistory(ctx, tenant, bank.ID, 1, 25)
		must(err)
		if total == 0 || len(history) == 0 {
			t.Fatal("history is empty")
		}
	})
	t.Run("parallel expenses negative balances and tenant isolation", func(t *testing.T) {
		var wg sync.WaitGroup
		failures := make(chan error, 20)
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); _, err := expense(bank.ID, 100, time.Now(), ""); failures <- err }()
		}
		wg.Wait()
		close(failures)
		for err := range failures {
			must(err)
		}
		assertBalance(bank.ID, 98000)
		_, err := expense(cash.ID, 20000, time.Now(), "")
		must(err)
		assertBalance(cash.ID, -10000)
		foreignCtx := ctxutil.WithTenantID(ctx, foreign)
		if _, err = repo.GetAccount(foreignCtx, foreign, bank.ID, time.Now()); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("cross-tenant account read: %v", err)
		}
		first, err := expense(bank.ID, 10, time.Now(), "")
		must(err)
		if _, err = txRepo.Get(foreignCtx, first.ID); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("cross-tenant transaction read: %v", err)
		}
		if err = txRepo.Delete(foreignCtx, first.ID); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("cross-tenant delete: %v", err)
		}
		foreignAccount, err := repo.CreateAccount(foreignCtx, domain.AssetAccount{TenantID: foreign, Name: "Foreign", Kind: "cash"}, []domain.AssetSnapshot{{TenantID: foreign, UserID: user, Kind: "opening", Amount: domain.Money{CurrencyCode: "RUB"}, AsOf: opening}}, "")
		must(err)
		if _, err = expense(foreignAccount.ID, 100, time.Now(), ""); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("cross-tenant linking: %v", err)
		}
		if _, err = repo.SetArchived(ctx, tenant, cash.ID, cash.Version, true); !errors.Is(err, domain.ErrAssetPrecondition) {
			t.Fatalf("nonzero archive: %v", err)
		}
	})
	t.Run("archive restore delete and protected opening", func(t *testing.T) {
		empty := create("Empty", "cash", 0)
		archived, err := svc.SetArchived(ctx, tenant, empty.ID, empty.Version, true)
		must(err)
		if !archived.Archived {
			t.Fatal("archive state missing")
		}
		if _, err = expense(empty.ID, 1, time.Now(), ""); !errors.Is(err, domain.ErrAssetPrecondition) {
			t.Fatalf("archived write: %v", err)
		}
		restored, err := svc.SetArchived(ctx, tenant, empty.ID, archived.Version, false)
		must(err)
		if restored.Archived {
			t.Fatal("restore state missing")
		}
		if err = svc.DeleteEmptyAccount(ctx, tenant, empty.ID, restored.Version); !errors.Is(err, domain.ErrAssetPrecondition) {
			t.Fatalf("delete with history: %v", err)
		}
		h, _, err := repo.ListAccountHistory(ctx, tenant, empty.ID, 1, 25)
		must(err)
		must(svc.DeleteSnapshot(ctx, tenant, h[0].Snapshot.ID, h[0].Snapshot.Version))
		must(svc.DeleteEmptyAccount(ctx, tenant, empty.ID, restored.Version))
		h, _, err = repo.ListAccountHistory(ctx, tenant, bank.ID, 1, 100)
		must(err)
		for _, v := range h {
			if v.Snapshot != nil && v.Snapshot.Kind == "opening" {
				if err = svc.DeleteSnapshot(ctx, tenant, v.Snapshot.ID, v.Snapshot.Version); !errors.Is(err, domain.ErrAssetPrecondition) {
					t.Fatalf("last opening delete: %v", err)
				}
			}
		}
	})
	t.Run("migration can be reapplied", func(t *testing.T) { applyMigrations(t, ctx, pool.DB); assertBalance(cash.ID, -10000) })
}

func TestAssetsMigrationPreservesLegacy_PG(t *testing.T) {
	pool, _ := withPg(t)
	ctx := context.Background()
	down, err := migrations.FS.ReadFile("0010_assets.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.DB.Exec(ctx, string(down)); err != nil {
		t.Fatal(err)
	}
	var tenant, user, category, id string
	for _, item := range []struct {
		sql  string
		args []any
		out  *string
	}{
		{`INSERT INTO tenants(name,default_currency_code) VALUES('Legacy','RUB') RETURNING id`, nil, &tenant},
		{`INSERT INTO users(email,password_hash) VALUES('legacy-assets@example.com','test') RETURNING id`, nil, &user},
	} {
		if err = pool.DB.QueryRow(ctx, item.sql, item.args...).Scan(item.out); err != nil {
			t.Fatal(err)
		}
	}
	if err = pool.DB.QueryRow(ctx, `INSERT INTO categories(tenant_id,kind,code) VALUES($1,'expense','legacy') RETURNING id`, tenant).Scan(&category); err != nil {
		t.Fatal(err)
	}
	if err = pool.DB.QueryRow(ctx, `INSERT INTO transactions(tenant_id,user_id,category_id,type,amount_numeric,currency_code,base_amount_numeric,base_currency_code,occurred_at,comment) VALUES($1,$2,$3,'expense',123.45,'RUB',123.45,'RUB',now(),'Legacy expense') RETURNING id`, tenant, user, category).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.DB.Exec(ctx, `INSERT INTO currency_exchanges(tenant_id,user_id,from_amount_numeric,from_currency_code,to_amount_numeric,to_currency_code,occurred_at) VALUES($1,$2,100,'USD',9000,'RUB',now())`, tenant, user); err != nil {
		t.Fatal(err)
	}
	up, err := migrations.FS.ReadFile("0010_assets.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.DB.Exec(ctx, string(up)); err != nil {
		t.Fatal(err)
	}
	tx, err := NewTransactionRepo(pool).Get(ctxutil.WithTenantID(ctx, tenant), id)
	if err != nil {
		t.Fatal(err)
	}
	if tx.Amount.MinorUnits != 12345 || tx.Comment != "Legacy expense" || tx.AssetAccountID != "" {
		t.Fatalf("legacy transaction changed: %+v", tx)
	}
	exchanges, count, err := NewCurrencyExchangeRepo(pool).List(ctx, tenant, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 || exchanges[0].FromAssetAccountID != "" || exchanges[0].ToAssetAccountID != "" || exchanges[0].ToAmount.MinorUnits != 900000 {
		t.Fatalf("legacy exchanges changed: %+v", exchanges)
	}
}
