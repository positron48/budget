package asset

import (
	"context"
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/positron48/budget/internal/domain"
)

type Repo interface {
	CreateAccount(context.Context, domain.AssetAccount, []domain.AssetSnapshot, string) (domain.AssetAccount, error)
	UpdateAccount(context.Context, domain.AssetAccount) (domain.AssetAccount, error)
	GetAccount(context.Context, string, string, time.Time) (domain.AssetAccount, error)
	ListAccounts(context.Context, string, bool, time.Time) ([]domain.AssetAccount, error)
	SetArchived(context.Context, string, string, int64, bool) (domain.AssetAccount, error)
	DeleteEmptyAccount(context.Context, string, string, int64) error
	CreateSnapshot(context.Context, domain.AssetSnapshot, string) (domain.AssetSnapshot, error)
	UpdateSnapshot(context.Context, domain.AssetSnapshot) (domain.AssetSnapshot, error)
	DeleteSnapshot(context.Context, string, string, int64) error
	CreateTransfer(context.Context, domain.AssetTransfer) (domain.AssetTransfer, error)
	UpdateTransfer(context.Context, domain.AssetTransfer) (domain.AssetTransfer, error)
	DeleteTransfer(context.Context, string, string, int64) error
	ListAccountHistory(context.Context, string, string, int, int) ([]domain.AssetHistoryItem, int64, error)
}
type Rates interface {
	GetAssetRates(context.Context, []string, string, time.Time) ([]domain.AssetFxRate, error)
}
type Tenants interface {
	GetByID(context.Context, string) (domain.Tenant, error)
}

type Service struct {
	repo    Repo
	rates   Rates
	tenants Tenants
}

func NewService(repo Repo, rates Rates, tenants Tenants) *Service {
	return &Service{repo, rates, tenants}
}

var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
var decimalPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)

func ValidCurrency(code string) bool { return currencyPattern.MatchString(code) }
func invalid(message string) error   { return fmt.Errorf("%w: %s", domain.ErrAssetInvalid, message) }

func ValidateAccount(a *domain.AssetAccount) error {
	a.Name = strings.TrimSpace(a.Name)
	a.Institution = strings.TrimSpace(a.Institution)
	a.FixedCurrencyCode = strings.ToUpper(strings.TrimSpace(a.FixedCurrencyCode))
	if len(a.Name) == 0 || len(a.Name) > 200 || len(a.Institution) > 200 || len(a.Note) > 5000 {
		return invalid("invalid account name or note")
	}
	switch a.Kind {
	case "cash", "bank":
		if a.FixedCurrencyCode != "" {
			return invalid("cash and bank accounts use currencies from their balances")
		}
	case "deposit", "investment", "property":
		if !ValidCurrency(a.FixedCurrencyCode) {
			return invalid("account currency is required")
		}
	default:
		return invalid("unknown account kind")
	}
	if a.Kind != "deposit" && (a.DepositRateDecimal != "" || a.DepositOpenedOn != "" || a.DepositMaturesOn != "") {
		return invalid("deposit fields require a deposit account")
	}
	if a.DepositRateDecimal != "" {
		r, ok := new(big.Rat).SetString(a.DepositRateDecimal)
		if !decimalPattern.MatchString(a.DepositRateDecimal) || !ok || r.Sign() < 0 || r.Cmp(big.NewRat(10000, 1)) > 0 {
			return invalid("invalid deposit rate")
		}
	}
	for _, d := range []string{a.DepositOpenedOn, a.DepositMaturesOn} {
		if d != "" {
			if _, err := time.Parse("2006-01-02", d); err != nil {
				return invalid("invalid deposit date")
			}
		}
	}
	if a.DepositOpenedOn != "" && a.DepositMaturesOn != "" && a.DepositMaturesOn < a.DepositOpenedOn {
		return invalid("deposit maturity precedes opening")
	}
	return nil
}

func ValidateSnapshot(s *domain.AssetSnapshot) error {
	s.Amount.CurrencyCode = strings.ToUpper(strings.TrimSpace(s.Amount.CurrencyCode))
	if !ValidCurrency(s.Amount.CurrencyCode) || s.AsOf.IsZero() || len(s.Note) > 5000 {
		return invalid("snapshot needs a currency and timestamp")
	}
	switch s.Kind {
	case "opening", "reconciliation", "valuation":
	default:
		return invalid("unknown snapshot kind")
	}
	if s.AsOf.After(time.Now().Add(time.Minute)) {
		return invalid("a confirmed balance cannot be in the future")
	}
	return nil
}

