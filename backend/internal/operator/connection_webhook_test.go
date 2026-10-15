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

func connectionRequest(t *testing.T, op admissionv1.Operation, conn *Connection) admission.Request {
	t.Helper()
	raw, err := json.Marshal(conn)
	if err != nil {
		t.Fatal(err)
	}
	return admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Operation: op, Namespace: connTestNS, Object: runtime.RawExtension{Raw: raw},
	}}
}

func TestConnectionValidator(t *testing.T) {
	existingGitHub := testConnection("clear-route-github", "github_app", "github.com", "clear-route", 0)
	existingKube := testConnection("prod-cluster", "kubernetes", "k8s.example:443", "", 0)

	tests := []struct {
		name        string
		op          admissionv1.Operation
		conn        *Connection
		existing    []client.Object
		wantAllowed bool
		wantNamed   string
	}{
		{"github same owner rejected", admissionv1.Create, testConnection("dup", "github_app", "github.com", "Clear-Route", 5), []client.Object{existingGitHub}, false, "clear-route-github"},
		{"github missing owner rejected", admissionv1.Create, testConnection("no-owner", "github_app", "github.com", "", 5), []client.Object{existingGitHub}, false, "clear-route-github"},
		{"github distinct owner allowed", admissionv1.Create, testConnection("runkora-github", "github_app", "github.com", "runkora", 5), []client.Object{existingGitHub}, true, ""},
		{"kubernetes same host rejected", admissionv1.Create, testConnection("prod-cluster-2", "kubernetes", "K8S.example:443", "", 5), []client.Object{existingKube}, false, "prod-cluster"},
		{"kubernetes distinct host allowed", admissionv1.Create, testConnection("staging-cluster", "kubernetes", "staging.example:443", "", 5), []client.Object{existingKube}, true, ""},
		{"update of the earlier connection allowed", admissionv1.Update, testConnection("clear-route-github", "github_app", "github.com", "clear-route", 0), []client.Object{existingGitHub, testConnection("later-dup", "github_app", "github.com", "clear-route", 5)}, true, ""},
		{"update of the later connection rejected", admissionv1.Update, testConnection("later-dup", "github_app", "github.com", "clear-route", 5), []client.Object{existingGitHub, testConnection("later-dup", "github_app", "github.com", "clear-route", 5)}, false, "clear-route-github"},
		{"update compared with itself allowed", admissionv1.Update, testConnection("clear-route-github", "github_app", "github.com", "", 0), []client.Object{existingGitHub}, true, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := fake.NewClientBuilder().WithScheme(NewScheme()).WithObjects(tc.existing...).Build()
			v := &ConnectionValidator{Client: c, Decoder: admission.NewDecoder(NewScheme())}
			resp := v.Handle(context.Background(), connectionRequest(t, tc.op, tc.conn))
			if resp.Allowed != tc.wantAllowed {
				t.Fatalf("allowed = %v, want %v (%s)", resp.Allowed, tc.wantAllowed, resp.Result.Message)
			}
			if tc.wantNamed != "" && !strings.Contains(resp.Result.Message, `"`+tc.wantNamed+`"`) {
				t.Errorf("message %q does not cite %q", resp.Result.Message, tc.wantNamed)
			}
		})
	}
}

func TestConnectionAggregator_KeepsEarlierOfConflictingConnections(t *testing.T) {
	objs := []client.Object{
		testConnection("b-later-dup", "github_app", "github.com", "clear-route", 5),
		testConnection("clear-route-github", "github_app", "github.com", "clear-route", 0),
		testConnection("runkora-github", "github_app", "github.com", "runkora", 3),
		testConnection("a-same-time", "kubernetes", "k8s.example:443", "", 1),
		testConnection("b-same-time", "kubernetes", "k8s.example:443", "", 1),
	}
	for _, o := range objs {
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
	if got, want := strings.Join(names, ","), "a-same-time,clear-route-github,runkora-github"; got != want {
		t.Errorf("rendered %s, want %s", got, want)
	}
}
