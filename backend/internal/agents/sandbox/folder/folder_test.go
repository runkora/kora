package folder

import (
	"strings"
	"testing"
)

func TestReadMountInfo(t *testing.T) {
	info := `6275 7156 0:1649 / /var/lib/kubelet/pods/u/volumes/kubernetes.io~local-volume/pv\040x/run/sandbox rw,relatime shared:903 - nfs4 172.19.0.7:/run rw,vers=4.2,soft
garbage`
	got, err := ReadMountInfo(strings.NewReader(info))
	want := MountEntry{ID: "6275", Parent: "7156", Point: "/var/lib/kubelet/pods/u/volumes/kubernetes.io~local-volume/pv x/run/sandbox", FSType: "nfs4", Source: "172.19.0.7:/run"}
	if err != nil || len(got) != 1 || got[0] != want {
		t.Errorf("ReadMountInfo = %+v, %v, want only %+v", got, err, want)
	}
}

// The topmost mount at a point wins, and an unmounted point has no source.
func TestSourceAt(t *testing.T) {
	info := strings.Join([]string{
		"1 0 0:1 / /workspaces rw - ext4 /dev/vdb1 rw",
		"2 1 0:2 / /workspaces/run-1/sandbox rw - nfs4 10.0.0.1:/old rw",
		"3 1 0:3 / /workspaces/run-1/sandbox rw - nfs4 10.0.0.1:/new rw",
	}, "\n")
	if got := SourceAt(strings.NewReader(info), "/workspaces/run-1/sandbox"); got != "10.0.0.1:/new" {
		t.Errorf("SourceAt = %q, want the topmost mount", got)
	}
	if got := SourceAt(strings.NewReader(info), "/workspaces/run-2/sandbox"); got != "" {
		t.Errorf("SourceAt unmounted = %q", got)
	}
}

func TestWorkspaceClaim(t *testing.T) {
	if got := WorkspaceClaim("coder-2"); got != "workspaces-coder-2" {
		t.Errorf("WorkspaceClaim = %q", got)
	}
	for claim, want := range map[string]string{"workspaces-coder-2": "coder-2", "workspaces-": "", "scratch": ""} {
		got, ok := WorkspacePod(claim)
		if got != want || ok != (want != "") {
			t.Errorf("WorkspacePod(%q) = %q, %v, want %q", claim, got, ok, want)
		}
	}
}
