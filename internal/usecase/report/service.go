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
	GetDateRange(ctx context.Context, tenantID string) (earliest, latest time.Time, err error)
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

type MonthlyCategoryData struct {
	CategoryID    string
	CategoryName  string
	Type          domain.TransactionType
	MonthlyTotals []domain.Money // 12 months of data
	Total         domain.Money   // sum of all months
}

type SummaryReport struct {
	Categories   []MonthlyCategoryData
	Months       []string // month labels like "2024-01", "2024-02", etc.
	TotalIncome  domain.Money
	TotalExpense domain.Money
}

type DateRange struct {
	EarliestDate string // YYYY-MM-DD format
	LatestDate   string // YYYY-MM-DD format
}

func (s *Service) GetMonthlySummary(ctx context.Context, tenantID string, year int, month int, locale string, targetCurrencyCode string, tzOffsetMinutes int) (MonthlySummary, error) {
	// compute month range using client timezone offset to avoid crossing day boundaries
	// tzOffsetMinutes comes from JS getTimezoneOffset(), e.g. Moscow is -180
	fromLocal := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.FixedZone("client", -tzOffsetMinutes*60))
	toLocal := fromLocal.AddDate(0, 1, 0)
	// convert to UTC instants for DB comparison
	from := fromLocal.UTC()
	to := toLocal.UTC()

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
		switch t.Type {
		case domain.TransactionTypeIncome:
			totalIncomeMinor += minor
		case domain.TransactionTypeExpense:
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

func (s *Service) GetSummaryReport(ctx context.Context, tenantID string, fromDate, toDate, locale, targetCurrencyCode string, tzOffsetMinutes int) (SummaryReport, error) {
	// Parse date range
	fromLocal, err := time.Parse("2006-01-02", fromDate)
	if err != nil {
		return SummaryReport{}, err
	}
	toLocal, err := time.Parse("2006-01-02", toDate)
	if err != nil {
		return SummaryReport{}, err
	}

	// Apply timezone offset
	fromLocal = fromLocal.In(time.FixedZone("client", -tzOffsetMinutes*60))
	toLocal = toLocal.In(time.FixedZone("client", -tzOffsetMinutes*60))

	// Convert to UTC for DB comparison
	from := fromLocal.UTC()
	to := toLocal.UTC()

	// Add one day to include the end date
	to = to.Add(24 * time.Hour)

	tenant, err := s.tenants.GetByID(ctx, tenantID)
	if err != nil {
		return SummaryReport{}, err
	}
	baseCurrency := tenant.DefaultCurrencyCode
	target := targetCurrencyCode
	if target == "" {
		target = baseCurrency
	}

	// Generate month labels for the date range
	months := generateMonthLabels(fromLocal, toLocal)

	// collect all transactions for the period (page through)
	var all []domain.Transaction
	page := 1
	pageSize := 500
	for {
		f := txusecase.ListFilter{From: &from, To: &to, Page: page, PageSize: pageSize}
		items, total, err := s.txsvc.List(ctx, tenantID, f)
		if err != nil {
			return SummaryReport{}, err
		}
		all = append(all, items...)
		if int64(page*pageSize) >= total || len(items) == 0 {
			break
		}
		page++
	}

	// aggregate by category+type+month
	type key struct {
		CatID string
		Type  domain.TransactionType
		Month int // 0-11 for months in the range
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
				return SummaryReport{}, err
			}
			minor, err = convertMinorByRate(baseMinor, rateDec)
			if err != nil {
				return SummaryReport{}, err
			}
		}

		// Determine which month this transaction belongs to
		txMonth := getMonthIndex(t.OccurredAt, fromLocal, len(months))
		if txMonth >= 0 && txMonth < len(months) {
			k := key{CatID: t.CategoryID, Type: t.Type, Month: txMonth}
			sums[k] += minor
		}

		switch t.Type {
		case domain.TransactionTypeIncome:
			totalIncomeMinor += minor
		case domain.TransactionTypeExpense:
			totalExpenseMinor += minor
		}
	}

	// batch fetch categories for names
	catIDs := make([]string, 0, len(sums))
	catSet := make(map[string]bool)
	for k := range sums {
		if !catSet[k.CatID] {
			catIDs = append(catIDs, k.CatID)
			catSet[k.CatID] = true
		}
	}
	catMap, _ := s.cats.GetMany(ctx, catIDs)

	// Group by category+type
	type catKey struct {
		CatID string
		Type  domain.TransactionType
	}
	catGroups := make(map[catKey][]int64) // monthly totals
	catTotals := make(map[catKey]int64)   // total for category

	for k, amount := range sums {
		ck := catKey{CatID: k.CatID, Type: k.Type}
		if catGroups[ck] == nil {
			catGroups[ck] = make([]int64, len(months))
		}
		catGroups[ck][k.Month] += amount
		catTotals[ck] += amount
	}

	categories := make([]MonthlyCategoryData, 0, len(catGroups))
	for ck, monthlyAmounts := range catGroups {
		name := ""
		if cat, ok := catMap[ck.CatID]; ok {
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

		monthlyTotals := make([]domain.Money, len(months))
		for i, amount := range monthlyAmounts {
			monthlyTotals[i] = domain.Money{CurrencyCode: target, MinorUnits: amount}
		}

		categories = append(categories, MonthlyCategoryData{
			CategoryID:    ck.CatID,
			CategoryName:  name,
			Type:          ck.Type,
			MonthlyTotals: monthlyTotals,
			Total:         domain.Money{CurrencyCode: target, MinorUnits: catTotals[ck]},
		})
	}

	return SummaryReport{
		Categories:   categories,
		Months:       months,
		TotalIncome:  domain.Money{CurrencyCode: target, MinorUnits: totalIncomeMinor},
		TotalExpense: domain.Money{CurrencyCode: target, MinorUnits: totalExpenseMinor},
	}, nil
}

