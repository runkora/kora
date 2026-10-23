package agents

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/runkora/kora/backend/internal/platform/logging"
	si "github.com/runkora/kora/backend/internal/platform/sandboximages/v1alpha1"
)

func TestEnvironmentValidate_Size(t *testing.T) {
	base := Environment{Slug: "go", Name: "Go", SandboxImage: si.BaseName}
	for _, tc := range []struct {
		name    string
		size    EnvironmentSize
		wantErr string
	}{
		{"at the maximum", testSizes.Max, ""},
		{"smaller", EnvironmentSize{CPU: "500m", Memory: "512Mi", Disk: "1Gi"}, ""},
		{"cpu above", EnvironmentSize{CPU: "9", Memory: "1Gi", Disk: "1Gi"}, "size.cpu 9 is above the maximum 8"},
		{"memory above", EnvironmentSize{CPU: "1", Memory: "17Gi", Disk: "1Gi"}, "size.memory"},
		{"disk above", EnvironmentSize{CPU: "1", Memory: "1Gi", Disk: "51Gi"}, "size.disk"},
		{"not a quantity", EnvironmentSize{CPU: "lots", Memory: "1Gi", Disk: "1Gi"}, "not a quantity"},
		{"zero", EnvironmentSize{CPU: "0", Memory: "1Gi", Disk: "1Gi"}, "greater than zero"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := base
			env.Size = tc.size
			err := env.Validate(testSizes.Max)
			if tc.wantErr == "" && err != nil {
				t.Fatalf("Validate: %v", err)
			}
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("Validate = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestEnvironmentSizes_Validate(t *testing.T) {
	if err := testSizes.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	big := testSizes
	big.Default.Memory = "32Gi"
	if err := big.Validate(); err == nil {
		t.Error("a default above the maximum was accepted")
	}
}

// fakeEnvStore is an in-memory environment store for the service tests.
type fakeEnvStore struct {
	envs      map[string]Environment
	deleteErr error
}

func (f *fakeEnvStore) CreateEnvironment(_ context.Context, env Environment) error {
	f.envs[env.ID] = env
	return nil
}

func (f *fakeEnvStore) GetEnvironment(_ context.Context, tenantID, id string) (*Environment, error) {
	e, ok := f.envs[id]
	if !ok || e.TenantID != tenantID {
		return nil, ErrNotFound
	}
	return &e, nil
}

func (f *fakeEnvStore) ListEnvironments(context.Context, string) ([]Environment, error) {
	return nil, nil
}

func (f *fakeEnvStore) UpdateEnvironment(_ context.Context, env Environment) error {
	f.envs[env.ID] = env
	return nil
}

func (f *fakeEnvStore) DeleteEnvironment(context.Context, string, string) error { return f.deleteErr }

func newEnvironmentService(store *fakeEnvStore, k8s *mockK8s) *EnvironmentService {
	return NewEnvironmentService(store, newMockRepository(), k8s, k8s, fakeWriteChecker{}, EnvironmentServiceConfig{
		TenantNamespaceFn: func(id string) string { return "kora-tenant-" + id },
		EnvironmentSizes:  testSizes,
	}, logging.NewLoggerWithOutput(io.Discard))
}

func TestCreateEnvironment(t *testing.T) {
	k8s := &mockK8s{sandboxImages: map[string]*si.SandboxImage{
		"acme-go":   validSandboxImage("acme-go", "ghcr.io/acme/go:1", "sha256:go"),
		"broken":    {Status: si.Status{}},
		si.BaseName: validSandboxImage(si.BaseName, "ghcr.io/runkora/kora-sandbox-base:1", "sha256:base"),
	}}
	ctx := withTenantCtx(context.Background(), "t-1")
	for _, tc := range []struct {
		name    string
		req     EnvironmentRequest
		wantErr string
	}{
		{"base image with default size", EnvironmentRequest{Name: "default-ish"}, ""},
		{"blank image is the base image", EnvironmentRequest{Name: "blank", SandboxImage: "  "}, ""},
		{"registered image", EnvironmentRequest{Name: "go", SandboxImage: "acme-go", Size: EnvironmentSize{CPU: "4"}}, ""},
		{"unregistered image", EnvironmentRequest{Name: "rust", SandboxImage: "acme-rust"}, "is not registered"},
		{"image not validated", EnvironmentRequest{Name: "x", SandboxImage: "broken"}, "is not validated yet"},
		{"too large", EnvironmentRequest{Name: "big", Size: EnvironmentSize{Memory: "64Gi"}}, "above the maximum"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := newEnvironmentService(&fakeEnvStore{envs: map[string]Environment{}}, k8s)
			env, err := svc.CreateEnvironment(ctx, tc.req)
			if tc.wantErr != "" {
				if !errors.Is(err, ErrInvalidEnvironment) || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want ErrInvalidEnvironment with %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if env.Size.Disk != testSizes.Default.Disk || env.Size.Memory != testSizes.Default.Memory {
				t.Errorf("size = %+v, want empty dimensions from the default", env.Size)
			}
			if tc.req.SandboxImage == "" && env.SandboxImage != si.BaseName {
				t.Errorf("sandbox_image = %q, want kora-base stored for an omitted image", env.SandboxImage)
			}
		})
	}
}

// A malformed image name is rejected before any registry lookup.
func TestCreateEnvironment_MalformedImageName(t *testing.T) {
	k8s := &mockK8s{}
	svc := newEnvironmentService(&fakeEnvStore{envs: map[string]Environment{}}, k8s)
	_, err := svc.CreateEnvironment(withTenantCtx(context.Background(), "t-1"), EnvironmentRequest{Name: "x", SandboxImage: "Not_A/Name"})
	if !errors.Is(err, ErrInvalidEnvironment) || !strings.Contains(err.Error(), "sandbox_image") {
		t.Fatalf("err = %v, want ErrInvalidEnvironment about sandbox_image", err)
	}
	if k8s.imageLookups != 0 {
		t.Errorf("looked up %d images for a malformed name", k8s.imageLookups)
	}
}

// An environment on the base image fails clearly while kora-base is missing or invalid.
func TestCreateEnvironment_BaseImageUnavailable(t *testing.T) {
	ctx := withTenantCtx(context.Background(), "t-1")
	for name, images := range map[string]map[string]*si.SandboxImage{
		"missing": {},
		"invalid": {si.BaseName: {Status: si.Status{}}},
	} {
		t.Run(name, func(t *testing.T) {
			svc := newEnvironmentService(&fakeEnvStore{envs: map[string]Environment{}}, &mockK8s{sandboxImages: images})
			_, err := svc.CreateEnvironment(ctx, EnvironmentRequest{Name: "base"})
			if !errors.Is(err, ErrInvalidEnvironment) || !strings.Contains(err.Error(), `"kora-base"`) {
				t.Fatalf("err = %v, want ErrInvalidEnvironment naming kora-base", err)
			}
		})
	}
}

func TestDeriveSlug(t *testing.T) {
	for in, want := range map[string]string{
		"Go services":          "go-services",
		"  Payments / Review ": "payments-review",
		"node22":               "node22",
		"---":                  "",
		"Café ML":              "caf-ml",
	} {
		if got := deriveSlug(in); got != want {
			t.Errorf("deriveSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

// A create takes the slug it is given, derives one from the name when it has none,
// and rejects a slug that is not a DNS-1123 label.
func TestCreateEnvironment_Slug(t *testing.T) {
	k8s := &mockK8s{sandboxImages: map[string]*si.SandboxImage{
		si.BaseName: validSandboxImage(si.BaseName, "ghcr.io/runkora/kora-sandbox-base:1", "sha256:base"),
	}}
	ctx := withTenantCtx(context.Background(), "t-1")
	for _, tc := range []struct {
		name     string
		req      EnvironmentRequest
		wantSlug string
		wantErr  string
	}{
		{"given", EnvironmentRequest{Name: "Go services", Slug: "go"}, "go", ""},
		{"derived", EnvironmentRequest{Name: "Go services"}, "go-services", ""},
		{"symbols only", EnvironmentRequest{Name: "***"}, "", "supply a slug"},
		{"too long", EnvironmentRequest{Name: strings.Repeat("a", 64)}, "", "supply a slug"},
		{"not a label", EnvironmentRequest{Name: "Go", Slug: "Go_Services"}, "", "supply a slug"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := newEnvironmentService(&fakeEnvStore{envs: map[string]Environment{}}, k8s)
			env, err := svc.CreateEnvironment(ctx, tc.req)
			if tc.wantErr != "" {
				if !errors.Is(err, ErrInvalidEnvironment) || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want ErrInvalidEnvironment with %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if env.Slug != tc.wantSlug || env.Name != tc.req.Name {
				t.Errorf("slug, name = %q, %q; want %q, %q", env.Slug, env.Name, tc.wantSlug, tc.req.Name)
			}
		})
	}
}

// An update keeps the stored slug, even one that is not a DNS-1123 label, and refuses to change it.
func TestUpdateEnvironment_SlugIsImmutable(t *testing.T) {
	k8s := &mockK8s{sandboxImages: map[string]*si.SandboxImage{
		si.BaseName: validSandboxImage(si.BaseName, "ghcr.io/runkora/kora-sandbox-base:1", "sha256:base"),
	}}
	ctx := withTenantCtx(context.Background(), "t-1")
	for _, tc := range []struct {
		name    string
		slug    string
		wantErr bool
	}{
		{"omitted", "", false},
		{"same", "Legacy Env", false},
		{"changed", "legacy-env", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeEnvStore{envs: map[string]Environment{
				"e1": {ID: "e1", TenantID: "t-1", Slug: "Legacy Env", Name: "Legacy Env", SandboxImage: si.BaseName},
			}}
			svc := newEnvironmentService(store, k8s)
			env, err := svc.UpdateEnvironment(ctx, EnvironmentRequest{ID: "e1", Slug: tc.slug, Name: "Legacy"})
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidEnvironment) || !strings.Contains(err.Error(), "slug cannot change") {
					t.Fatalf("err = %v, want ErrInvalidEnvironment about the slug", err)
				}
				if store.envs["e1"].Name != "Legacy Env" {
					t.Errorf("stored name = %q, want it unchanged", store.envs["e1"].Name)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if env.Slug != "Legacy Env" || store.envs["e1"].Name != "Legacy" {
				t.Errorf("slug %q, stored name %q; want the stored slug and the new name", env.Slug, store.envs["e1"].Name)
			}
		})
	}
}

// An update that changes the slug is a 400.
func TestHandleUpdateEnvironment_SlugChange(t *testing.T) {
	k8s := &mockK8s{sandboxImages: map[string]*si.SandboxImage{
		si.BaseName: validSandboxImage(si.BaseName, "ghcr.io/runkora/kora-sandbox-base:1", "sha256:base"),
	}}
	store := &fakeEnvStore{envs: map[string]Environment{
		"env-1": {ID: "env-1", TenantID: "t-1", Slug: "go", Name: "Go", SandboxImage: si.BaseName},
	}}
	c := NewController(nil, nil, newEnvironmentService(store, k8s), nil, nil, nil, &allowAllEvaluator{}, nil, "", logging.NewLoggerWithOutput(io.Discard))
	req := withTenantRequest(httptest.NewRequest(http.MethodPut, "/api/v1/environments/env-1", strings.NewReader(`{"slug":"node","name":"Go"}`)), "t-1")
	req.SetPathValue("id", "env-1")
	w := httptest.NewRecorder()
	c.handleUpdateEnvironment(w, req)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "slug cannot change") {
		t.Errorf("status %d body %s, want 400 about the slug", w.Code, w.Body.String())
	}
}

// Create and update require a name for people.
func TestHandleEnvironment_NameRequired(t *testing.T) {
	svc := newEnvironmentService(&fakeEnvStore{envs: map[string]Environment{}}, &mockK8s{})
	c := NewController(nil, nil, svc, nil, nil, nil, &allowAllEvaluator{}, nil, "", logging.NewLoggerWithOutput(io.Discard))
	for _, handle := range []func(http.ResponseWriter, *http.Request){c.handleCreateEnvironment, c.handleUpdateEnvironment} {
		req := withTenantRequest(httptest.NewRequest(http.MethodPost, "/api/v1/environments", strings.NewReader(`{"slug":"go","name":"  "}`)), "t-1")
		req.SetPathValue("id", "env-1")
		w := httptest.NewRecorder()
		handle(w, req)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "name is required") {
			t.Errorf("status %d body %s, want 400 name is required", w.Code, w.Body.String())
		}
	}
}

// Deleting an attached environment is a 409. The frontend tells the user to detach it first.
func TestHandleDeleteEnvironment_InUse(t *testing.T) {
	svc := newEnvironmentService(&fakeEnvStore{deleteErr: ErrEnvironmentInUse}, &mockK8s{})
	c := NewController(nil, nil, svc, nil, nil, nil, &allowAllEvaluator{}, nil, "", logging.NewLoggerWithOutput(io.Discard))
	req := withTenantRequest(httptest.NewRequest(http.MethodDelete, "/api/v1/environments/env-1", http.NoBody), "t-1")
	req.SetPathValue("id", "env-1")
	w := httptest.NewRecorder()
	c.handleDeleteEnvironment(w, req)
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", w.Code)
	}
}

func TestValidateEnvironments(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	repo.envs["a"] = &Environment{ID: "a", TenantID: "t-1", Slug: "a"}
	repo.envs["b"] = &Environment{ID: "b", TenantID: "t-1", Slug: "b"}
	repo.envs["other"] = &Environment{ID: "other", TenantID: "t-2", Slug: "other"}
	for _, tc := range []struct {
		name    string
		in      []AgentEnvironment
		want    []AgentEnvironment
		wantErr bool
	}{
		{"none is the built-in default", nil, []AgentEnvironment{}, false},
		{"a lone environment is the default", []AgentEnvironment{{EnvironmentID: "a"}}, []AgentEnvironment{{EnvironmentID: "a", IsDefault: true}}, false},
		{"one default of two", []AgentEnvironment{{EnvironmentID: "a"}, {EnvironmentID: "b", IsDefault: true}}, []AgentEnvironment{{EnvironmentID: "a"}, {EnvironmentID: "b", IsDefault: true}}, false},
		{"no default of two", []AgentEnvironment{{EnvironmentID: "a"}, {EnvironmentID: "b"}}, nil, true},
		{"two defaults", []AgentEnvironment{{EnvironmentID: "a", IsDefault: true}, {EnvironmentID: "b", IsDefault: true}}, nil, true},
		{"duplicate", []AgentEnvironment{{EnvironmentID: "a", IsDefault: true}, {EnvironmentID: "a"}}, nil, true},
		{"another tenant's", []AgentEnvironment{{EnvironmentID: "other"}}, nil, true},
		{"unknown", []AgentEnvironment{{EnvironmentID: "nope"}}, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := svc.at.validateEnvironments(context.Background(), "t-1", tc.in)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidEnvironment) {
					t.Fatalf("err = %v, want ErrInvalidEnvironment", err)
				}
				return
			}
			if err != nil || !slices.Equal(got, tc.want) {
				t.Fatalf("got %+v, %v; want %+v", got, err, tc.want)
			}
		})
	}
}

