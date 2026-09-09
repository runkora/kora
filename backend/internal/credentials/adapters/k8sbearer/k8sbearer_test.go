package k8sbearer_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/clear-route/orbit/internal/credentials"
	"github.com/clear-route/orbit/internal/credentials/adapters/k8sbearer"
	"github.com/clear-route/orbit/internal/platform/logging"
)

type stubSecrets struct {
	token []byte
	err   error
}

func (s stubSecrets) Resolve(context.Context, string) ([]byte, error) { return s.token, s.err }

func newAdapter(secrets credentials.SecretResolver) *k8sbearer.Adapter {
	return k8sbearer.New(secrets, logging.NewLoggerWithOutput(io.Discard))
}

func TestAdapter_TypeAndActions(t *testing.T) {
	a := newAdapter(stubSecrets{})
	if a.Type() != credentials.AdapterKubernetesBearer {
		t.Errorf("Type() = %q, want %q", a.Type(), credentials.AdapterKubernetesBearer)
	}

	wantPairs := []string{
		k8sbearer.ActionWorkloadsRead, k8sbearer.ActionWorkloadsWrite,
		k8sbearer.ActionConfigRead, k8sbearer.ActionConfigWrite,
		k8sbearer.ActionSecretsRead, k8sbearer.ActionSecretsWrite,
		k8sbearer.ActionNetworkingRead, k8sbearer.ActionNetworkingWrite,
		k8sbearer.ActionStorageRead, k8sbearer.ActionStorageWrite,
		k8sbearer.ActionRBACRead, k8sbearer.ActionRBACWrite,
		k8sbearer.ActionClusterRead, k8sbearer.ActionClusterWrite,
	}
	actions := a.Actions()
	if len(actions) != len(wantPairs) {
		t.Fatalf("Actions() has %d entries, want %d: %+v", len(actions), len(wantPairs), actions)
	}
	got := make(map[string]bool, len(actions))
	for _, a := range actions {
		got[a.Action] = true
	}
	for _, want := range wantPairs {
		if !got[want] {
			t.Errorf("Actions() missing %q", want)
		}
	}
}

// TestClassifyAction_ResourceCategorization is the core coverage for the
// path-parsing taxonomy: one representative path per category, across both
// the core API group (/api/v1/...) and named groups (/apis/{group}/{version}/...),
// both namespaced and cluster-scoped resource shapes, plus the edge cases the
// parser has to get right (the Namespace object itself, the deprecated
// /watch/ prefix, subresources, and non-resource API discovery paths).
func TestClassifyAction_ResourceCategorization(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		want   string
	}{
		// workloads: core and named groups, namespaced and all-namespaces list.
		{"pods namespaced", http.MethodGet, "/api/v1/namespaces/default/pods", k8sbearer.ActionWorkloadsRead},
		{"pods all-namespaces list", http.MethodGet, "/api/v1/pods", k8sbearer.ActionWorkloadsRead},
		{"deployments write", http.MethodPost, "/apis/apps/v1/namespaces/default/deployments", k8sbearer.ActionWorkloadsWrite},
		{"cronjobs", http.MethodGet, "/apis/batch/v1/namespaces/default/cronjobs", k8sbearer.ActionWorkloadsRead},
		{"pod log subresource", http.MethodGet, "/api/v1/namespaces/default/pods/foo/log", k8sbearer.ActionWorkloadsRead},
		{"pod eviction subresource", http.MethodPost, "/api/v1/namespaces/default/pods/foo/eviction", k8sbearer.ActionWorkloadsWrite},
		{"deployment scale subresource", http.MethodPatch, "/apis/apps/v1/namespaces/default/deployments/foo/scale", k8sbearer.ActionWorkloadsWrite},
		{"horizontalpodautoscalers read", http.MethodGet, "/apis/autoscaling/v2/namespaces/default/horizontalpodautoscalers", k8sbearer.ActionWorkloadsRead},
		{"horizontalpodautoscalers write", http.MethodPut, "/apis/autoscaling/v2/namespaces/default/horizontalpodautoscalers/foo", k8sbearer.ActionWorkloadsWrite},

		// config vs. secrets: split into separate categories.
		{"configmaps read", http.MethodGet, "/api/v1/namespaces/default/configmaps", k8sbearer.ActionConfigRead},
		{"secrets read", http.MethodGet, "/api/v1/namespaces/default/secrets", k8sbearer.ActionSecretsRead},
		{"secrets write", http.MethodDelete, "/api/v1/namespaces/default/secrets/foo", k8sbearer.ActionSecretsWrite},

		// networking.
		{"services", http.MethodGet, "/api/v1/namespaces/default/services", k8sbearer.ActionNetworkingRead},
		{"ingresses", http.MethodGet, "/apis/networking.k8s.io/v1/namespaces/default/ingresses", k8sbearer.ActionNetworkingRead},

		// storage: mixed namespaced/cluster-scoped.
		{"persistentvolumeclaims", http.MethodGet, "/api/v1/namespaces/default/persistentvolumeclaims", k8sbearer.ActionStorageRead},
		{"persistentvolumes (cluster-scoped)", http.MethodGet, "/api/v1/persistentvolumes", k8sbearer.ActionStorageRead},
		{"storageclasses (cluster-scoped)", http.MethodGet, "/apis/storage.k8s.io/v1/storageclasses", k8sbearer.ActionStorageRead},

		// rbac: namespaced and cluster-scoped variants of the same concept.
		{"roles (namespaced)", http.MethodGet, "/apis/rbac.authorization.k8s.io/v1/namespaces/default/roles", k8sbearer.ActionRBACRead},
		{"clusterroles (cluster-scoped)", http.MethodGet, "/apis/rbac.authorization.k8s.io/v1/clusterroles", k8sbearer.ActionRBACRead},
		{"serviceaccounts write", http.MethodPut, "/api/v1/namespaces/default/serviceaccounts/foo", k8sbearer.ActionRBACWrite},

		// cluster catch-all: well-known cluster-scoped/admin resources.
		{"nodes", http.MethodGet, "/api/v1/nodes", k8sbearer.ActionClusterRead},
		{"namespace list (the resource itself)", http.MethodGet, "/api/v1/namespaces", k8sbearer.ActionClusterRead},
		{"single namespace object", http.MethodGet, "/api/v1/namespaces/default", k8sbearer.ActionClusterRead},
		{"events", http.MethodGet, "/api/v1/namespaces/default/events", k8sbearer.ActionClusterRead},

		// non-resource API discovery: always bucketed into cluster, never denied.
		{"root", http.MethodGet, "/", k8sbearer.ActionClusterRead},
		{"api root", http.MethodGet, "/api", k8sbearer.ActionClusterRead},
		{"apis root", http.MethodGet, "/apis", k8sbearer.ActionClusterRead},
		{"apis group root", http.MethodGet, "/apis/apps/v1", k8sbearer.ActionClusterRead},
		{"version", http.MethodGet, "/version", k8sbearer.ActionClusterRead},
		{"openapi", http.MethodGet, "/openapi/v2", k8sbearer.ActionClusterRead},

		// deprecated /watch/ prefix form.
		{"deprecated watch prefix", http.MethodGet, "/api/v1/watch/namespaces/default/pods", k8sbearer.ActionWorkloadsRead},
	}

	a := newAdapter(stubSecrets{})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, "https://k8s.example.com"+tc.path, nil)
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			got, err := a.ClassifyAction(req, credentials.Connection{})
			if err != nil {
				t.Fatalf("ClassifyAction(%s %s): unexpected error %v", tc.method, tc.path, err)
			}
			if got.Action != tc.want {
				t.Errorf("ClassifyAction(%s %s) = %q, want %q", tc.method, tc.path, got.Action, tc.want)
			}
		})
	}
}

