package policy

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// scopeSegmentCharClass excludes ':' — segments are joined by ':' in a full
// scope, so allowing it inside a segment would corrupt the scope hierarchy.
const scopeSegmentCharClass = "a-z0-9_-"

// scopeNameCharClass is the segment class plus ':' as the segment separator.
// The ':' is placed between the ranges and the standalone chars so the class
// reads left-to-right as [a-z0-9:_-] — the conventional order — rather than
// [:a-z0-9_-] which can be misread as POSIX character-class syntax.
const scopeNameCharClass = "a-z0-9:_-"

// ScopeSegmentGrammar constrains a single scope segment (no ':') — e.g. the
// virtual model family/name in "inference:{family}:{name}:use".
const ScopeSegmentGrammar = "[" + scopeSegmentCharClass + "]{1,255}"

// ScopeNameGrammar constrains a full, possibly multi-segment scope string
// (e.g. a PAT scope).
const ScopeNameGrammar = "[" + scopeNameCharClass + "]{1,255}"

var (
	scopeSegmentRe        = regexp.MustCompile("^" + ScopeSegmentGrammar + "$")
	scopeSegmentInvalidRe = regexp.MustCompile("[^" + scopeSegmentCharClass + "]")
	scopeNameRe           = regexp.MustCompile("^" + ScopeNameGrammar + "$")
)

func IsValidScopeSegment(s string) bool { return scopeSegmentRe.MatchString(s) }
func IsValidScopeName(s string) bool    { return scopeNameRe.MatchString(s) }

// InvalidScopeSegmentChars returns the distinct offending runes in first-seen
// order, for building error messages that name what's wrong.
func InvalidScopeSegmentChars(s string) []rune {
	matches := scopeSegmentInvalidRe.FindAllString(s, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[rune]struct{}, len(matches))
	out := make([]rune, 0, len(matches))
	for _, m := range matches {
		// Go's regexp treats character classes as rune classes on UTF-8
		// input, so each match is exactly one whole rune — never a partial
		// byte. Covered by the "multi-byte rune in name" test case.
		r, _ := utf8.DecodeRuneInString(m)
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
