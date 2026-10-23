package operator

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func mcpWorkloadSrv(name, namespace, endpoint string, selector map[string]string) MCPServer {
	spec := MCPServerSpec{Endpoint: endpoint}
	if selector != nil {
		spec.Workload = &MCPServerWorkload{Selector: selector}
	}
	return MCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       spec,
	}
}

// The CR decides which pods are MCP workloads. A selector that does not match,
// or no selector at all, classifies nothing.
func TestMCPServer_Classifies(t *testing.T) {
	cases := map[string]struct {
		selector  map[string]string
		podLabels map[string]string
		want      bool
	}{
		"exact match":            {map[string]string{"app": "gh"}, map[string]string{"app": "gh"}, true},
		"pod carries extra":      {map[string]string{"app": "gh"}, map[string]string{"app": "gh", "x": "y"}, true},
		"value differs":          {map[string]string{"app": "gh"}, map[string]string{"app": "other"}, false},
		"key absent":             {map[string]string{"app": "gh"}, map[string]string{"other": "gh"}, false},
		"selector is a superset": {map[string]string{"app": "gh", "tier": "mcp"}, map[string]string{"app": "gh"}, false},
		"no workload block":      {nil, map[string]string{"app": "gh"}, false},
		"no pod labels":          {map[string]string{"app": "gh"}, nil, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			srv := mcpWorkloadSrv("gh", "kora-tenant-acme", "http://gh:8000/mcp", c.selector)
			if got := srv.classifies(c.podLabels); got != c.want {
				t.Errorf("classifies = %v, want %v", got, c.want)
			}
		})
	}
}

func TestMCPServer_EndpointPort(t *testing.T) {
	cases := map[string]struct {
		endpoint string
		want     int32
		wantErr  bool
	}{
		"explicit port":     {"http://gh.acme.svc.cluster.local:8000/mcp", 8000, false},
		"http default":      {"http://gh.acme.svc/mcp", 80, false},
		"https default":     {"https://gh.acme.svc/mcp", 443, false},
		"unknown scheme":    {"tcp://gh.acme.svc/mcp", 0, true},
		"port out of range": {"http://gh.acme.svc:70000/mcp", 0, true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			srv := mcpWorkloadSrv("gh", "kora-tenant-acme", c.endpoint, map[string]string{"app": "gh"})
			got, err := srv.endpointPort()
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %q", c.endpoint)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Errorf("endpointPort = %d, want %d", got, c.want)
			}
		})
	}
}

// The ingress policy admits the tool-gateway in the system namespace and
// nothing else, on the endpoint's port.
func TestMCPServer_IngressPolicy(t *testing.T) {
	srv := mcpWorkloadSrv("gh", "kora-tenant-acme", "http://gh:8000/mcp", map[string]string{"app": "gh"})

	policy := srv.ingressPolicy(8000)

	if policy.Namespace != "kora-tenant-acme" {
		t.Errorf("namespace: got %q", policy.Namespace)
	}
	if policy.Spec.PodSelector.MatchLabels["app"] != "gh" {
		t.Errorf("podSelector: got %v", policy.Spec.PodSelector.MatchLabels)
	}
	if len(policy.Spec.PolicyTypes) != 1 || policy.Spec.PolicyTypes[0] != "Ingress" {
		t.Errorf("policyTypes: got %v, want Ingress only", policy.Spec.PolicyTypes)
	}
	if len(policy.Spec.Ingress) != 1 || len(policy.Spec.Ingress[0].From) != 1 {
		t.Fatalf("expected exactly one source, got %+v", policy.Spec.Ingress)
	}
	from := policy.Spec.Ingress[0].From[0]
	if from.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] != systemNamespace {
		t.Errorf("source namespace: got %v", from.NamespaceSelector.MatchLabels)
	}
	if from.PodSelector.MatchLabels["app"] != mcpToolGatewayApp {
		t.Errorf("source pod: got %v", from.PodSelector.MatchLabels)
	}
	if got := policy.Spec.Ingress[0].Ports[0].Port.IntValue(); got != 8000 {
		t.Errorf("port: got %d", got)
	}
}

