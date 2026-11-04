package connections

import (
	"encoding/json"
	"net"
	"strings"
)

// Identity is what tells two connections in one tenant apart at the egress
// gateway: the hosts they serve and, for github_app, the account they are bound to.
type Identity struct {
	Adapter  string
	BaseHost string
	Owner    string
}

// Conflict reports whether the egress gateway cannot tell a and b apart: they
// serve a common host and their owners do not distinguish them. See
// docs/adr/2026-10-15-same-host-connection-selection.md.
func Conflict(a, b Identity) bool {
	if !hostsIntersect(servedHosts(a), servedHosts(b)) {
		return false
	}
	if a.Adapter != AdapterGitHubApp || b.Adapter != AdapterGitHubApp {
		return true
	}
	oa, ob := strings.ToLower(strings.TrimSpace(a.Owner)), strings.ToLower(strings.TrimSpace(b.Owner))
	return oa == "" || ob == "" || oa == ob
}

// ConfigOwner returns the owner field of a connection's adapter config, or ""
// when the config declares none or does not parse.
func ConfigOwner(config []byte) string {
	var c struct {
		Owner string `json:"owner"`
	}
	if json.Unmarshal(config, &c) != nil {
		return ""
	}
	return c.Owner
}

// servedHosts lists the base host and the adapter's host family, normalized the
// way the gateway registry compares them.
func servedHosts(id Identity) []string {
	hosts := []string{normalizeHost(id.BaseHost)}
	for _, h := range EgressHosts(id.Adapter, id.BaseHost) {
		hosts = append(hosts, normalizeHost(h))
	}
	return hosts
}

// hostsIntersect reports whether some host or "*.suffix" pattern in a matches one in b.
func hostsIntersect(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x != "" && y != "" && (hostMatches(x, y) || hostMatches(y, x)) {
				return true
			}
		}
	}
	return false
}

// hostMatches reports whether host equals pattern or falls under its "*." wildcard.
func hostMatches(pattern, host string) bool {
	if pattern == host {
		return true
	}
	suffix, ok := strings.CutPrefix(pattern, "*")
	return ok && strings.HasPrefix(suffix, ".") && strings.HasSuffix(host, suffix) && len(host) > len(suffix)
}

// normalizeHost lowercases host and drops any port and trailing dot.
func normalizeHost(host string) string {
	h := strings.ToLower(strings.TrimSpace(host))
	if stripped, _, err := net.SplitHostPort(h); err == nil {
		h = stripped
	}
	if len(h) > 1 {
		h = strings.TrimSuffix(h, ".")
	}
	return h
}
