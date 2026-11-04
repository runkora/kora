package config

import (
	corev1 "k8s.io/api/core/v1"
	"os"
	"path/filepath"
	"testing"
)

// ---- helpers ----------------------------------------------------------------

// makeTempProject creates a temporary directory tree that looks like a Go
// project root with a go.mod file, so the path-resolution logic can be tested
// in isolation without depending on the real workspace layout.
//
//	tmpRoot/
//	  go.mod
//	  {module}/config/{env}.yaml   ← written with yamlContent
func makeTempProject(t *testing.T, module, env, yamlContent string) (root string) {
	t.Helper()

	root = t.TempDir()

	// Create a minimal go.mod so findProjectRoot can anchor on it.
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/test\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	cfgDir := filepath.Join(root, module, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", cfgDir, err)
	}

	cfgFile := filepath.Join(cfgDir, env+".yaml")
	if err := os.WriteFile(cfgFile, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("write config yaml: %v", err)
	}

	return root
}

// chdirTo changes the working directory to dir for the duration of the test,
// restoring the original directory when the test completes.
func chdirTo(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(orig); err != nil {
			t.Errorf("restore chdir to %s: %v", orig, err)
		}
	})
}

// ---- Environment() ----------------------------------------------------------

func TestEnvironment_DefaultsToProduction(t *testing.T) {
	t.Setenv("APP_ENV", "")
	if got := Environment(); got != "production" {
		t.Errorf("Environment() = %q, want %q", got, "production")
	}
}

func TestEnvironment_ReadsAppEnv(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	if got := Environment(); got != "production" {
		t.Errorf("Environment() = %q, want %q", got, "production")
	}
}

// ---- Load[T]() --------------------------------------------------------------

type testConfig struct {
	Server struct {
		Port int    `yaml:"port"`
		Host string `yaml:"host"`
	} `yaml:"server"`
	Debug bool `yaml:"debug"`
}

const validYAML = `
server:
  port: 8080
  host: localhost
debug: true
`

func TestLoad_HappyPath(t *testing.T) {
	root := makeTempProject(t, "internal/myfeature", "local", validYAML)
	chdirTo(t, root)

	cfg, err := Load[testConfig]("internal/myfeature", "local")
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	if cfg.Server.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cfg.Server.Port)
	}
	if cfg.Server.Host != "localhost" {
		t.Errorf("Host = %q, want %q", cfg.Server.Host, "localhost")
	}
	if !cfg.Debug {
		t.Errorf("Debug = false, want true")
	}
}

// TestLoad_WorkingDirectoryIndependence verifies that Load resolves the correct
// config file even when the working directory is a subdirectory of the project
// root — which happens during `go test ./...` runs.
func TestLoad_WorkingDirectoryIndependence(t *testing.T) {
	root := makeTempProject(t, "internal/myfeature", "development", validYAML)

	// Simulate running tests from a deep subdirectory inside the project.
	subDir := filepath.Join(root, "internal", "myfeature")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}
	chdirTo(t, subDir)

	cfg, err := Load[testConfig]("internal/myfeature", "development")
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	if cfg.Server.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cfg.Server.Port)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	root := makeTempProject(t, "internal/myfeature", "local", validYAML)
	chdirTo(t, root)

	_, err := Load[testConfig]("internal/myfeature", "nonexistent")
	if err == nil {
		t.Fatal("Load() expected error for missing file, got nil")
	}
}

func TestLoad_MalformedYAML(t *testing.T) {
	root := makeTempProject(t, "internal/myfeature", "bad", "server: [\ninvalid yaml")
	chdirTo(t, root)

	_, err := Load[testConfig]("internal/myfeature", "bad")
	if err == nil {
		t.Fatal("Load() expected error for malformed YAML, got nil")
	}
}

func TestLoad_EmptyFile(t *testing.T) {
	root := makeTempProject(t, "internal/myfeature", "empty", "")
	chdirTo(t, root)

	// An empty YAML file is valid — it simply leaves all fields at their zero
	// values. Load should succeed.
	cfg, err := Load[testConfig]("internal/myfeature", "empty")
	if err != nil {
		t.Fatalf("Load() unexpected error for empty file: %v", err)
	}
	if cfg.Server.Port != 0 {
		t.Errorf("Port = %d, want 0 for empty config", cfg.Server.Port)
	}
}

