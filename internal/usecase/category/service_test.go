package category

import (
    "context"
    "reflect"
    "testing"

    "github.com/positron48/budget/internal/domain"
)

type stubRepo struct{
    created domain.Category
    updated domain.Category
    got domain.Category
    list []domain.Category
    delID string
}

func (s *stubRepo) Create(ctx context.Context, tenantID string, kind domain.CategoryKind, code string, parentID *string, isActive bool, translations []domain.CategoryTranslation) (domain.Category, error) {
    s.created = domain.Category{ID: "c1", TenantID: tenantID, Kind: kind, Code: code, ParentID: parentID, IsActive: isActive, Translations: translations}
    return s.created, nil
}
func (s *stubRepo) Update(ctx context.Context, id string, code string, parentID *string, isActive bool, translations []domain.CategoryTranslation) (domain.Category, error) {
    s.updated = domain.Category{ID: id, Code: code, ParentID: parentID, IsActive: isActive, Translations: translations}
    return s.updated, nil
}
func (s *stubRepo) Delete(ctx context.Context, id string) error { s.delID = id; return nil }
func (s *stubRepo) Get(ctx context.Context, id string) (domain.Category, error) { s.got = domain.Category{ID: id}; return s.got, nil }
func (s *stubRepo) List(ctx context.Context, tenantID string, kind domain.CategoryKind, includeInactive bool) ([]domain.Category, error) {
    s.list = []domain.Category{{ID: "c1", TenantID: tenantID, Kind: kind}}
    return s.list, nil
}

func TestService_CRUD(t *testing.T) {
    r := &stubRepo{}
    svc := NewService(r)
    ctx := context.Background()

    tr := []domain.CategoryTranslation{{Locale: "en", Name: "Food"}}
    c, err := svc.Create(ctx, "t1", domain.CategoryKindExpense, "food", nil, true, tr)
    if err != nil || c.ID == "" { t.Fatalf("create: %v %#v", err, c) }
    if !reflect.DeepEqual(c.Translations, tr) { t.Fatalf("translations mismatch") }

    up, err := svc.Update(ctx, "c1", "food2", nil, false, tr)
    if err != nil || up.Code != "food2" || up.IsActive { t.Fatalf("update: %v %#v", err, up) }

    if err := svc.Delete(ctx, "c1"); err != nil || r.delID != "c1" { t.Fatalf("delete: %v", err) }

    g, err := svc.Get(ctx, "c1")
    if err != nil || g.ID != "c1" { t.Fatalf("get: %v %#v", err, g) }

    lst, err := svc.List(ctx, "t1", domain.CategoryKindExpense, false)
    if err != nil || len(lst) != 1 || lst[0].TenantID != "t1" { t.Fatalf("list: %v %#v", err, lst) }
}


