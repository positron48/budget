package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/positron48/budget/internal/domain"
	txusecase "github.com/positron48/budget/internal/usecase/transaction"
)

type TransactionRepo struct{ pool *Pool }

func NewTransactionRepo(pool *Pool) *TransactionRepo { return &TransactionRepo{pool: pool} }

func (r *TransactionRepo) Create(ctx context.Context, tx domain.Transaction) (domain.Transaction, error) {
	var id string
	var fxRate *string
	var fxProvider *string
	var fxAsOf *time.Time
	if tx.Fx != nil {
		fxRate = strPtr(tx.Fx.RateDecimal)
		fxProvider = strPtr(tx.Fx.Provider)
		asOf := tx.Fx.AsOf.Truncate(24 * time.Hour)
		fxAsOf = &asOf
	}
	if err := r.pool.DB.QueryRow(ctx,
		`INSERT INTO transactions (tenant_id, user_id, category_id, type, amount_numeric, currency_code,
                                   base_amount_numeric, base_currency_code, fx_rate, fx_provider, fx_as_of, occurred_at, comment)
          VALUES ($1,$2,$3,$4,$5::numeric,$6,
                  CASE WHEN $8::numeric IS NULL THEN $5::numeric ELSE ($5::numeric * $8::numeric) END,
                  $7, $8::numeric, $9, $10, $11, $12)
          RETURNING id`,
		tx.TenantID, tx.UserID, tx.CategoryID, string(tx.Type),
		toDecimal(tx.Amount.MinorUnits), tx.Amount.CurrencyCode,
		tx.BaseAmount.CurrencyCode,
		fxRate, fxProvider, fxAsOf, tx.OccurredAt, tx.Comment,
	).Scan(&id); err != nil {
		return domain.Transaction{}, err
	}
	return r.Get(ctx, id)
}

func (r *TransactionRepo) Update(ctx context.Context, tx domain.Transaction) (domain.Transaction, error) {
	var fxRate *string
	var fxProvider *string
	var fxAsOf *time.Time
	if tx.Fx != nil {
		fxRate = strPtr(tx.Fx.RateDecimal)
		fxProvider = strPtr(tx.Fx.Provider)
		asOf := tx.Fx.AsOf.Truncate(24 * time.Hour)
		fxAsOf = &asOf
	}
	_, err := r.pool.DB.Exec(ctx,
		`UPDATE transactions
           SET category_id=$2, type=$3, amount_numeric=$4::numeric, currency_code=$5,
               base_amount_numeric=CASE WHEN $7::numeric IS NULL THEN $4::numeric ELSE ($4::numeric * $7::numeric) END,
               base_currency_code=$6, fx_rate=$7::numeric, fx_provider=$8, fx_as_of=$9, occurred_at=$10, comment=$11
         WHERE id=$1`,
		tx.ID, tx.CategoryID, string(tx.Type), toDecimal(tx.Amount.MinorUnits), tx.Amount.CurrencyCode,
		tx.BaseAmount.CurrencyCode, fxRate, fxProvider, fxAsOf, tx.OccurredAt, tx.Comment,
	)
	if err != nil {
		return domain.Transaction{}, err
	}
	return r.Get(ctx, tx.ID)
}

func (r *TransactionRepo) Delete(ctx context.Context, id string) error {
	_, err := r.pool.DB.Exec(ctx, `DELETE FROM transactions WHERE id=$1`, id)
	return err
}

func (r *TransactionRepo) Get(ctx context.Context, id string) (domain.Transaction, error) {
	var t domain.Transaction
	var amountDec, baseDec string
	var typ string
	var fxRate, fxProvider *string
	var fxAsOf *time.Time
	err := r.pool.DB.QueryRow(ctx,
		`SELECT id, tenant_id, user_id, category_id, type::text, amount_numeric::text, currency_code,
                base_amount_numeric::text, base_currency_code, fx_rate::text, fx_provider, fx_as_of, occurred_at, comment, created_at
           FROM transactions WHERE id=$1`, id,
	).Scan(&t.ID, &t.TenantID, &t.UserID, &t.CategoryID, &typ, &amountDec, &t.Amount.CurrencyCode, &baseDec, &t.BaseAmount.CurrencyCode, &fxRate, &fxProvider, &fxAsOf, &t.OccurredAt, &t.Comment, &t.CreatedAt)
	if err != nil {
		return domain.Transaction{}, err
	}
	t.Type = domain.TransactionType(typ)
	t.Amount.MinorUnits = fromDecimal(amountDec)
	t.BaseAmount.MinorUnits = fromDecimal(baseDec)
	if fxRate != nil && *fxRate != "" {
		var asOf time.Time
		if fxAsOf != nil {
			asOf = fxAsOf.Truncate(24 * time.Hour)
		}
		t.Fx = &domain.FxInfo{FromCurrency: t.Amount.CurrencyCode, ToCurrency: t.BaseAmount.CurrencyCode, RateDecimal: *fxRate, Provider: deref(fxProvider), AsOf: asOf}
	}
	return t, nil
}

