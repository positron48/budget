package currencyexchange

import (
	"context"
	"errors"
	"testing"

	"github.com/positron48/budget/internal/domain"
)

type repoStub struct {
	created        domain.CurrencyExchange
	updated        domain.CurrencyExchange
	listedTenantID string
	deletedTenant  string
	deletedID      string
}

func (r *repoStub) Create(_ context.Context, exchange domain.CurrencyExchange) (domain.CurrencyExchange, error) {
	r.created = exchange
	return exchange, nil
}

func (r *repoStub) Update(_ context.Context, exchange domain.CurrencyExchange) (domain.CurrencyExchange, error) {
	r.updated = exchange
	return exchange, nil
}

func (r *repoStub) List(_ context.Context, tenantID string, _, _ int) ([]domain.CurrencyExchange, int64, error) {
	r.listedTenantID = tenantID
	return []domain.CurrencyExchange{{TenantID: tenantID}}, 1, nil
}

func (r *repoStub) Delete(_ context.Context, tenantID, id string) error {
	r.deletedTenant = tenantID
	r.deletedID = id
	return nil
}

func TestServiceCreateNormalizesCurrencies(t *testing.T) {
	repo := &repoStub{}
	service := NewService(repo)
	created, err := service.Create(context.Background(), domain.CurrencyExchange{
		FromAmount: domain.Money{CurrencyCode: " usd ", MinorUnits: 10000},
		ToAmount:   domain.Money{CurrencyCode: "rub", MinorUnits: 950000},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.FromAmount.CurrencyCode != "USD" || created.ToAmount.CurrencyCode != "RUB" {
		t.Fatalf("currencies were not normalized: %#v", created)
	}
}

func TestServiceCreateValidatesAmountsAndCurrencies(t *testing.T) {
	service := NewService(&repoStub{})
	tests := []struct {
		name     string
		exchange domain.CurrencyExchange
		wantErr  error
	}{
		{
			name: "zero from amount",
			exchange: domain.CurrencyExchange{
				FromAmount: domain.Money{CurrencyCode: "USD"},
				ToAmount:   domain.Money{CurrencyCode: "RUB", MinorUnits: 1},
			},
			wantErr: ErrInvalidAmount,
		},
		{
			name: "same currency",
			exchange: domain.CurrencyExchange{
				FromAmount: domain.Money{CurrencyCode: "USD", MinorUnits: 1},
				ToAmount:   domain.Money{CurrencyCode: "USD", MinorUnits: 1},
			},
			wantErr: ErrInvalidCurrency,
		},
		{
			name: "invalid code",
			exchange: domain.CurrencyExchange{
				FromAmount: domain.Money{CurrencyCode: "US", MinorUnits: 1},
				ToAmount:   domain.Money{CurrencyCode: "RUB", MinorUnits: 1},
			},
			wantErr: ErrInvalidCurrency,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := service.Create(context.Background(), test.exchange)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("got %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestServiceUpdateNormalizesAndPropagatesExchange(t *testing.T) {
	repo := &repoStub{}
	service := NewService(repo)
	updated, err := service.Update(context.Background(), domain.CurrencyExchange{
		ID:         "exchange-1",
		TenantID:   "tenant-1",
		FromAmount: domain.Money{CurrencyCode: " eur ", MinorUnits: 100},
		ToAmount:   domain.Money{CurrencyCode: "rub", MinorUnits: 9500},
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.ID != "exchange-1" || repo.updated.TenantID != "tenant-1" {
		t.Fatalf("exchange identity was not propagated: %#v", repo.updated)
	}
	if updated.FromAmount.CurrencyCode != "EUR" || updated.ToAmount.CurrencyCode != "RUB" {
		t.Fatalf("currencies were not normalized: %#v", updated)
	}
}

func TestServiceScopesListAndDeleteToTenant(t *testing.T) {
	repo := &repoStub{}
	service := NewService(repo)
	if _, _, err := service.List(context.Background(), "tenant-1", 1, 20); err != nil {
		t.Fatalf("list: %v", err)
	}
	if err := service.Delete(context.Background(), "tenant-1", "exchange-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if repo.listedTenantID != "tenant-1" || repo.deletedTenant != "tenant-1" || repo.deletedID != "exchange-1" {
		t.Fatalf("tenant scope was not propagated: %#v", repo)
	}
}
