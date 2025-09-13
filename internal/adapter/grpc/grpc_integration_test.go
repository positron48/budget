package grpcadapter

import (
	"context"
	"net"
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
	budgetv1 "github.com/positron48/budget/gen/go/budget/v1"
	aauth "github.com/positron48/budget/internal/adapter/auth"
	"github.com/positron48/budget/internal/domain"
	useauth "github.com/positron48/budget/internal/usecase/auth"
	"github.com/positron48/budget/internal/usecase/category"
	repuse "github.com/positron48/budget/internal/usecase/report"
	useTenant "github.com/positron48/budget/internal/usecase/tenant"
	txuse "github.com/positron48/budget/internal/usecase/transaction"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const bufSize = 1024 * 1024

type memCategoryRepo struct {
	store map[string]domain.Category
	seq   int
}

func (m *memCategoryRepo) Create(ctx context.Context, tenantID string, kind domain.CategoryKind, code string, parentID *string, isActive bool, translations []domain.CategoryTranslation) (domain.Category, error) {
	m.seq++
	id := time.Now().Format("20060102150405") + "-" + code
	c := domain.Category{ID: id, TenantID: tenantID, Kind: kind, Code: code, ParentID: parentID, IsActive: isActive, CreatedAt: time.Now(), Translations: translations}
	if m.store == nil {
		m.store = map[string]domain.Category{}
	}
	m.store[id] = c
	return c, nil
}

func (m *memCategoryRepo) Update(ctx context.Context, id string, code string, parentID *string, isActive bool, translations []domain.CategoryTranslation) (domain.Category, error) {
	c := m.store[id]
	c.Code, c.ParentID, c.IsActive, c.Translations = code, parentID, isActive, translations
	m.store[id] = c
	return c, nil
}

func (m *memCategoryRepo) Delete(ctx context.Context, id string) error {
	delete(m.store, id)
	return nil
}

func (m *memCategoryRepo) Get(ctx context.Context, id string) (domain.Category, error) {
	return m.store[id], nil
}

func (m *memCategoryRepo) List(ctx context.Context, tenantID string, kind domain.CategoryKind, includeInactive bool) ([]domain.Category, error) {
	out := []domain.Category{}
	for _, c := range m.store {
		if c.TenantID == tenantID && c.Kind == kind {
			if includeInactive || c.IsActive {
				out = append(out, c)
			}
		}
	}
	return out, nil
}

func issueToken(signKey, userID, tenantID string) (string, error) {
	claims := jwt.MapClaims{"sub": userID, "tenant_id": tenantID, "iat": time.Now().Unix(), "exp": time.Now().Add(15 * time.Minute).Unix(), "typ": "access"}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(signKey))
}

func dialBufConn(ctx context.Context, lis *bufconn.Listener) (*grpc.ClientConn, error) {
	dialer := func(ctx context.Context, s string) (net.Conn, error) { return lis.Dial() }
	// staticcheck note: DialContext is deprecated in favor of NewClient, but is acceptable in tests and still supported in 1.x
	return grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(dialer), grpc.WithTransportCredentials(insecure.NewCredentials())) //nolint:staticcheck
}

