package grpcadapter

import (
	"context"
	"net"
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
	budgetv1 "github.com/positron48/budget/gen/go/budget/v1"
	"github.com/positron48/budget/internal/domain"
	"github.com/positron48/budget/internal/usecase/category"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
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
