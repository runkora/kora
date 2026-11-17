package agents

import (
	"context"
	"errors"
	"strings"
	"testing"

	si "github.com/runkora/kora/backend/internal/platform/sandboximages/v1alpha1"
)

// A GitHub connector action the adapter does not publish, such as a removed
// one, is refused on create and update of agents and environments, naming the
// action. See docs/adr/2026-11-15-github-connector-route-coverage.md, item 4.
func TestConnectorActions_RemovedActionRejected(t *testing.T) {
	resolver := NewScopeResolver(nil, stubConnResolver{adapter: "github_app"})
	removed := []Connector{{Connection: "github", Actions: []string{"contents:read", "repo:read"}}}
	allowed := []Connector{{Connection: "github", Actions: []string{"contents:read", "pulls:write", "read"}}}
	assertRejected := func(t *testing.T, err, sentinel error) {
		t.Helper()
		if !errors.Is(err, sentinel) || !errors.Is(err, ErrInvalidConnector) || !strings.Contains(err.Error(), `"repo:read"`) {
			t.Errorf("err = %v, want %v naming repo:read", err, sentinel)
		}
	}

	t.Run("agent", func(t *testing.T) {
		svc, _, _, _ := newTestService(t)
		svc.at.SetScopeResolver(resolver)
		ctx := context.Background()
		_, err := svc.CreateAgentType(ctx, CreateAgentTypeRequest{TenantID: "t-1", Name: "Bot", Slug: "bot", Connectors: removed})
		assertRejected(t, err, ErrInvalidConnector)

		at, err := svc.CreateAgentType(ctx, CreateAgentTypeRequest{TenantID: "t-1", Name: "Bot", Slug: "bot", Connectors: allowed})
		if err != nil {
			t.Fatalf("create with published actions: %v", err)
		}
		_, err = svc.UpdateAgentType(ctx, UpdateAgentTypeRequest{ID: at.ID, Connectors: removed})
		assertRejected(t, err, ErrInvalidConnector)
	})

	t.Run("environment", func(t *testing.T) {
		k8s := &mockK8s{sandboxImages: map[string]*si.SandboxImage{
			si.BaseName: validSandboxImage(si.BaseName, "ghcr.io/runkora/kora-sandbox-base:1", "sha256:base"),
		}}
		store := &fakeEnvStore{envs: map[string]Environment{}}
		svc := newEnvironmentService(store, k8s)
		svc.SetScopeResolver(resolver)
		ctx := withTenantCtx(context.Background(), "t-1")
		_, err := svc.CreateEnvironment(ctx, EnvironmentRequest{Name: "gh", Connectors: removed})
		assertRejected(t, err, ErrInvalidEnvironment)

		env, err := svc.CreateEnvironment(ctx, EnvironmentRequest{Name: "gh", Connectors: allowed})
		if err != nil {
			t.Fatalf("create with published actions: %v", err)
		}
		_, err = svc.UpdateEnvironment(ctx, EnvironmentRequest{ID: env.ID, Name: "gh", Connectors: removed})
		assertRejected(t, err, ErrInvalidEnvironment)
	})
}
