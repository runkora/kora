package policy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/open-policy-agent/opa/v1/bundle"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// benchBundle is a tenant bundle built from the policy service's rego with
// grants principals, each holding a handful of scopes, and a human caller
// holding trigger:fire.
func benchBundle(b *testing.B, grants int) []byte {
	b.Helper()
	var modules []bundle.ModuleFile
	for _, name := range []string{"authz.rego", "budget.rego"} {
		raw, err := os.ReadFile("../../policyservice/bundle/" + name)
		if err != nil {
			b.Fatalf("read %s: %v", name, err)
		}
		modules = append(modules, bundle.ModuleFile{URL: "policy/" + name, Path: "policy/" + name, Raw: raw})
	}
	entries := []map[string]any{{
		"id": "g-caller", "principal_type": "HUMAN", "principal_id": "user-1",
		"scopes": []string{"triggers:read", "trigger:fire", "agents:read"},
	}}
	for i := range grants {
		entries = append(entries, map[string]any{
			"id": fmt.Sprintf("g-%d", i), "principal_type": "AGENT", "principal_id": fmt.Sprintf("agent-%d", i),
			"scopes": []string{"agents:read", "models:read", fmt.Sprintf("connection:c-%d:use", i), "egress:connect"},
		})
	}
	// The index the policy service compiles beside the grants.
	byPrincipal := map[string][]int{}
	for i, g := range entries {
		key := g["principal_type"].(string) + ":" + g["principal_id"].(string)
		byPrincipal[key] = append(byPrincipal[key], i)
	}
	var buf bytes.Buffer
	err := bundle.NewWriter(&buf).Write(bundle.Bundle{
		Manifest: bundle.Manifest{Roots: &[]string{"kora/authz", "kora/budget", "grants", "roles", "vocabulary", "routes", "config"}},
		Data: map[string]any{
			"grants":     map[string]any{"grants": entries, "by_principal": byPrincipal},
			"roles":      map[string]any{"role_scopes": map[string]any{}},
			"vocabulary": map[string]any{"scope_vocabulary": map[string]any{}},
			"routes":     map[string]any{"route_scopes": []any{}},
			"config":     map[string]any{"jwks_url": "", "issuer": "", "audience": ""},
		},
		Modules: modules,
	})
	if err != nil {
		b.Fatalf("write bundle: %v", err)
	}
	return buf.Bytes()
}

// One Check per trigger is what GET /api/v1/triggers costs, so its cost at a
// tenant's grant count is the list's cost per row.
func BenchmarkCheck(b *testing.B) {
	claims := &token.PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user-1"},
		Kora:             token.KoraClaims{TenantID: "t1", PrincipalType: token.PrincipalTypeHuman},
	}
	for _, grants := range []int{10, 100, 1000, 5000} {
		b.Run(fmt.Sprintf("grants=%d", grants), func(b *testing.B) {
			e := newEngine("", 0, logging.NewLoggerWithOutput(io.Discard))
			if err := e.compileBundle(context.Background(), "t1", benchBundle(b, grants), "r1"); err != nil {
				b.Fatalf("compile: %v", err)
			}
			in := CheckInput{Claims: claims, ResourceType: "trigger", PathSegments: []string{"trg-1"}, Action: "fire"}
			if res, err := e.Check(context.Background(), in); err != nil || !res.Allow {
				b.Fatalf("Check = %+v, %v; want allow", res, err)
			}
			b.ResetTimer()
			for range b.N {
				if _, err := e.Check(context.Background(), in); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