// The grant is the agent's own scopes plus the union of every attached environment.
func TestEffectiveScopes_UnionOfEnvironments(t *testing.T) {
	repo := newMockRepository()
	repo.envs["go"] = &Environment{ID: "go", TenantID: "acme", Slug: "go", EgressHosts: []string{"proxy.golang.org"}}
	repo.envs["node"] = &Environment{ID: "node", TenantID: "acme", Slug: "node", EgressHosts: []string{"registry.npmjs.org"},
		Connectors: []Connector{{Connection: "github", Actions: []string{"contents:read"}}}}
	at := AgentType{
		TenantID:     "acme",
		Connectivity: Connectivity{Mode: ConnectivityWhitelist, Hosts: []string{"api.example.com"}},
		Environments: []AgentEnvironment{{EnvironmentID: "go", IsDefault: true}, {EnvironmentID: "node"}},
	}
	scopes, err := NewScopeResolver(repo, nil).EffectiveScopes(context.Background(), at)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"egress:api.example.com:connect",
		"egress:proxy.golang.org:connect",
		"egress:registry.npmjs.org:connect",
		"connection:github:contents:read",
	} {
		if !slices.Contains(scopes, want) {
			t.Errorf("scopes %v missing %q", scopes, want)
		}
	}

	access, err := NewScopeResolver(repo, nil).EffectiveAccess(context.Background(), at)
	if err != nil {
		t.Fatal(err)
	}
	if len(access.Environments) != 2 || !slices.Equal(access.EnvironmentHosts, []string{"proxy.golang.org", "registry.npmjs.org"}) {
		t.Errorf("access = %+v, want both environments and their hosts", access)
	}
}

