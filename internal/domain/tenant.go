package domain

import "time"

type Tenant struct {
	ID                  string
	Name                string
	Slug                string
	DefaultCurrencyCode string
	CreatedAt           time.Time
}

type TenantRole string

const (
	TenantRoleOwner  TenantRole = "owner"
	TenantRoleAdmin  TenantRole = "admin"
	TenantRoleMember TenantRole = "member"
)

type TenantMembership struct {
	Tenant    Tenant
	Role      TenantRole
	IsDefault bool
}
