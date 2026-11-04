package config_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/yaml"
)

// TestChartMetricsNetworkPolicies_ScopesOperatorSeparately renders the kora
// chart with metrics scraping enabled and asserts the two Prometheus
// scrape-ingress NetworkPolicies carry the ports and selectors they're meant
// to: the generic rule opens only the shared metrics port on every pod, and
// the operator's own rule opens its fixed controller-runtime port but only on
// the operator's own pods. Port 8080 is also identity's and kora-agents'
// real API port, so a regression that puts 8080 back onto the podSelector:{}
// rule would open their application traffic to the scraper namespace — this
// is exactly the mistake the two-rule split exists to prevent, and the one a
// prior version of this policy made. Needs helm and the chart's dependencies
// (make chart-deps).
func TestChartMetricsNetworkPolicies_ScopesOperatorSeparately(t *testing.T) {
	chart := filepath.Join("..", "..", "..", "..", "charts", "kora")
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm not installed")
	}
	if deps, _ := filepath.Glob(filepath.Join(chart, "charts", "*.tgz")); len(deps) == 0 {
		t.Skip("chart dependencies not built")
	}

	overrides := filepath.Join(t.TempDir(), "metrics-netpol.yaml")
	overridesYAML := "global:\n" +
		"  networkPolicy:\n" +
		"    enabled: true\n" +
		"  metrics:\n" +
		"    enabled: true\n" +
		"    port: 9464\n" +
		"    scraperNamespace: monitoring\n"
	if err := os.WriteFile(overrides, []byte(overridesYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	args := []string{
		"template", "kora", chart, "-n", "kora-system",
		"-f", filepath.Join(chart, "ci", "default-values.yaml"),
		"-f", overrides,
		"-s", "templates/platform/netpols.yaml",
	}
	out, err := exec.Command("helm", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, out)
	}

	policies := map[string]networkingv1.NetworkPolicy{}
	for doc := range strings.SplitSeq(string(out), "\n---\n") {
		if strings.TrimSpace(doc) == "" {
			continue
		}
		var np networkingv1.NetworkPolicy
		if err := yaml.Unmarshal([]byte(doc), &np); err != nil {
			t.Fatalf("parse rendered NetworkPolicy: %v\n%s", err, doc)
		}
		if np.Kind == "NetworkPolicy" {
			policies[np.Name] = np
		}
	}

	generic, ok := policies["allow-prometheus-scrape-ingress"]
	if !ok {
		t.Fatal("allow-prometheus-scrape-ingress not rendered")
	}
	if len(generic.Spec.PodSelector.MatchLabels) != 0 || len(generic.Spec.PodSelector.MatchExpressions) != 0 {
		t.Errorf("allow-prometheus-scrape-ingress podSelector = %+v, want empty (every pod)", generic.Spec.PodSelector)
	}
	if ports := collectPorts(t, generic); !equalPortSets(ports, []string{"9464"}) {
		t.Errorf("allow-prometheus-scrape-ingress ports = %v, want [9464]", ports)
	}

	operator, ok := policies["allow-prometheus-scrape-ingress-operator"]
	if !ok {
		t.Fatal("allow-prometheus-scrape-ingress-operator not rendered")
	}
	if got := operator.Spec.PodSelector.MatchLabels["app"]; got != "kora-operator" {
		t.Errorf("allow-prometheus-scrape-ingress-operator podSelector app label = %q, want kora-operator", got)
	}
	if ports := collectPorts(t, operator); !equalPortSets(ports, []string{"8080"}) {
		t.Errorf("allow-prometheus-scrape-ingress-operator ports = %v, want [8080]", ports)
	}
}

// collectPorts flattens every port named across a NetworkPolicy's ingress
// rules to strings, for an order-independent comparison.
func collectPorts(t *testing.T, np networkingv1.NetworkPolicy) []string {
	t.Helper()
	var ports []string
	for _, rule := range np.Spec.Ingress {
		for _, p := range rule.Ports {
			if p.Port != nil {
				ports = append(ports, p.Port.String())
			}
		}
	}
	return ports
}

func equalPortSets(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := map[string]bool{}
	for _, p := range got {
		seen[p] = true
	}
	for _, p := range want {
		if !seen[p] {
			return false
		}
	}
	return true
}
