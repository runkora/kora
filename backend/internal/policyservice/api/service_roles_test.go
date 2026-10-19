package api

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/runkora/kora/backend/internal/policyservice/bundle"
	"github.com/runkora/kora/backend/internal/policyservice/grants"
	"github.com/runkora/kora/backend/internal/policyservice/roles"
	"github.com/runkora/kora/backend/internal/policyservice/vocabulary"
)

type listVocab struct {
	vocabulary.Repository
	entries []*vocabulary.ScopeEntry
}

func (v listVocab) List(context.Context, string) ([]*vocabulary.ScopeEntry, error) {
	return v.entries, nil
}

type storedRoles struct {
	roles.Repository
	entries []*roles.RoleEntry
}

func (r storedRoles) ListByTenant(context.Context, string, int, int) ([]*roles.RoleEntry, error) {
	return slices.Clone(r.entries), nil
}

func (r storedRoles) GetByRole(_ context.Context, _, role string) (*roles.RoleEntry, error) {
	for _, e := range r.entries {
		if e.Role == role {
			return e, nil
		}
	}
	return nil, roles.ErrRoleNotFound
}

func rolesTestService() *Service {
	return &Service{
		vocabRepo: listVocab{entries: []*vocabulary.ScopeEntry{
			{Scope: "agents:types:read"},
			{Scope: "inference:invoke"},
			{Scope: "platform:grants:write"},
		}},
		roleRepo: storedRoles{entries: []*roles.RoleEntry{
			{Role: bundle.ViewerRole, Scopes: []string{"platform:grants:write"}},
			{Role: "engineer", Scopes: []string{"inference:invoke"}},
		}},
	}
}

func TestReservedRoles_RejectWrites(t *testing.T) {
	t.Parallel()

	svc := rolesTestService()
	for _, role := range bundle.ReservedRoleNames {
		t.Run(role, func(t *testing.T) {
			t.Parallel()
			var reserved *ReservedRoleError
			if _, err := svc.UpsertRole(context.Background(), "acme", role, []string{"agents:types:read"}, nil); !errors.As(err, &reserved) {
				t.Errorf("UpsertRole(%q) err = %v, want ReservedRoleError", role, err)
			}
			if err := svc.DeleteRole(context.Background(), "acme", role); !errors.As(err, &reserved) {
				t.Errorf("DeleteRole(%q) err = %v, want ReservedRoleError", role, err)
			}
		})
	}
}

func TestListRoles_PrependsReservedRoles(t *testing.T) {
	t.Parallel()

	got, err := rolesTestService().ListRoles(context.Background(), "acme", 50, 0)
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	var names []string
	for _, e := range got {
		names = append(names, e.Role)
	}
	want := append(slices.Clone(bundle.ReservedRoleNames), "engineer")
	if !slices.Equal(names, want) {
		t.Fatalf("roles = %v, want %v", names, want)
	}
	viewer := got[1]
	if !viewer.Reserved || !slices.Equal(viewer.Scopes, []string{"agents:types:read"}) {
		t.Errorf("kora-viewer = reserved %v scopes %v, want reserved [agents:types:read]", viewer.Reserved, viewer.Scopes)
	}
	if !slices.Contains(viewer.UnregisteredScopes, "triggers:read") {
		t.Errorf("kora-viewer unregistered = %v, want triggers:read listed", viewer.UnregisteredScopes)
	}
	if got[len(got)-1].Reserved {
		t.Error("a tenant role must not be marked reserved")
	}
}

func TestGetRole_ReservedIsDerived(t *testing.T) {
	t.Parallel()

	got, err := rolesTestService().GetRole(context.Background(), "acme", bundle.UserRole)
	if err != nil {
		t.Fatalf("GetRole: %v", err)
	}
	want := []string{"agents:types:read", "inference:invoke"}
	if !got.Reserved || !slices.Equal(got.Scopes, want) {
		t.Fatalf("kora-user = reserved %v scopes %v, want reserved %v", got.Reserved, got.Scopes, want)
	}
}

func TestAddRoleScopes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		roles []string
		want  []string
	}{
		{"built-in role ignores a stored row of the same name", []string{bundle.ViewerRole}, []string{"agents:types:read"}},
		{"built-in role with tenant role", []string{bundle.UserRole, "engineer"}, []string{"agents:types:read", "inference:invoke"}},
		{"platform-admin", []string{bundle.PlatformAdminRole}, []string{"agents:types:read", "inference:invoke", "platform:grants:write"}},
		{"unknown role", []string{"nobody"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			set := map[string]struct{}{}
			if err := rolesTestService().addRoleScopes(context.Background(), "acme", tc.roles, set, set); err != nil {
				t.Fatalf("addRoleScopes: %v", err)
			}
			var got []string
			for sc := range set {
				got = append(got, sc)
			}
			slices.Sort(got)
			if !slices.Equal(got, tc.want) {
				t.Errorf("scopes = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestEffectiveScopes_BuiltinRoleTakesNoRequires pins the ceiling to the bundle:
// a requires edge on a leaf under a built-in parent does not reach the holder.
func TestEffectiveScopes_BuiltinRoleTakesNoRequires(t *testing.T) {
	t.Parallel()

	svc := &Service{
		vocabRepo: listVocab{entries: []*vocabulary.ScopeEntry{
			{Scope: "agents:types:read"},
			{Scope: "agents:types:planted:read", Requires: []string{"platform:grants:write"}},
			{Scope: "platform:grants:write"},
		}},
		roleRepo:  storedRoles{},
		grantRepo: noGrants{},
	}
	got, err := svc.EffectiveScopes(context.Background(), "acme", "HUMAN", "u1", []string{bundle.ViewerRole})
	if err != nil {
		t.Fatalf("EffectiveScopes: %v", err)
	}
	want := []string{"agents:types:planted:read", "agents:types:read"}
	if !slices.Equal(got, want) {
		t.Fatalf("scopes = %v, want %v", got, want)
	}
}

type noGrants struct{ grants.Repository }

func (noGrants) List(context.Context, string, grants.PrincipalType, string, int, int) ([]*grants.Grant, int, error) {
	return nil, 0, nil
}
