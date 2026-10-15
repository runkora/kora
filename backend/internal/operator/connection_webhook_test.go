package operator

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/clear-route/orbit/internal/platform/logging"
)

const connTestNS = "orbit-tenant-acme"

var connTestEpoch = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

// testConnection builds a Connection created age minutes after connTestEpoch.
func testConnection(name, adapter, baseHost, owner string, age int) *Connection {
	cfg := map[string]any{"app_id": "1", "installation_id": 1}
	if owner != "" {
		cfg["owner"] = owner
	}
	raw, _ := json.Marshal(cfg)
	return &Connection{
		TypeMeta: metav1.TypeMeta{APIVersion: GroupVersion.String(), Kind: "Connection"},
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: connTestNS,
			CreationTimestamp: metav1.NewTime(connTestEpoch.Add(time.Duration(age) * time.Minute)),
		},
		Spec: ConnectionSpec{Adapter: adapter, BaseHost: baseHost, SecretRef: name, Config: runtime.RawExtension{Raw: raw}},
	}
}

// connectionRequest builds an admission request. A non-nil old makes it an UPDATE.
func connectionRequest(t *testing.T, conn, old *Connection) admission.Request {
	t.Helper()
	encode := func(c *Connection) runtime.RawExtension {
		raw, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		return runtime.RawExtension{Raw: raw}
	}
	req := admissionv1.AdmissionRequest{Operation: admissionv1.Create, Namespace: connTestNS, Object: encode(conn)}
	if old != nil {
		req.Operation, req.OldObject = admissionv1.Update, encode(old)
	}
	return admission.Request{AdmissionRequest: req}
}

func TestConnectionValidator(t *testing.T) {
	existingGitHub := testConnection("contoso-github", "github_app", "github.com", "contoso", 0)
	existingKube := testConnection("prod-cluster", "kubernetes", "k8s.example:443", "", 0)
	laterDup := testConnection("later-dup", "github_app", "github.com", "contoso", 5)
	labeled := testConnection("later-dup", "github_app", "github.com", "contoso", 5)
	labeled.Labels = map[string]string{"team": "platform"}
	generated := testConnection("", "github_app", "github.com", "contoso", 5)
	generated.GenerateName = "github-"

	tests := []struct {
		name        string
		conn, old   *Connection
		existing    []client.Object
		wantAllowed bool
		wantNamed   string
	}{
		{"github same owner rejected", testConnection("dup", "github_app", "github.com", "Contoso", 5), nil, []client.Object{existingGitHub}, false, "contoso-github"},
		{"github missing owner rejected", testConnection("no-owner", "github_app", "github.com", "", 5), nil, []client.Object{existingGitHub}, false, "contoso-github"},
		{"github distinct owner allowed", testConnection("fabrikam-github", "github_app", "github.com", "fabrikam", 5), nil, []client.Object{existingGitHub}, true, ""},
		{"kubernetes same host rejected", testConnection("prod-cluster-2", "kubernetes", "K8S.example:443", "", 5), nil, []client.Object{existingKube}, false, "prod-cluster"},
		{"kubernetes distinct host allowed", testConnection("staging-cluster", "kubernetes", "staging.example:443", "", 5), nil, []client.Object{existingKube}, true, ""},
		{"generateName create rejected with its prefix", generated, nil, []client.Object{existingGitHub}, false, "contoso-github"},
		{"update keeping an existing conflict allowed", labeled, laterDup, []client.Object{existingGitHub, laterDup}, true, ""},
		{"update of the earlier connection keeping its conflict allowed", existingGitHub, existingGitHub, []client.Object{existingGitHub, laterDup}, true, ""},
		{"update introducing a conflict with a later connection rejected", testConnection("prod-cluster", "kubernetes", "b.example:443", "", 0), existingKube, []client.Object{existingKube, testConnection("b-cluster", "kubernetes", "b.example:443", "", 5)}, false, "b-cluster"},
		{"update dropping the owner rejected", testConnection("fabrikam-github", "github_app", "github.com", "", 5), testConnection("fabrikam-github", "github_app", "github.com", "fabrikam", 5), []client.Object{existingGitHub}, false, "contoso-github"},
		{"update compared with itself allowed", testConnection("contoso-github", "github_app", "github.com", "", 0), existingGitHub, []client.Object{existingGitHub}, true, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := fake.NewClientBuilder().WithScheme(NewScheme()).WithObjects(tc.existing...).Build()
			v := &ConnectionValidator{Client: c, Decoder: admission.NewDecoder(NewScheme())}
			resp := v.Handle(context.Background(), connectionRequest(t, tc.conn, tc.old))
			if resp.Allowed != tc.wantAllowed {
				t.Fatalf("allowed = %v, want %v (%s)", resp.Allowed, tc.wantAllowed, resp.Result.Message)
			}
			if tc.wantNamed != "" && !strings.Contains(resp.Result.Message, `existing connection "`+tc.wantNamed+`"`) {
				t.Errorf("message %q does not cite %q", resp.Result.Message, tc.wantNamed)
			}
			if tc.conn.GenerateName != "" && !strings.Contains(resp.Result.Message, `"github-"`) {
				t.Errorf("message %q does not cite the generateName prefix", resp.Result.Message)
			}
		})
	}
}

func TestConnectionAggregator_KeepsEarlierOfConflictingConnections(t *testing.T) {
	objs := []client.Object{
		testConnection("b-later-dup", "github_app", "github.com", "contoso", 5),
		testConnection("contoso-github", "github_app", "github.com", "contoso", 0),
		testConnection("fabrikam-github", "github_app", "github.com", "fabrikam", 3),
		testConnection("a-same-time", "kubernetes", "k8s.example:443", "", 1),
		testConnection("b-same-time", "kubernetes", "k8s.example:443", "", 1),
		testConnection("staging-no-secret", "kubernetes", "staging.example:443", "", 2),
	}
	for _, o := range objs {
		if o.GetName() == "staging-no-secret" {
			continue
		}
		objs = append(objs, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: o.GetName(), Namespace: connTestNS},
			Data:       map[string][]byte{"key": []byte("material")},
		})
	}
	c := fake.NewClientBuilder().WithScheme(NewScheme()).WithObjects(objs...).Build()
	a := NewConnectionAggregator(c, logging.NewLoggerWithOutput(io.Discard))

	entries, _, err := a.collect(context.Background(), []OrbitTenant{{ObjectMeta: metav1.ObjectMeta{Name: "acme"}}})
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name)
	}
	if got, want := strings.Join(names, ","), "a-same-time,contoso-github,fabrikam-github"; got != want {
		t.Errorf("rendered %s, want %s", got, want)
	}

	// The earlier connection still wins while its Secret is missing.
	later := testConnection("staging-later", "kubernetes", "staging.example:443", "", 9)
	laterSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: later.Name, Namespace: connTestNS},
		Data:       map[string][]byte{"key": []byte("material")},
	}
	for _, o := range []client.Object{later, laterSecret} {
		if err := c.Create(context.Background(), o); err != nil {
			t.Fatal(err)
		}
	}
	entries, _, err = a.collect(context.Background(), []OrbitTenant{{ObjectMeta: metav1.ObjectMeta{Name: "acme"}}})
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	names = names[:0]
	for _, e := range entries {
		names = append(names, e.Name)
	}
	if got, want := strings.Join(names, ","), "a-same-time,contoso-github,fabrikam-github"; got != want {
		t.Errorf("rendered %s, want %s", got, want)
	}
}
