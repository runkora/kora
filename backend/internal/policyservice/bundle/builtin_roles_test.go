package bundle

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/policyservice/roles"
	"github.com/runkora/kora/backend/internal/policyservice/routes"
	"github.com/runkora/kora/backend/internal/policyservice/vocabulary"
)

func TestBuiltinRoleScopes(t *testing.T) {
	t.Parallel()

	vocab := []*vocabulary.ScopeEntry{
		entry("agents:types:read"),
		entry("agents:environments:read"),
		entry("triggers:read"),
		entry("inference:list"),
		entry("inference:invoke"),
		entry("platform:connections:read"),
		entry("platform:grants:write"),
	}
	cases := []struct {
		role        string
		wantHeld    []string
		wantMissing []string
	}{
		{ViewerRole, viewerScopes, nil},
		{UserRole, append(slices.Clone(viewerScopes), "inference:invoke"), []string{"agents:runs:delegated:write", "mcp:invoke"}},
		{FinOpsRole, []string{"agents:types:read", "inference:list"}, []string{
			"platform:analytics:read", "agents:usage:read", "platform:budgets:read", "platform:budgets:write",
			"platform:roles:read", "platform:grants:read", "identity:users:read",
		}},
		{"engineer", []string{}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.role, func(t *testing.T) {
			t.Parallel()
			held, missing := BuiltinRoleScopes(tc.role, vocab)
			if !slices.Equal(held, tc.wantHeld) || !slices.Equal(missing, tc.wantMissing) {
				t.Fatalf("BuiltinRoleScopes(%q) = %v / %v, want %v / %v", tc.role, held, missing, tc.wantHeld, tc.wantMissing)
			}
		})
	}

	// An empty vocabulary compiles every reserved role to nothing.
	for role, scopes := range ReservedRoles(nil) {
		if len(scopes) != 0 {
			t.Errorf("ReservedRoles(nil)[%q] = %v, want none", role, scopes)
		}
	}

	reserved := ReservedRoles(vocab)
	for _, role := range ReservedRoleNames {
		if _, ok := reserved[role]; !ok {
			t.Errorf("ReservedRoles is missing %q", role)
		}
	}
	if !slices.Contains(reserved[PlatformAdminRole], "platform:grants:write") {
		t.Errorf("platform-admin = %v, want the vocabulary's covering set", reserved[PlatformAdminRole])
	}
}

// TestCompile_BuiltinRoles pins both bundles: built-in roles compile from the
// vocabulary without requires expansion, and a stored row under a reserved
// name cannot override them. Tenant roles keep requires expansion.
func TestCompile_BuiltinRoles(t *testing.T) {
	t.Parallel()

	// A requires edge on a leaf under agents:types:read must not reach a built-in role.
	planted := &vocabulary.ScopeEntry{Scope: "agents:types:planted:read", Requires: []string{"platform:grants:write"}}
	vocab := []*vocabulary.ScopeEntry{entry("platform:grants:write"), planted}
	for _, sc := range viewerScopes {
		vocab = append(vocab, entry(sc))
	}
	stored := []*roles.RoleEntry{
		{TenantID: "acme", Role: ViewerRole, Scopes: []string{"platform:grants:write"}},
		{TenantID: "acme", Role: "engineer", Scopes: []string{"inference:list"}},
		{TenantID: "acme", Role: "reader", Scopes: []string{"agents:types:read"}},
	}
	compiler := NewCompiler(Config{JWKSURL: "http://127.0.0.1:1/jwks", Issuer: "orbit", Audience: "orbit-platform"}, routes.NewStore())

	cases := []struct {
		name    string
		compile func() (*CachedBundle, error)
		file    string
	}{
		{"tenant", func() (*CachedBundle, error) {
			return compiler.Compile("acme", nil, vocab, stored, nil, nil)
		}, "roles/data.json"},
		{"gateway", func() (*CachedBundle, error) {
			return compiler.CompileGateway([]string{"acme"}, nil, map[string][]*roles.RoleEntry{"acme": stored}, vocab, nil)
		}, "tenants/acme/roles/data.json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cached, err := tc.compile()
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			var doc rolesDocument
			if err := json.Unmarshal(extractBundleFile(t, cached.Data, tc.file), &doc); err != nil {
				t.Fatalf("unmarshal roles: %v", err)
			}
			want := map[string][]string{
				ViewerRole:   viewerScopes,
				UserRole:     viewerScopes,
				BuilderRole:  viewerScopes,
				OperatorRole: viewerScopes,
				FinOpsRole:   {"agents:types:read", "inference:list"},
				"engineer":   {"inference:list"},
				"reader":     {"agents:types:read", "platform:grants:write"},
			}
			for role, scopes := range want {
				got := slices.Sorted(slices.Values(doc.RoleScopes[role]))
				if !slices.Equal(got, slices.Sorted(slices.Values(scopes))) {
					t.Errorf("%s = %v, want %v", role, got, scopes)
				}
			}
			if !slices.Contains(doc.RoleScopes[PlatformAdminRole], "platform:grants:write") {
				t.Errorf("platform-admin = %v, want platform:grants:write", doc.RoleScopes[PlatformAdminRole])
			}
		})
	}
}