// Istio prepends spiffe://, so the principal is written without it.
func TestMCPServer_AuthorizationPolicy(t *testing.T) {
	srv := mcpWorkloadSrv("gh", "kora-tenant-acme", "http://gh:8000/mcp", map[string]string{"app": "gh"})

	original := trustDomain
	t.Cleanup(func() { trustDomain = original })
	trustDomain = "example.com"

	policy := srv.authorizationPolicy(8000)
	if policy == nil {
		t.Fatal("expected a policy")
	}
	if policy.GetKind() != "AuthorizationPolicy" || policy.GetNamespace() != "kora-tenant-acme" {
		t.Errorf("got %s in %q", policy.GetKind(), policy.GetNamespace())
	}
	rules, found, err := unstructuredSlice(policy.Object, "spec", "rules")
	if err != nil || !found {
		t.Fatalf("rules: found=%v err=%v", found, err)
	}
	rule, _ := rules[0].(map[string]any)
	from, _ := rule["from"].([]any)
	source, _ := from[0].(map[string]any)
	principals, _ := source["source"].(map[string]any)["principals"].([]any)
	if want := "example.com/system/tool-gateway"; principals[0] != want {
		t.Errorf("principal: got %v, want %q without the spiffe:// prefix", principals[0], want)
	}
}

// A DestinationRule matches on the FQDN, so a short in-cluster endpoint has to
// be expanded or the rule never applies to the call the gateway actually makes.
func TestMCPServer_EndpointHost(t *testing.T) {
	cases := map[string]string{
		"http://gh.acme.svc:8000/mcp":               "gh.acme.svc.cluster.local",
		"http://gh.acme.svc.cluster.local:8000/mcp": "gh.acme.svc.cluster.local",
		"https://mcp.example.com/mcp":               "mcp.example.com",
	}
	for endpoint, want := range cases {
		t.Run(endpoint, func(t *testing.T) {
			srv := mcpWorkloadSrv("gh", "acme", endpoint, map[string]string{"app": "gh"})
			got, err := srv.endpointHost()
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Errorf("endpointHost = %q, want %q", got, want)
			}
		})
	}
}

// Istio derives the expected SAN from the destination ServiceAccount, which
// never matches a SPIRE-issued identity, so the rule names it explicitly.
func TestMCPServer_DestinationRule(t *testing.T) {
	srv := mcpWorkloadSrv("github", "kora-tenant-acme", "http://github-mcp.kora-tenant-acme.svc:8000/mcp", map[string]string{"app": "gh"})

	original := trustDomain
	t.Cleanup(func() { trustDomain = original })
	trustDomain = "example.com"

	rule := srv.destinationRule(8000, "github-mcp.kora-tenant-acme.svc.cluster.local")

	if rule.GetKind() != "DestinationRule" || rule.GetNamespace() != "kora-tenant-acme" {
		t.Errorf("got %s in %q", rule.GetKind(), rule.GetNamespace())
	}
	spec, _ := rule.Object["spec"].(map[string]any)
	if spec["host"] != "github-mcp.kora-tenant-acme.svc.cluster.local" {
		t.Errorf("host: got %v", spec["host"])
	}
	// The caller is in the system namespace, so a rule in the server's own
	// namespace only reaches it when exported.
	exportTo, _ := spec["exportTo"].([]any)
	if len(exportTo) != 1 || exportTo[0] != "*" {
		t.Errorf("exportTo: got %v, want [*]", exportTo)
	}
	settings, _ := spec["trafficPolicy"].(map[string]any)["portLevelSettings"].([]any)
	first, _ := settings[0].(map[string]any)
	if got := first["port"].(map[string]any)["number"]; got != int64(8000) {
		t.Errorf("port: got %v", got)
	}
	tls, _ := first["tls"].(map[string]any)
	if tls["mode"] != "ISTIO_MUTUAL" {
		t.Errorf("tls mode: got %v", tls["mode"])
	}
	sans, _ := tls["subjectAltNames"].([]any)
	if want := "spiffe://example.com/tenant/kora-tenant-acme/service/github"; sans[0] != want {
		t.Errorf("SAN: got %v, want %q", sans[0], want)
	}
}

// A workload block with no selector would classify every pod in the namespace.
func TestValidateMCPServerSpec_WorkloadSelectorRequired(t *testing.T) {
	spec := MCPServerSpec{Endpoint: "http://gh:8000/mcp", Workload: &MCPServerWorkload{}}
	if err := validateMCPServerSpec(spec, "gh", "kora-tenant-acme"); err != ErrMCPServerNoWorkloadSelector {
		t.Errorf("got %v, want ErrMCPServerNoWorkloadSelector", err)
	}
}