func TestCategory_CreateAndList_WithAuth(t *testing.T) {
	const signKey = "test-secret"
	lis := bufconn.Listen(bufSize)
	t.Cleanup(func() { _ = lis.Close() })

	lg, _ := zap.NewDevelopment()
	sug := lg.Sugar()
	defer lg.Sync() //nolint:errcheck

	var tenantGuard grpc.UnaryServerInterceptor = func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		return handler(ctx, req)
	}
	srv := grpc.NewServer(grpc.ChainUnaryInterceptor(
		NewAuthUnaryInterceptor(signKey),
		LoggingUnaryInterceptor(sug),
		RecoveryUnaryInterceptor(sug),
		func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
			return tenantGuard(ctx, req, info, handler)
		},
	))
	tenantGuard = NewTenantGuardUnaryInterceptor(func(ctx context.Context, userID, tenantID string) (bool, error) { return true, nil })

	// register category service
	repo := &memCategoryRepo{store: map[string]domain.Category{}}
	svc := category.NewService(repo)
	budgetv1.RegisterCategoryServiceServer(srv, NewCategoryServer(svc))

	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	ctx := context.Background()
	conn, err := dialBufConn(ctx, lis)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := budgetv1.NewCategoryServiceClient(conn)

	token, err := issueToken(signKey, "u1", "t1")
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	md := metadata.New(map[string]string{
		"authorization": "Bearer " + token,
		"x-tenant-id":   "t1",
	})
	authCtx := metadata.NewOutgoingContext(ctx, md)

	// create
	_, err = client.CreateCategory(authCtx, &budgetv1.CreateCategoryRequest{
		Kind:         budgetv1.CategoryKind_CATEGORY_KIND_EXPENSE,
		Code:         "food",
		IsActive:     true,
		Translations: []*budgetv1.CategoryTranslation{{Locale: "en", Name: "Food"}},
	})
	if err != nil {
		t.Fatalf("CreateCategory failed: %v", err)
	}

	// list
	list, err := client.ListCategories(authCtx, &budgetv1.ListCategoriesRequest{Kind: budgetv1.CategoryKind_CATEGORY_KIND_EXPENSE})
	if err != nil {
		t.Fatalf("ListCategories failed: %v", err)
	}
	if len(list.GetCategories()) != 1 {
		t.Fatalf("expected 1 category, got %d", len(list.GetCategories()))
	}
}

// --- Auth flow integration ---

type memUserRepo struct {
	users       map[string]useauth.User
	byEmail     map[string]string // email -> userID
	memberships map[string][]useauth.TenantMembership
}

func (r *memUserRepo) CreateWithDefaultTenant(ctx context.Context, email, passwordHash, name, locale, tenantName string) (useauth.User, useauth.Tenant, error) {
	if r.users == nil {
		r.users = map[string]useauth.User{}
	}
	if r.byEmail == nil {
		r.byEmail = map[string]string{}
	}
	if r.memberships == nil {
		r.memberships = map[string][]useauth.TenantMembership{}
	}
	id := "u1"
	t := useauth.Tenant{ID: "t1", Name: tenantName, DefaultCurrencyCode: "USD"}
	u := useauth.User{ID: id, Email: email, Name: name, Locale: locale, PasswordHash: passwordHash}
	r.users[id] = u
	r.byEmail[email] = id
	r.memberships[id] = []useauth.TenantMembership{{TenantID: t.ID, Role: "owner", IsDefault: true}}
	return u, t, nil
}

func (r *memUserRepo) GetByEmail(ctx context.Context, email string) (useauth.User, []useauth.TenantMembership, error) {
	if id, ok := r.byEmail[email]; ok {
		return r.users[id], r.memberships[id], nil
	}
	return useauth.User{}, nil, nil
}

type memRTRepo struct {
	rows map[string]struct {
		UserID    string
		TenantID  string
		ExpiresAt time.Time
		RevokedAt *time.Time
	}
}

func (m *memRTRepo) Store(ctx context.Context, userID, tenantID, token string, expiresAt time.Time) error {
	if m.rows == nil {
		m.rows = map[string]struct {
			UserID    string
			TenantID  string
			ExpiresAt time.Time
			RevokedAt *time.Time
		}{}
	}
	m.rows[token] = struct {
		UserID    string
		TenantID  string
		ExpiresAt time.Time
		RevokedAt *time.Time
	}{UserID: userID, TenantID: tenantID, ExpiresAt: expiresAt}
	return nil
}

func (m *memRTRepo) Rotate(ctx context.Context, oldToken, newToken string, newExpiresAt time.Time) error {
	if m.rows == nil {
		m.rows = map[string]struct {
			UserID    string
			TenantID  string
			ExpiresAt time.Time
			RevokedAt *time.Time
		}{}
	}
	row := m.rows[oldToken]
	now := time.Now()
	row.RevokedAt = &now
	m.rows[oldToken] = row
	m.rows[newToken] = struct {
		UserID    string
		TenantID  string
		ExpiresAt time.Time
		RevokedAt *time.Time
	}{UserID: row.UserID, TenantID: row.TenantID, ExpiresAt: newExpiresAt}
	return nil
}

