package operator

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

const (
	skillMarketplaceCRDPath = "../../../charts/kora-crds/generated/runkora.io_skillmarketplaces.yaml"
)

type crdNode = map[string]any

// loadCRDSchema loads a chart CRD's openAPIV3Schema, dropping the Helm label
// lines, which carry no schema. CEL rules are asserted as declared, not run:
// running them needs k8s.io/apiserver and cel-go, which this module does not
// take on. The API server enforces them.
func loadCRDSchema(t *testing.T, path string) crdNode {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var kept []string
	for line := range strings.SplitSeq(string(raw), "\n") {
		if !strings.Contains(line, "{{") {
			kept = append(kept, line)
		}
	}
	var crd crdNode
	if err := yaml.Unmarshal([]byte(strings.Join(kept, "\n")), &crd); err != nil {
		t.Fatal(err)
	}
	versions, _ := crdChild(crd, "spec")["versions"].([]any)
	if len(versions) != 1 {
		t.Fatalf("want one version, got %d", len(versions))
	}
	schema := crdChild(crdChild(versions[0].(crdNode), "schema"), "openAPIV3Schema")
	if schema == nil {
		t.Fatal("version has no openAPIV3Schema")
	}
	return schema
}

func crdChild(n crdNode, key string) crdNode {
	c, _ := n[key].(crdNode)
	return c
}

func crdProp(n crdNode, name string) crdNode { return crdChild(crdChild(n, "properties"), name) }

func crdPattern(t *testing.T, n crdNode) *regexp.Regexp {
	t.Helper()
	p, ok := n["pattern"].(string)
	if !ok {
		t.Fatal("schema has no pattern")
	}
	return regexp.MustCompile(p)
}

func crdRules(n crdNode) []string {
	var out []string
	rules, _ := n["x-kubernetes-validations"].([]any)
	for _, r := range rules {
		if m, ok := r.(crdNode); ok {
			if s, ok := m["rule"].(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

func assertMatches(t *testing.T, re *regexp.Regexp, ok, bad []string) {
	t.Helper()
	for _, s := range ok {
		if !re.MatchString(s) {
			t.Errorf("pattern %s rejects %q", re, s)
		}
	}
	for _, s := range bad {
		if re.MatchString(s) {
			t.Errorf("pattern %s accepts %q", re, s)
		}
	}
}

func assertRuleDeclared(t *testing.T, n crdNode, fragment string) {
	t.Helper()
	for _, r := range crdRules(n) {
		if strings.Contains(r, fragment) {
			return
		}
	}
	t.Errorf("no x-kubernetes-validations rule containing %q in %v", fragment, crdRules(n))
}

func marketplaceGitHubSchema(t *testing.T) crdNode {
	return crdProp(crdProp(loadCRDSchema(t, skillMarketplaceCRDPath), "spec"), "github")
}

func TestSkillMarketplaceCRD_Owner(t *testing.T) {
	assertMatches(t, crdPattern(t, crdProp(marketplaceGitHubSchema(t), "owner")),
		[]string{"acme-corp", "a", "A1", strings.Repeat("a", 39)},
		[]string{"", "-acme", "acme-", "acme_corp", "acme/corp", strings.Repeat("a", 40)})
}

func TestSkillMarketplaceCRD_Repo(t *testing.T) {
	repo := crdProp(marketplaceGitHubSchema(t), "repo")
	assertMatches(t, crdPattern(t, repo),
		[]string{"internal-skills", "skills.v2", "a_b"},
		[]string{"", "a/b", "with space", "skills?"})
	assertRuleDeclared(t, repo, "self != '.' && self != '..'")
}

func TestSkillMarketplaceCRD_Ref(t *testing.T) {
	assertMatches(t, crdPattern(t, crdProp(marketplaceGitHubSchema(t), "ref")),
		[]string{"main", "release/1.2", "0123456789abcdef0123456789abcdef01234567"},
		[]string{"", "-main", "/main", "main branch", "main?"})
}

func TestSkillMarketplaceCRD_App(t *testing.T) {
	app := crdProp(marketplaceGitHubSchema(t), "app")
	assertMatches(t, crdPattern(t, crdProp(app, "appID")), []string{"4015347"}, []string{"", "app-1", "12a"})
	key := crdProp(crdProp(app, "privateKeySecretRef"), "key")
	assertMatches(t, crdPattern(t, key), []string{"private-key.pem", "key_1"}, []string{"", "a/b", "with space"})
	if got := app["required"]; !reflect.DeepEqual(got, []any{"appID", "installationID", "privateKeySecretRef"}) {
		t.Errorf("app required = %v", got)
	}
}

// The name is the slug and part of the aggregated Secret's data key.
func TestSkillMarketplaceCRD_NameLength(t *testing.T) {
	assertRuleDeclared(t, loadCRDSchema(t, skillMarketplaceCRDPath), "size(self.metadata.name) <= 63")
}

func assertMaxLength(t *testing.T, n crdNode, want int) {
	t.Helper()
	var got float64
	switch v := n["maxLength"].(type) {
	case float64:
		got = v
	case int:
		got = float64(v)
	case int64:
		got = float64(v)
	default:
		t.Errorf("schema has no numeric maxLength, got %v", n["maxLength"])
		return
	}
	if got != float64(want) {
		t.Errorf("maxLength = %v, want %d", got, want)
	}
}

func TestSkillMarketplaceCRD_MaxLengths(t *testing.T) {
	github := marketplaceGitHubSchema(t)
	for _, c := range []struct {
		name string
		node crdNode
		want int
	}{
		{"github.owner", crdProp(github, "owner"), 39},
		{"github.repo", crdProp(github, "repo"), 100},
	} {
		t.Run(c.name, func(t *testing.T) { assertMaxLength(t, c.node, c.want) })
	}
}

func TestSkillMarketplaceCRD_ConditionsCoverMetav1Condition(t *testing.T) {
	status := crdProp(loadCRDSchema(t, skillMarketplaceCRDPath), "status")
	props := crdChild(crdChild(crdProp(status, "conditions"), "items"), "properties")
	typ := reflect.TypeFor[metav1.Condition]()
	checked := 0
	for field := range typ.Fields() {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		checked++
		if _, ok := props[name]; !ok {
			t.Errorf("status.conditions.items.properties has no %q, so the API server would prune it", name)
		}
	}
	if checked == 0 {
		t.Fatal("metav1.Condition has no json-tagged fields")
	}
}
