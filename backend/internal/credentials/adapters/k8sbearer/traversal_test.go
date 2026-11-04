package k8sbearer_test

import (
	"net/http"
	"testing"

	"github.com/runkora/kora/backend/internal/credentials/adapters/k8sbearer"
)

// TestClassify_DotSegmentsResolvedBeforeCategorizing locks the resource
// category against the path the API server will actually serve. Classifying
// the raw path let a workloads-only grant reach Secrets, because the upstream
// resolves the dot segments the classifier had ignored.
func TestClassify_DotSegmentsResolvedBeforeCategorizing(t *testing.T) {
	c := newConnector(t, stubSecrets{})
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
		got, err := c.Classify(req)
		if err != nil {
			t.Fatalf("Classify(%s): %v", path, err)
		}
		if got.Action != want {
			t.Errorf("Classify(%s) = %q, want %q", path, got.Action, want)
		}
	}
}
