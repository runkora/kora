package runfs

import (
	"slices"
	"testing"
)

func TestLayout(t *testing.T) {
	ws := "/workspaces/run-1"
	for _, tc := range []struct{ name, got, want string }{
		{"home", HomeDir(ws), "/workspaces/run-1/.runhome"},
		{"tmp beside the workspace", TmpDir(ws), "/workspaces/.tmp/run-1"},
		{"config beside the workspace", ConfigDir(ws), "/workspaces/.runconfig/run-1"},
		{"local beside the workspace", LocalDir(ws), "/workspaces/.runlocal/run-1"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

// Installs and caches land under the local dir, and its bin directories come last on PATH.
func TestInstallEnv(t *testing.T) {
	dir := LocalDir("/workspaces/run-1")
	got := InstallEnv(dir, "/usr/bin:/bin")
	want := []string{
		"NPM_CONFIG_PREFIX=" + dir + "/.local",
		"NPM_CONFIG_CACHE=" + dir + "/.npm",
		"PYTHONUSERBASE=" + dir + "/.local",
		"PIP_BREAK_SYSTEM_PACKAGES=1",
		"PIP_CACHE_DIR=" + dir + "/.cache/pip",
		"GOPATH=" + dir + "/go",
		"GOMODCACHE=" + dir + "/go/pkg/mod",
		"GOCACHE=" + dir + "/.cache/go-build",
		"CARGO_HOME=" + dir + "/.cargo",
		"PATH=/usr/bin:/bin:/usr/local/go/bin:/usr/local/cargo/bin:" + dir + "/.local/bin:" + dir + "/go/bin:" + dir + "/.cargo/bin",
	}
	if !slices.Equal(got, want) {
		t.Errorf("InstallEnv =\n%v\nwant\n%v", got, want)
	}
	for _, e := range got {
		if e == "PIP_USER=1" {
			t.Error("PIP_USER breaks pip inside a virtualenv and must stay unset")
		}
	}
}