// TestBuiltinRoles_Tiers pins that each tier holds the one below it and that
// no built-in role reaches a scope reserved for administrators.
func TestBuiltinRoles_Tiers(t *testing.T) {
	t.Parallel()

	tiers := [][]string{viewerScopes, userScopes, builderScopes, operatorScopes}
	for i := 1; i < len(tiers); i++ {
		for _, sc := range tiers[i-1] {
			if !slices.Contains(tiers[i], sc) {
				t.Errorf("%s lacks %q from the tier below", ReservedRoleNames[i+1], sc)
			}
		}
	}

	adminOnly := []string{
		"platform:grants:write", "platform:roles:write", "platform:budgets:write", "platform:catalog:write",
		"agents:types:write", "agents:runs:write", "agents:environments:write",
		"agents:delegated:read", "mcp:orbit:use",
	}
	for role, scopes := range builtinRoles {
		for _, sc := range scopes {
			for _, a := range adminOnly {
				if role == FinOpsRole && a == "platform:budgets:write" {
					continue // kora-finops sets budgets by design
				}
				if policy.ScopeCovers(sc, a) {
					t.Errorf("%s holds %q, which covers admin-only %q", role, sc, a)
				}
			}
			if sc == "platform:budgets:write" && role != FinOpsRole {
				t.Errorf("%s holds platform:budgets:write, which only kora-finops holds", role)
			}
			if sc == "egress:connect" && role != OperatorRole {
				t.Errorf("%s holds egress:connect, which only kora-operator holds", role)
			}
			if strings.HasPrefix(sc, "connection:") || (strings.HasPrefix(sc, "egress:") && sc != "egress:connect") ||
				strings.HasSuffix(sc, ":use") {
				t.Errorf("%s holds %q: model, connection, egress and MCP tool scopes stay out of built-in roles", role, sc)
			}
		}
	}
}

// pendingScopes are declared by built-in roles ahead of the service change
// that registers them. Remove an entry once its service registers the scope.
var pendingScopes []string

// TestBuiltinRoles_ScopesRegistered fails when a built-in role declares a
// scope no service registers, which catches a misspelling. The registered set
// is read from the ScopeDeclaration literals in backend/cmd and backend/internal:
// most declarations live in main packages, which a test cannot import.
func TestBuiltinRoles_ScopesRegistered(t *testing.T) {
	t.Parallel()

	registered := registeredScopes(t, "../../../cmd", "../../../internal")
	if len(registered) < 20 {
		t.Fatalf("found %d registered scopes; the declaration parser is broken", len(registered))
	}
	for role, scopes := range builtinRoles {
		for _, sc := range scopes {
			_, ok := registered[sc]
			pending := slices.Contains(pendingScopes, sc)
			switch {
			case !ok && !pending:
				t.Errorf("%s declares %q, which no service registers", role, sc)
			case ok && pending:
				t.Errorf("%q is registered now; remove it from pendingScopes", sc)
			}
		}
	}
}

// registeredScopes collects the Scope field of every ScopeDeclaration literal
// under roots, resolving string constants by name or as <package>.<Name>.
func registeredScopes(t *testing.T, roots ...string) map[string]struct{} {
	t.Helper()
	files := map[string][]*ast.File{}
	fset := token.NewFileSet()
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			files[filepath.Dir(path)] = append(files[filepath.Dir(path)], f)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}

	// Exported constants are also reachable as <package>.<Name> from other packages.
	qualified := map[string]string{}
	for _, pkg := range files {
		for name, v := range stringConsts(pkg) {
			qualified[pkg[0].Name.Name+"."+name] = v
		}
	}
	out := map[string]struct{}{}
	for _, pkg := range files {
		consts := stringConsts(pkg)
		maps.Copy(consts, qualified)
		for _, f := range pkg {
			ast.Inspect(f, func(n ast.Node) bool {
				lit, ok := n.(*ast.CompositeLit)
				if !ok {
					return true
				}
				if isScopeDeclaration(lit.Type) {
					addScopeField(lit, consts, out)
				}
				if arr, ok := lit.Type.(*ast.ArrayType); ok && isScopeDeclaration(arr.Elt) {
					for _, el := range lit.Elts {
						if c, ok := el.(*ast.CompositeLit); ok && c.Type == nil {
							addScopeField(c, consts, out)
						}
					}
				}
				return true
			})
		}
	}
	return out
}

func isScopeDeclaration(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name == "ScopeDeclaration"
	case *ast.SelectorExpr:
		return v.Sel.Name == "ScopeDeclaration"
	}
	return false
}

func stringConsts(pkg []*ast.File) map[string]string {
	out := map[string]string{}
	for _, f := range pkg {
		ast.Inspect(f, func(n ast.Node) bool {
			gd, ok := n.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				return true
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, name := range vs.Names {
					if i < len(vs.Values) {
						if s, ok := stringLit(vs.Values[i]); ok {
							out[name.Name] = s
						}
					}
				}
			}
			return true
		})
	}
	return out
}

func addScopeField(lit *ast.CompositeLit, consts map[string]string, out map[string]struct{}) {
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if key, ok := kv.Key.(*ast.Ident); !ok || key.Name != "Scope" {
			continue
		}
		if s, ok := stringLit(kv.Value); ok {
			out[s] = struct{}{}
		} else if id, ok := kv.Value.(*ast.Ident); ok && consts[id.Name] != "" {
			out[consts[id.Name]] = struct{}{}
		} else if sel, ok := kv.Value.(*ast.SelectorExpr); ok {
			if pkg, ok := sel.X.(*ast.Ident); ok && consts[pkg.Name+"."+sel.Sel.Name] != "" {
				out[consts[pkg.Name+"."+sel.Sel.Name]] = struct{}{}
			}
		}
	}
}

func stringLit(e ast.Expr) (string, bool) {
	bl, ok := e.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(bl.Value)
	return s, err == nil
}
