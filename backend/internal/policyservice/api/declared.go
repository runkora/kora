package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/runkora/kora/backend/internal/platform/declarative"
	"github.com/runkora/kora/backend/internal/platform/declarative/v1alpha1"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/policyservice/bundle"
	"github.com/runkora/kora/backend/internal/policyservice/roles"
	"github.com/runkora/kora/backend/internal/policyservice/tenantbudget"
)

// RoleApplier applies the PolicyRole items the operator projects. The
// operator resolves the tenant and refuses platform scopes in tenant
// namespaces before it calls. See docs/adr/2026-10-24-declarative-resources.md.
type RoleApplier struct {
	svc *Service
}

// NewRoleApplier returns the declarative.Applier for declarative.KindPolicyRoles.
func NewRoleApplier(svc *Service) *RoleApplier {
	return &RoleApplier{svc: svc}
}

func invalidRole(message string) declarative.Result {
	return declarative.Result{Outcome: declarative.OutcomeInvalid, Message: message}
}

// Apply projects the item into a crd row keyed by tenant and role name. The
// role is spec.role, or the resource name when unset. It validates as the role
// API does. The caller's scope ceiling and budget permission do not apply: the
// resource has no caller.
func (a *RoleApplier) Apply(ctx context.Context, item declarative.Item) (declarative.Result, error) {
	var spec v1alpha1.PolicyRoleSpec
	if err := json.Unmarshal(item.Spec, &spec); err != nil {
		return invalidRole("spec is not a PolicyRole spec"), nil
	}
	entry := roles.RoleEntry{TenantID: item.TenantID, Role: spec.Role, Scopes: spec.Scopes, ResourceName: item.Name, ResourceUID: item.UID, Repository: item.Repository}
	if entry.Role == "" {
		entry.Role = item.Name
	}
	if bundle.IsReservedRole(entry.Role) {
		return invalidRole((&ReservedRoleError{Role: entry.Role}).Error()), nil
	}
	if apiErr := validateRole(entry.Role, entry.Scopes); apiErr != nil {
		return invalidRole(apiErr.Message), nil
	}
	if b := spec.Budget; b != nil {
		valid, apiErr := validRoleBudget(&roles.BudgetAssignment{LimitUSD: b.LimitUSD, ResetCadence: b.ResetCadence, Pooled: b.Pooled})
		if apiErr != nil {
			return invalidRole(apiErr.Message), nil
		}
		entry.Budget = valid
	}
	missing, err := a.svc.missingScopes(ctx, entry.Scopes)
	if err != nil {
		return declarative.Result{}, err
	}
	if len(missing) > 0 {
		return declarative.Result{Outcome: declarative.OutcomePending, Message: "scopes not in the scope vocabulary: " + strings.Join(missing, ", ")}, nil
	}

	// A re-apply of an unchanged resource writes nothing and recompiles nothing.
	stored, err := a.svc.roleRepo.GetByRole(ctx, entry.TenantID, entry.Role)
	if err == nil && sameDeclaredRole(stored, &entry) {
		return declarative.Result{Outcome: declarative.OutcomeProjected, ID: stored.ID, Ready: true}, nil
	}
	err = a.svc.roleRepo.UpsertDeclared(ctx, &entry)
	if errors.Is(err, roles.ErrRoleTaken) {
		return declarative.Result{Outcome: declarative.OutcomeNameConflict,
			Message: fmt.Sprintf("role %s is held by a role built through the API or by another PolicyRole", entry.Role)}, nil
	}
	if err != nil {
		return declarative.Result{}, fmt.Errorf("apply PolicyRole %s: %w", item.Name, err)
	}
	a.svc.recordRoleWrite(ctx, entry.TenantID)
	return declarative.Result{Outcome: declarative.OutcomeProjected, ID: entry.ID, Ready: true}, nil
}

// Delete removes the row projected from the named resource.
func (a *RoleApplier) Delete(ctx context.Context, tenantID, name string) error {
	deleted, err := a.svc.roleRepo.DeleteDeclared(ctx, tenantID, name)
	if err != nil || !deleted {
		return err
	}
	a.svc.recordRoleWrite(ctx, tenantID)
	return nil
}

// List returns the resource behind each of the tenant's crd rows.
func (a *RoleApplier) List(ctx context.Context, tenantID string) ([]declarative.Declared, error) {
	return a.svc.roleRepo.ListDeclared(ctx, tenantID)
}

