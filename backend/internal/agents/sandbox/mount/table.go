package mount

import (
	"bufio"
	"io"
	"strconv"
	"strings"
)

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
