package operator

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/runkora/kora/backend/internal/platform/logging"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
)

const (
	testTenantNS = "orbit-tenant-acme"
	secretA      = "sk-alpha-value"
	secretB      = "sk-bravo-value"
)

func authHeader(name, prefix, secret, key string) MCPServerAuthHeader {
	return MCPServerAuthHeader{Name: name, Prefix: prefix, SecretRef: MCPServerSecretRef{Name: secret, Key: key}}
}

func TestValidateMCPServerAuth(t *testing.T) {
	ok := authHeader("x-api-key", "", "exa", "key")
	cases := []struct {
		name    string
		spec    MCPServerSpec
		wantErr error
	}{
		{"no auth", MCPServerSpec{}, nil},
		{"valid", MCPServerSpec{Auth: &MCPServerAuth{Headers: []MCPServerAuthHeader{ok, authHeader("Authorization", "Bearer ", "exa", "token")}}}, nil},
		{"with forward", MCPServerSpec{CallerIdentity: CallerIdentityForward, Auth: &MCPServerAuth{Headers: []MCPServerAuthHeader{ok}}}, ErrMCPServerAuthWithForward},
		{"empty list", MCPServerSpec{Auth: &MCPServerAuth{}}, ErrMCPServerBadAuthHeader},
		{"not a token", MCPServerSpec{Auth: &MCPServerAuth{Headers: []MCPServerAuthHeader{authHeader("x api key", "", "exa", "key")}}}, ErrMCPServerBadAuthHeader},
		{"empty name", MCPServerSpec{Auth: &MCPServerAuth{Headers: []MCPServerAuthHeader{authHeader("", "", "exa", "key")}}}, ErrMCPServerBadAuthHeader},
		{"host reserved", MCPServerSpec{Auth: &MCPServerAuth{Headers: []MCPServerAuthHeader{authHeader("Host", "", "exa", "key")}}}, ErrMCPServerBadAuthHeader},
		{"delegation reserved", MCPServerSpec{Auth: &MCPServerAuth{Headers: []MCPServerAuthHeader{authHeader(mw.DelegationTokenHeader, "", "exa", "key")}}}, ErrMCPServerBadAuthHeader},
		{"mcp session reserved", MCPServerSpec{Auth: &MCPServerAuth{Headers: []MCPServerAuthHeader{authHeader("MCP-Session-Id", "", "exa", "key")}}}, ErrMCPServerBadAuthHeader},
		{"forwarded prefix reserved", MCPServerSpec{Auth: &MCPServerAuth{Headers: []MCPServerAuthHeader{authHeader("X-Forwarded-For", "", "exa", "key")}}}, ErrMCPServerBadAuthHeader},
		{"envoy prefix reserved", MCPServerSpec{Auth: &MCPServerAuth{Headers: []MCPServerAuthHeader{authHeader("x-envoy-original-dst-host", "", "exa", "key")}}}, ErrMCPServerBadAuthHeader},
		{"duplicate ignoring case", MCPServerSpec{Auth: &MCPServerAuth{Headers: []MCPServerAuthHeader{ok, authHeader("X-API-Key", "", "exa", "other")}}}, ErrMCPServerBadAuthHeader},
		{"prefix with CRLF", MCPServerSpec{Auth: &MCPServerAuth{Headers: []MCPServerAuthHeader{authHeader("Authorization", "Bearer\r\nX-Injected: 1 ", "exa", "key")}}}, ErrMCPServerBadAuthHeader},
		{"prefix with tab", MCPServerSpec{Auth: &MCPServerAuth{Headers: []MCPServerAuthHeader{authHeader("Authorization", "Bearer\t", "exa", "key")}}}, nil},
		{"missing key", MCPServerSpec{Auth: &MCPServerAuth{Headers: []MCPServerAuthHeader{authHeader("x-api-key", "", "exa", "")}}}, ErrMCPServerBadAuthHeader},
		{"secret name with slash", MCPServerSpec{Auth: &MCPServerAuth{Headers: []MCPServerAuthHeader{authHeader("x-api-key", "", "a/b", "key")}}}, ErrMCPServerBadAuthHeader},
		{"secret name dot-dot", MCPServerSpec{Auth: &MCPServerAuth{Headers: []MCPServerAuthHeader{authHeader("x-api-key", "", "..", "key")}}}, ErrMCPServerBadAuthHeader},
		{"secret key with slash", MCPServerSpec{Auth: &MCPServerAuth{Headers: []MCPServerAuthHeader{authHeader("x-api-key", "", "exa", "a/b")}}}, ErrMCPServerBadAuthHeader},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.spec.Endpoint = "https://mcp.example.com/mcp"
			err := validateMCPServerSpec(tc.spec, "exa", systemNamespace)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("validateMCPServerSpec = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestDelegationHeaderMatchesMiddleware(t *testing.T) {
	if delegationTokenHeader != strings.ToLower(mw.DelegationTokenHeader) {
		t.Errorf("delegationTokenHeader = %q, want %q", delegationTokenHeader, strings.ToLower(mw.DelegationTokenHeader))
	}
}

func exaServer(headers ...MCPServerAuthHeader) *MCPServer {
	return &MCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "exa", Namespace: testTenantNS, Generation: 1},
		Spec:       MCPServerSpec{Endpoint: "https://mcp.exa.ai/mcp", Auth: &MCPServerAuth{Headers: headers}},
	}
}

