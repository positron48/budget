package postgres

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/positron48/budget/internal/domain"
	rp "github.com/positron48/budget/internal/usecase/recurringpayment"
	"time"
)

type RecurringPaymentRepo struct{ pool *Pool }

func NewRecurringPaymentRepo(p *Pool) *RecurringPaymentRepo { return &RecurringPaymentRepo{p} }
func paymentError(e error) error {
	if errors.Is(e, pgx.ErrNoRows) {
		return rp.ErrNotFound
	}
	return e
}

const templateSelect = `SELECT id,tenant_id,name,category_id,currency_code,COALESCE(account_id::text,''),note,timezone,amount_mode,fixed_amount,initial_estimate,to_char(first_month,'YYYY-MM'),COALESCE(to_char(last_month,'YYYY-MM'),''),to_char(effective_month,'YYYY-MM'),interval_months,day,last_day,period_offset,archived,version,user_id,created_at,updated_at FROM recurring_payment_templates WHERE tenant_id=$1 ORDER BY created_at,id`

func (r *RecurringPaymentRepo) Templates(ctx context.Context, tenant string) ([]domain.PaymentTemplate, error) {
	rows, e := r.pool.query(ctx).Query(ctx, templateSelect, tenant)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []domain.PaymentTemplate{}
	for rows.Next() {
		var t domain.PaymentTemplate
		e = rows.Scan(&t.ID, &t.TenantID, &t.Name, &t.CategoryID, &t.CurrencyCode, &t.AccountID, &t.Note, &t.Timezone, &t.AmountMode, &t.FixedAmount, &t.InitialEstimate, &t.FirstMonth, &t.LastMonth, &t.EffectiveMonth, &t.IntervalMonths, &t.Day, &t.LastDay, &t.PeriodOffset, &t.Archived, &t.Version, &t.UserID, &t.CreatedAt, &t.UpdatedAt)
		if e != nil {
			return nil, e
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// The tenant lock shared with financial writes also serializes read-time generation and template edits.
func (r *RecurringPaymentRepo) generate(ctx context.Context, tenant, through string, now time.Time) error {
	templates, e := r.Templates(ctx, tenant)
	if e != nil {
		return e
	}
	end, e := rp.Month(through)
	if e != nil {
		return e
	}
	for _, t := range templates {
		if t.Archived {
			continue
		}
		start, _ := rp.Month(t.FirstMonth)
		last := end
		loc, e := time.LoadLocation(t.Timezone)
		if e != nil {
			return rp.ErrInvalid
		}
		localNow := now.In(loc)
		horizon := time.Date(localNow.Year(), localNow.Month()+12, 1, 0, 0, 0, 0, time.UTC)
		if last.Before(horizon) {
			last = horizon
		}
		if t.LastMonth != "" {
			m, _ := rp.Month(t.LastMonth)
			if m.Before(last) {
				last = m
			}
		}
		if (last.Year()-start.Year())*12+int(last.Month()-start.Month()) > 1200 {
			return rp.ErrInvalid
		}
		for m := start; !m.After(last); m = m.AddDate(0, t.IntervalMonths, 0) {
			if m.Format("2006-01") < t.EffectiveMonth {
				continue
			}
			o := rp.Occurrence(t, m)
			if e = r.insertOccurrence(ctx, o); e != nil {
				return e
			}
		}
	}
	return nil
}

const insertPaymentOccurrence = `INSERT INTO recurring_payment_occurrences(tenant_id,template_id,scheduled_month,period_month,due_date,name,category_id,currency_code,account_id,note,timezone,amount_mode,exact_amount,initial_estimate,user_id) VALUES($1,$2,$3::date,$4::date,$5::date,$6,$7,$8,NULLIF($9,'')::uuid,$10,$11,$12,$13,$14,$15) ON CONFLICT(tenant_id,template_id,scheduled_month) DO NOTHING`

func (r *RecurringPaymentRepo) insertOccurrence(ctx context.Context, o domain.PaymentOccurrence) error {
	_, e := r.pool.query(ctx).Exec(ctx, insertPaymentOccurrence, o.TenantID, o.TemplateID, o.ScheduledMonth+"-01", o.PeriodMonth+"-01", o.DueDate, o.Name, o.CategoryID, o.CurrencyCode, o.AccountID, o.Note, o.Timezone, o.AmountMode, o.ExactAmount, o.InitialEstimate, o.UserID)
	return e
}
func (r *RecurringPaymentRepo) Read(ctx context.Context, tenant, through string, now time.Time) ([]domain.PaymentOccurrence, error) {
	return financialWrite(ctx, r.pool, tenant, "materialize_payments", "", nil, func(ctx context.Context) ([]domain.PaymentOccurrence, error) {
		if e := r.generate(ctx, tenant, through, now); e != nil {
			return nil, e
		}
		return r.read(ctx, tenant)
	})
}
func (r *RecurringPaymentRepo) read(ctx context.Context, tenant string) ([]domain.PaymentOccurrence, error) {
	rows, e := r.pool.query(ctx).Query(ctx, `SELECT o.id,o.tenant_id,o.template_id,to_char(o.scheduled_month,'YYYY-MM'),to_char(o.period_month,'YYYY-MM'),to_char(o.due_date,'YYYY-MM-DD'),o.name,o.category_id,o.currency_code,COALESCE(o.account_id::text,''),o.note,o.timezone,o.amount_mode,o.exact_amount,o.estimate_override,o.initial_estimate,o.skipped,o.skip_reason,o.manual,o.version,o.user_id,o.updated_at,l.transaction_id::text,l.confirmed_amount,COALESCE(l.confirmed_currency,''),COALESCE(l.confirmed_type,''),COALESCE(l.confirmed_by::text,''),l.confirmed_at FROM recurring_payment_occurrences o LEFT JOIN recurring_payment_links l ON l.tenant_id=o.tenant_id AND l.occurrence_id=o.id WHERE o.tenant_id=$1 ORDER BY o.due_date,o.id`, tenant)
	if e != nil {
		return nil, e
	}
	out := []domain.PaymentOccurrence{}
	txIDs := []string{}
	for rows.Next() {
		var o domain.PaymentOccurrence
		var txID *string
		e = rows.Scan(&o.ID, &o.TenantID, &o.TemplateID, &o.ScheduledMonth, &o.PeriodMonth, &o.DueDate, &o.Name, &o.CategoryID, &o.CurrencyCode, &o.AccountID, &o.Note, &o.Timezone, &o.AmountMode, &o.ExactAmount, &o.EstimateOverride, &o.InitialEstimate, &o.Skipped, &o.SkipReason, &o.Manual, &o.Version, &o.UserID, &o.UpdatedAt, &txID, &o.ConfirmedAmount, &o.ConfirmedCurrency, &o.ConfirmedType, &o.ConfirmedBy, &o.ConfirmedAt)
		if e != nil {
			rows.Close()
			return nil, e
		}
		id := ""
		if txID != nil {
			id = *txID
		}
		txIDs = append(txIDs, id)
		out = append(out, o)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	repo := NewTransactionRepo(r.pool)
	for i, id := range txIDs {
		if id != "" {
			t, e := repo.Get(ctx, id)
			if e != nil {
				return nil, e
			}
			out[i].Transaction = &t
		}
	}
	return out, nil
}
func (r *RecurringPaymentRepo) get(ctx context.Context, tenant, id string, lock bool) (domain.PaymentOccurrence, error) {
	if _, e := uuid.Parse(id); e != nil {
		return domain.PaymentOccurrence{}, rp.ErrInvalid
	}
	if lock {
		var found string
		if e := r.pool.query(ctx).QueryRow(ctx, `SELECT id FROM recurring_payment_occurrences WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, id).Scan(&found); e != nil {
			return domain.PaymentOccurrence{}, paymentError(e)
		}
	}
	all, e := r.read(ctx, tenant)
	if e != nil {
		return domain.PaymentOccurrence{}, e
	}
	for _, o := range all {
		if o.ID == id {
			return o, nil
		}
	}
	return domain.PaymentOccurrence{}, rp.ErrNotFound
}
func (r *RecurringPaymentRepo) validateTemplate(ctx context.Context, tenant string, t domain.PaymentTemplate) error {
	var valid bool
	e := r.pool.query(ctx).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM categories WHERE tenant_id=$1 AND id=$2 AND kind='expense' AND is_active)`, tenant, t.CategoryID).Scan(&valid)
	if e != nil {
		return e
	}
	if !valid {
		return rp.ErrPrecondition
	}
	if t.AccountID != "" {
		_, e = validateAssetLeg(ctx, r.pool, tenant, t.AccountID, t.CurrencyCode, true)
		if e != nil {
			return rp.ErrPrecondition
		}
	}
	return nil
}
func (r *RecurringPaymentRepo) ChangeTemplate(ctx context.Context, tenant, user string, c rp.TemplateChange, now time.Time) (rp.TemplateResult, error) {
	if c.Archive && c.Template.ID == "" {
		return rp.TemplateResult{}, rp.ErrInvalid
	}
	if c.RequestKey == "" || len(c.RequestKey) > 128 {
		return rp.TemplateResult{}, rp.ErrInvalid
	}
	return financialWrite(ctx, r.pool, tenant, "change_payment_template", c.RequestKey, c, func(ctx context.Context) (rp.TemplateResult, error) {
		if c.Template.ID != "" {
			if _, e := uuid.Parse(c.Template.ID); e != nil {
				return rp.TemplateResult{}, rp.ErrInvalid
			}
		}
		t := c.Template
		t.TenantID = tenant
		t.UserID = user
		if !c.Archive {
			if e := r.validateTemplate(ctx, tenant, t); e != nil {
				return rp.TemplateResult{}, e
			}
		}
		if e := r.generate(ctx, tenant, now.Format("2006-01"), now); e != nil {
			return rp.TemplateResult{}, e
		}
		var old *domain.PaymentTemplate
		if t.ID != "" {
			all, e := r.Templates(ctx, tenant)
			if e != nil {
				return rp.TemplateResult{}, e
			}
			for _, v := range all {
				if v.ID == t.ID {
					v := v
					old = &v
				}
			}
			if old == nil {
				return rp.TemplateResult{}, rp.ErrNotFound
			}
			if old.Version != t.Version {
				return rp.TemplateResult{}, rp.ErrConflict
			}
			if old.Archived {
				return rp.TemplateResult{}, rp.ErrPrecondition
			}
		}
		cutoff := now.Format("2006-01") + "-01"
		if old != nil {
			loc, _ := time.LoadLocation(old.Timezone)
			cutoff = now.In(loc).Format("2006-01") + "-01"
		}
		result := rp.TemplateResult{Template: t}
		if old != nil {
			items, e := r.read(ctx, tenant)
			if e != nil {
				return result, e
			}
			for _, o := range items {
				if o.TemplateID != t.ID || o.ScheduledMonth+"-01" <= cutoff {
					continue
				}
				if o.Manual || o.Transaction != nil || o.Skipped {
					result.Preserved = append(result.Preserved, o)
				} else {
					result.Affected = append(result.Affected, o)
				}
			}
		}

		if old != nil && !c.Archive {
			first, _ := rp.Month(t.FirstMonth)
			start, _ := rp.Month(cutoff[:7])
			end := start.AddDate(0, 12, 0)
			preserved := map[string]bool{}
			for _, o := range result.Preserved {
				preserved[o.ScheduledMonth] = true
			}
			for m := first; !m.After(end); m = m.AddDate(0, t.IntervalMonths, 0) {
				if !m.After(start) || t.LastMonth != "" && m.Format("2006-01") > t.LastMonth || preserved[m.Format("2006-01")] {
					continue
				}
				result.Proposed = append(result.Proposed, rp.Occurrence(t, m))
			}
		}
		if c.Preview {
			return result, nil
		}
		if c.Archive {
			_, e := r.pool.query(ctx).Exec(ctx, `UPDATE recurring_payment_templates SET archived=true,version=version+1,user_id=$3,updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenant, t.ID, user)
			if e != nil {
				return result, e
			}
		} else if old == nil {
			e := r.pool.query(ctx).QueryRow(ctx, `INSERT INTO recurring_payment_templates(tenant_id,name,category_id,currency_code,account_id,note,timezone,amount_mode,fixed_amount,initial_estimate,effective_month,first_month,last_month,interval_months,day,last_day,period_offset,created_by,user_id) VALUES($1,$2,$3,$4,NULLIF($5,'')::uuid,$6,$7,$8,$9,$10,$11::date,$11::date,NULLIF($12,'')::date,$13,$14,$15,$16,$17,$17) RETURNING id`, tenant, t.Name, t.CategoryID, t.CurrencyCode, t.AccountID, t.Note, t.Timezone, t.AmountMode, t.FixedAmount, t.InitialEstimate, t.FirstMonth+"-01", monthDate(t.LastMonth), t.IntervalMonths, t.Day, t.LastDay, t.PeriodOffset, user).Scan(&t.ID)
			if e != nil {
				return result, e
			}
		} else {
			_, e := r.pool.query(ctx).Exec(ctx, `UPDATE recurring_payment_templates SET name=$3,category_id=$4,currency_code=$5,account_id=NULLIF($6,'')::uuid,note=$7,timezone=$8,amount_mode=$9,fixed_amount=$10,initial_estimate=$11,first_month=$12::date,last_month=NULLIF($13,'')::date,interval_months=$14,day=$15,last_day=$16,period_offset=$17,effective_month=($19::date + interval '1 month')::date,version=version+1,user_id=$18,updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenant, t.ID, t.Name, t.CategoryID, t.CurrencyCode, t.AccountID, t.Note, t.Timezone, t.AmountMode, t.FixedAmount, t.InitialEstimate, t.FirstMonth+"-01", monthDate(t.LastMonth), t.IntervalMonths, t.Day, t.LastDay, t.PeriodOffset, user, cutoff)
			if e != nil {
				return result, e
			}
		}
		if old != nil {
			_, e := r.pool.query(ctx).Exec(ctx, deleteUntouchedPaymentOccurrences, tenant, t.ID, cutoff)
			if e != nil {
				return result, e
			}
		}
		// Generate future rows only for an edited template; historical snapshots retain their original settings.
		if old != nil && !c.Archive {
			first, _ := rp.Month(t.FirstMonth)
			start, _ := rp.Month(cutoff[:7])
			end := start.AddDate(0, 12, 0)
			for m := first; !m.After(end); m = m.AddDate(0, t.IntervalMonths, 0) {
				if !m.After(start) || t.LastMonth != "" && m.Format("2006-01") > t.LastMonth {
					continue
				}
				if e := r.insertOccurrence(ctx, rp.Occurrence(t, m)); e != nil {
					return result, e
				}
			}
		} else if old == nil {
			if e := r.generate(ctx, tenant, now.Format("2006-01"), now); e != nil {
				return result, e
			}
		}
		templates, e := r.Templates(ctx, tenant)
		if e != nil {
			return result, e
		}
		for _, v := range templates {
			if v.ID == t.ID {
				result.Template = v
			}
		}
		return result, nil
	})
}
func monthDate(s string) string {
	if s == "" {
		return ""
	}
	return s + "-01"
}
func (r *RecurringPaymentRepo) ChangeOccurrence(ctx context.Context, tenant, user string, c rp.Change) (domain.PaymentOccurrence, error) {
	return financialWrite(ctx, r.pool, tenant, "change_payment_occurrence", c.RequestKey, c, func(ctx context.Context) (domain.PaymentOccurrence, error) {
		o, e := r.get(ctx, tenant, c.ID, true)
		if e != nil {
			return o, e
		}
		if o.Version != c.Version {
			return o, rp.ErrConflict
		}
		switch c.Action {
		case "unlink":
			if o.Transaction == nil {
				return o, rp.ErrPrecondition
			}
			_, e = r.pool.query(ctx).Exec(ctx, `DELETE FROM recurring_payment_links WHERE tenant_id=$1 AND occurrence_id=$2`, tenant, o.ID)
		case "skip":
			if o.Transaction != nil {
				return o, rp.ErrPrecondition
			}
			_, e = r.pool.query(ctx).Exec(ctx, `UPDATE recurring_payment_occurrences SET skipped=$3,skip_reason=$4 WHERE tenant_id=$1 AND id=$2`, tenant, o.ID, c.Skipped, c.Reason)
		case "update":
			if o.Transaction != nil {
				return o, rp.ErrPrecondition
			}
			_, e = r.pool.query(ctx).Exec(ctx, `UPDATE recurring_payment_occurrences SET exact_amount=$3,estimate_override=$4,due_date=$5::date,note=$6 WHERE tenant_id=$1 AND id=$2`, tenant, o.ID, c.ExactAmount, c.Estimate, c.DueDate, c.Note)
		default:
			return o, rp.ErrInvalid
		}
		if e != nil {
			return o, e
		}
		_, e = r.pool.query(ctx).Exec(ctx, `UPDATE recurring_payment_occurrences SET version=version+1,manual=true,user_id=$3,updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenant, o.ID, user)
		if e != nil {
			return o, e
		}
		return r.get(ctx, tenant, o.ID, false)
	})
}
func (r *RecurringPaymentRepo) Confirm(ctx context.Context, tenant, user string, c rp.Confirmation, now time.Time, create func(context.Context, domain.PaymentOccurrence) (domain.Transaction, error)) (domain.PaymentOccurrence, error) {
	return financialWrite(ctx, r.pool, tenant, "confirm_recurring_payment", c.RequestKey, c, func(ctx context.Context) (domain.PaymentOccurrence, error) {
		o, e := r.get(ctx, tenant, c.ID, true)
		if e != nil {
			return o, e
		}
		if o.Version != c.Version {
			return o, rp.ErrConflict
		}
		if o.Skipped {
			return o, rp.ErrPrecondition
		}
		if o.Transaction != nil && (rp.Status(o, now) != "needs_review" || c.TransactionID != o.Transaction.ID) {
			return o, rp.ErrPrecondition
		}
		if c.CurrencyCode != o.CurrencyCode || c.Amount <= 0 || c.OccurredAt.After(now) {
			return o, rp.ErrPrecondition
		}
		if o.ExactAmount != nil && *o.ExactAmount != c.Amount && !c.AcceptAmountChange {
			return o, rp.ErrPrecondition
		}
		var t domain.Transaction
		if c.TransactionID != "" {
			t, e = NewTransactionRepo(r.pool).get(ctx, c.TransactionID, true)
			if e != nil {
				return o, paymentError(e)
			}
			if t.TenantID != tenant {
				return o, rp.ErrNotFound
			}
			if t.Type != domain.TransactionTypeExpense || t.Amount.MinorUnits != c.Amount || t.Amount.CurrencyCode != c.CurrencyCode || !t.OccurredAt.Equal(c.OccurredAt) || t.OccurredAt.After(now) {
				return o, rp.ErrConflict
			}
			var used bool
			e = r.pool.query(ctx).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM recurring_payment_links WHERE tenant_id=$1 AND transaction_id=$2 AND occurrence_id<>$3)`, tenant, t.ID, o.ID).Scan(&used)
			if e != nil {
				return o, e
			}
			if used {
				return o, rp.ErrPrecondition
			}
		} else {
			var valid bool
			e = r.pool.query(ctx).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM categories WHERE tenant_id=$1 AND id=$2 AND kind='expense' AND is_active)`, tenant, func() string {
				if c.CategoryID != "" {
					return c.CategoryID
				}
				return o.CategoryID
			}()).Scan(&valid)
			if e != nil {
				return o, e
			}
			if !valid {
				return o, rp.ErrPrecondition
			}
			t, e = create(ctx, o)
			if e != nil {
				return o, e
			}
		}
		_, e = r.pool.query(ctx).Exec(ctx, `INSERT INTO recurring_payment_links(tenant_id,occurrence_id,transaction_id,confirmed_amount,confirmed_currency,confirmed_type,confirmed_by) VALUES($1,$2,$3,$4,$5,'expense',$6) ON CONFLICT(tenant_id,occurrence_id) DO UPDATE SET transaction_id=EXCLUDED.transaction_id,confirmed_amount=EXCLUDED.confirmed_amount,confirmed_currency=EXCLUDED.confirmed_currency,confirmed_type=EXCLUDED.confirmed_type,confirmed_by=EXCLUDED.confirmed_by,confirmed_at=now()`, tenant, o.ID, t.ID, t.Amount.MinorUnits, t.Amount.CurrencyCode, user)
		if e != nil {
			return o, e
		}
		_, e = r.pool.query(ctx).Exec(ctx, `UPDATE recurring_payment_occurrences SET exact_amount=$3,version=version+1,manual=true,user_id=$4,updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenant, o.ID, t.Amount.MinorUnits, user)
		if e != nil {
			return o, e
		}
		return r.get(ctx, tenant, o.ID, false)
	})
}

const deleteUntouchedPaymentOccurrences = `DELETE FROM recurring_payment_occurrences o WHERE o.tenant_id=$1 AND o.template_id=$2 AND o.scheduled_month>$3::date AND NOT o.manual AND NOT o.skipped AND NOT EXISTS(SELECT 1 FROM recurring_payment_links l WHERE l.tenant_id=o.tenant_id AND l.occurrence_id=o.id)`
