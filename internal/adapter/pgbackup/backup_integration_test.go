package pgbackup_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/positron48/budget/internal/adapter/pgbackup"
	"github.com/positron48/budget/internal/adapter/postgres"
	d "github.com/positron48/budget/internal/domain"
	"github.com/positron48/budget/internal/pkg/ctxutil"
	"github.com/positron48/budget/internal/usecase/asset"
	inv "github.com/positron48/budget/internal/usecase/investment"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type hookedTools struct {
	pgbackup.NativeTools
	beforeDump   func()
	afterRestore func(context.Context, string)
}

func (t *hookedTools) Dump(ctx context.Context, dsn, snapshot string, schema bool, out io.Writer) error {
	if !schema && t.beforeDump != nil {
		t.beforeDump()
		t.beforeDump = nil
	}
	return t.NativeTools.Dump(ctx, dsn, snapshot, schema, out)
}
func (t *hookedTools) Restore(ctx context.Context, dsn string, in io.Reader) error {
	if e := t.NativeTools.Restore(ctx, dsn, in); e != nil {
		return e
	}
	if t.afterRestore != nil {
		t.afterRestore(ctx, dsn)
		t.afterRestore = nil
	}
	return nil
}
func TestBudgetFullBackupRestoresJointDocumentsAndSnapshot(t *testing.T) {
	if os.Getenv("PG_INTEGRATION") != "1" {
		t.Skip("set PG_INTEGRATION=1")
	}
	image := os.Getenv("BUDGET_BACKUP_TEST_IMAGE")
	if image == "" {
		image = "postgres:16-alpine"
	}
	switch image {
	case "postgres:15", "postgres:15-alpine", "postgres:16", "postgres:16-alpine":
	default:
		t.Fatal("unsupported disposable PostgreSQL test image")
	}
	ctx := context.Background()
	container, e := tcpostgres.Run(ctx, image, tcpostgres.WithDatabase("budget"), tcpostgres.WithUsername("budget"), tcpostgres.WithPassword("budget"), tcpostgres.BasicWaitStrategies())
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = container.Terminate(ctx) }()
	dsn, e := container.ConnectionString(ctx, "sslmode=disable")
	if e != nil {
		t.Fatal(e)
	}
	if e = postgres.RunMigrations(dsn); e != nil {
		t.Fatal(e)
	}
	source, e := postgres.NewPool(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer source.Close()
	// Native pg_dump/pg_restore are provided by the same PostgreSQL image. This
	// test wrapper uses local socket trust; no password or DSN enters argv.
	dockerPath, e := exec.LookPath("docker")
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	makeTool := func(name string) string {
		p := filepath.Join(dir, name)
		script := "#!/bin/sh\nexec " + dockerPath + " exec -i -e PGHOST=/var/run/postgresql -e PGPORT=5432 -e PGUSER -e PGDATABASE -e PGSSLMODE=disable " + container.GetContainerID() + " " + name + " \"$@\"\n"
		if e := os.WriteFile(p, []byte(script), 0700); e != nil {
			t.Fatal(e)
		}
		return p
	}
	tools := &hookedTools{NativeTools: pgbackup.NativeTools{DumpPath: makeTool("pg_dump"), RestorePath: makeTool("pg_restore")}}
	var tenant, user, foreign string
	for _, dst := range []*string{&tenant, &foreign} {
		if e = source.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES($1,'RUB') RETURNING id`, uuid.NewString()).Scan(dst); e != nil {
			t.Fatal(e)
		}
	}
	if e = source.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash,name) VALUES($1,'fixture-password-hash','Before snapshot') RETURNING id`, uuid.NewString()+"@example.test").Scan(&user); e != nil {
		t.Fatal(e)
	}
	if _, e = source.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner');`, tenant, user); e != nil {
		t.Fatal(e)
	}
	if _, e = source.DB.Exec(ctx, `INSERT INTO refresh_tokens(user_id,token_hash,expires_at) VALUES($1,'fixture-refresh-hash',now()+interval '1 day')`, user); e != nil {
		t.Fatal(e)
	}
	ctx = ctxutil.WithUserID(ctxutil.WithTenantID(ctx, tenant), user)
	market := postgres.NewMarketDataRepo(source)
	marketScope := md.Scope{TenantID: tenant, Provider: "tinvest", AccessScope: "backup-fixture"}
	if e = market.ConfigureSource(ctx, marketScope, "Synthetic market", true); e != nil {
		t.Fatal(e)
	}
	marketDay := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, e = market.RequestRefresh(ctx, marketScope, md.JobRequest{InstrumentID: uuid.NewString(), Kind: "daily_candle", From: marketDay, To: marketDay.AddDate(0, 0, 1)}); e != nil {
		t.Fatal(e)
	}
	// A non-default demand flag must survive the native dump, not just schema.
	if _, e = source.DB.Exec(ctx, `UPDATE market_data_subscriptions SET benchmark_demand=true,fx_demand=true WHERE tenant_id=$1 AND access_scope='backup-fixture'`, tenant); e != nil {
		t.Fatal(e)
	}
	fxPolicy := md.FXPolicy{Key: "market", Mode: "market", Source: "tinvest", AccessScope: "backup-fixture", SourceReference: "Synthetic provider", TimeConvention: "Reviewed candle end", ValueConvention: "Historical nominal", ValidityConvention: "Reviewed three days", Series: []md.FXSeries{{InstrumentID: uuid.NewString(), FromCurrency: "USD", ToCurrency: "RUB", From: marketDay, To: marketDay.AddDate(1, 0, 0), RateKind: "exchange_spot", SourceKind: "exchange", PriceUnit: "provider_unspecified", Adjustment: "provider_unspecified", Multiplier: "1", Divisor: "1000", EffectiveOffsetSeconds: 86400, AvailableOffsetSeconds: 86400, ValidSeconds: 3 * 86400, RequirePositiveVolume: true, SourceReference: "Synthetic terms", Evidence: "Synthetic historical source review"}}}
	if _, e = market.RegisterFXPolicy(ctx, tenant, user, fxPolicy, 0); e != nil {
		t.Fatal(e)
	}
	officialPolicy := md.FXPolicy{Key: "cbr", Mode: "cbr", Source: "cbr", AccessScope: "public", SourceReference: "https://www.cbr.ru/currency_base/", TimeConvention: "Effective Moscow date", ValueConvention: "RUB per one currency unit", ValidityConvention: "Verified calendar day", Official: &md.OfficialFXPolicy{From: time.Date(2019, 10, 1, 0, 0, 0, 0, time.UTC), Convention: "effective_moscow_date_v1"}}
	if _, e = market.RegisterFXPolicy(ctx, tenant, user, officialPolicy, 0); e != nil {
		t.Fatal(e)
	}
	dividendRequest := md.JobRequest{InstrumentID: uuid.NewString(), Kind: "dividend_facts", From: marketDay, To: marketDay.AddDate(0, 0, 30)}
	if _, e = market.Enqueue(ctx, marketScope, dividendRequest); e != nil {
		t.Fatal(e)
	}
	for range 2 {
		job, err := market.Claim(ctx, marketScope, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if job.Kind == "dividend_facts" {
			err = market.CompleteDividendFacts(ctx, job, md.DividendFacts{Instrument: md.Instrument{ID: job.InstrumentID, Ticker: "FIXTURE", Name: "Synthetic issuer", Kind: "share", Currency: "RUB", Detailed: true}, From: job.From, To: job.To, AsOf: marketDay, Dividends: []md.Dividend{{RecordDate: marketDay.AddDate(0, 0, 2), DividendType: "Cancelled", DividendNet: &md.BondAmount{Amount: "0", Currency: "RUB"}}}})
		} else {
			err = market.Complete(ctx, job, nil)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	dividendBefore, e := market.DividendFacts(ctx, marketScope, dividendRequest)
	if e != nil || dividendBefore == nil {
		t.Fatal(dividendBefore, e)
	}
	assets := postgres.NewAssetRepo(source)
	aSvc := asset.NewService(assets, postgres.NewFxRepo(source), postgres.NewTenantRepo(source))
	repo := postgres.NewInvestmentRepo(source)
	svc := inv.NewService(repo, postgres.NewTenantRepo(source))
	if _, e = svc.SaveForecastEvent(ctx, tenant, user, inv.ForecastEvent{ID: uuid.NewString(), InstrumentKey: "FIXTURE", Kind: "dividend", Nature: "forecast", Currency: "RUB", PerShare: "12.345678901234", PaymentDate: "2027-01-15", Timezone: "Europe/Moscow", EntitlementRule: "unknown", SourceReference: "Synthetic backup schedule", Evidence: "Synthetic forecast scenario", Reason: "Backup fixture"}, 0); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.SaveDividendHistory(ctx, tenant, user, inv.DividendHistory{ID: uuid.NewString(), InstrumentKey: "FIXTURE", Currency: "RUB", Timezone: "Europe/Moscow", BasisAt: time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC), ReviewedFrom: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC), SourceReference: "Synthetic issuer", Evidence: "All unit changes reviewed", Reason: "Backup fixture", Years: []inv.DividendYearReview{{Year: 2023, Complete: true, SourceReference: "Issuer", Evidence: "Complete year"}}, Dividends: []inv.DividendDeclaration{{ID: uuid.NewString(), PaymentDate: "2023-06-01", Amount: "1.234567890123", Class: "special", Status: "paid", UnitBasisAt: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC), SourceReference: "Issuer", Evidence: "Special payout"}}}, 0); e != nil {
		t.Fatal(e)
	}
	day := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	cut := day.Add(time.Hour)
	create := func(kind string, amount int64) d.AssetAccount {
		t.Helper()
		fixed := ""
		if kind == "investment" {
			fixed = "RUB"
		}
		a, e := aSvc.CreateAccount(ctx, d.AssetAccount{TenantID: tenant, Name: kind, Kind: kind, FixedCurrencyCode: fixed}, []d.AssetSnapshot{{UserID: user, Amount: d.Money{CurrencyCode: "RUB", MinorUnits: amount}, AsOf: day}}, uuid.NewString())
		if e != nil {
			t.Fatal(e)
		}
		return a
	}
	bank, account := create("bank", 200000), create("investment", 100000)
	ledger, e := svc.CreateLedger(ctx, tenant, user, "Broker", day, "ledger")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.Mutate(ctx, tenant, user, inv.Mutation{Action: "create", ExpectedLedgerVersion: 1, RequestKey: "initial", Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "deposit", Currency: "RUB", Amount: "1000", OccurredAt: day}}); e != nil {
		t.Fatal(e)
	}
	am := d.InvestmentAssetActivation{AccountID: account.ID, LedgerID: ledger.ID, ValuationFrom: cut}
	p, _, e := svc.AssetActivation(ctx, tenant, user, am, false)
	if e != nil {
		t.Fatal(e)
	}
	am.PreviewHash = p.PreviewHash
	am.ExpectedAccountVersion = p.Account.Version
	am.ExpectedLedgerVersion = p.Ledger.Version
	am.Evidence = "Synthetic reconciliation"
	am.RequestKey = "activate"
	_, account, e = svc.AssetActivation(ctx, tenant, user, am, true)
	if e != nil {
		t.Fatal(e)
	}
	cash := inv.CashTransferMutation{Timing: &d.InvestmentOperationTiming{Timezone: "Europe/Moscow", OccurredDate: "2024-01-02"}, Action: "create", RequestKey: "fund", ExpectedLedgerVersion: 2, ExpectedFromAccountVersion: bank.Version, ExpectedToAccountVersion: account.Version, Transfer: d.AssetTransfer{FromAccountID: bank.ID, ToAccountID: account.ID, FromAmount: d.Money{CurrencyCode: "RUB", MinorUnits: 20000}, ToAmount: d.Money{CurrencyCode: "RUB", MinorUnits: 20000}}}
	cashPreview, e := svc.PreviewCashTransfer(ctx, tenant, user, cash)
	if e != nil || !cashPreview.Preview.Valid {
		t.Fatal(cashPreview, e)
	}
	cash.PreviewHash = cashPreview.Preview.Hash
	funded, e := svc.CashTransfer(ctx, tenant, user, cash)
	if e != nil {
		t.Fatal(e)
	}
	buy, e := svc.Mutate(ctx, tenant, user, inv.Mutation{Action: "create", RequestKey: "buy", ExpectedLedgerVersion: 3, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "buy", InstrumentKey: "FIXTURE", Currency: "RUB", Quantity: "2", Amount: "200", OccurredAt: cut.Add(2 * time.Hour)}})
	if e != nil {
		t.Fatal(e)
	}
	other, e := svc.CreateLedger(ctx, tenant, user, "Other", day, "other-ledger")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.Transfer(ctx, tenant, user, inv.TransferMutation{Action: "create", RequestKey: "security-transfer", ExpectedFromVersion: 4, ExpectedToVersion: 1, Transfer: d.InvestmentTransfer{FromLedgerID: ledger.ID, ToLedgerID: other.ID, InstrumentKey: "FIXTURE", Currency: "RUB", Quantity: "1", OccurredAt: cut.Add(48 * time.Hour)}}); e != nil {
		t.Fatal(e)
	}
	asof := cut.Add(96 * time.Hour)
	before, e := svc.Snapshot(ctx, tenant, user, ledger.ID, asof)
	if e != nil {
		t.Fatal(e)
	}
	beforeOther, e := svc.Snapshot(ctx, tenant, user, other.ID, asof)
	if e != nil {
		t.Fatal(e)
	}
	beforeAccount, e := assets.GetAccount(ctx, tenant, account.ID, asof)
	if e != nil {
		t.Fatal(e)
	}
	// Ownership is intentionally remapped to the restore user; financial data,
	// DDL and application access are verified independently of cluster roles.
	if _, e = source.DB.Exec(ctx, `CREATE ROLE fixture_object_owner; ALTER TABLE refresh_tokens OWNER TO fixture_object_owner`); e != nil {
		t.Fatal(e)
	}
	// This committed concurrent change must not leak into the exported snapshot.
	tools.beforeDump = func() {
		if _, e := source.DB.Exec(ctx, `UPDATE users SET name='After snapshot' WHERE id=$1`, user); e != nil {
			t.Fatal(e)
		}
	}
	archiveDir := filepath.Join(dir, "backup")
	inspection, e := container.Inspect(ctx)
	if e != nil {
		t.Fatal(e)
	}
	manifest, sha, e := pgbackup.Backup(ctx, tools, dsn, archiveDir, pgbackup.Release{BackendImage: "fixture/backend@sha256:local", WebImage: "fixture/web@sha256:local", DatabaseImage: inspection.Image, Revision: "local-test"})
	if e != nil {
		t.Fatal(e)
	}
	if manifest.State.Migration.Version != 61 || manifest.State.Migration.Dirty || len(manifest.State.Tables) < 50 {
		t.Fatal("missing full schema evidence")
	}
	for _, file := range []string{"manifest.json", "database.dump"} {
		info, e := os.Stat(filepath.Join(archiveDir, file))
		if e != nil || info.Mode().Perm() != 0600 {
			t.Fatal("private backup mode", e)
		}
	}
	if _, _, e = pgbackup.Backup(ctx, tools, dsn, archiveDir, manifest.Release); e == nil {
		t.Fatal("existing backup overwritten")
	}
	// A real different-major destination must be rejected before CREATE DATABASE.
	otherImage := "postgres:15-alpine"
	if strings.HasPrefix(image, "postgres:15") {
		otherImage = "postgres:16-alpine"
	}
	otherServer, err := tcpostgres.Run(ctx, otherImage, tcpostgres.WithDatabase("budget"), tcpostgres.WithUsername("budget"), tcpostgres.WithPassword("budget"), tcpostgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = otherServer.Terminate(ctx) }()
	otherDSN, err := otherServer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pgbackup.Restore(ctx, tools, otherDSN, archiveDir, "budget_restore_wrong_major", sha); err == nil || !strings.Contains(err.Error(), "same PostgreSQL major version") {
		t.Fatal("cross-major restore accepted or failed for unrelated reason", err)
	}
	otherConn, err := pgx.Connect(ctx, otherDSN)
	if err != nil {
		t.Fatal(err)
	}
	var created bool
	err = otherConn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname='budget_restore_wrong_major')`).Scan(&created)
	_ = otherConn.Close(ctx)
	if err != nil || created {
		t.Fatal("cross-major refusal created a destination database", err)
	}
	name := "budget_restore_verified"
	report, e := pgbackup.Restore(ctx, tools, dsn, archiveDir, name, sha)
	if e != nil {
		t.Fatal("full restore", e)
	}
	if report.CompletedAt.IsZero() || report.VerifiedRows < 10 || report.VerifiedTables != len(manifest.State.Tables) {
		t.Fatal("restore not certified")
	}
	if e = pgbackup.SaveReport(filepath.Join(dir, "restore-report.json"), report); e != nil {
		t.Fatal(e)
	}
	if path := os.Getenv("BUDGET_BACKUP_TEST_REPORT"); path != "" {
		if e = pgbackup.SaveReport(path, report); e != nil {
			t.Fatal(e)
		}
	}
	t.Logf("Synthetic full restore verified: schema %d, %d tables, %d rows, elapsed %s", report.State.Migration.Version, report.VerifiedTables, report.VerifiedRows, report.CompletedAt.Sub(report.StartedAt).Round(time.Millisecond))
	parsed, _ := url.Parse(dsn)
	parsed.Path = "/" + name
	restored, e := postgres.NewPool(ctx, parsed.String())
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close()
	dividendAfter, dividendErr := postgres.NewMarketDataRepo(restored).DividendFacts(ctx, marketScope, dividendRequest)
	if dividendErr != nil || !reflect.DeepEqual(dividendBefore, dividendAfter) {
		t.Fatal("dividend snapshot lost in native restore", dividendErr)
	}
	restoredPolicy, restoredPoints, fxErr := postgres.NewMarketDataRepo(restored).ReadFXSnapshot(ctx, tenant, "market", marketDay.AddDate(0, 0, 2), time.Time{})
	if fxErr != nil || restoredPolicy.Version != 1 || len(restoredPoints) != 0 || !reflect.DeepEqual(restoredPolicy.Series, fxPolicy.Series) {
		t.Fatal("restored FX source review", fxErr, restoredPolicy, restoredPoints)
	}
	restoredOfficial, _, officialErr := postgres.NewMarketDataRepo(restored).ReadFXSnapshot(ctx, tenant, "cbr", marketDay, time.Time{})
	if officialErr != nil || !reflect.DeepEqual(restoredOfficial.Official, officialPolicy.Official) {
		t.Fatal("official policy lost in restore", officialErr, restoredOfficial)
	}
	var subscriptions int
	if e = restored.DB.QueryRow(ctx, `SELECT count(*) FROM market_data_subscriptions WHERE tenant_id=$1 AND access_scope='backup-fixture' AND requested_until>now() AND benchmark_demand AND fx_demand`, tenant).Scan(&subscriptions); e != nil || subscriptions != 1 {
		t.Fatal("market subscription lost in restore", e)
	}
	restoredAssets := postgres.NewAssetRepo(restored)
	restoredRepo := postgres.NewInvestmentRepo(restored)
	restoredSvc := inv.NewService(restoredRepo, postgres.NewTenantRepo(restored))
	after, e := restoredSvc.Snapshot(ctx, tenant, user, ledger.ID, asof)
	if e != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("restored journal differs", e)
	}
	afterOther, e := restoredSvc.Snapshot(ctx, tenant, user, other.ID, asof)
	if e != nil || !reflect.DeepEqual(beforeOther, afterOther) {
		t.Fatal("restored security transfer differs", e)
	}
	afterAccount, e := restoredAssets.GetAccount(ctx, tenant, account.ID, asof)
	if e != nil || !reflect.DeepEqual(beforeAccount, afterAccount) {
		t.Fatal("restored calculated assets differ", e)
	}
	if len(afterOther.Positions) != 1 || afterOther.Positions[0].Lots[0].AcquisitionOperationID != buy.ID || afterOther.Positions[0].Lots[0].OriginLedgerID != ledger.ID {
		t.Fatal("FIFO acquisition provenance lost")
	}
	var userName string
	if e = restored.DB.QueryRow(ctx, `SELECT name FROM users WHERE id=$1`, user).Scan(&userName); e != nil || userName != "Before snapshot" {
		t.Fatal("dump was not from the same snapshot")
	}
	var foreignCount int
	if e = restored.DB.QueryRow(ctx, `SELECT count(*) FROM tenants WHERE id=$1`, foreign).Scan(&foreignCount); e != nil || foreignCount != 1 {
		t.Fatal("full tenant data lost")
	}
	// The restored Budget bridge remains editable as one document, unlike the
	// canonical investment-only bundle. Historical ordinary legs move once.
	amend := cash
	amend.Action = "amend"
	amend.Transfer = funded.Transfer
	amend.Transfer.FromAmount.MinorUnits = 25000
	amend.Transfer.ToAmount.MinorUnits = 25000
	amend.ExpectedVersion = 1
	amend.ExpectedLedgerVersion = 5
	amend.Reason = "Restored document correction"
	amend.RequestKey = "restored-amend"
	amend.PreviewHash = ""
	review, e := restoredSvc.PreviewCashTransfer(ctx, tenant, user, amend)
	if e != nil || !review.Preview.Valid {
		t.Fatal(review, e)
	}
	amend.PreviewHash = review.Preview.Hash
	changed, e := restoredSvc.CashTransfer(ctx, tenant, user, amend)
	if e != nil || changed.Transfer.Version != 2 || changed.Timing == nil || changed.Timing.OccurredDate != "2024-01-02" {
		t.Fatal("restored joint edit", e)
	}
	bankAfter, e := restoredAssets.GetAccount(ctx, tenant, bank.ID, asof)
	if e != nil || bankAfter.Balances[0].Amount.MinorUnits != 175000 {
		t.Fatal("restored ordinary balance", e)
	}
	again, e := restoredSvc.CashTransfer(ctx, tenant, user, cash)
	if e != nil || again.OperationID != changed.OperationID || again.Transfer.Version != 2 {
		t.Fatal("restored idempotency", e)
	}
	original, e := svc.ReadCashTransfer(ctx, tenant, user, funded.Transfer.ID)
	if e != nil || original.Transfer.Version != 1 {
		t.Fatal("source changed by restore")
	}
	if _, e = pgbackup.Restore(ctx, tools, dsn, archiveDir, name, sha); e == nil {
		t.Fatal("existing target overwritten")
	}
	if _, e = pgbackup.Restore(ctx, tools, dsn, archiveDir, "budget", sha); e == nil {
		t.Fatal("source name accepted")
	}
	if _, e = pgbackup.Restore(ctx, tools, dsn, archiveDir, "budget_restore_bad_checksum", strings.Repeat("0", 64)); e == nil {
		t.Fatal("wrong checksum accepted")
	}
	var exists bool
	if e = source.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname='budget_restore_bad_checksum')`).Scan(&exists); e != nil || exists {
		t.Fatal("checksum failure created database")
	}
	// Matching row counts alone must not certify changed data or schema.
	for _, scenario := range []struct{ name, sql string }{
		{"budget_restore_row_mismatch", `UPDATE users SET name='Wrong restored content'`},
		{"budget_restore_schema_mismatch", `CREATE INDEX restore_extra_index ON users(name)`},
	} {
		tools.afterRestore = func(ctx context.Context, dsn string) {
			c, e := pgx.Connect(ctx, dsn)
			if e != nil {
				t.Fatal(e)
			}
			defer func() { _ = c.Close(ctx) }()
			if _, e = c.Exec(ctx, scenario.sql); e != nil {
				t.Fatal(e)
			}
		}
		if _, e = pgbackup.Restore(ctx, tools, dsn, archiveDir, scenario.name, sha); e == nil {
			t.Fatal("changed restore certified", scenario.name)
		}
	}
	// A damaged but re-sealed custom dump must fail real pg_restore and leave no
	// partially restored application tables. It is never certified as successful.
	brokenDir := filepath.Join(dir, "broken")
	if e = os.Mkdir(brokenDir, 0700); e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(filepath.Join(archiveDir, "database.dump"))
	if e != nil {
		t.Fatal(e)
	}
	raw = raw[:len(raw)-80]
	hash := sha256.Sum256(raw)
	manifest.DumpSHA256 = hex.EncodeToString(hash[:])
	if e = os.WriteFile(filepath.Join(brokenDir, "database.dump"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	encoded, _ := json.Marshal(manifest)
	if e = os.WriteFile(filepath.Join(brokenDir, "manifest.json"), encoded, 0600); e != nil {
		t.Fatal(e)
	}
	hash = sha256.Sum256(encoded)
	if _, e = pgbackup.Restore(ctx, tools, dsn, brokenDir, "budget_restore_broken", hex.EncodeToString(hash[:])); e == nil {
		t.Fatal("damaged restore certified")
	}
	parsed.Path = "/budget_restore_broken"
	broken, e := pgx.Connect(ctx, parsed.String())
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = broken.Close(ctx) }()
	var tables int
	if e = broken.QueryRow(ctx, `SELECT count(*) FROM pg_tables WHERE schemaname='public'`).Scan(&tables); e != nil || tables != 0 {
		t.Fatal("partial restore remained", tables, e)
	}
	// Changing bytes without resealing is rejected even by offline verification.
	f, e := os.OpenFile(filepath.Join(archiveDir, "database.dump"), os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	if _, err := f.WriteString("tamper"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, e = pgbackup.Verify(archiveDir, sha); e == nil {
		t.Fatal("tampered dump accepted")
	}
}
