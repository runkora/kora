package connections

import "testing"

func TestConflict(t *testing.T) {
	gh := func(owner string) Identity {
		return Identity{Adapter: AdapterGitHubApp, BaseHost: "github.com", Owner: owner}
	}
	kube := func(host string) Identity { return Identity{Adapter: AdapterKubernetesBearer, BaseHost: host} }
	tests := []struct {
		name string
		a, b Identity
		want bool
	}{
		{"github same owner", gh("fabrikam"), gh("Fabrikam "), true},
		{"github missing owner on one side", gh("fabrikam"), gh(""), true},
		{"github missing owner on both sides", gh(""), gh(""), true},
		{"github distinct owners", gh("fabrikam"), gh("contoso"), false},
		{"github enterprise hosts differ", Identity{Adapter: AdapterGitHubApp, BaseHost: "ghe.a.example"}, Identity{Adapter: AdapterGitHubApp, BaseHost: "ghe.b.example"}, false},
		{"github enterprise same host no owners", Identity{Adapter: AdapterGitHubApp, BaseHost: "ghe.a.example"}, Identity{Adapter: AdapterGitHubApp, BaseHost: "GHE.a.example"}, true},
		{"kubernetes same host", kube("k8s.example:443"), kube("K8S.example"), true},
		{"kubernetes distinct hosts", kube("a.example:443"), kube("b.example:443"), false},
		{"different adapters on different hosts", gh("fabrikam"), kube("k8s.example:443"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Conflict(tc.a, tc.b); got != tc.want {
				t.Errorf("Conflict = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestConfigOwner(t *testing.T) {
	for raw, want := range map[string]string{`{"owner":"fabrikam","app_id":"1"}`: "fabrikam", `{"app_id":"1"}`: "", `not json`: "", ``: ""} {
		if got := ConfigOwner([]byte(raw)); got != want {
			t.Errorf("ConfigOwner(%q) = %q, want %q", raw, got, want)
		}
	}
}
