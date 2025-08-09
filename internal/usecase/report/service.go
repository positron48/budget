package report

import (
    "context"
    "math/big"
    "time"

    "github.com/positron48/budget/internal/domain"
    txusecase "github.com/positron48/budget/internal/usecase/transaction"
)

type FxRepo interface {
	GetRateAsOf(ctx context.Context, from, to string, asOf time.Time) (rateDecimal string, provider string, err error)
}

type TenantRepo interface {
	GetByID(ctx context.Context, id string) (domain.Tenant, error)
}

type CategoryRepo interface {
	Get(ctx context.Context, id string) (domain.Category, error)
	GetMany(ctx context.Context, ids []string) (map[string]domain.Category, error)
}

type Service struct {
    txsvc   TxLister
	fx      FxRepo
	tenants TenantRepo
	cats    CategoryRepo
}

func NewService(txsvc TxLister, fx FxRepo, tenants TenantRepo, cats CategoryRepo) *Service {
	return &Service{txsvc: txsvc, fx: fx, tenants: tenants, cats: cats}
}

// TxLister abstracts listing transactions for reports
type TxLister interface {
    List(ctx context.Context, tenantID string, filter txusecase.ListFilter) ([]domain.Transaction, int64, error)
}

type MonthlyItem struct {
	CategoryID   string
	CategoryName string
	Type         domain.TransactionType
	Total        domain.Money
}

type MonthlySummary struct {
	Items        []MonthlyItem
	TotalIncome  domain.Money
	TotalExpense domain.Money
}

func (s *Service) GetMonthlySummary(ctx context.Context, tenantID string, year int, month int, locale string, targetCurrencyCode string) (MonthlySummary, error) {
	// compute month range
	from := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, 0)

	tenant, err := s.tenants.GetByID(ctx, tenantID)
	if err != nil {
		return MonthlySummary{}, err
	}
	baseCurrency := tenant.DefaultCurrencyCode
	target := targetCurrencyCode
	if target == "" {
		target = baseCurrency
	}

	// collect all transactions for month (page through)
	var all []domain.Transaction
	page := 1
	pageSize := 500
	for {
		f := txusecase.ListFilter{From: &from, To: &to, Page: page, PageSize: pageSize}
		items, total, err := s.txsvc.List(ctx, tenantID, f)
		if err != nil {
			return MonthlySummary{}, err
		}
		all = append(all, items...)
		if int64(page*pageSize) >= total || len(items) == 0 {
			break
		}
		page++
	}

	// aggregate by category+type
	type key struct {
		CatID string
		Type  domain.TransactionType
	}
	sums := make(map[key]int64)
	var totalIncomeMinor int64
	var totalExpenseMinor int64

	// use base_amount if target==base else convert base->target by rate per tx date
	for _, t := range all {
		baseMinor := t.BaseAmount.MinorUnits
		var minor int64
		if target == baseCurrency {
			minor = baseMinor
		} else {
			rateDec, _, err := s.fx.GetRateAsOf(ctx, baseCurrency, target, t.OccurredAt)
			if err != nil {
				return MonthlySummary{}, err
			}
			minor, err = convertMinorByRate(baseMinor, rateDec)
			if err != nil {
				return MonthlySummary{}, err
			}
		}
		k := key{CatID: t.CategoryID, Type: t.Type}
		sums[k] += minor
		if t.Type == domain.TransactionTypeIncome {
			totalIncomeMinor += minor
		} else if t.Type == domain.TransactionTypeExpense {
			totalExpenseMinor += minor
		}
	}

	// batch fetch categories for names
	catIDs := make([]string, 0, len(sums))
	for k := range sums {
		catIDs = append(catIDs, k.CatID)
	}
	catMap, _ := s.cats.GetMany(ctx, catIDs)
	items := make([]MonthlyItem, 0, len(sums))
	for k, mu := range sums {
		name := ""
		if cat, ok := catMap[k.CatID]; ok {
			for _, tr := range cat.Translations {
				if tr.Locale == locale && tr.Name != "" {
					name = tr.Name
					break
				}
			}
			if name == "" && len(cat.Translations) > 0 {
				name = cat.Translations[0].Name
			}
			if name == "" {
				name = cat.Code
			}
		}
		items = append(items, MonthlyItem{CategoryID: k.CatID, CategoryName: name, Type: k.Type, Total: domain.Money{CurrencyCode: target, MinorUnits: mu}})
	}

	return MonthlySummary{
		Items:        items,
		TotalIncome:  domain.Money{CurrencyCode: target, MinorUnits: totalIncomeMinor},
		TotalExpense: domain.Money{CurrencyCode: target, MinorUnits: totalExpenseMinor},
	}, nil
}

// convertMinorByRate multiplies integer minor units (2 decimals) by decimal rate and rounds to nearest minor unit
func convertMinorByRate(minor int64, rateDecimal string) (int64, error) {
	// amount as rational (minor / 100)
	num := big.NewInt(minor)
	den := big.NewInt(100)
	amount := new(big.Rat).SetFrac(num, den)
	// rate as rational
	r := new(big.Rat)
	if _, ok := r.SetString(rateDecimal); !ok {
		return 0, ErrParseRate
	}
	prod := new(big.Rat).Mul(amount, r) // value in target currency
	// to minor units: prod * 100, rounded
	prodMul := new(big.Rat).Mul(prod, big.NewRat(100, 1))
	p := new(big.Int).Set(prodMul.Num())
	q := new(big.Int).Set(prodMul.Denom())
	// round to nearest: (p + q/2) / q
	halfQ := new(big.Int).Rsh(q, 1) // q/2 floor
	p.Add(p, halfQ)
	res := new(big.Int).Quo(p, q)
	return res.Int64(), nil
}

var ErrParseRate = domainError("invalid fx rate")

type domainError string

func (e domainError) Error() string { return string(e) }
