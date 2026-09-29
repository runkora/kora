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
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

// Installs and caches land under home, and home's bin directories come last on PATH.
func TestInstallEnv(t *testing.T) {
	home := "/workspaces/run-1/.runhome"
	got := InstallEnv(home, "/usr/bin:/bin")
	want := []string{
		"NPM_CONFIG_PREFIX=" + home + "/.local",
		"NPM_CONFIG_CACHE=" + home + "/.npm",
		"PYTHONUSERBASE=" + home + "/.local",
		"PIP_BREAK_SYSTEM_PACKAGES=1",
		"PIP_CACHE_DIR=" + home + "/.cache/pip",
		"GOPATH=" + home + "/go",
		"GOCACHE=" + home + "/.cache/go-build",
		"CARGO_HOME=" + home + "/.cargo",
		"PATH=/usr/bin:/bin:/usr/local/go/bin:/usr/local/cargo/bin:" + home + "/.local/bin:" + home + "/go/bin:" + home + "/.cargo/bin",
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