func (m *memRTRepo) GetByToken(ctx context.Context, token string) (struct {
	UserID    string
	TenantID  string
	ExpiresAt time.Time
	RevokedAt *time.Time
}, error,
) {
	if row, ok := m.rows[token]; ok {
		return row, nil
	}
	return struct {
		UserID    string
		TenantID  string
		ExpiresAt time.Time
		RevokedAt *time.Time
	}{}, nil
}

type hasherStub struct{}

func (hasherStub) Hash(password string) (string, error) { return "hash:" + password, nil }
func (hasherStub) Verify(hash, password string) bool    { return hash == "hash:"+password }

func TestAuth_Register_Login_Refresh_AndAccess(t *testing.T) {
	const signKey = "test-secret"
	lis := bufconn.Listen(bufSize)
	t.Cleanup(func() { _ = lis.Close() })

	lg, _ := zap.NewDevelopment()
	sug := lg.Sugar()
	defer lg.Sync() //nolint:errcheck

	var tenantGuard grpc.UnaryServerInterceptor = func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		return handler(ctx, req)
	}
	srv := grpc.NewServer(grpc.ChainUnaryInterceptor(
		NewAuthUnaryInterceptor(signKey),
		LoggingUnaryInterceptor(sug),
		RecoveryUnaryInterceptor(sug),
		func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
			return tenantGuard(ctx, req, info, handler)
		},
	))
	tenantGuard = NewTenantGuardUnaryInterceptor(func(ctx context.Context, userID, tenantID string) (bool, error) { return true, nil })

	// Auth service wiring with in-memory repos
	urepo := &memUserRepo{}
	rtrepo := &memRTRepo{}
	issuer := aauth.NewJWTIssuer(signKey)
	authSvc := useauth.NewService(urepo, rtrepo, hasherStub{}, issuer, 15*time.Minute, 24*time.Hour)
	budgetv1.RegisterAuthServiceServer(srv, NewAuthServer(authSvc))

	// Category service to test protected access
	crepo := &memCategoryRepo{store: map[string]domain.Category{}}
	catSvc := category.NewService(crepo)
	budgetv1.RegisterCategoryServiceServer(srv, NewCategoryServer(catSvc))

	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	ctx := context.Background()
	conn, err := dialBufConn(ctx, lis)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	authClient := budgetv1.NewAuthServiceClient(conn)
	catClient := budgetv1.NewCategoryServiceClient(conn)

	// Register
	reg, err := authClient.Register(ctx, &budgetv1.RegisterRequest{Email: "u@example.com", Password: "Passw0rd!", Name: "User", Locale: "ru", TenantName: "Дом"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if reg.GetTokens().GetAccessToken() == "" || reg.GetTenant().GetId() == "" {
		t.Fatalf("invalid register resp: %#v", reg)
	}

	// Access protected ListCategories using access token
	md := metadata.New(map[string]string{"authorization": "Bearer " + reg.GetTokens().GetAccessToken()})
	authCtx := metadata.NewOutgoingContext(ctx, md)
	// create a category to list
	_, _ = catClient.CreateCategory(authCtx, &budgetv1.CreateCategoryRequest{Kind: budgetv1.CategoryKind_CATEGORY_KIND_EXPENSE, Code: "food", Translations: []*budgetv1.CategoryTranslation{{Locale: "en", Name: "Food"}}})
	if _, err := catClient.ListCategories(authCtx, &budgetv1.ListCategoriesRequest{Kind: budgetv1.CategoryKind_CATEGORY_KIND_EXPENSE}); err != nil {
		t.Fatalf("protected access failed: %v", err)
	}

	// Login
	login, err := authClient.Login(ctx, &budgetv1.LoginRequest{Email: "u@example.com", Password: "Passw0rd!"})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if login.GetTokens().GetAccessToken() == "" || len(login.GetMemberships()) == 0 {
		t.Fatalf("invalid login resp: %#v", login)
	}

	// Refresh
	ref, err := authClient.RefreshToken(ctx, &budgetv1.RefreshTokenRequest{RefreshToken: reg.GetTokens().GetRefreshToken()})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if ref.GetTokens().GetAccessToken() == "" {
		t.Fatalf("invalid refresh resp: %#v", ref)
	}
}

func TestAuth_TokenRefresh_PreservesTenantID(t *testing.T) {
	const signKey = "test-secret"
	lis := bufconn.Listen(bufSize)
	t.Cleanup(func() { _ = lis.Close() })

	lg, _ := zap.NewDevelopment()
	sug := lg.Sugar()
	defer lg.Sync() //nolint:errcheck

	var tenantGuard grpc.UnaryServerInterceptor = func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		return handler(ctx, req)
	}
	srv := grpc.NewServer(grpc.ChainUnaryInterceptor(
		NewAuthUnaryInterceptor(signKey),
		LoggingUnaryInterceptor(sug),
		RecoveryUnaryInterceptor(sug),
		func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
			return tenantGuard(ctx, req, info, handler)
		},
	))
	tenantGuard = NewTenantGuardUnaryInterceptor(func(ctx context.Context, userID, tenantID string) (bool, error) { return true, nil })

	// Auth service wiring with in-memory repos
	urepo := &memUserRepo{}
	rtrepo := &memRTRepo{}
	issuer := aauth.NewJWTIssuer(signKey)
	authSvc := useauth.NewService(urepo, rtrepo, hasherStub{}, issuer, 15*time.Minute, 24*time.Hour)
	budgetv1.RegisterAuthServiceServer(srv, NewAuthServer(authSvc))

	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	ctx := context.Background()
	conn, err := dialBufConn(ctx, lis)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	authClient := budgetv1.NewAuthServiceClient(conn)

	// Register a user
	reg, err := authClient.Register(ctx, &budgetv1.RegisterRequest{
		Email:      "test@example.com",
		Password:   "Passw0rd!",
		Name:       "Test User",
		Locale:     "en",
		TenantName: "Test Tenant",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	// Decode initial access token to get tenant_id
	initialToken := reg.GetTokens().GetAccessToken()
	initialPayload := decodeJWTToken(t, initialToken, signKey)
	initialTenantID := initialPayload["tenant_id"].(string)
	initialUserID := initialPayload["sub"].(string)

	t.Logf("Initial token - user_id: %s, tenant_id: %s", initialUserID, initialTenantID)

	// Refresh the token
	refreshResp, err := authClient.RefreshToken(ctx, &budgetv1.RefreshTokenRequest{
		RefreshToken: reg.GetTokens().GetRefreshToken(),
	})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}

	// Decode refreshed access token to check tenant_id
	refreshedToken := refreshResp.GetTokens().GetAccessToken()
	refreshedPayload := decodeJWTToken(t, refreshedToken, signKey)
	refreshedTenantID := refreshedPayload["tenant_id"].(string)
	refreshedUserID := refreshedPayload["sub"].(string)

	t.Logf("Refreshed token - user_id: %s, tenant_id: %s", refreshedUserID, refreshedTenantID)

	// Verify that tenant_id is preserved
	if initialTenantID != refreshedTenantID {
		t.Errorf("tenant_id was lost during refresh: initial=%s, refreshed=%s", initialTenantID, refreshedTenantID)
	}

	// Verify that user_id is preserved
	if initialUserID != refreshedUserID {
		t.Errorf("user_id was lost during refresh: initial=%s, refreshed=%s", initialUserID, refreshedUserID)
	}

	t.Logf("✅ SUCCESS: tenant_id and user_id preserved during token refresh")
}