// TestLoad_ReturnsPointer ensures Load always returns a non-nil pointer on
// success — callers rely on this and should not need a nil check.
func TestLoad_ReturnsPointer(t *testing.T) {
	root := makeTempProject(t, "internal/myfeature", "local", validYAML)
	chdirTo(t, root)

	cfg, err := Load[testConfig]("internal/myfeature", "local")
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if cfg == nil {
		t.Error("Load() returned nil pointer, want non-nil")
	}
}

// ---- MustLoad[T]() ----------------------------------------------------------

func TestMustLoad_HappyPath(t *testing.T) {
	root := makeTempProject(t, "internal/myfeature", "local", validYAML)
	chdirTo(t, root)

	cfg := MustLoad[testConfig]("internal/myfeature", "local")
	if cfg == nil {
		t.Fatal("MustLoad() returned nil, want non-nil pointer")
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cfg.Server.Port)
	}
}

func TestMustLoad_PanicsOnMissingFile(t *testing.T) {
	root := makeTempProject(t, "internal/myfeature", "local", validYAML)
	chdirTo(t, root)

	defer func() {
		if r := recover(); r == nil {
			t.Error("MustLoad() expected panic for missing file, but did not panic")
		}
	}()

	MustLoad[testConfig]("internal/myfeature", "nonexistent")
}

func TestMustLoad_PanicsOnMalformedYAML(t *testing.T) {
	root := makeTempProject(t, "internal/myfeature", "bad", "server: [\ninvalid yaml")
	chdirTo(t, root)

	defer func() {
		if r := recover(); r == nil {
			t.Error("MustLoad() expected panic for malformed YAML, but did not panic")
		}
	}()

	MustLoad[testConfig]("internal/myfeature", "bad")
}

// ---- findProjectRoot() ------------------------------------------------------

func TestFindProjectRoot_FindsGoMod(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Start search from a deep subdirectory.
	subDir := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := findProjectRoot(subDir)
	if err != nil {
		t.Fatalf("findProjectRoot() unexpected error: %v", err)
	}
	// Normalise want through EvalSymlinks so the comparison is stable on
	// macOS where t.TempDir() may return a symlinked path.
	wantRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if got != wantRoot {
		t.Errorf("findProjectRoot() = %q, want %q", got, wantRoot)
	}
}

func TestFindProjectRoot_ErrorWhenNotFound(t *testing.T) {
	// Use a temp directory with no go.mod anywhere in the tree.
	dir := t.TempDir()

	_, err := findProjectRoot(dir)
	if err == nil {
		t.Error("findProjectRoot() expected error when go.mod absent, got nil")
	}
}

// ---- resolvePath() ----------------------------------------------------------

func TestResolvePath_ConstructsCorrectPath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	chdirTo(t, root)

	got, err := resolvePath("internal/identity", "production")
	if err != nil {
		t.Fatalf("resolvePath() unexpected error: %v", err)
	}

	// EvalSymlinks normalises the root so the comparison is stable on macOS
	// where t.TempDir() may return a path that goes through /private/var/...
	// symlinks, while os.Getwd() returns the resolved real path.
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	want := filepath.Join(realRoot, "internal", "identity", "config", "production.yaml")
	if got != want {
		t.Errorf("resolvePath() = %q, want %q", got, want)
	}
}

// ---- rendered config overlay ------------------------------------------------

