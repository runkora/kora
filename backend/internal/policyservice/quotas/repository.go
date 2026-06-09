// Package quotas manages per-principal and group-based budget quota records
// owned by the policy service. Quota data is compiled into OPA bundles and
// evaluated at inference time via the orbit.budget Rego module.
package quotas

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrGroupNotFound is returned when a quota group cannot be located.
	ErrGroupNotFound = errors.New("quota group not found")
	// ErrPrincipalQuotaNotFound is returned when no individual quota exists.
	ErrPrincipalQuotaNotFound = errors.New("principal quota not found")
)

// QuotaGroup is a named budget that can be shared across principals.
type QuotaGroup struct {
	ID               string
	TenantID         string
	Name             string
	MonthlyBudgetUSD float64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Repository defines the storage contract for quota data.
type Repository interface {
	// Groups
	CreateGroup(ctx context.Context, g QuotaGroup) error
	GetGroup(ctx context.Context, tenantID, id string) (QuotaGroup, error)
	ListGroups(ctx context.Context, tenantID string) ([]QuotaGroup, error)
	UpdateGroupBudget(ctx context.Context, tenantID, id string, budgetUSD float64) error
	DeleteGroup(ctx context.Context, tenantID, id string) error

	// Memberships
	SetMembership(ctx context.Context, tenantID, principalID, groupID string) error
	RemoveMembership(ctx context.Context, tenantID, principalID, groupID string) error
	ListMembers(ctx context.Context, tenantID, groupID string) ([]string, error)

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
	// Groups maps group_id → GroupDoc for group budget definitions.
	Groups map[string]GroupDoc `json:"groups"`
	// Memberships maps principal_id → group_id.
	Memberships map[string]string `json:"memberships"`
}

// GroupDoc is the per-group sub-document inside BundleDocument.
type GroupDoc struct {
	Name             string  `json:"name"`
	MonthlyBudgetUSD float64 `json:"monthly_budget_usd"`
}