func ValidateTransfer(t *domain.AssetTransfer) error {
	t.FromAmount.CurrencyCode = strings.ToUpper(strings.TrimSpace(t.FromAmount.CurrencyCode))
	t.ToAmount.CurrencyCode = strings.ToUpper(strings.TrimSpace(t.ToAmount.CurrencyCode))
	if t.FromAccountID == "" && t.ToAccountID == "" {
		return invalid("at least one tracked account is required")
	}
	if t.FromAccountID != "" && t.FromAccountID == t.ToAccountID {
		return invalid("use a currency exchange for movements within one account")
	}
	if t.FromAmount.MinorUnits <= 0 || t.ToAmount.MinorUnits <= 0 || !ValidCurrency(t.FromAmount.CurrencyCode) || !ValidCurrency(t.ToAmount.CurrencyCode) || t.OccurredAt.IsZero() || len(t.Note) > 5000 {
		return invalid("transfer amounts must be positive and have currencies")
	}
	return nil
}

func (s *Service) CreateAccount(ctx context.Context, a domain.AssetAccount, openings []domain.AssetSnapshot, key string) (domain.AssetAccount, error) {
	if err := ValidateAccount(&a); err != nil {
		return domain.AssetAccount{}, err
	}
	if len(openings) == 0 || len(openings) > 50 {
		return domain.AssetAccount{}, invalid("at least one opening balance is required")
	}
	seen := map[string]bool{}
	for i := range openings {
		openings[i].Kind = "opening"
		if err := ValidateSnapshot(&openings[i]); err != nil {
			return domain.AssetAccount{}, err
		}
		if a.Kind == "property" && openings[i].Amount.MinorUnits < 0 {
			return domain.AssetAccount{}, invalid("property valuation cannot be negative")
		}
		code := openings[i].Amount.CurrencyCode
		if seen[code] || (a.FixedCurrencyCode != "" && a.FixedCurrencyCode != code) {
			return domain.AssetAccount{}, invalid("opening currencies must be unique and match the account")
		}
		seen[code] = true
	}
	return s.repo.CreateAccount(ctx, a, openings, key)
}
func (s *Service) UpdateAccount(ctx context.Context, a domain.AssetAccount) (domain.AssetAccount, error) {
	if err := ValidateAccount(&a); err != nil {
		return domain.AssetAccount{}, err
	}
	if a.Version < 1 {
		return domain.AssetAccount{}, invalid("account version is required")
	}
	return s.repo.UpdateAccount(ctx, a)
}
func (s *Service) GetAccount(ctx context.Context, tenant, id string) (domain.AssetAccount, error) {
	return s.repo.GetAccount(ctx, tenant, id, time.Now())
}
func (s *Service) ListAccounts(ctx context.Context, tenant string, archived bool) ([]domain.AssetAccount, error) {
	return s.repo.ListAccounts(ctx, tenant, archived, time.Now())
}
func (s *Service) SetArchived(ctx context.Context, tenant, id string, version int64, value bool) (domain.AssetAccount, error) {
	return s.repo.SetArchived(ctx, tenant, id, version, value)
}
func (s *Service) DeleteEmptyAccount(ctx context.Context, tenant, id string, version int64) error {
	return s.repo.DeleteEmptyAccount(ctx, tenant, id, version)
}
func (s *Service) CreateSnapshot(ctx context.Context, snapshot domain.AssetSnapshot, key string) (domain.AssetSnapshot, error) {
	if err := ValidateSnapshot(&snapshot); err != nil {
		return domain.AssetSnapshot{}, err
	}
	return s.repo.CreateSnapshot(ctx, snapshot, key)
}
func (s *Service) UpdateSnapshot(ctx context.Context, snapshot domain.AssetSnapshot) (domain.AssetSnapshot, error) {
	if err := ValidateSnapshot(&snapshot); err != nil {
		return domain.AssetSnapshot{}, err
	}
	if snapshot.Version < 1 {
		return domain.AssetSnapshot{}, invalid("snapshot version is required")
	}
	return s.repo.UpdateSnapshot(ctx, snapshot)
}
func (s *Service) DeleteSnapshot(ctx context.Context, tenant, id string, version int64) error {
	return s.repo.DeleteSnapshot(ctx, tenant, id, version)
}
func (s *Service) CreateTransfer(ctx context.Context, t domain.AssetTransfer) (domain.AssetTransfer, error) {
	if err := ValidateTransfer(&t); err != nil {
		return domain.AssetTransfer{}, err
	}
	return s.repo.CreateTransfer(ctx, t)
}
func (s *Service) UpdateTransfer(ctx context.Context, t domain.AssetTransfer) (domain.AssetTransfer, error) {
	if err := ValidateTransfer(&t); err != nil {
		return domain.AssetTransfer{}, err
	}
	if t.Version < 1 {
		return domain.AssetTransfer{}, invalid("transfer version is required")
	}
	return s.repo.UpdateTransfer(ctx, t)
}
func (s *Service) DeleteTransfer(ctx context.Context, tenant, id string, version int64) error {
	return s.repo.DeleteTransfer(ctx, tenant, id, version)
}
func (s *Service) ListAccountHistory(ctx context.Context, tenant, id string, page, size int) ([]domain.AssetHistoryItem, int64, error) {
	return s.repo.ListAccountHistory(ctx, tenant, id, page, size)
}