// setOverlay points the loader at a temporary overlay root holding files, keyed by module.
func setOverlay(t *testing.T, files map[string]string) {
	t.Helper()
	root := t.TempDir()
	for module, content := range files {
		path := filepath.Join(root, module+".yaml")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	orig := overlayRoot
	overlayRoot = root
	t.Cleanup(func() { overlayRoot = orig })
}

// writeLayer writes {module}/config/{name}.yaml under root.
func writeLayer(t *testing.T, root, module, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, module, "config", name+".yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadService_OverlayMergesLast(t *testing.T) {
	root := makeTempProject(t, "cmd/svc", "base", "server:\n  port: 1\n  host: base\ndebug: true\n")
	writeLayer(t, root, "cmd/svc", "production", "server:\n  port: 2\n")
	chdirTo(t, root)
	setOverlay(t, map[string]string{"cmd/svc": "server:\n  port: 3\n"})

	cfg, err := LoadService[testConfig]("cmd/svc", "production")
	if err != nil {
		t.Fatalf("LoadService() error: %v", err)
	}
	if cfg.Server.Port != 3 || cfg.Server.Host != "base" || !cfg.Debug {
		t.Errorf("got %+v, want port 3 from the overlay with host and debug kept from base", cfg)
	}
}

func TestLoad_OverlayMergesLast(t *testing.T) {
	root := makeTempProject(t, "internal/feat", "production", validYAML)
	chdirTo(t, root)
	setOverlay(t, map[string]string{"internal/feat": "server:\n  host: rendered\n"})

	cfg, err := Load[testConfig]("internal/feat", "production")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Server.Host != "rendered" || cfg.Server.Port != 8080 {
		t.Errorf("got %+v, want host from the overlay and port from the file", cfg)
	}
}

func TestLoadService_MissingOverlayIsNoOverlay(t *testing.T) {
	root := makeTempProject(t, "cmd/svc", "base", validYAML)
	chdirTo(t, root)
	setOverlay(t, map[string]string{"cmd/other": "server:\n  port: 9\n"})

	cfg, err := LoadService[testConfig]("cmd/svc", "production")
	if err != nil {
		t.Fatalf("LoadService() error: %v", err)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("Port = %d, want 8080 from base", cfg.Server.Port)
	}
}

func TestLoadService_OverlayErrors(t *testing.T) {
	tests := map[string]string{
		"unknown key":   "server:\n  prot: 3\n",
		"wrong type":    "server:\n  port: three\n",
		"malformed":     "server: [\n",
		"not a mapping": "- a\n",
	}
	for name, overlay := range tests {
		t.Run(name, func(t *testing.T) {
			root := makeTempProject(t, "cmd/svc", "base", validYAML)
			chdirTo(t, root)
			setOverlay(t, map[string]string{"cmd/svc": overlay})

			if _, err := LoadService[testConfig]("cmd/svc", "production"); err == nil {
				t.Fatal("LoadService() expected an error, got nil")
			}
		})
	}
}

type kubeConfig struct {
	Resources   corev1.ResourceRequirements `yaml:"resources"`
	Tolerations []corev1.Toleration         `yaml:"tolerations"`
	Affinity    *corev1.Affinity            `yaml:"affinity"`
	Labels      map[string]string           `yaml:"labels"`
}

func TestLoadService_DecodesKubernetesTypes(t *testing.T) {
	root := makeTempProject(t, "cmd/svc", "base", "")
	chdirTo(t, root)
	setOverlay(t, map[string]string{"cmd/svc": `
resources:
  requests: {cpu: 50m, memory: 64Mi}
  limits: {cpu: "1", nvidia.com/gpu: 1}
tolerations:
  - {key: dedicated, operator: Equal, value: agents, effect: NoSchedule, tolerationSeconds: 30}
affinity:
  nodeAffinity:
    requiredDuringSchedulingIgnoredDuringExecution:
      nodeSelectorTerms:
        - matchExpressions:
            - {key: kubernetes.io/arch, operator: In, values: [arm64]}
labels:
  App.Example.com/Name: kora
`})

	cfg, err := LoadService[kubeConfig]("cmd/svc", "production")
	if err != nil {
		t.Fatalf("LoadService() error: %v", err)
	}
	if got := cfg.Resources.Requests.Cpu().String(); got != "50m" {
		t.Errorf("requests.cpu = %s, want 50m", got)
	}
	if got := cfg.Resources.Limits[corev1.ResourceName("nvidia.com/gpu")]; got.Value() != 1 {
		t.Errorf("limits[nvidia.com/gpu] = %s, want 1", got.String())
	}
	if tol := cfg.Tolerations[0]; tol.Key != "dedicated" || tol.TolerationSeconds == nil || *tol.TolerationSeconds != 30 {
		t.Errorf("toleration = %+v", tol)
	}
	expr := cfg.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions[0]
	if expr.Key != "kubernetes.io/arch" || expr.Values[0] != "arm64" {
		t.Errorf("match expression = %+v", expr)
	}
	if cfg.Labels["App.Example.com/Name"] != "kora" {
		t.Errorf("labels = %v, want the dotted, mixed-case key kept", cfg.Labels)
	}
}

func TestLoadService_RejectsUnknownKubernetesField(t *testing.T) {
	root := makeTempProject(t, "cmd/svc", "base", "")
	chdirTo(t, root)
	setOverlay(t, map[string]string{"cmd/svc": "resources:\n  limit: {cpu: 1}\n"})

	if _, err := LoadService[kubeConfig]("cmd/svc", "production"); err == nil {
		t.Fatal("LoadService() expected an error for resources.limit, got nil")
	}
}
