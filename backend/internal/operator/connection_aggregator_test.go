package operator

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
)

// secretRefKeyPattern is the Kubernetes Secret data-key charset the rendered
// secret_ref must satisfy (no slashes, no "..").
var secretRefKeyPattern = regexp.MustCompile(`^[-._a-zA-Z0-9]+$`)

func TestConnectionSecretRef_FlatAndSafe(t *testing.T) {
	cases := []struct {
		tenant, name, want string
	}{
		{"acme", "github", "acme.github.key"},
		{"acme-corp", "gh_prod", "acme-corp.gh_prod.key"},
		// Slashes and dots inside a segment are folded to "_", leaving only the
		// two structural "." separators — no ".." traversal token survives.
		{"a/b", "../etc", "a_b.___etc.key"},
		// Intra-segment dots are sanitized, so the only dots are structural.
		{"t.1", "n.2", "t_1.n_2.key"},
	}
	for _, c := range cases {
		got := connectionSecretRef(c.tenant, c.name)
		if got != c.want {
			t.Errorf("connectionSecretRef(%q,%q) = %q, want %q", c.tenant, c.name, got, c.want)
		}
		if !secretRefKeyPattern.MatchString(got) {
			t.Errorf("secret_ref %q is not a valid flat Secret key", got)
		}
		if strings.Contains(got, "..") {
			t.Errorf("secret_ref %q contains a '..' traversal token", got)
		}
	}
}

// TestConnectionSecretRef_NoCrossTenantCollision asserts the security property
// that distinct (tenant, name) pairs never fold to the same secret_ref. The
// aggregated Secret is cluster-wide keyed by this ref, so a collision would be
// cross-tenant credential bleed. The classic ambiguity is a dot straddling the
// segment boundary: tenant "a"+conn "b.c" vs tenant "a.b"+conn "c".
func TestConnectionSecretRef_NoCrossTenantCollision(t *testing.T) {
	a := connectionSecretRef("a", "b.c")
	b := connectionSecretRef("a.b", "c")
	if a == b {
		t.Errorf("cross-tenant collision: (a,b.c)=%q == (a.b,c)=%q", a, b)
	}
}

func TestSelectSecretKey(t *testing.T) {
	// Named key present.
	if v, ok := selectSecretKey(map[string][]byte{"app.pem": []byte("x"), "other": []byte("y")}, "app.pem"); !ok || string(v) != "x" {
		t.Errorf("named key: got %q ok=%v, want x true", v, ok)
	}
	// Named key absent.
	if _, ok := selectSecretKey(map[string][]byte{"app.pem": []byte("x")}, "missing"); ok {
		t.Error("missing named key: want ok=false")
	}
	// Empty key + single key → that key.
	if v, ok := selectSecretKey(map[string][]byte{"only": []byte("z")}, ""); !ok || string(v) != "z" {
		t.Errorf("single-key default: got %q ok=%v, want z true", v, ok)
	}
	// Empty key + multiple keys → ambiguous, not ok.
	if _, ok := selectSecretKey(map[string][]byte{"a": {1}, "b": {2}}, ""); ok {
		t.Error("ambiguous default: want ok=false")
	}
	// Empty key + no keys → not ok.
	if _, ok := selectSecretKey(map[string][]byte{}, ""); ok {
		t.Error("no keys: want ok=false")
	}
}

func TestRawConfig_NormalizesEmpty(t *testing.T) {
	if got := rawConfig(runtime.RawExtension{}); string(got) != "{}" {
		t.Errorf("empty config = %q, want {}", got)
	}
	raw := json.RawMessage(`{"appId":"1"}`)
	if got := rawConfig(runtime.RawExtension{Raw: raw}); string(got) != string(raw) {
		t.Errorf("config = %q, want %q", got, raw)
	}
}