// TestClassifyAction_UnrecognizedResourceDenied confirms the fail-closed
// decision: a resource type not present in the category map (a CRD, or
// anything not yet enumerated) is denied outright rather than folded into
// the cluster catch-all.
func TestClassifyAction_UnrecognizedResourceDenied(t *testing.T) {
	a := newAdapter(stubSecrets{})
	req, _ := http.NewRequest(http.MethodGet, "https://k8s.example.com/apis/example.com/v1/namespaces/default/widgets", nil)
	_, err := a.ClassifyAction(req, credentials.Connection{})
	if !errors.Is(err, credentials.ErrUnsupportedAction) {
		t.Errorf("ClassifyAction(unrecognized CRD) error = %v, want ErrUnsupportedAction", err)
	}
}

func TestClassifyAction_UnsupportedVerbDenied(t *testing.T) {
	a := newAdapter(stubSecrets{})
	req, _ := http.NewRequest("PROPFIND", "https://k8s.example.com/api/v1/pods", nil)
	_, err := a.ClassifyAction(req, credentials.Connection{})
	if !errors.Is(err, credentials.ErrUnsupportedAction) {
		t.Errorf("ClassifyAction(PROPFIND) error = %v, want ErrUnsupportedAction", err)
	}
}

func TestClassifyAction_UpgradeRequestDenied(t *testing.T) {
	a := newAdapter(stubSecrets{})
	// kubectl exec/attach/port-forward negotiate a SPDY or WebSocket upgrade.
	// Those are out of scope and must fail cleanly rather than be misclassified as a
	// REST GET, regardless of what resource category the path would otherwise
	// map to.
	req, _ := http.NewRequest(http.MethodGet, "https://k8s.example.com/api/v1/namespaces/default/pods/foo/exec", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "SPDY/3.1")

	_, err := a.ClassifyAction(req, credentials.Connection{})
	if !errors.Is(err, credentials.ErrUnsupportedAction) {
		t.Errorf("ClassifyAction(upgrade) error = %v, want ErrUnsupportedAction", err)
	}
}

func TestClassifyAction_NilRequestDenied(t *testing.T) {
	a := newAdapter(stubSecrets{})
	if _, err := a.ClassifyAction(nil, credentials.Connection{}); !errors.Is(err, credentials.ErrUnsupportedAction) {
		t.Errorf("ClassifyAction(nil) error = %v, want ErrUnsupportedAction", err)
	}
}

func TestResolve_ReturnsBearerCredential(t *testing.T) {
	a := newAdapter(stubSecrets{token: []byte("sa-token-abc")})
	conn := credentials.Connection{Name: "k8s-local", SecretRef: "k8s-local-token"}

	cred, err := a.Resolve(context.Background(), conn, credentials.CallerIdentity{TenantID: "acme", RunID: "run-1"})
	if err != nil {
		t.Fatalf("Resolve: unexpected error %v", err)
	}
	if cred.Header != "Authorization" || cred.Value != "Bearer sa-token-abc" || cred.Token != "sa-token-abc" {
		t.Errorf("Resolve() = %+v, want Bearer sa-token-abc", cred)
	}
	if !cred.ExpiresAt.IsZero() {
		t.Errorf("ExpiresAt = %v, want zero (static token, never expires)", cred.ExpiresAt)
	}
}

func TestResolve_SecretResolveFailurePropagates(t *testing.T) {
	a := newAdapter(stubSecrets{err: errors.New("secret not mounted")})
	_, err := a.Resolve(context.Background(), credentials.Connection{SecretRef: "missing"}, credentials.CallerIdentity{})
	if err == nil {
		t.Fatal("Resolve: expected error, got nil")
	}
}
