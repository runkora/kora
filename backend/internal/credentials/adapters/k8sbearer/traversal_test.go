package k8sbearer_test

import (
	"net/http"
	"testing"

	"github.com/clear-route/orbit/internal/credentials"
	"github.com/clear-route/orbit/internal/credentials/adapters/k8sbearer"
)

// TestClassifyAction_DotSegmentsResolvedBeforeCategorizing locks the resource
// category against the path the API server will actually serve. Classifying
// the raw path let a workloads-only grant reach Secrets, because the upstream
// resolves the dot segments the classifier had ignored.
func TestClassifyAction_DotSegmentsResolvedBeforeCategorizing(t *testing.T) {
	a := newAdapter(stubSecrets{})
	cases := map[string]string{
		"/api/v1/namespaces/default/pods/../../../secrets":     k8sbearer.ActionSecretsRead,
		"/api/v1/namespaces/default/pods/../secrets":           k8sbearer.ActionSecretsRead,
		"/api/v1/namespaces/default/configmaps/../../../nodes": k8sbearer.ActionClusterRead,
		"/api/v1/namespaces/default/pods":                      k8sbearer.ActionWorkloadsRead,
	}
	for path, want := range cases {
		req, err := http.NewRequest(http.MethodGet, "https://k8s.example.com"+path, nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		got, err := a.ClassifyAction(req, credentials.Connection{})
		if err != nil {
			t.Fatalf("ClassifyAction(%s): %v", path, err)
		}
		if got.Action != want {
			t.Errorf("ClassifyAction(%s) = %q, want %q", path, got.Action, want)
		}
	}
}
