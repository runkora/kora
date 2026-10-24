// Package declared applies the items the operator projects from declarative
// resources: PolicyRole resources and the tenant budgets in the tenant registry.
//
// See docs/adr/2026-10-24-declarative-resources.md.
package declared

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/runkora/kora/backend/internal/platform/declarative"
	"github.com/runkora/kora/backend/internal/platform/declarative/v1alpha1"
	"github.com/runkora/kora/backend/internal/policyservice/api"
	"github.com/runkora/kora/backend/internal/policyservice/roles"
)

// roleService writes the rows projected from PolicyRole resources.
type roleService interface {
	UpsertDeclaredRole(ctx context.Context, in *roles.RoleEntry) (*roles.RoleEntry, error)
	DeleteDeclaredRole(ctx context.Context, tenantID, resourceName string) error
	ListDeclaredRoles(ctx context.Context, tenantID string) ([]*roles.RoleEntry, error)
}

// RoleApplier applies PolicyRole items. The operator resolves the tenant and
// refuses platform scopes in tenant namespaces before it calls.
type RoleApplier struct {
	svc roleService
}

// NewRoleApplier returns the declarative.Applier for declarative.KindPolicyRoles.
func NewRoleApplier(svc roleService) *RoleApplier {
	return &RoleApplier{svc: svc}
}

// Apply projects the item into a crd row keyed by tenant and role name.
func (a *RoleApplier) Apply(ctx context.Context, item declarative.Item) (declarative.Result, error) {
	var spec v1alpha1.PolicyRoleSpec
	if err := json.Unmarshal(item.Spec, &spec); err != nil {
		return declarative.Result{Outcome: declarative.OutcomeInvalid, Message: "spec is not a PolicyRole spec"}, nil
	}
	role := spec.Role
	if role == "" {
		role = item.Name
	}
	in := &roles.RoleEntry{TenantID: item.TenantID, Role: role, Scopes: spec.Scopes, ResourceName: item.Name, ResourceUID: item.UID}
	if b := spec.Budget; b != nil {
		in.Budget = &roles.BudgetAssignment{LimitUSD: b.LimitUSD, ResetCadence: b.ResetCadence, Pooled: b.Pooled}
	}

	entry, err := a.svc.UpsertDeclaredRole(ctx, in)
	var missing *api.MissingScopesError
	switch {
	case err == nil:
		return declarative.Result{Outcome: declarative.OutcomeProjected, ID: entry.ID, Ready: true}, nil
	case errors.Is(err, api.ErrInvalidDeclaredRole):
		return declarative.Result{Outcome: declarative.OutcomeInvalid, Message: err.Error()}, nil
	case errors.Is(err, roles.ErrRoleTaken):
		return declarative.Result{Outcome: declarative.OutcomeNameConflict,
			Message: fmt.Sprintf("role %s is held by a role built through the API or by another PolicyRole", role)}, nil
	case errors.As(err, &missing):
		return declarative.Result{Outcome: declarative.OutcomePending, Message: missing.Error()}, nil
	default:
		return declarative.Result{}, fmt.Errorf("apply PolicyRole %s: %w", item.Name, err)
	}
}

// Delete removes the row projected from the named resource.
func (a *RoleApplier) Delete(ctx context.Context, tenantID, name string) error {
	return a.svc.DeleteDeclaredRole(ctx, tenantID, name)
}

// List returns the resource behind each of the tenant's crd rows.
func (a *RoleApplier) List(ctx context.Context, tenantID string) ([]declarative.Declared, error) {
	entries, err := a.svc.ListDeclaredRoles(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]declarative.Declared, 0, len(entries))
	for _, e := range entries {
		out = append(out, declarative.Declared{Name: e.ResourceName, UID: e.ResourceUID})
	}
	return out, nil
}
