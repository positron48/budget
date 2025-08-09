//go:build !ignore
// +build !ignore

package grpcadapter

import (
    "context"

    budgetv1 "github.com/positron48/budget/gen/go/budget/v1"
    "github.com/positron48/budget/internal/domain"
    "github.com/positron48/budget/internal/pkg/ctxutil"
    "github.com/positron48/budget/internal/usecase/category"
)

type CategoryServer struct {
	budgetv1.UnimplementedCategoryServiceServer
	svc *category.Service
}

func NewCategoryServer(svc *category.Service) *CategoryServer { return &CategoryServer{svc: svc} }

func (s *CategoryServer) CreateCategory(ctx context.Context, req *budgetv1.CreateCategoryRequest) (*budgetv1.CreateCategoryResponse, error) {
    if req.GetCode() == "" {
        return nil, invalidArg("code is required")
    }
	trs := make([]domain.CategoryTranslation, 0, len(req.GetTranslations()))
	for _, tr := range req.GetTranslations() {
		trs = append(trs, domain.CategoryTranslation{Locale: tr.GetLocale(), Name: tr.GetName(), Description: tr.GetDescription()})
	}
    c, err := s.svc.Create(ctx, ctxTenantID(ctx), toKind(req.GetKind()), req.GetCode(), optString(req.GetParentId()), req.GetIsActive(), trs)
    if err != nil { return nil, mapError(err) }
	return &budgetv1.CreateCategoryResponse{Category: toProtoCategory(c)}, nil
}

func (s *CategoryServer) UpdateCategory(ctx context.Context, req *budgetv1.UpdateCategoryRequest) (*budgetv1.UpdateCategoryResponse, error) {
	trs := make([]domain.CategoryTranslation, 0, len(req.GetTranslations()))
	for _, tr := range req.GetTranslations() {
		trs = append(trs, domain.CategoryTranslation{Locale: tr.GetLocale(), Name: tr.GetName(), Description: tr.GetDescription()})
	}
    c, err := s.svc.Update(ctx, req.GetId(), req.GetCode(), optString(req.GetParentId()), req.GetIsActive(), trs)
    if err != nil { return nil, mapError(err) }
	return &budgetv1.UpdateCategoryResponse{Category: toProtoCategory(c)}, nil
}

func (s *CategoryServer) DeleteCategory(ctx context.Context, req *budgetv1.DeleteCategoryRequest) (*budgetv1.DeleteCategoryResponse, error) {
    if err := s.svc.Delete(ctx, req.GetId()); err != nil { return nil, mapError(err) }
	return &budgetv1.DeleteCategoryResponse{}, nil
}

func (s *CategoryServer) GetCategory(ctx context.Context, req *budgetv1.GetCategoryRequest) (*budgetv1.GetCategoryResponse, error) {
    c, err := s.svc.Get(ctx, req.GetId())
    if err != nil { return nil, mapError(err) }
    if l := req.GetLocale(); l != "" {
        for i := range c.Translations {
            if c.Translations[i].Locale == l {
                if i != 0 { c.Translations[0], c.Translations[i] = c.Translations[i], c.Translations[0] }
                break
            }
        }
    }
    return &budgetv1.GetCategoryResponse{Category: toProtoCategory(c)}, nil
}

func (s *CategoryServer) ListCategories(ctx context.Context, req *budgetv1.ListCategoriesRequest) (*budgetv1.ListCategoriesResponse, error) {
    cs, err := s.svc.List(ctx, ctxTenantID(ctx), toKind(req.GetKind()), req.GetIncludeInactive())
    if err != nil { return nil, mapError(err) }
    if l := req.GetLocale(); l != "" {
        for i := range cs {
            for j := range cs[i].Translations {
                if cs[i].Translations[j].Locale == l {
                    if j != 0 { cs[i].Translations[0], cs[i].Translations[j] = cs[i].Translations[j], cs[i].Translations[0] }
                    break
                }
            }
        }
    }
    out := make([]*budgetv1.Category, 0, len(cs))
    for _, c := range cs { out = append(out, toProtoCategory(c)) }
    return &budgetv1.ListCategoriesResponse{Categories: out}, nil
}

func toProtoCategory(c domain.Category) *budgetv1.Category {
	trs := make([]*budgetv1.CategoryTranslation, 0, len(c.Translations))
	for _, tr := range c.Translations {
		trs = append(trs, &budgetv1.CategoryTranslation{Locale: tr.Locale, Name: tr.Name, Description: tr.Description})
	}
	var parent string
	if c.ParentID != nil {
		parent = *c.ParentID
	}
	return &budgetv1.Category{Id: c.ID, TenantId: c.TenantID, Kind: toProtoKind(c.Kind), Code: c.Code, ParentId: parent, IsActive: c.IsActive, Translations: trs}
}

func toKind(k budgetv1.CategoryKind) domain.CategoryKind {
	switch k {
	case budgetv1.CategoryKind_CATEGORY_KIND_INCOME:
		return domain.CategoryKindIncome
	case budgetv1.CategoryKind_CATEGORY_KIND_EXPENSE:
		return domain.CategoryKindExpense
	default:
		return ""
	}
}

func toProtoKind(k domain.CategoryKind) budgetv1.CategoryKind {
	switch k {
	case domain.CategoryKindIncome:
		return budgetv1.CategoryKind_CATEGORY_KIND_INCOME
	case domain.CategoryKindExpense:
		return budgetv1.CategoryKind_CATEGORY_KIND_EXPENSE
	default:
		return budgetv1.CategoryKind_CATEGORY_KIND_UNSPECIFIED
	}
}

func optString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// контекстные helpers (интерсептор аутентификации/тенанта)
func ctxTenantID(ctx context.Context) string {
    if v, ok := ctxutil.TenantIDFromContext(ctx); ok {
        return v
    }
    return ""
}
