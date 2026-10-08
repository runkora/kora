package mount

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// exportLines renders the exports table. The export root is the NFSv4 pseudo-root,
// read-only and listed for every client. Each sandbox folder is exported to its
// session-manager's node only, with every client user mapped to the sandbox's user.
func exportLines(root string, exports []export) []string {
	if len(exports) == 0 {
		return nil
	}
	var clients, lines []string
	for _, e := range exports {
		if !slices.Contains(clients, e.client) {
			clients = append(clients, e.client)
		}
		lines = append(lines, fmt.Sprintf("%s/%s %s(rw,async,secure,no_subtree_check,all_squash,anonuid=%d,anongid=%d,fsid=%s)",
			root, e.name, e.client, e.uid, e.gid, e.name))
	}
	slices.Sort(clients)
	slices.Sort(lines)
	rootLine := root
	for _, c := range clients {
		rootLine += " " + c + "(ro,secure,no_subtree_check,all_squash,fsid=0)"
	}
	return append([]string{rootLine}, lines...)
}

// parseMountInfo reads the mount point, filesystem type and source from one /proc/self/mountinfo line.
func parseMountInfo(line string) (mountEntry, bool) {
	before, after, ok := strings.Cut(line, " - ")
	if !ok {
		return mountEntry{}, false
	}
	pre, post := strings.Fields(before), strings.Fields(after)
	if len(pre) < 5 || len(post) < 2 {
		return mountEntry{}, false
	}
	return mountEntry{point: unescape(pre[4]), fstype: post[0], source: unescape(post[1])}, true
}

// mountEntry is one line of /proc/self/mountinfo.
type mountEntry struct{ point, fstype, source string }

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