// Helper function to decode JWT token
func decodeJWTToken(t *testing.T, token, signKey string) map[string]interface{} {
	parsed, err := jwt.Parse(token, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(signKey), nil
	})
	if err != nil {
		t.Fatalf("failed to parse JWT: %v", err)
	}
	if !parsed.Valid {
		t.Fatal("JWT token is invalid")
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatal("failed to get JWT claims")
	}
	return claims
}

// --- Transaction & Report integration ---

type memTxSvc struct{}

func (memTxSvc) ComputeBaseAmount(ctx context.Context, tenantID string, amount domain.Money, occurredAt time.Time) (domain.Money, *domain.FxInfo, error) {
	fx := &domain.FxInfo{FromCurrency: amount.CurrencyCode, ToCurrency: amount.CurrencyCode, RateDecimal: "1", AsOf: occurredAt, Provider: "mem"}
	return amount, fx, nil
}

func (memTxSvc) Create(ctx context.Context, tx domain.Transaction) (domain.Transaction, error) {
	tx.ID = "tx-created"
	tx.BaseAmount = tx.Amount
	occ := tx.OccurredAt
	if occ.IsZero() {
		occ = time.Now()
	}
	tx.Fx = &domain.FxInfo{FromCurrency: tx.Amount.CurrencyCode, ToCurrency: tx.Amount.CurrencyCode, RateDecimal: "1", AsOf: occ, Provider: "mem"}
	return tx, nil
}

