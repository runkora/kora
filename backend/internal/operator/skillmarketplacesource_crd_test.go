package operator

import (
	"maps"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

const (
	skillMarketplaceSourceCRDPath = "../../../charts/orbit-crds/templates/crd-skillmarketplacesource.yaml"
	orbitTenantCRDPath            = "../../../charts/orbit-crds/templates/crd-orbittenant.yaml"
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
	return crdProp(crdProp(loadCRDSchema(t, skillMarketplaceSourceCRDPath), "spec"), "github")
}

func TestSkillMarketplaceSourceCRD_Owner(t *testing.T) {
	assertMatches(t, crdPattern(t, crdProp(marketplaceGitHubSchema(t), "owner")),
		[]string{"acme-corp", "a", "A1", strings.Repeat("a", 39)},
		[]string{"", "-acme", "acme-", "acme_corp", "acme/corp", strings.Repeat("a", 40)})
}

func TestSkillMarketplaceSourceCRD_Repo(t *testing.T) {
	repo := crdProp(marketplaceGitHubSchema(t), "repo")
	assertMatches(t, crdPattern(t, repo),
		[]string{"internal-skills", "skills.v2", "a_b"},
		[]string{"", "a/b", "with space", "skills?"})
	assertRuleDeclared(t, repo, "self != '.' && self != '..'")
}

// skills-service names a marketplace after its repository and reads the default
// branch, so a field the operator cannot pass on must not appear in the spec: a
// tenant would set it and see nothing happen.
func TestSkillMarketplaceSourceCRD_SpecCarriesOnlyTheRepository(t *testing.T) {
	spec := crdProp(loadCRDSchema(t, skillMarketplaceSourceCRDPath), "spec")
	if got := slices.Sorted(maps.Keys(crdChild(spec, "properties"))); !slices.Equal(got, []string{"github"}) {
		t.Errorf("spec properties = %v, want only github", got)
	}
	if got := spec["required"]; !reflect.DeepEqual(got, []any{"github"}) {
		t.Errorf("spec required = %v, want [github]", got)
	}
	github := crdProp(spec, "github")
	if got := slices.Sorted(maps.Keys(crdChild(github, "properties"))); !slices.Equal(got, []string{"owner", "repo"}) {
		t.Errorf("github properties = %v, want owner and repo", got)
	}
}

func TestOrbitTenantCRD_AllowedMarketplaceRepos(t *testing.T) {
	items := crdChild(crdProp(crdProp(loadCRDSchema(t, orbitTenantCRDPath), "spec"), "allowedMarketplaceRepos"), "items")
	assertMatches(t, crdPattern(t, items),
		[]string{"acme-corp/internal-skills", "a/b.c"},
		[]string{"acme-corp", "acme/internal/skills", "https://github.com/acme/skills", "/skills", "acme/"})
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

func allowedMarketplaceReposItems(t *testing.T) crdNode {
	return crdChild(crdProp(crdProp(loadCRDSchema(t, orbitTenantCRDPath), "spec"), "allowedMarketplaceRepos"), "items")
}

func TestSkillMarketplaceSourceCRD_MaxLengths(t *testing.T) {
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

func TestOrbitTenantCRD_AllowedMarketplaceReposMaxLength(t *testing.T) {
	assertMaxLength(t, allowedMarketplaceReposItems(t), 140)
}

func TestMarketplaceCRD_ValidRepoPairsAcceptedByEveryPattern(t *testing.T) {
	github := marketplaceGitHubSchema(t)
	owner := crdPattern(t, crdProp(github, "owner"))
	repo := crdPattern(t, crdProp(github, "repo"))
	allowed := crdPattern(t, allowedMarketplaceReposItems(t))
	for _, c := range []struct{ name, pair string }{
		{"simple", "acme-corp/internal-skills"},
		{"dotted_repo", "a/b.c"},
		{"mixed_case", "Acme-Corp/Internal_Skills.V2"},
		{"digits", "A1/9"},
		{"max_length_owner_and_repo", strings.Repeat("o", 39) + "/" + strings.Repeat("r", 100)},
		{"hyphen_inside_owner", "a-b-c/-leading-hyphen-repo"},
	} {
		t.Run(c.name, func(t *testing.T) {
			o, r, ok := strings.Cut(c.pair, "/")
			if !ok {
				t.Fatalf("test pair %q has no slash", c.pair)
			}
			if !owner.MatchString(o) {
				t.Errorf("owner pattern %s rejects %q", owner, o)
			}
			if !repo.MatchString(r) {
				t.Errorf("repo pattern %s rejects %q", repo, r)
			}
			if !allowed.MatchString(c.pair) {
				t.Errorf("allowlist pattern %s rejects %q", allowed, c.pair)
			}
		})
	}
}

func TestMarketplaceCRD_InvalidRepoPairsRejectedByAllowlist(t *testing.T) {
	allowed := crdPattern(t, allowedMarketplaceReposItems(t))
	for _, c := range []struct{ name, pair string }{
		{"leading_hyphen_owner", "-acme/skills"},
		{"trailing_hyphen_owner", "acme-/skills"},
		{"underscore_owner", "acme_corp/skills"},
		{"owner_over_39", strings.Repeat("o", 40) + "/skills"},
		{"repo_with_space", "acme/with space"},
		{"repo_with_slash", "acme/a/b"},
		{"repo_with_question_mark", "acme/skills?"},
		{"repo_over_100", "acme/" + strings.Repeat("r", 101)},
		{"owner_only", "acme"},
		{"empty_repo", "acme/"},
		{"empty_owner", "/skills"},
		{"three_segments", "acme/internal/skills"},
		{"url", "https://github.com/acme/skills"},
		{"leading_space", " acme/skills"},
		{"trailing_newline", "acme/skills\n"},
		{"empty", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			if allowed.MatchString(c.pair) {
				t.Errorf("allowlist pattern %s accepts %q", allowed, c.pair)
			}
		})
	}
}

func TestSkillMarketplaceSourceCRD_ConditionsCoverMetav1Condition(t *testing.T) {
	status := crdProp(loadCRDSchema(t, skillMarketplaceSourceCRDPath), "status")
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
