package connections

import (
	"errors"
	"slices"
	"testing"
)

// TestActionNames locks the grantable action vocabulary. These names are
// published to the policy service and offered in the connector picker, so a
// change here must be a conscious vocabulary change, not an accident.
func TestActionNames(t *testing.T) {
	cases := map[string][]string{
		AdapterGitHubApp:   {"repo:read", "repo:write"},
		AdapterPassthrough: {"use"},
		AdapterKubernetesBearer: {
			"workloads:read", "workloads:write",
			"config:read", "config:write",
			"secrets:read", "secrets:write",
			"networking:read", "networking:write",
			"storage:read", "storage:write",
			"rbac:read", "rbac:write",
			"cluster:read", "cluster:write",
		},
		"unknown_adapter": {},
	}
	for adapter, want := range cases {
		got := ActionNames(adapter)
		if !slices.Equal(got, want) {
			t.Errorf("ActionNames(%q) = %v, want %v", adapter, got, want)
		}
	}
}

// TestActions_NonNil guards the JSON contract: an unknown adapter yields an
// empty, non-nil slice so it serializes as [] rather than null.
func TestActions_NonNil(t *testing.T) {
	if got := Actions("unknown_adapter"); got == nil {
		t.Fatal("Actions(unknown) returned nil; want non-nil empty slice")
	}
	if got := ActionNames("unknown_adapter"); got == nil {
		t.Fatal("ActionNames(unknown) returned nil; want non-nil empty slice")
	}
}

// TestEgressHosts_GitHub locks the host family a GitHub connector auto-allows.
func TestEgressHosts_GitHub(t *testing.T) {
	saas := []string{"github.com", "api.github.com", "codeload.github.com", "*.githubusercontent.com"}

	if got := EgressHosts(AdapterGitHubApp, "github.com"); !slices.Equal(got, saas) {
		t.Errorf("EgressHosts(github_app, github.com) = %v, want %v", got, saas)
	}
	// An empty base host defaults to the SaaS family.
	if got := EgressHosts(AdapterGitHubApp, ""); !slices.Equal(got, saas) {
		t.Errorf("EgressHosts(github_app, \"\") = %v, want %v", got, saas)
	}
	// GitHub Enterprise: the appliance serves git + API + uploads on one host.
	if got := EgressHosts(AdapterGitHubApp, "github.acme.com"); !slices.Equal(got, []string{"github.acme.com"}) {
		t.Errorf("EgressHosts(github_app, github.acme.com) = %v, want [github.acme.com]", got)
	}
}

// TestEgressHosts_Passthrough returns just the declared host, or nil when none.
func TestEgressHosts_Passthrough(t *testing.T) {
	if got := EgressHosts(AdapterPassthrough, "example.com"); !slices.Equal(got, []string{"example.com"}) {
		t.Errorf("EgressHosts(passthrough, example.com) = %v, want [example.com]", got)
	}
	if got := EgressHosts(AdapterPassthrough, ""); got != nil {
		t.Errorf("EgressHosts(passthrough, \"\") = %v, want nil", got)
	}
	if got := EgressHosts("unknown_adapter", "example.com"); got != nil {
		t.Errorf("EgressHosts(unknown, …) = %v, want nil", got)
	}
}

// TestEgressHosts_KubernetesBearer locks that a cluster connection serves
// exactly its own declared host — no host family the way GitHub's does.
func TestEgressHosts_KubernetesBearer(t *testing.T) {
	want := []string{"k8s.acme.internal:6443"}
	if got := EgressHosts(AdapterKubernetesBearer, "k8s.acme.internal:6443"); !slices.Equal(got, want) {
		t.Errorf("EgressHosts(kubernetes, k8s.acme.internal:6443) = %v, want %v", got, want)
	}
	if got := EgressHosts(AdapterKubernetesBearer, ""); got != nil {
		t.Errorf("EgressHosts(kubernetes, \"\") = %v, want nil", got)
	}
}

// TestValidateBaseHost_KubernetesBearer locks that kubernetes rejects a bare
// IP literal base host (Blocker 1: no TLS SNI for an IP-literal destination,
// so the sandbox's egress interception can never authorize it) but accepts
// any DNS hostname, with or without a port.
func TestValidateBaseHost_KubernetesBearer(t *testing.T) {
	rejected := []string{
		"34.147.135.56",
		"34.147.135.56:6443",
		"https://34.147.135.56",
		"2001:db8::1",
		"[2001:db8::1]:6443",
	}
	for _, host := range rejected {
		if err := ValidateBaseHost(AdapterKubernetesBearer, host); !errors.Is(err, ErrBaseHostMustBeHostname) {
			t.Errorf("ValidateBaseHost(kubernetes, %q) = %v, want ErrBaseHostMustBeHostname", host, err)
		}
	}

	accepted := []string{
		"",
		"k8s.acme.internal",
		"k8s.acme.internal:6443",
		"gke-c4c0472d49e6471d92316a4b9be133063d35-27365390763.europe-west2-a.gke.goog",
	}
	for _, host := range accepted {
		if err := ValidateBaseHost(AdapterKubernetesBearer, host); err != nil {
			t.Errorf("ValidateBaseHost(kubernetes, %q) = %v, want nil", host, err)
		}
	}
}

// TestValidateBaseHost_NoRequirement locks that adapters with no hostname
// requirement (and unknown adapters) never reject an IP literal.
func TestValidateBaseHost_NoRequirement(t *testing.T) {
	for _, adapter := range []string{AdapterGitHubApp, AdapterPassthrough, "unknown_adapter"} {
		if err := ValidateBaseHost(adapter, "34.147.135.56"); err != nil {
			t.Errorf("ValidateBaseHost(%q, IP) = %v, want nil", adapter, err)
		}
	}
}
