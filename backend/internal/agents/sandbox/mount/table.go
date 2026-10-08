package mount

import (
	"bufio"
	"io"
	"path/filepath"
	"strconv"
	"strings"
)

// parseMountInfo reads the mount and parent IDs, mount point, filesystem type and source from one /proc/self/mountinfo line.
func parseMountInfo(line string) (mountEntry, bool) {
	before, after, ok := strings.Cut(line, " - ")
	if !ok {
		return mountEntry{}, false
	}
	pre, post := strings.Fields(before), strings.Fields(after)
	if len(pre) < 5 || len(post) < 2 {
		return mountEntry{}, false
	}
	return mountEntry{id: pre[0], parent: pre[1], point: unescape(pre[4]), fstype: post[0], source: unescape(post[1])}, true
}

// propagated is a copy of a sandbox folder mount, and the mount it sits on.
type propagated struct{ point, parentPoint string }

// copiesUnder returns the sandbox folder mounts below the given pod directories,
// with the mounts they sit on. These are copies the host propagated into other
// binds of a session-manager's volume, such as a co-located sandbox pod's.
func copiesUnder(entries []mountEntry, podDirs []string) []propagated {
	points := make(map[string]string, len(entries))
	for _, e := range entries {
		points[e.id] = e.point
	}
	var out []propagated
	for _, e := range entries {
		if e.fstype != "nfs4" || filepath.Base(e.point) != Dir {
			continue
		}
		for _, dir := range podDirs {
			if strings.HasPrefix(e.point, dir) && points[e.parent] != "" {
				out = append(out, propagated{point: e.point, parentPoint: points[e.parent]})
				break
			}
		}
	}
	return out
}

// SourceAt returns the source of the topmost mount at point in a mountinfo
// listing, or an empty string when nothing is mounted there.
func SourceAt(mountinfo io.Reader, point string) string {
	var source string
	sc := bufio.NewScanner(mountinfo)
	for sc.Scan() {
		if m, ok := parseMountInfo(sc.Text()); ok && m.point == point {
			source = m.source
		}
	}
	return source
}

// mountEntry is one line of /proc/self/mountinfo.
type mountEntry struct{ id, parent, point, fstype, source string }

// unescape decodes the octal escapes mountinfo uses for spaces, tabs, newlines and backslashes.
func unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
