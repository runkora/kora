package config_test

import (
	"slices"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

// sidecar is the part of an Istio Sidecar the chart sets.
type sidecar struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		WorkloadSelector *struct {
			Labels map[string]string `json:"labels"`
		} `json:"workloadSelector"`
		Egress []struct {
			Hosts []string `json:"hosts"`
		} `json:"egress"`
	} `json:"spec"`
}

// baseSystemHosts are the namespace default's hosts without a collector.
var baseSystemHosts = []string{
	"./*", "istio-system/*",
	"default/kubernetes.default.svc.cluster.local", "kube-system/kube-dns.kube-system.svc.cluster.local",
}

// renderSidecars renders the chart and returns its Sidecars by name.
func renderSidecars(t *testing.T, values string) map[string]sidecar {
	t.Helper()
	out := renderChart(t, values)
	got := map[string]sidecar{}
	for doc := range strings.SplitSeq(out, "\n---\n") {
		if !strings.Contains(doc, "kind: Sidecar\n") {
			continue
		}
		var s sidecar
		if err := yaml.Unmarshal([]byte(doc), &s); err != nil {
			t.Fatal(err)
		}
		got[s.Metadata.Name] = s
	}
	return got
}

// The namespace default keeps tenant Services out of kora-system proxies. kora-agents and the tool-gateway call
// into tenant namespaces and keep every host.
func TestChartSystemSidecars(t *testing.T) {
	got := renderSidecars(t, "{}\n")
	if len(got) != 3 {
		t.Fatalf("Sidecars = %v, want default, kora-agents and kora-tool-gateway", got)
	}
	def := got["default"]
	if def.Metadata.Namespace != "kora-system" || def.Spec.WorkloadSelector != nil {
		t.Errorf("default = %+v, want a namespace default in kora-system", def)
	}
	if hosts := def.Spec.Egress[0].Hosts; !slices.Equal(hosts, baseSystemHosts) {
		t.Errorf("default hosts = %v, want %v", hosts, baseSystemHosts)
	}
	for _, app := range []string{"kora-agents", "kora-tool-gateway"} {
		s := got[app]
		if s.Spec.WorkloadSelector == nil || s.Spec.WorkloadSelector.Labels["app"] != app {
			t.Errorf("%s selector = %+v, want app=%s", app, s.Spec.WorkloadSelector, app)
		}
		if hosts := s.Spec.Egress[0].Hosts; !slices.Equal(hosts, []string{"*/*"}) {
			t.Errorf("%s hosts = %v, want [*/*]", app, hosts)
		}
	}
}

// The namespace default adds the collector: its namespace when it runs in the cluster, or its host otherwise. An
// endpoint with no usable host adds nothing.
func TestChartSystemSidecarCollector(t *testing.T) {
	for _, tc := range []struct{ endpoint, namespace, want string }{
		{"http://lgtm.observability.svc:4317", "observability", "observability/*"},
		{"https://otel.example.com:4317", "", "*/otel.example.com"},
		{"http://otel-collector.observability.svc:4317", "", "*/otel-collector.observability.svc.cluster.local"},
		{"otel.example.com:4317", "", ""},
		{"http://[fd00::1]:4317", "", ""},
	} {
		values := "global:\n  observability:\n    otlpEndpoint: \"" + tc.endpoint + "\"\n    collectorNamespace: \"" + tc.namespace + "\"\n"
		want := slices.Clone(baseSystemHosts)
		if tc.want != "" {
			want = append(want, tc.want)
		}
		if hosts := renderSidecars(t, values)["default"].Spec.Egress[0].Hosts; !slices.Equal(hosts, want) {
			t.Errorf("%s: hosts = %v, want %v", tc.endpoint, hosts, want)
		}
	}
}
