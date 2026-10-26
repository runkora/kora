package config_test

import (
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

// renderDocs renders templates of the kora chart with the CI values, one
// decoded document per resource.
func renderDocs(t *testing.T, templates ...string) []map[string]any {
	t.Helper()
	var args []string
	for _, tpl := range templates {
		args = append(args, "-s", tpl)
	}
	var docs []map[string]any
	for doc := range strings.SplitSeq(renderChart(t, "", args...), "\n---\n") {
		var m map[string]any
		if err := yaml.Unmarshal([]byte(doc), &m); err != nil {
			t.Fatalf("parse rendered document: %v\n%s", err, doc)
		}
		if m != nil {
			docs = append(docs, m)
		}
	}
	return docs
}

func field(m any, path ...string) any {
	for _, p := range path {
		mm, ok := m.(map[string]any)
		if !ok {
			return nil
		}
		m = mm[p]
	}
	return m
}

// firstRoute returns the first api VirtualService rule that matches path,
// as Istio evaluates them: in order, a rule without match matching all.
func firstRoute(t *testing.T, vs map[string]any, path string) map[string]any {
	t.Helper()
	rules, _ := field(vs, "spec", "http").([]any)
	for _, r := range rules {
		rule := r.(map[string]any)
		matches, _ := rule["match"].([]any)
		if len(matches) == 0 {
			return rule
		}
		for _, m := range matches {
			uri, _ := field(m, "uri").(map[string]any)
			if p, ok := uri["prefix"].(string); ok && strings.HasPrefix(path, p) {
				return rule
			}
			if e, ok := uri["exact"].(string); ok && path == e {
				return rule
			}
		}
	}
	t.Fatalf("no route matches %s", path)
	return nil
}

// TestDeclaredEndpointsAreNotRoutedFromTheEdge: every service's declared-item
// path, under its base path, gets a 404 at the gateway instead of a route.
func TestDeclaredEndpointsAreNotRoutedFromTheEdge(t *testing.T) {
	docs := renderDocs(t, "templates/api/virtualservice.yaml")
	if len(docs) != 1 {
		t.Fatalf("want one VirtualService, got %d", len(docs))
	}
	for _, path := range []string{
		"/internal/declared/agents",
		"/v1/internal/declared/virtualmodels",
		"/api/v1/internal/declared/triggers/acme/t1",
		"/api/v1/internal/declared/policyroles/acme",
	} {
		rule := firstRoute(t, docs[0], path)
		if field(rule, "directResponse", "status") != float64(404) {
			t.Errorf("%s is routed by %v, want a 404 directResponse", path, rule)
		}
	}
	if rule := firstRoute(t, docs[0], "/v1/chat/completions"); rule["route"] == nil {
		t.Error("inference traffic must still reach the gateway")
	}
}

// TestOperatorJoinsTheMesh checks the operator's sidecar keeps its webhook,
// probe and metrics ports out of the mesh and its non-mesh egress direct, and
// that each service denies its declared-item paths to every other principal.
func TestOperatorJoinsTheMesh(t *testing.T) {
	docs := renderDocs(t, "templates/operator/deployment.yaml", "templates/authorizationpolicy-declared.yaml")
	denied := map[string]string{}
	for _, d := range docs {
		switch d["kind"] {
		case "Deployment":
			ann, _ := field(d, "spec", "template", "metadata", "annotations").(map[string]any)
			if _, off := ann["sidecar.istio.io/inject"]; off {
				t.Error("the operator must not opt out of sidecar injection")
			}
			if ann["traffic.sidecar.istio.io/excludeInboundPorts"] != "9443,8081,8080" {
				t.Errorf("excludeInboundPorts = %v", ann["traffic.sidecar.istio.io/excludeInboundPorts"])
			}
			if ann["traffic.sidecar.istio.io/excludeOutboundPorts"] != "443,6443" {
				t.Errorf("excludeOutboundPorts = %v", ann["traffic.sidecar.istio.io/excludeOutboundPorts"])
			}
		case "AuthorizationPolicy":
			app, _ := field(d, "spec", "selector", "matchLabels", "app").(string)
			rules, _ := field(d, "spec", "rules").([]any)
			paths, _ := field(rules[0], "to").([]any)
			p, _ := field(paths[0], "operation", "paths").([]any)
			not, _ := field(rules[0], "from").([]any)
			principals, _ := field(not[0], "source", "notPrincipals").([]any)
			if field(d, "spec", "action") != "DENY" || len(principals) != 1 || !strings.HasSuffix(principals[0].(string), "/system/operator") {
				t.Errorf("%s policy = %v", app, d["spec"])
			}
			denied[app] = p[0].(string)
		}
	}
	want := map[string]string{
		"kora-agents":            "/internal/declared/*",
		"kora-triggers":          "/api/v1/internal/declared/*",
		"kora-policy-service":    "/api/v1/internal/declared/*",
		"kora-inference-gateway": "/v1/internal/declared/*",
	}
	for app, path := range want {
		if denied[app] != path {
			t.Errorf("%s denies %q, want %q", app, denied[app], path)
		}
	}
}

// TestOperatorAccess checks the operator may read the six kinds and patch
// their status, and that kora-triggers admits it only on the declared paths.
func TestOperatorAccess(t *testing.T) {
	docs := renderDocs(t, "templates/operator/clusterrole.yaml", "templates/triggers/authorizationpolicy.yaml")
	kinds := []string{"virtualmodels", "environments", "agents", "triggersources", "triggers", "policyroles"}
	granted := map[string][]any{}
	operatorRule := false
	for _, d := range docs {
		rules, _ := field(d, "rules").([]any)
		if d["kind"] == "AuthorizationPolicy" {
			rules, _ = field(d, "spec", "rules").([]any)
		}
		for _, r := range rules {
			resources, _ := field(r, "resources").([]any)
			verbs, _ := field(r, "verbs").([]any)
			for _, res := range resources {
				granted[res.(string)] = verbs
			}
			from, _ := field(r, "from").([]any)
			if len(from) == 0 {
				continue
			}
			principals, _ := field(from[0], "source", "principals").([]any)
			to, _ := field(r, "to").([]any)
			if len(principals) == 1 && strings.HasSuffix(principals[0].(string), "/system/operator") && len(to) == 1 {
				paths, _ := field(to[0], "operation", "paths").([]any)
				operatorRule = len(paths) == 1 && paths[0] == "/api/v1/internal/declared/*"
			}
		}
	}
	for _, k := range kinds {
		if v := granted[k]; len(v) != 3 {
			t.Errorf("%s verbs = %v, want get/list/watch", k, v)
		}
		if v := granted[k+"/status"]; len(v) != 2 || v[1] != "patch" {
			t.Errorf("%s/status verbs = %v, want get/patch", k, v)
		}
	}
	if !operatorRule {
		t.Error("kora-triggers has no rule admitting the operator to /api/v1/internal/declared/*")
	}
}