func credentialSecret(data map[string]string) *corev1.Secret {
	s := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "exa-credentials", Namespace: testTenantNS}, Data: map[string][]byte{}}
	for k, v := range data {
		s.Data[k] = []byte(v)
	}
	return s
}

// renderedMCP runs Sync and returns servers.json and the aggregated Secret.
func renderedMCP(t *testing.T, c client.Client) (string, map[string][]byte) {
	t.Helper()
	ctx := context.Background()
	if err := NewMCPServerAggregator(c, logging.NewLoggerWithOutput(io.Discard)).Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	cm := &corev1.ConfigMap{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: systemNamespace, Name: aggregatedMCPServersConfigMapName}, cm); err != nil {
		t.Fatalf("get ConfigMap: %v", err)
	}
	sec := &corev1.Secret{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: systemNamespace, Name: aggregatedMCPServerSecretsName}, sec); err != nil {
		t.Fatalf("get Secret: %v", err)
	}
	return cm.Data[aggregatedMCPServersConfigMapKey], sec.Data
}

func TestMCPAggregator_CopiesCredentialsOutOfTheRegistry(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(NewScheme()).WithObjects(
		tenant("acme", true),
		exaServer(authHeader("Authorization", "Bearer ", "exa-credentials", "token"), authHeader("x-api-key", "", "exa-credentials", "key")),
		credentialSecret(map[string]string{"token": secretA + "\n", "key": secretB + "\r\n"}),
	).Build()

	registry, data := renderedMCP(t, c)

	if strings.Contains(registry, secretA) || strings.Contains(registry, secretB) {
		t.Fatalf("servers.json holds a secret value: %s", registry)
	}
	var entries []mcpServerRegistryEntry
	if err := json.Unmarshal([]byte(registry), &entries); err != nil || len(entries) != 1 {
		t.Fatalf("servers.json = %s (%v)", registry, err)
	}
	want := []mcpAuthHeaderEntry{
		{Name: "Authorization", Prefix: "Bearer ", SecretKey: testTenantNS + ".exa.0"},
		{Name: "x-api-key", SecretKey: testTenantNS + ".exa.1"},
	}
	if got := entries[0].AuthHeaders; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("auth_headers = %+v, want %+v", got, want)
	}
	if string(data[want[0].SecretKey]) != secretA || string(data[want[1].SecretKey]) != secretB {
		t.Errorf("aggregated Secret = %q, want trimmed values", data)
	}

	// Rotation lands on the next pass.
	rotated := credentialSecret(map[string]string{"token": "rotated", "key": secretB})
	if err := c.Update(context.Background(), rotated); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if _, data = renderedMCP(t, c); string(data[want[0].SecretKey]) != "rotated" {
		t.Errorf("after rotation = %q, want rotated", data[want[0].SecretKey])
	}
}

func TestMCPAggregator_MissingCredentialKeepsServer(t *testing.T) {
	cases := []struct {
		name   string
		secret *corev1.Secret
	}{
		{"no Secret", nil},
		{"no key", credentialSecret(map[string]string{"other": secretA})},
		{"empty value", credentialSecret(map[string]string{"key": "\n"})},
		{"control character", credentialSecret(map[string]string{"key": secretA + "\r\nX-Injected: 1"})},
		{"too large", credentialSecret(map[string]string{"key": strings.Repeat("a", maxMCPCredentialSize+1)})},
		{"operator managed", func() *corev1.Secret {
			sec := credentialSecret(map[string]string{"key": secretA})
			sec.Labels = map[string]string{"app.kubernetes.io/managed-by": "orbit-operator"}
			return sec
		}()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			objs := []client.Object{tenant("acme", true), exaServer(authHeader("x-api-key", "", "exa-credentials", "key"))}
			if tc.secret != nil {
				objs = append(objs, tc.secret)
			}
			c := fake.NewClientBuilder().WithScheme(NewScheme()).WithObjects(objs...).Build()

			registry, data := renderedMCP(t, c)

			if !strings.Contains(registry, `"name": "exa"`) {
				t.Errorf("server dropped from the registry: %s", registry)
			}
			if len(data) != 0 {
				t.Errorf("aggregated Secret = %q, want empty", data)
			}
		})
	}
}

func TestMCPServerReconciler_ReportsUnavailableCredential(t *testing.T) {
	srv := exaServer(authHeader("x-api-key", "", "exa-credentials", "key"))
	c := fake.NewClientBuilder().WithScheme(NewScheme()).
		WithObjects(tenant("acme", true), srv, credentialSecret(map[string]string{"other": secretA})).
		WithStatusSubresource(srv).Build()
	r := &MCPServerReconciler{Client: c, Scheme: NewScheme(), PodReader: c,
		Aggregator: NewMCPServerAggregator(c, logging.NewLoggerWithOutput(io.Discard)), Logger: logging.NewLoggerWithOutput(io.Discard)}

	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: testTenantNS, Name: "exa"}})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if res.RequeueAfter != credentialRecheckInterval {
		t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, credentialRecheckInterval)
	}
	got := &MCPServer{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(srv), got); err != nil {
		t.Fatalf("get: %v", err)
	}
	cond := got.Status.Conditions[0]
	if got.Status.Phase != MCPServerPhaseAggregated || cond.Reason != "CredentialUnavailable" ||
		!strings.Contains(cond.Message, `Secret "exa-credentials" has no key "key"`) {
		t.Errorf("status = %s %+v", got.Status.Phase, cond)
	}
	if strings.Contains(cond.Message, secretA) {
		t.Errorf("condition message holds a secret value: %q", cond.Message)
	}
}
