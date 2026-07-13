package kubeconfig

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServerURL(t *testing.T) {
	cases := map[string]string{
		"cluster.example.com:443":    "https://cluster.example.com:443",
		"cluster.example.com":        "https://cluster.example.com",
		"https://already.example":    "https://already.example",
		"  spaced.example.com:6443 ": "https://spaced.example.com:6443",
		"":                           "",
	}
	for in, want := range cases {
		if got := ServerURL(in); got != want {
			t.Errorf("ServerURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestRender_CarriesPlaceholderToken locks the behaviour the whole design rests
// on. kubectl's clientcmd will NOT send an unauthenticated request: given a user
// with no auth info it prompts on stdin and never opens a connection. So the
// rendered user must carry a non-empty token, which the egress gateway then
// overwrites on the wire. An empty token here silently breaks every kubectl call
// in every sandbox, with a "Please enter Username" prompt as the only symptom.
func TestRender_CarriesPlaceholderToken(t *testing.T) {
	data, err := Render([]Cluster{{Name: "prod", Server: "https://k8s.example:443"}})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	var cfg kubeconfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("rendered kubeconfig is not valid JSON (kubectl parses YAML, a superset): %v", err)
	}
	if len(cfg.Users) != 1 {
		t.Fatalf("users = %d, want 1", len(cfg.Users))
	}
	if cfg.Users[0].User.Token == "" {
		t.Error("user token is empty — kubectl will prompt for a username instead of sending a request")
	}
	if cfg.Users[0].User.Token != PlaceholderToken {
		t.Errorf("token = %q, want the placeholder", cfg.Users[0].User.Token)
	}
	if cfg.CurrentContext != "prod" {
		t.Errorf("current-context = %q, want prod", cfg.CurrentContext)
	}
	if cfg.Clusters[0].Cluster.Server != "https://k8s.example:443" {
		t.Errorf("server = %q", cfg.Clusters[0].Cluster.Server)
	}
}

// TestRender_NoCertificateAuthority locks that we never pin a CA. The sandbox
// mounts the tenant CA over the OS trust bundle; the gateway's re-originated
// leaf chains to that CA, not to the target cluster's, so pinning the cluster's
// own CA would fail TLS.
func TestRender_NoCertificateAuthority(t *testing.T) {
	data, err := Render([]Cluster{{Name: "c", Server: "https://x:443"}})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, banned := range []string{"certificate-authority", "insecure-skip-tls-verify"} {
		if strings.Contains(string(data), banned) {
			t.Errorf("rendered kubeconfig must not set %q", banned)
		}
	}
}

func TestRender_MultipleClustersFirstIsCurrent(t *testing.T) {
	data, err := Render([]Cluster{
		{Name: "alpha", Server: "https://a:443"},
		{Name: "beta", Server: "https://b:443"},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	var cfg kubeconfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.CurrentContext != "alpha" {
		t.Errorf("current-context = %q, want alpha", cfg.CurrentContext)
	}
	if len(cfg.Contexts) != 2 || cfg.Contexts[1].Name != "beta" {
		t.Errorf("contexts = %+v, want one per cluster", cfg.Contexts)
	}
}

func TestRender_EmptyAndInvalid(t *testing.T) {
	if data, err := Render(nil); data != nil || err != nil {
		t.Errorf("Render(nil) = (%v, %v), want (nil, nil)", data, err)
	}
	if _, err := Render([]Cluster{{Name: "c"}}); !errors.Is(err, ErrNoServer) {
		t.Errorf("Render with no server = %v, want ErrNoServer", err)
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	in := []Cluster{{Name: "c", Server: "https://x:443"}}
	enc, err := Encode(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Decode(enc)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0] != in[0] {
		t.Errorf("round trip = %+v, want %+v", out, in)
	}

	// An agent with no Kubernetes connection encodes to "" so the env var is
	// omitted entirely, and decoding "" yields no clusters and no error.
	if enc, err := Encode(nil); enc != "" || err != nil {
		t.Errorf("Encode(nil) = (%q, %v), want (\"\", nil)", enc, err)
	}
	if out, err := Decode("  "); out != nil || err != nil {
		t.Errorf("Decode(blank) = (%v, %v), want (nil, nil)", out, err)
	}
	if _, err := Decode("{not json"); err == nil {
		t.Error("Decode of malformed JSON should error")
	}
}

func TestWriteTo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "config")

	if err := WriteTo(path, nil); err != nil {
		t.Fatalf("WriteTo(nil) should be a no-op: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("WriteTo(nil) must not create a file")
	}

	if err := WriteTo(path, []Cluster{{Name: "c", Server: "https://x:443"}}); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600", perm)
	}
	b, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), PlaceholderToken) {
		t.Error("written kubeconfig does not carry the placeholder token")
	}
}

// TestRender_TrimsAndValidatesNames locks that Render does not trust its input.
// The cluster list crosses a process boundary via ClustersEnvVar, and a blank or
// whitespace name would emit a document whose current-context resolves to no
// context at all.
func TestRender_TrimsAndValidatesNames(t *testing.T) {
	data, err := Render([]Cluster{{Name: "  padded  ", Server: "  https://x:443  "}})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	var cfg kubeconfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.CurrentContext != "padded" {
		t.Errorf("current-context = %q, want the trimmed name", cfg.CurrentContext)
	}
	if cfg.Clusters[0].Cluster.Server != "https://x:443" {
		t.Errorf("server = %q, want trimmed", cfg.Clusters[0].Cluster.Server)
	}

	for _, bad := range []Cluster{
		{Name: "", Server: "https://x:443"},
		{Name: "   ", Server: "https://x:443"},
	} {
		if _, err := Render([]Cluster{bad}); !errors.Is(err, ErrNoName) {
			t.Errorf("Render(%+v) = %v, want ErrNoName", bad, err)
		}
	}
	if _, err := Render([]Cluster{{Name: "c", Server: "   "}}); !errors.Is(err, ErrNoServer) {
		t.Error("whitespace-only server should be ErrNoServer")
	}
}