func (r *TransactionRepo) List(ctx context.Context, tenantID string, filter txusecase.ListFilter) ([]domain.Transaction, int64, error) {
	var where []string
	var args []any
	add := func(cond string, val any) {
		where = append(where, fmt.Sprintf(cond, len(args)+1))
		args = append(args, val)
	}
	add("tenant_id=$%d", tenantID)
	if filter.From != nil {
		add("occurred_at >= $%d", *filter.From)
	}
	if filter.To != nil {
		add("occurred_at < $%d", *filter.To)
	}
	if len(filter.CategoryIDs) > 0 {
		add("category_id = ANY($%d)", filter.CategoryIDs)
	}
	if filter.Type != nil {
		add("type = $%d", string(*filter.Type))
	}
	if filter.MinMinorUnits != nil {
		add("amount_numeric >= $%d::numeric", toDecimal(*filter.MinMinorUnits))
	}
	if filter.MaxMinorUnits != nil {
		add("amount_numeric <= $%d::numeric", toDecimal(*filter.MaxMinorUnits))
	}
	if filter.CurrencyCode != nil && *filter.CurrencyCode != "" {
		add("currency_code = $%d", *filter.CurrencyCode)
	}
	if filter.Search != nil && *filter.Search != "" {
		add("comment ILIKE $%d", "%"+*filter.Search+"%")
	}
	clause := strings.Join(where, " AND ")

	// pagination
	page := filter.Page
	if page < 1 {
		page = 1
	}
	size := filter.PageSize
	if size <= 0 || size > 500 {
		size = 50
	}
	offset := (page - 1) * size

	var total int64
	if err := r.pool.DB.QueryRow(ctx, "SELECT COUNT(*) FROM transactions WHERE "+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// OFFSET/LIMIT placeholders
	offIdx := len(args) + 1
	limIdx := len(args) + 2
	
	// Build ORDER BY clause
	orderBy := "occurred_at DESC" // default sort
	if filter.Sort != "" {
		// Validate and sanitize sort parameter
		validSortFields := map[string]string{
			"occurred_at":        "occurred_at",
			"occurred_at asc":    "occurred_at ASC",
			"occurred_at desc":   "occurred_at DESC",
			"amount_numeric":     "CASE WHEN type = 'expense' THEN -amount_numeric ELSE amount_numeric END",
			"amount_numeric asc": "CASE WHEN type = 'expense' THEN -amount_numeric ELSE amount_numeric END ASC",
			"amount_numeric desc": "CASE WHEN type = 'expense' THEN -amount_numeric ELSE amount_numeric END DESC",
			"comment":            "comment",
			"comment asc":        "comment ASC",
			"comment desc":       "comment DESC",
			"type":               "type",
			"type asc":           "type ASC",
			"type desc":          "type DESC",
			"created_at":         "created_at",
			"created_at asc":     "created_at ASC",
			"created_at desc":    "created_at DESC",
			"category_code":      "category_code",
			"category_code asc":  "category_code ASC",
			"category_code desc": "category_code DESC",
		}
		if validSort, exists := validSortFields[strings.ToLower(filter.Sort)]; exists {
			orderBy = validSort
		}
	}
	
	// Check if we need to join with categories for sorting
	needsCategoryJoin := strings.Contains(strings.ToLower(filter.Sort), "category_code")
	
	var query string
	if needsCategoryJoin {
		// Replace category_code with c.code in ORDER BY clause
		orderByWithJoin := strings.ReplaceAll(orderBy, "category_code", "c.code")
		query = fmt.Sprintf(
			"SELECT t.id, t.tenant_id, t.user_id, t.category_id, t.type::text, t.amount_numeric::text, t.currency_code, t.base_amount_numeric::text, t.base_currency_code, t.fx_rate::text, t.fx_provider, t.fx_as_of, t.occurred_at, t.comment, t.created_at FROM transactions t LEFT JOIN categories c ON t.category_id = c.id WHERE %s ORDER BY %s OFFSET $%d LIMIT $%d",
			strings.ReplaceAll(clause, "tenant_id=$1", "t.tenant_id=$1"), orderByWithJoin, offIdx, limIdx,
		)
	} else {
		query = fmt.Sprintf(
			"SELECT id, tenant_id, user_id, category_id, type::text, amount_numeric::text, currency_code, base_amount_numeric::text, base_currency_code, fx_rate::text, fx_provider, fx_as_of, occurred_at, comment, created_at FROM transactions WHERE %s ORDER BY %s OFFSET $%d LIMIT $%d",
			clause, orderBy, offIdx, limIdx,
		)
	}
	rows, err := r.pool.DB.Query(ctx, query, append(args, offset, size)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var list []domain.Transaction
	for rows.Next() {
		var t domain.Transaction
		var typ, amountDec, baseDec string
		var fxRate, fxProvider *string
		var fxAsOf *time.Time
		if err := rows.Scan(&t.ID, &t.TenantID, &t.UserID, &t.CategoryID, &typ, &amountDec, &t.Amount.CurrencyCode, &baseDec, &t.BaseAmount.CurrencyCode, &fxRate, &fxProvider, &fxAsOf, &t.OccurredAt, &t.Comment, &t.CreatedAt); err != nil {
			return nil, 0, err
		}
		t.Type = domain.TransactionType(typ)
		t.Amount.MinorUnits = fromDecimal(amountDec)
		t.BaseAmount.MinorUnits = fromDecimal(baseDec)
		if fxRate != nil && *fxRate != "" {
			var asOf time.Time
			if fxAsOf != nil {
				asOf = fxAsOf.Truncate(24 * time.Hour)
			}
			t.Fx = &domain.FxInfo{FromCurrency: t.Amount.CurrencyCode, ToCurrency: t.BaseAmount.CurrencyCode, RateDecimal: *fxRate, Provider: deref(fxProvider), AsOf: asOf}
		}
		list = append(list, t)
	}
	return list, total, rows.Err()
}

// Totals computes income/expense totals in base currency according to filter (ignoring pagination)
func (r *TransactionRepo) Totals(ctx context.Context, tenantID string, filter txusecase.ListFilter) (int64, int64, string, error) {
	var where []string
	var args []any
	add := func(cond string, val any) {
		where = append(where, fmt.Sprintf(cond, len(args)+1))
		args = append(args, val)
	}
	add("tenant_id=$%d", tenantID)
	if filter.From != nil {
		add("occurred_at >= $%d", *filter.From)
	}
	if filter.To != nil {
		add("occurred_at < $%d", *filter.To)
	}
	if len(filter.CategoryIDs) > 0 {
		add("category_id = ANY($%d)", filter.CategoryIDs)
	}
	if filter.Type != nil {
		add("type = $%d", string(*filter.Type))
	}
	if filter.MinMinorUnits != nil {
		add("amount_numeric >= $%d::numeric", toDecimal(*filter.MinMinorUnits))
	}
	if filter.MaxMinorUnits != nil {
		add("amount_numeric <= $%d::numeric", toDecimal(*filter.MaxMinorUnits))
	}
	if filter.CurrencyCode != nil && *filter.CurrencyCode != "" {
		add("currency_code = $%d", *filter.CurrencyCode)
	}
	if filter.Search != nil && *filter.Search != "" {
		add("comment ILIKE $%d", "%"+*filter.Search+"%")
	}
	clause := strings.Join(where, " AND ")

	// Sum by base amount to avoid FX conversion per-request
	// base_currency_code is same for tenant (default), but keep it just in case
	var incomeDec, expenseDec, baseCurrency string
	err := r.pool.DB.QueryRow(ctx,
		"SELECT "+
			"COALESCE(SUM(CASE WHEN type='income' THEN base_amount_numeric END), 0)::text AS income, "+
			"COALESCE(SUM(CASE WHEN type='expense' THEN base_amount_numeric END), 0)::text AS expense, "+
			"COALESCE(MAX(base_currency_code), '') AS base_ccy "+
			"FROM transactions WHERE "+clause,
		args...,
	).Scan(&incomeDec, &expenseDec, &baseCurrency)
	if err != nil {
		return 0, 0, "", err
	}
	return fromDecimal(incomeDec), fromDecimal(expenseDec), baseCurrency, nil
}

// Helpers
func toDecimal(minor int64) string { // 2 decimals
	neg := minor < 0
	if neg {
		minor = -minor
	}
	s := fmt.Sprintf("%d", minor)
	if len(s) <= 2 {
		s = "0." + strings.Repeat("0", 2-len(s)) + s
	} else {
		s = s[:len(s)-2] + "." + s[len(s)-2:]
	}
	if neg {
		s = "-" + s
	}
	return s
}

func fromDecimal(dec string) int64 { // parse "123.45" → 12345
	if dec == "" {
		return 0
	}
	neg := strings.HasPrefix(dec, "-")
	dec = strings.TrimPrefix(dec, "-")
	parts := strings.SplitN(dec, ".", 2)
	whole := parts[0]
	frac := "00"
	if len(parts) == 2 {
		frac = parts[1]
	}
	if len(frac) < 2 {
		frac = frac + strings.Repeat("0", 2-len(frac))
	}
	if len(frac) > 2 {
		frac = frac[:2]
	}
	numStr := whole + frac
	var v int64
	if _, err := fmt.Sscanf(numStr, "%d", &v); err != nil {
		return 0
	}
	if neg {
		v = -v
	}
	return v
}

func strPtr(v string) *string { return &v }
func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
