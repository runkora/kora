package mount

import (
	"strings"
	"testing"
)

func TestParseMountInfo(t *testing.T) {
	line := `6275 7156 0:1649 / /var/lib/kubelet/pods/u/volumes/kubernetes.io~local-volume/pv\040x/run/sandbox rw,relatime shared:903 - nfs4 172.19.0.7:/run rw,vers=4.2,soft`
	got, ok := parseMountInfo(line)
	want := mountEntry{point: "/var/lib/kubelet/pods/u/volumes/kubernetes.io~local-volume/pv x/run/sandbox", fstype: "nfs4", source: "172.19.0.7:/run"}
	if !ok || got != want {
		t.Errorf("parseMountInfo = %+v, %v, want %+v", got, ok, want)
	}
	if _, ok := parseMountInfo("garbage"); ok {
		t.Error("parseMountInfo accepted a line without a separator")
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