func (s *Service) GetOverview(ctx context.Context, tenant, target string, at time.Time) (domain.AssetOverview, error) {
	if at.IsZero() {
		at = time.Now()
	}
	if target == "" {
		t, err := s.tenants.GetByID(ctx, tenant)
		if err != nil {
			return domain.AssetOverview{}, err
		}
		target = t.DefaultCurrencyCode
	}
	if !ValidCurrency(target) {
		return domain.AssetOverview{}, invalid("invalid target currency")
	}
	accounts, err := s.repo.ListAccounts(ctx, tenant, false, at)
	if err != nil {
		return domain.AssetOverview{}, err
	}
	currencies := map[string]bool{}
	for _, a := range accounts {
		for _, b := range a.Balances {
			if b.Amount.CurrencyCode != target {
				currencies[b.Amount.CurrencyCode] = true
			}
		}
	}
	codes := []string{}
	for code := range currencies {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	rates, err := s.rates.GetAssetRates(ctx, codes, target, at)
	if err != nil {
		return domain.AssetOverview{}, err
	}
	return Summarize(accounts, rates, target, at)
}

// Summarize values holdings at the overview's rates, never at historical transaction FX.
func Summarize(accounts []domain.AssetAccount, rates []domain.AssetFxRate, target string, at time.Time) (domain.AssetOverview, error) {
	out := domain.AssetOverview{Accounts: accounts, Total: domain.Money{CurrencyCode: target}, Rates: rates, AsOf: at}
	lookup := map[string]string{}
	for _, r := range rates {
		lookup[r.From] = r.RateDecimal
	}
	missing := map[string]bool{}
	total := new(big.Int)
	for _, a := range accounts {
		for _, b := range a.Balances {
			amount := b.Amount.MinorUnits
			if b.Amount.CurrencyCode != target {
				rate, ok := lookup[b.Amount.CurrencyCode]
				if !ok {
					if amount != 0 {
						missing[b.Amount.CurrencyCode] = true
					}
					continue
				}
				converted, err := ConvertMinor(amount, rate)
				if err != nil {
					return out, err
				}
				amount = converted
			}
			total.Add(total, big.NewInt(amount))
		}
	}
	if !total.IsInt64() {
		return out, invalid("total exceeds supported amount range")
	}
	out.Total.MinorUnits = total.Int64()
	for code := range missing {
		out.MissingCurrencies = append(out.MissingCurrencies, code)
	}
	sort.Strings(out.MissingCurrencies)
	out.Incomplete = len(missing) != 0
	return out, nil
}

func ConvertMinor(amount int64, rate string) (int64, error) {
	r, ok := new(big.Rat).SetString(rate)
	if !ok || !decimalPattern.MatchString(rate) || r.Sign() <= 0 {
		return 0, invalid("invalid FX rate")
	}
	r.Mul(r, new(big.Rat).SetInt64(amount))
	n := new(big.Int).Abs(r.Num())
	q, rem := new(big.Int), new(big.Int)
	q.QuoRem(n, r.Denom(), rem)
	if rem.Lsh(rem, 1).Cmp(r.Denom()) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	if r.Sign() < 0 {
		q.Neg(q)
	}
	if !q.IsInt64() {
		return 0, invalid("converted amount exceeds supported range")
	}
	return q.Int64(), nil
}
