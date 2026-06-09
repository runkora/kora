// Package quotas manages per-principal and role-based budget quota records
// owned by the policy service. Quota data is compiled into OPA bundles and
// evaluated at inference time via the orbit.budget Rego module.
package quotas

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrRoleNotFound is returned when a quota role cannot be located.
	ErrRoleNotFound = errors.New("quota role not found")
	// ErrPrincipalQuotaNotFound is returned when no individual quota exists.
	ErrPrincipalQuotaNotFound = errors.New("principal quota not found")
)

// QuotaRole is a named budget that can be shared across principals.
type QuotaRole struct {
	ID               string
	TenantID         string
	Name             string
	MonthlyBudgetUSD float64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Repository defines the storage contract for quota data.
type Repository interface {
	// Roles
	CreateRole(ctx context.Context, g QuotaRole) error
	GetRole(ctx context.Context, tenantID, id string) (QuotaRole, error)
	ListRoles(ctx context.Context, tenantID string) ([]QuotaRole, error)
	UpdateRoleBudget(ctx context.Context, tenantID, id string, budgetUSD float64) error
	DeleteRole(ctx context.Context, tenantID, id string) error

	// Memberships
	SetMembership(ctx context.Context, tenantID, principalID, roleID string) error
	RemoveMembership(ctx context.Context, tenantID, principalID, roleID string) error
	ListMembers(ctx context.Context, tenantID, roleID string) ([]string, error)

	// Individual principal quotas
	SetPrincipalQuota(ctx context.Context, tenantID, principalID string, budgetUSD float64) error
	GetPrincipalQuota(ctx context.Context, tenantID, principalID string) (float64, bool, error)
	ClearPrincipalQuota(ctx context.Context, tenantID, principalID string) error

	// BundleData returns the quota document for the given tenant.
	// The document is structured for inclusion in the OPA bundle as data/quotas/data.json.
	BundleData(ctx context.Context, tenantID string) (*BundleDocument, error)
}

// BundleDocument is the shape written to data/quotas/data.json inside each
// tenant's OPA bundle. The orbit.budget Rego module reads these fields by name.
type BundleDocument struct {
	// PrincipalQuotas maps principal_id → monthly_budget_usd for individual limits.
	PrincipalQuotas map[string]float64 `json:"principal_quotas"`
	// Roles maps role_id → RoleDoc for role budget definitions.
	Roles map[string]RoleDoc `json:"roles"`
	// Memberships maps principal_id → role_id.
	Memberships map[string]string `json:"memberships"`
}

// RoleDoc is the per-role sub-document inside BundleDocument.
type RoleDoc struct {
	Name             string  `json:"name"`
	MonthlyBudgetUSD float64 `json:"monthly_budget_usd"`
}