func (memTxSvc) Update(ctx context.Context, tx domain.Transaction) (domain.Transaction, error) {
	return tx, nil
}
func (memTxSvc) Delete(ctx context.Context, id string) error { return nil }
func (memTxSvc) Get(ctx context.Context, id string) (domain.Transaction, error) {
	return domain.Transaction{ID: id, TenantID: "t1", Amount: domain.Money{CurrencyCode: "USD", MinorUnits: 100}, BaseAmount: domain.Money{CurrencyCode: "USD", MinorUnits: 100}, OccurredAt: time.Now()}, nil
}

func (memTxSvc) List(ctx context.Context, tenantID string, filter txuse.ListFilter) ([]domain.Transaction, int64, error) {
	return []domain.Transaction{{ID: "tx1"}}, 1, nil
}

func (memTxSvc) Totals(ctx context.Context, tenantID string, filter txuse.ListFilter) (domain.Money, domain.Money, error) {
	return domain.Money{CurrencyCode: "USD", MinorUnits: 100}, domain.Money{CurrencyCode: "USD", MinorUnits: 50}, nil
}

func (memTxSvc) CreateForUser(ctx context.Context, tenantID, userID string, txType domain.TransactionType, categoryID string, amount domain.Money, occurredAt time.Time, comment string) (domain.Transaction, error) {
	return domain.Transaction{ID: "tx1", TenantID: tenantID, UserID: userID, CategoryID: categoryID, Type: txType, Amount: amount, BaseAmount: amount, OccurredAt: occurredAt, CreatedAt: time.Now()}, nil
}

func (memTxSvc) GetDateRange(ctx context.Context, tenantID string) (earliest, latest time.Time, err error) {
	return time.Time{}, time.Time{}, nil
}

type memReportSvc struct{}

func (memReportSvc) GetMonthlySummary(ctx context.Context, tenantID string, year int, month int, locale string, targetCurrencyCode string, tzOffsetMinutes int) (repuse.MonthlySummary, error) {
	return repuse.MonthlySummary{
		Items:        []repuse.MonthlyItem{{CategoryID: "c1", CategoryName: "Food", Type: domain.TransactionTypeExpense, Total: domain.Money{CurrencyCode: "USD", MinorUnits: 500}}},
		TotalIncome:  domain.Money{CurrencyCode: "USD", MinorUnits: 0},
		TotalExpense: domain.Money{CurrencyCode: "USD", MinorUnits: 500},
	}, nil
}

func (memReportSvc) GetDateRange(ctx context.Context, tenantID string, locale string, tzOffsetMinutes int) (repuse.DateRange, error) {
	return repuse.DateRange{}, nil
}

func (memReportSvc) GetSummaryReport(ctx context.Context, tenantID string, fromDate string, toDate string, locale string, targetCurrencyCode string, tzOffsetMinutes int) (repuse.SummaryReport, error) {
	return repuse.SummaryReport{}, nil
}