// missingScopes returns the scopes the vocabulary does not hold.
func (s *Service) missingScopes(ctx context.Context, scopes []string) ([]string, error) {
	var missing []string
	for _, scope := range scopes {
		var scopeErr *ScopeVocabularyError
		err := validateScopes(ctx, s.vocabRepo.Exists, []string{scope}, false)
		switch {
		case errors.As(err, &scopeErr):
			missing = append(missing, scope)
		case err != nil:
			return nil, err
		}
	}
	return missing, nil
}

// sameDeclaredRole reports whether the stored row already holds the declared entry.
func sameDeclaredRole(stored, entry *roles.RoleEntry) bool {
	return stored.Source == declarative.SourceCRD && stored.ResourceUID == entry.ResourceUID && stored.ResourceName == entry.ResourceName && stored.Repository == entry.Repository &&
		slices.Equal(stored.Scopes, entry.Scopes) && reflect.DeepEqual(stored.Budget, entry.Budget)
}

// recordRoleWrite recompiles the tenant and gateway bundles after a role change.
func (s *Service) recordRoleWrite(ctx context.Context, tenantID string) {
	s.recordWrite(tenantID)
	s.scheduleRecompile(ctx, tenantID)
	s.scheduleGatewayRecompile(ctx)
}

// isDeclaredRole reports whether the tenant's role is projected from a resource.
func (s *Service) isDeclaredRole(ctx context.Context, tenantID, role string) (bool, error) {
	stored, err := s.roleRepo.GetByRole(ctx, tenantID, role)
	if errors.Is(err, roles.ErrRoleNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return stored.Source == declarative.SourceCRD, nil
}

// budgetSyncInterval is how often the registry's tenant budgets are compared with the projected rows.
const budgetSyncInterval = 30 * time.Second

// budgetRegistry is the part of the tenant registry budget projection reads.
type budgetRegistry interface {
	TenantIDs() []string
	TenantBudget(tenantID string) (declarative.Budget, bool)
	Err() error
}

// RunTenantBudgets keeps the crd tenant budgets in line with the registry until
// ctx ends. Each sync is idempotent, so every replica may run it.
func (s *Service) RunTenantBudgets(ctx context.Context, registry budgetRegistry) {
	ticker := time.NewTicker(budgetSyncInterval)
	defer ticker.Stop()
	for {
		if err := s.syncTenantBudgets(ctx, registry); err != nil {
			s.logger.Error(ctx, "declared tenant budget sync failed", logging.Field{Key: "error", Value: err.Error()})
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// syncTenantBudgets makes the crd tenant budgets match the registry. A budget
// the registry declares replaces an API budget. A crd budget the registry no
// longer declares is deleted. An invalid declared budget keeps its last row.
func (s *Service) syncTenantBudgets(ctx context.Context, registry budgetRegistry) error {
	if err := registry.Err(); err != nil {
		return fmt.Errorf("read tenant registry: %w", err)
	}
	tenantIDs := registry.TenantIDs()
	// An empty or missing registry is more likely a lost mount than every tenant gone.
	if len(tenantIDs) == 0 {
		return nil
	}
	rows, err := s.tenantBudgetRepo.ListDeclared(ctx)
	if err != nil {
		return err
	}
	stale := make(map[string]*tenantbudget.TenantBudget, len(rows))
	for _, row := range rows {
		stale[row.TenantID] = row
	}
	for _, tenantID := range tenantIDs {
		b, declared := registry.TenantBudget(tenantID)
		if !declared {
			continue
		}
		row := stale[tenantID]
		delete(stale, tenantID)
		cadence, apiErr := validTenantBudget(b.LimitUSD, b.ResetCadence)
		if apiErr != nil {
			s.logger.Warn(ctx, "tenant budget in the registry is invalid",
				logging.Field{Key: "tenant_id", Value: tenantID}, logging.Field{Key: "reason", Value: apiErr.Message})
			continue
		}
		if row != nil && row.LimitUSD == b.LimitUSD && row.ResetCadence == cadence {
			continue
		}
		if err := s.tenantBudgetRepo.UpsertDeclared(ctx, &tenantbudget.TenantBudget{TenantID: tenantID, LimitUSD: b.LimitUSD, ResetCadence: cadence}); err != nil {
			return err
		}
		s.recordWrite(tenantID)
		s.scheduleRecompile(ctx, tenantID)
	}
	for tenantID := range stale {
		if err := s.tenantBudgetRepo.DeleteDeclared(ctx, tenantID); err != nil {
			return err
		}
		s.recordWrite(tenantID)
		s.scheduleRecompile(ctx, tenantID)
	}
	return nil
}