// The name goes into a label value on the generated policies, so a name the API
// server would accept on the CR but reject on a label is caught up front.
func TestValidateMCPServerSpec_NameBoundedByLabelValue(t *testing.T) {
	spec := MCPServerSpec{Endpoint: "http://gh:8000/mcp"}

	if err := validateMCPServerSpec(spec, strings.Repeat("g", 63), "kora-tenant-acme"); err != nil {
		t.Errorf("63 characters should be accepted, got %v", err)
	}
	if err := validateMCPServerSpec(spec, strings.Repeat("g", 64), "kora-tenant-acme"); err != ErrMCPServerNameTooLong {
		t.Errorf("got %v, want ErrMCPServerNameTooLong", err)
	}
}

// The spec carries a map, so a shallow copy would alias it between CRs.
func TestMCPServer_DeepCopyIsolatesTheSelector(t *testing.T) {
	srv := mcpWorkloadSrv("gh", "kora-tenant-acme", "http://gh:8000/mcp", map[string]string{"app": "gh"})

	copied := srv.DeepCopy()
	copied.Spec.Workload.Selector["app"] = "changed"

	if srv.Spec.Workload.Selector["app"] != "gh" {
		t.Error("mutating the copy changed the original")
	}
}

func unstructuredSlice(obj map[string]any, path ...string) ([]any, bool, error) {
	cur := any(obj)
	for _, key := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false, nil
		}
		cur, ok = m[key]
		if !ok {
			return nil, false, nil
		}
	}
	out, ok := cur.([]any)
	return out, ok, nil
}

var _ = corev1.Pod{}

// An MCP pod gets the mesh identity wiring and none of the agent enforcement:
// identity-proxy would overwrite the credential it authenticates upstream with.
func TestMutateMCPServer_SkipsAgentEnforcement(t *testing.T) {
	m := testMutator()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "github-mcp-1", Namespace: "kora-tenant-acme",
			Labels: map[string]string{"app": "gh"},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "server", Image: "gh"}}},
	}

	if err := m.mutate(pod, "", "github"); err != nil {
		t.Fatal(err)
	}

	if pod.Labels[podLabelMCPServer] != "github" {
		t.Errorf("%s = %q", podLabelMCPServer, pod.Labels[podLabelMCPServer])
	}
	if pod.Labels[podLabelSpiffeService] != "github" {
		t.Errorf("%s = %q, want the SPIRE registration label", podLabelSpiffeService, pod.Labels[podLabelSpiffeService])
	}
	if pod.Labels[podLabelIdentityProxy] != "" || pod.Labels[podLabelOPAPolicy] != "" {
		t.Errorf("enforcement labels should be absent: %v", pod.Labels)
	}
	for _, c := range pod.Spec.Containers {
		if c.Name == containerIdentityProxy || c.Name == containerOPA {
			t.Errorf("container %q was injected", c.Name)
		}
	}
	if !strings.Contains(pod.Annotations[podAnnotationProxyConfig], "spire") {
		t.Errorf("proxy config should point the sidecar at SPIRE: %q", pod.Annotations[podAnnotationProxyConfig])
	}
	var hasSocket bool
	for _, v := range pod.Spec.Volumes {
		if v.Name == volumeSpireSocket {
			hasSocket = true
		}
		if v.Name == volumeOPAConfig || v.Name == volumeIdentityProxy {
			t.Errorf("volume %q was injected", v.Name)
		}
	}
	if !hasSocket {
		t.Error("the SPIFFE socket volume is missing, so the sidecar has no certificate")
	}
}

// A pod labelled as an MCP server with no CR selecting it is enforced as an
// agent pod: the label alone grants nothing.
func TestValidator_MarkerLabelOnlyExemptsWithTheContainersAbsent(t *testing.T) {
	v := &PodValidator{}
	ns := tenantNS(map[string]string{"istio-injection": "enabled"})

	mcp := injectedPod()
	mcp.Labels = map[string]string{podLabelMCPServer: "github"}
	mcp.Spec.Containers = []corev1.Container{{Name: "server"}}
	if reason := v.rejectionReason(mcp, ns); reason != "" {
		t.Errorf("an MCP pod should be admitted without kora's containers, got %q", reason)
	}

	forged := injectedPod()
	forged.Spec.Containers = []corev1.Container{{Name: "server"}}
	if reason := v.rejectionReason(forged, ns); reason == "" {
		t.Error("a pod without the marker must still require identity-proxy and OPA")
	}

	// Still denied for an MCP pod: the sidecar carries the identity the
	// generated AuthorizationPolicy matches on.
	optOut := injectedPod()
	optOut.Labels = map[string]string{podLabelMCPServer: "github"}
	optOut.Annotations["sidecar.istio.io/inject"] = "false"
	if reason := v.rejectionReason(optOut, ns); reason == "" {
		t.Error("opting out of injection must be denied even for an MCP pod")
	}
}