func TestTransaction_Create_And_Report_WithAuth(t *testing.T) {
	const signKey = "test-secret"
	lis := bufconn.Listen(bufSize)
	t.Cleanup(func() { _ = lis.Close() })

	lg, _ := zap.NewDevelopment()
	sug := lg.Sugar()
	defer lg.Sync() //nolint:errcheck

	var tenantGuard grpc.UnaryServerInterceptor = func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		return handler(ctx, req)
	}
	srv := grpc.NewServer(grpc.ChainUnaryInterceptor(
		NewAuthUnaryInterceptor(signKey),
		LoggingUnaryInterceptor(sug),
		RecoveryUnaryInterceptor(sug),
		func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
			return tenantGuard(ctx, req, info, handler)
		},
	))
	tenantGuard = NewTenantGuardUnaryInterceptor(func(ctx context.Context, userID, tenantID string) (bool, error) { return true, nil })

	// Wire services
	budgetv1.RegisterTransactionServiceServer(srv, NewTransactionServer(memTxSvc{}))
	budgetv1.RegisterReportServiceServer(srv, NewReportServer(memReportSvc{}))

	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	ctx := context.Background()
	conn, err := dialBufConn(ctx, lis)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	token, err := issueToken(signKey, "u1", "t1")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	md := metadata.New(map[string]string{"authorization": "Bearer " + token, "x-tenant-id": "t1"})
	authCtx := metadata.NewOutgoingContext(ctx, md)

	txc := budgetv1.NewTransactionServiceClient(conn)
	rc := budgetv1.NewReportServiceClient(conn)

	// Create transaction
	creq := &budgetv1.CreateTransactionRequest{Type: budgetv1.TransactionType_TRANSACTION_TYPE_EXPENSE, CategoryId: "c1", Amount: &budgetv1.Money{CurrencyCode: "USD", MinorUnits: 100}}
	cresp, err := txc.CreateTransaction(authCtx, creq)
	if err != nil || cresp.GetTransaction().GetId() == "" {
		t.Fatalf("create tx: %v %#v", err, cresp)
	}

	// Report monthly summary
	rresp, err := rc.GetMonthlySummary(authCtx, &budgetv1.GetMonthlySummaryRequest{Year: int32(time.Now().Year()), Month: int32(time.Now().Month())})
	if err != nil || len(rresp.GetItems()) != 1 {
		t.Fatalf("report: %v %#v", err, rresp)
	}
}

// --- Fx integration (adapter server only) ---
type memFxRepo struct{}

func (memFxRepo) GetRateAsOf(ctx context.Context, from, to string, asOf time.Time) (string, string, error) {
	return "1.2345", "prov", nil
}

func (memFxRepo) UpsertRate(ctx context.Context, from, to, rateDecimal string, asOf time.Time, provider string) (struct {
	From, To, Rate string
	AsOf           time.Time
	Provider       string
}, error,
) {
	return struct {
		From, To, Rate string
		AsOf           time.Time
		Provider       string
	}{From: from, To: to, Rate: rateDecimal, AsOf: asOf, Provider: provider}, nil
}

func (memFxRepo) BatchGetRates(ctx context.Context, fromCurrencies []string, to string, asOf time.Time) ([]struct {
	From, To, Rate string
	AsOf           time.Time
	Provider       string
}, error,
) {
	return []struct {
		From, To, Rate string
		AsOf           time.Time
		Provider       string
	}{{From: fromCurrencies[0], To: to, Rate: "2.0", AsOf: asOf, Provider: "prov"}}, nil
}

func TestFx_Upsert_Get_Batch(t *testing.T) {
	lis := bufconn.Listen(bufSize)
	t.Cleanup(func() { _ = lis.Close() })

	lg, _ := zap.NewDevelopment()
	sug := lg.Sugar()
	defer lg.Sync() //nolint:errcheck

	srv := grpc.NewServer(grpc.ChainUnaryInterceptor(LoggingUnaryInterceptor(sug), RecoveryUnaryInterceptor(sug)))
	budgetv1.RegisterFxServiceServer(srv, NewFxServer(memFxRepo{}))
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	ctx := context.Background()
	conn, err := dialBufConn(ctx, lis)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	c := budgetv1.NewFxServiceClient(conn)

	now := time.Now()
	u, err := c.UpsertRate(ctx, &budgetv1.UpsertRateRequest{Rate: &budgetv1.FxRate{FromCurrencyCode: "USD", ToCurrencyCode: "RUB", RateDecimal: "2.0", AsOf: timestamppb.New(now), Provider: "prov"}})
	if err != nil || u.GetRate().GetRateDecimal() != "2.0" {
		t.Fatalf("upsert: %v %#v", err, u)
	}
	g, err := c.GetRate(ctx, &budgetv1.GetRateRequest{FromCurrencyCode: "USD", ToCurrencyCode: "RUB", AsOf: timestamppb.New(now)})
	if err != nil || g.GetRate().GetProvider() != "prov" {
		t.Fatalf("get: %v %#v", err, g)
	}
	b, err := c.BatchGetRates(ctx, &budgetv1.BatchGetRatesRequest{FromCurrencyCodes: []string{"USD"}, ToCurrencyCode: "RUB", AsOf: timestamppb.New(now)})
	if err != nil || len(b.GetRates()) == 0 {
		t.Fatalf("batch: %v %#v", err, b)
	}
}

