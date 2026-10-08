package mount

import (
	"slices"
	"strings"
	"testing"
)

// Each client node has its own read-only root, and each folder is exported to its own client only.
func TestExportLines(t *testing.T) {
	got := exportLines("/exports", []export{
		{name: "b", client: "10.0.0.2", uid: 100, gid: 100},
		{name: "a", client: "10.0.0.3", uid: 100, gid: 101},
		{name: "c", client: "10.0.0.2", uid: 100, gid: 100},
	})
	want := []string{
		"/exports/10.0.0.2 10.0.0.2(ro,secure,no_subtree_check,all_squash,fsid=0)",
		"/exports/10.0.0.2/b 10.0.0.2(rw,async,secure,no_subtree_check,all_squash,anonuid=100,anongid=100,fsid=b)",
		"/exports/10.0.0.2/c 10.0.0.2(rw,async,secure,no_subtree_check,all_squash,anonuid=100,anongid=100,fsid=c)",
		"/exports/10.0.0.3 10.0.0.3(ro,secure,no_subtree_check,all_squash,fsid=0)",
		"/exports/10.0.0.3/a 10.0.0.3(rw,async,secure,no_subtree_check,all_squash,anonuid=100,anongid=101,fsid=a)",
	}
	if !slices.Equal(got, want) {
		t.Errorf("exportLines =\n%v\nwant\n%v", got, want)
	}
	// No line grants a client anything under another client's root.
	for _, l := range got {
		dir, acl, _ := strings.Cut(l, " ")
		client := strings.Split(strings.TrimPrefix(dir, "/exports/"), "/")[0]
		if !strings.HasPrefix(acl, client+"(") || strings.Contains(acl, " ") {
			t.Errorf("line %q grants a client other than %s", l, client)
		}
	}
	if lines := exportLines("/exports", nil); lines != nil {
		t.Errorf("empty exportLines = %v, want nil", lines)
	}
}

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
