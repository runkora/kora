// Package evaluation provides OPA-based policy evaluation for the Policy Gateway.
package evaluation

import "strings"

// ScopeCovers reports whether scope a covers scope b according to the Orbit
// OAuth scope hierarchy rule:
//
//	A covers B if and only if:
//	  1. Same resource_type (first segment)
//	  2. Same action (last segment)
//	  3. A's path segments are a prefix of B's path segments
//
// This mirrors the scope_covers Rego function in authz.rego exactly.
// It is used in the query API to filter candidate resources without a
// full OPA round-trip per candidate.
func ScopeCovers(a, b string) bool {
	if a == b {
		return true
	}

	partsA := strings.Split(a, ":")
	partsB := strings.Split(b, ":")

	if len(partsA) < 2 || len(partsB) < 2 {
		return false
	}

	// Same resource_type (first segment)
	if partsA[0] != partsB[0] {
		return false
	}

	// Same action (last segment)
	if partsA[len(partsA)-1] != partsB[len(partsB)-1] {
		return false
	}

	// Extract middle path segments
	pathA := partsA[1 : len(partsA)-1]
	pathB := partsB[1 : len(partsB)-1]

	// A's path must be a prefix of B's path
	if len(pathA) > len(pathB) {
		return false
	}
	for i, seg := range pathA {
		if seg != pathB[i] {
			return false
		}
	}
	return true
}

// DeriveRequiredScope builds the most specific scope string from resource
// attributes and action. The calling service provides type/provider/path
// details; this function concatenates them into the canonical scope format.
//
// Example:
//
//	DeriveRequiredScope("inference", []string{"anthropic", "claude-sonnet-4-6"}, "invoke")
//	→ "inference:anthropic:claude-sonnet-4-6:invoke"
func DeriveRequiredScope(resourceType string, pathSegments []string, action string) string {
	if len(pathSegments) == 0 {
		return resourceType + ":" + action
	}
	return resourceType + ":" + strings.Join(pathSegments, ":") + ":" + action
}

// AnyScopeCovers returns true if any scope in the granted set covers required.
func AnyScopeCovers(granted []string, required string) bool {
	for _, g := range granted {
		if ScopeCovers(g, required) {
			return true
		}
	}
	return false
}
