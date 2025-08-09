package grpcadapter

import (
    "context"
    "errors"
    "testing"

    budgetv1 "github.com/positron48/budget/gen/go/budget/v1"
    "github.com/positron48/budget/internal/domain"
    "github.com/positron48/budget/internal/pkg/ctxutil"
    usecat "github.com/positron48/budget/internal/usecase/category"
)

type catStubRepo struct{
    item domain.Category
    list []domain.Category
}

func (s *catStubRepo) Create(ctx context.Context, tenantID string, kind domain.CategoryKind, code string, parentID *string, isActive bool, translations []domain.CategoryTranslation) (domain.Category, error) {
    s.item = domain.Category{ID: "c1", TenantID: tenantID, Kind: kind, Code: code, ParentID: parentID, IsActive: isActive, Translations: translations}
    return s.item, nil
}
func (s *catStubRepo) Update(ctx context.Context, id string, code string, parentID *string, isActive bool, translations []domain.CategoryTranslation) (domain.Category, error) {
    s.item = domain.Category{ID: id, Code: code, ParentID: parentID, IsActive: isActive, Translations: translations}
    return s.item, nil
}
func (s *catStubRepo) Delete(ctx context.Context, id string) error { return nil }
func (s *catStubRepo) Get(ctx context.Context, id string) (domain.Category, error) {
    if s.item.ID == "" || len(s.item.Translations) == 0 {
        s.item = domain.Category{ID: id, Translations: []domain.CategoryTranslation{{Locale: "ru", Name: "Еда"}, {Locale: "en", Name: "Food"}}}
    }
    return s.item, nil
}
func (s *catStubRepo) List(ctx context.Context, tenantID string, kind domain.CategoryKind, includeInactive bool) ([]domain.Category, error) {
    if s.list == nil { s.list = []domain.Category{{ID: "c1", TenantID: tenantID, Kind: kind, Translations: []domain.CategoryTranslation{{Locale: "en", Name: "Food"}, {Locale: "ru", Name: "Еда"}}}} }
    return s.list, nil
}

func TestCategoryServer_CRUD_And_Locale(t *testing.T) {
    repo := &catStubRepo{}
    svc := usecat.NewService(repo)
    srv := NewCategoryServer(svc)
    ctx := ctxutil.WithTenantID(context.Background(), "t1")

    // invalid create (no code)
    if _, err := srv.CreateCategory(ctx, &budgetv1.CreateCategoryRequest{Kind: budgetv1.CategoryKind_CATEGORY_KIND_EXPENSE}); err == nil {
        t.Fatal("expected invalid argument for empty code")
    }

    // create
    out, err := srv.CreateCategory(ctx, &budgetv1.CreateCategoryRequest{Kind: budgetv1.CategoryKind_CATEGORY_KIND_EXPENSE, Code: "food", Translations: []*budgetv1.CategoryTranslation{{Locale: "en", Name: "Food"}}})
    if err != nil || out.GetCategory().GetId() == "" { t.Fatalf("create: %v %#v", err, out) }

    // update
    up, err := srv.UpdateCategory(ctx, &budgetv1.UpdateCategoryRequest{Id: out.GetCategory().GetId(), Code: "food2", IsActive: false})
    if err != nil || up.GetCategory().GetCode() != "food2" || up.GetCategory().GetIsActive() { t.Fatalf("update: %v %#v", err, up) }

    // get with locale preference
    g, err := srv.GetCategory(ctx, &budgetv1.GetCategoryRequest{Id: out.GetCategory().GetId(), Locale: "ru"})
    if err != nil || len(g.GetCategory().GetTranslations()) == 0 || g.GetCategory().GetTranslations()[0].GetLocale() != "ru" { t.Fatalf("get locale: %v %#v", err, g) }

    // list with locale
    lst, err := srv.ListCategories(ctx, &budgetv1.ListCategoriesRequest{Kind: budgetv1.CategoryKind_CATEGORY_KIND_EXPENSE, Locale: "en"})
    if err != nil || len(lst.GetCategories()) != 1 || lst.GetCategories()[0].GetTranslations()[0].GetLocale() != "en" { t.Fatalf("list: %v %#v", err, lst) }

    // delete
    if _, err := srv.DeleteCategory(ctx, &budgetv1.DeleteCategoryRequest{Id: out.GetCategory().GetId()}); err != nil { t.Fatalf("delete: %v", err) }
}

type errCatRepo struct{}
func (errCatRepo) Create(ctx context.Context, tenantID string, kind domain.CategoryKind, code string, parentID *string, isActive bool, translations []domain.CategoryTranslation) (domain.Category, error) { return domain.Category{}, errors.New("boom") }
func (errCatRepo) Update(ctx context.Context, id string, code string, parentID *string, isActive bool, translations []domain.CategoryTranslation) (domain.Category, error) { return domain.Category{}, errors.New("boom") }
func (errCatRepo) Delete(ctx context.Context, id string) error { return errors.New("boom") }
func (errCatRepo) Get(ctx context.Context, id string) (domain.Category, error) { return domain.Category{}, errors.New("boom") }
func (errCatRepo) List(ctx context.Context, tenantID string, kind domain.CategoryKind, includeInactive bool) ([]domain.Category, error) { return nil, errors.New("boom") }

func TestCategoryServer_MapError(t *testing.T) {
    svc := usecat.NewService(errCatRepo{})
    srv := NewCategoryServer(svc)
    ctx := ctxutil.WithTenantID(context.Background(), "t1")
    if _, err := srv.CreateCategory(ctx, &budgetv1.CreateCategoryRequest{Kind: budgetv1.CategoryKind_CATEGORY_KIND_EXPENSE, Code: "x"}); err == nil { t.Fatal("expected error") }
    if _, err := srv.UpdateCategory(ctx, &budgetv1.UpdateCategoryRequest{Id: "c1"}); err == nil { t.Fatal("expected error") }
    if _, err := srv.GetCategory(ctx, &budgetv1.GetCategoryRequest{Id: "c1"}); err == nil { t.Fatal("expected error") }
    if _, err := srv.ListCategories(ctx, &budgetv1.ListCategoriesRequest{Kind: budgetv1.CategoryKind_CATEGORY_KIND_EXPENSE}); err == nil { t.Fatal("expected error") }
    if _, err := srv.DeleteCategory(ctx, &budgetv1.DeleteCategoryRequest{Id: "c1"}); err == nil { t.Fatal("expected error") }
}


