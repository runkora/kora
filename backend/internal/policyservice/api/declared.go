package api

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/runkora/kora/backend/internal/policyservice/bundle"
	"github.com/runkora/kora/backend/internal/policyservice/roles"
	"github.com/runkora/kora/backend/internal/policyservice/tenantbudget"
)

// ErrInvalidDeclaredRole wraps why a PolicyRole spec fails the API's role validation.
var ErrInvalidDeclaredRole = errors.New("invalid role")

// MissingScopesError lists a declared role's scopes the vocabulary does not hold yet.
type MissingScopesError struct {
	Scopes []string
}

func (e *MissingScopesError) Error() string {
	return "scopes not in the scope vocabulary: " + strings.Join(e.Scopes, ", ")
}

// UpsertDeclaredRole projects a PolicyRole into a crd row and recompiles the
// tenant's bundle. in carries the tenant, role, scopes, budget and resource
// name and UID. It validates as the role API does. The caller's scope ceiling
// and budget permission do not apply: the resource has no caller. It returns
// a *MissingScopesError and roles.ErrRoleTaken as well as validation errors.
func (s *Service) UpsertDeclaredRole(ctx context.Context, in *roles.RoleEntry) (*roles.RoleEntry, error) {
	if bundle.IsReservedRole(in.Role) {
		return nil, fmt.Errorf("%w: %w", ErrInvalidDeclaredRole, &ReservedRoleError{Role: in.Role})
	}
	if apiErr := validateRole(in.Role, in.Scopes); apiErr != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidDeclaredRole, apiErr.Message)
	}
	entry := *in
	if in.Budget != nil {
		valid, apiErr := validRoleBudget(in.Budget)
		if apiErr != nil {
			return nil, fmt.Errorf("%w: %s", ErrInvalidDeclaredRole, apiErr.Message)
		}
		entry.Budget = valid
	}
	if err := s.refuseMissingScopes(ctx, in.Scopes); err != nil {
		return nil, err
	}
	// A re-apply of an unchanged resource writes nothing and recompiles nothing.
	if stored, err := s.roleRepo.GetByRole(ctx, in.TenantID, in.Role); err == nil && sameDeclaredRole(stored, &entry) {
		return stored, nil
	}
	entry.ID = ""
	if err := s.roleRepo.UpsertDeclared(ctx, &entry); err != nil {
		return nil, err
	}
	s.recordWrite(in.TenantID)
	s.scheduleRecompile(ctx, in.TenantID)
	s.scheduleGatewayRecompile(ctx)
	return &entry, nil
}

// refuseMissingScopes returns a *MissingScopesError listing every scope the vocabulary lacks.
func (s *Service) refuseMissingScopes(ctx context.Context, scopes []string) error {
	var missing []string
	for _, scope := range scopes {
		var scopeErr *ScopeVocabularyError
		err := validateScopes(ctx, s.vocabRepo.Exists, []string{scope}, false)
		switch {
		case errors.As(err, &scopeErr):
			missing = append(missing, scope)
		case err != nil:
			return err
		}
	}
	if len(missing) > 0 {
		return &MissingScopesError{Scopes: missing}
	}
	return nil
}

// sameDeclaredRole reports whether the stored row already holds the declared entry.
func sameDeclaredRole(stored, entry *roles.RoleEntry) bool {
	return stored.Source == roles.SourceCRD && stored.ResourceUID == entry.ResourceUID && stored.ResourceName == entry.ResourceName &&
		slices.Equal(stored.Scopes, entry.Scopes) && reflect.DeepEqual(stored.Budget, entry.Budget)
}

// DeleteDeclaredRole deletes the tenant's crd row projected from the named
// resource and recompiles the tenant's bundle when it deleted one.
func (s *Service) DeleteDeclaredRole(ctx context.Context, tenantID, resourceName string) error {
	deleted, err := s.roleRepo.DeleteDeclared(ctx, tenantID, resourceName)
	if err != nil || !deleted {
		return err
	}
	s.recordWrite(tenantID)
	s.scheduleRecompile(ctx, tenantID)
	s.scheduleGatewayRecompile(ctx)
	return nil
}

// ListDeclaredRoles returns the tenant's crd role rows.
func (s *Service) ListDeclaredRoles(ctx context.Context, tenantID string) ([]*roles.RoleEntry, error) {
	return s.roleRepo.ListDeclared(ctx, tenantID)
}

// isDeclaredRole reports whether the tenant's role is projected from a resource.
func (s *Service) isDeclaredRole(ctx context.Context, tenantID, role string) bool {
	stored, err := s.roleRepo.GetByRole(ctx, tenantID, role)
	return err == nil && stored.Source == roles.SourceCRD
}

// ListDeclaredTenantBudgets returns every tenant budget projected from a KoraTenant.
func (s *Service) ListDeclaredTenantBudgets(ctx context.Context) ([]*tenantbudget.TenantBudget, error) {
	return s.tenantBudgetRepo.ListDeclared(ctx)
}

// SetDeclaredTenantBudget projects a KoraTenant budget, replacing any API
// budget, and recompiles the tenant's bundle.
func (s *Service) SetDeclaredTenantBudget(ctx context.Context, tenantID string, limitUSD float64, cadence string) error {
	if err := s.tenantBudgetRepo.UpsertDeclared(ctx, &tenantbudget.TenantBudget{TenantID: tenantID, LimitUSD: limitUSD, ResetCadence: cadence}); err != nil {
		return err
	}
	s.recordWrite(tenantID)
	s.scheduleRecompile(ctx, tenantID)
	return nil
}

// RemoveDeclaredTenantBudget deletes the tenant's projected budget and
// recompiles the tenant's bundle. An API budget stays.
func (s *Service) RemoveDeclaredTenantBudget(ctx context.Context, tenantID string) error {
	if err := s.tenantBudgetRepo.DeleteDeclared(ctx, tenantID); err != nil {
		return err
	}
	s.recordWrite(tenantID)
	s.scheduleRecompile(ctx, tenantID)
	return nil
}

// ValidTenantBudget checks a tenant budget as the tenant budget API does and
// returns its normalized cadence.
func ValidTenantBudget(limitUSD float64, cadence string) (string, bool) {
	normalized, ok := normalizeCadence(cadence)
	return normalized, ok && limitUSD > 0
}