// generateMonthLabels creates month labels for the date range
func generateMonthLabels(from, to time.Time) []string {
	var months []string
	current := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, from.Location())
	end := time.Date(to.Year(), to.Month(), 1, 0, 0, 0, 0, to.Location())

	for current.Before(end) || current.Equal(end) {
		months = append(months, current.Format("2006-01"))
		current = current.AddDate(0, 1, 0)
	}

	return months
}

// getMonthIndex returns the index of the month for a transaction within the date range
func getMonthIndex(txTime time.Time, from time.Time, numMonths int) int {
	txLocal := txTime.In(from.Location())
	fromStart := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, from.Location())

	// Calculate months difference
	monthsDiff := int(txLocal.Year()-fromStart.Year())*12 + int(txLocal.Month()-fromStart.Month())

	if monthsDiff >= 0 && monthsDiff < numMonths {
		return monthsDiff
	}
	return -1
}

func (s *Service) GetDateRange(ctx context.Context, tenantID string, locale string, tzOffsetMinutes int) (DateRange, error) {
	// Get min and max dates directly from database with one efficient query
	earliest, latest, err := s.txsvc.GetDateRange(ctx, tenantID)
	if err != nil {
		return DateRange{}, err
	}

	var earliestDate, latestDate string

	// If no transactions found, use current date
	if earliest.IsZero() && latest.IsZero() {
		now := time.Now().In(time.FixedZone("client", -tzOffsetMinutes*60))
		earliestDate = now.Format("2006-01-02")
		latestDate = now.Format("2006-01-02")
	} else {
		// Convert to local timezone
		earliestLocal := earliest.In(time.FixedZone("client", -tzOffsetMinutes*60))
		latestLocal := latest.In(time.FixedZone("client", -tzOffsetMinutes*60))

		earliestDate = earliestLocal.Format("2006-01-02")
		latestDate = latestLocal.Format("2006-01-02")
	}

	return DateRange{
		EarliestDate: earliestDate,
		LatestDate:   latestDate,
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