// An agent write with an invalid environment set is a 400.
func TestHandleUpdateAgentType_InvalidEnvironmentsIs400(t *testing.T) {
	c, repo, _, _ := newTestController(t)
	seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
	repo.envs["a"] = &Environment{ID: "a", TenantID: "t-1", Slug: "a"}
	repo.envs["b"] = &Environment{ID: "b", TenantID: "t-1", Slug: "b"}
	req := withTenantRequest(httptest.NewRequest(http.MethodPatch, "/api/v1/agent-types/at-1",
		strings.NewReader(`{"environments":[{"environment_id":"a"},{"environment_id":"b"}]}`)), "t-1")
	req.SetPathValue("id", "at-1")
	w := httptest.NewRecorder()
	c.handleUpdateAgentType(w, req)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "exactly one environment as the default") {
		t.Errorf("status %d body %s, want 400 naming the default rule", w.Code, w.Body.String())
	}
}

// The sizes endpoint returns the install's default and maximum sizes.
func TestHandleEnvironmentSizes(t *testing.T) {
	svc := newEnvironmentService(&fakeEnvStore{envs: map[string]Environment{}}, &mockK8s{})
	c := NewController(nil, nil, svc, nil, nil, nil, &allowAllEvaluator{}, nil, "", logging.NewLoggerWithOutput(io.Discard))
	w := httptest.NewRecorder()
	c.handleEnvironmentSizes(w, withTenantRequest(httptest.NewRequest(http.MethodGet, "/api/v1/environment-sizes", http.NoBody), "t-1"))
	want := `{"default":{"cpu":"2","memory":"4Gi","disk":"10Gi"},"max":{"cpu":"8","memory":"16Gi","disk":"50Gi"}}`
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != want {
		t.Errorf("status %d body %s, want %s", w.Code, w.Body.String(), want)
	}
}

// The SQLSTATE helpers match the wrapped Postgres code and nothing else.
func TestSQLStateHelpers(t *testing.T) {
	fk := fmt.Errorf("delete: %w", &pgconn.PgError{Code: "23503"})
	unique := fmt.Errorf("insert: %w", &pgconn.PgError{Code: "23505"})
	if !isForeignKeyViolation(fk) || isForeignKeyViolation(unique) || isForeignKeyViolation(errors.New("plain")) {
		t.Error("isForeignKeyViolation matched the wrong errors")
	}
	if !isUniqueViolation(unique) || isUniqueViolation(fk) {
		t.Error("isUniqueViolation matched the wrong errors")
	}
}

// The built-in environment is selected by the slug default and shown as Default.
func TestDefaultEnvironment_SlugAndName(t *testing.T) {
	env := DefaultEnvironment(testSizes.Default, "Acme")
	if env.Slug != DefaultEnvironmentName || env.Name != "Default" {
		t.Errorf("slug, name = %q, %q", env.Slug, env.Name)
	}
}