// --- Tenant integration ---
type memTenantRepo struct{}

func (memTenantRepo) Create(ctx context.Context, name, slug, defaultCurrency string, ownerUserID string) (domain.Tenant, error) {
	return domain.Tenant{ID: "t1", Name: name, Slug: slug, DefaultCurrencyCode: defaultCurrency}, nil
}

func (memTenantRepo) ListForUser(ctx context.Context, userID string) ([]domain.TenantMembership, error) {
	return []domain.TenantMembership{{Tenant: domain.Tenant{ID: "t1"}, Role: "owner", IsDefault: true}}, nil
}

func (memTenantRepo) UpdateTenant(ctx context.Context, tenantID, name, slug, defaultCurrency string) (domain.Tenant, error) {
	return domain.Tenant{ID: tenantID, Name: name, Slug: slug, DefaultCurrencyCode: defaultCurrency}, nil
}

func (memTenantRepo) ListMembers(ctx context.Context, tenantID string) ([]domain.TenantMembership, error) {
	return []domain.TenantMembership{{Tenant: domain.Tenant{ID: tenantID}, Role: domain.TenantRoleOwner, IsDefault: true}}, nil
}

func (memTenantRepo) AddMember(ctx context.Context, tenantID, userEmail string, role domain.TenantRole) (domain.TenantMembership, error) {
	return domain.TenantMembership{Tenant: domain.Tenant{ID: tenantID}, Role: role, IsDefault: false}, nil
}

func (memTenantRepo) UpdateMemberRole(ctx context.Context, tenantID, userID string, role domain.TenantRole) (domain.TenantMembership, error) {
	return domain.TenantMembership{Tenant: domain.Tenant{ID: tenantID}, Role: role, IsDefault: false}, nil
}

func (memTenantRepo) RemoveMember(ctx context.Context, tenantID, userID string) error { return nil }

func (memTenantRepo) GetUserRole(ctx context.Context, tenantID, userID string) (domain.TenantRole, error) {
	return domain.TenantRoleOwner, nil
}

func TestTenant_Create_And_List_WithAuth(t *testing.T) {
	const signKey = "test-secret"
	lis := bufconn.Listen(bufSize)
	t.Cleanup(func() { _ = lis.Close() })

	lg, _ := zap.NewDevelopment()
	sug := lg.Sugar()
	defer lg.Sync() //nolint:errcheck

	var tenantGuard grpc.UnaryServerInterceptor = func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		return handler(ctx, req)
	}
	srv := grpc.NewServer(grpc.ChainUnaryInterceptor(
		NewAuthUnaryInterceptor(signKey),
		LoggingUnaryInterceptor(sug),
		RecoveryUnaryInterceptor(sug),
		func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
			return tenantGuard(ctx, req, info, handler)
		},
	))
	tenantGuard = NewTenantGuardUnaryInterceptor(func(ctx context.Context, userID, tenantID string) (bool, error) { return true, nil })

	// wire tenant service
	trepo := memTenantRepo{}
	tsvc := useTenant.NewService(trepo)
	budgetv1.RegisterTenantServiceServer(srv, NewTenantServer(tsvc))

	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	ctx := context.Background()
	conn, err := dialBufConn(ctx, lis)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	token, _ := issueToken(signKey, "u1", "t1")
	md := metadata.New(map[string]string{"authorization": "Bearer " + token})
	authCtx := metadata.NewOutgoingContext(ctx, md)

	c := budgetv1.NewTenantServiceClient(conn)
	if _, err := c.CreateTenant(authCtx, &budgetv1.CreateTenantRequest{Name: "Home", Slug: "home", DefaultCurrencyCode: "USD"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	lst, err := c.ListMyTenants(authCtx, &budgetv1.ListMyTenantsRequest{})
	if err != nil || len(lst.GetMemberships()) == 0 {
		t.Fatalf("list: %v %#v", err, lst)
	}
}
