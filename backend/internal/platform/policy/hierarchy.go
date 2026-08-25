package policy

import (
	"regexp"
	"strings"
)

// ScopeNameGrammar is the source form of the regex a policy scope name must
// match. Single source of truth for scope-safe identifiers: any user-supplied
// value that ends up in a scope (PAT scope, virtual model family/name, …) must
// satisfy this grammar so the admission and enforcement sides cannot drift.
const ScopeNameGrammar = "[a-z0-9:_-]{1,255}"

var scopeNameRe = regexp.MustCompile("^" + ScopeNameGrammar + "$")

// IsValidScopeName reports whether s matches ScopeNameGrammar.
func IsValidScopeName(s string) bool { return scopeNameRe.MatchString(s) }

// InvalidScopeNameChars returns the distinct characters in s that violate
// ScopeNameGrammar, in first-seen order. Used to build error messages that
// name the offending character(s).
func InvalidScopeNameChars(s string) []rune {
	seen := map[rune]struct{}{}
	var out []rune
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') ||
			r == '-' || r == '_' || r == ':' {
			continue
		}
		if _, ok := seen[r]; ok {
			continue
		}
		seen[r] = struct{}{}
		out = append(out, r)
	}
	return out
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

// ScopeCovers reports whether scope a covers scope b according to the Orbit
// OAuth scope hierarchy rule:
//
//	A covers B if and only if:
//	  1. Same resource_type (first segment)
//	  2. Same action (last segment)
//	  3. A's path segments are a prefix of B's path segments
func ScopeCovers(a, b string) bool {
	if a == b {
		return true
	}

	partsA := strings.Split(a, ":")
	partsB := strings.Split(b, ":")

	if len(partsA) < 2 || len(partsB) < 2 {
		return false
	}

	if partsA[0] != partsB[0] {
		return false
	}

	if partsA[len(partsA)-1] != partsB[len(partsB)-1] {
		return false
	}

	pathA := partsA[1 : len(partsA)-1]
	pathB := partsB[1 : len(partsB)-1]

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

// AnyScopeCovers returns true if any scope in the granted set covers required.
func AnyScopeCovers(granted []string, required string) bool {
	for _, g := range granted {
		if ScopeCovers(g, required) {
			return true
		}
	}
	return false
}
