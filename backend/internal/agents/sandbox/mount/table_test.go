package mount

import (
	"slices"
	"testing"
)

// The root is listed for every client read-only, and each folder only for its own client.
func TestExportLines(t *testing.T) {
	got := exportLines("/exports", []export{
		{name: "b", client: "10.0.0.2", uid: 100, gid: 100},
		{name: "a", client: "10.0.0.3", uid: 100, gid: 101},
		{name: "c", client: "10.0.0.2", uid: 100, gid: 100},
	})
	want := []string{
		"/exports 10.0.0.2(ro,secure,no_subtree_check,all_squash,fsid=0) 10.0.0.3(ro,secure,no_subtree_check,all_squash,fsid=0)",
		"/exports/a 10.0.0.3(rw,async,secure,no_subtree_check,all_squash,anonuid=100,anongid=101,fsid=a)",
		"/exports/b 10.0.0.2(rw,async,secure,no_subtree_check,all_squash,anonuid=100,anongid=100,fsid=b)",
		"/exports/c 10.0.0.2(rw,async,secure,no_subtree_check,all_squash,anonuid=100,anongid=100,fsid=c)",
	}
	if !slices.Equal(got, want) {
		t.Errorf("exportLines =\n%v\nwant\n%v", got, want)
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
