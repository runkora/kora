package bedrock

import (
	"strings"

	"github.com/clear-route/orbit/internal/inferencegw"
)

// knownRegionPrefixes are the cross-region inference profile prefixes that
// Bedrock prepends to a base model ID. Sourced from the real
// ListInferenceProfiles API — the set varies by region and expands over time.
var knownRegionPrefixes = []string{
	"apac.", "au.", "global.",
	"us.", "eu.", "ap.", // US/EU/APAC cross-region prefixes
}

// knownNamespacePrefixes are the provider namespace segments embedded in
// Bedrock model IDs (e.g. "anthropic.claude-..." → strip "anthropic.").
var knownNamespacePrefixes = []string{
	"anthropic.", "amazon.", "meta.", "mistral.", "cohere.",
	"ai21.", "deepseek.", "writer.", "google.", "nvidia.",
	"qwen.", "openai.", "moonshotai.", "moonshot.", "minimax.",
	"zai.", "twelvelabs.",
}

// titleCaseSlug converts a hyphen/underscore-separated slug to Title Case.
// e.g. "nova-pro-v1" → "Nova Pro V1".
// Used only as a fallback when the API provides no model name.
func titleCaseSlug(s string) string {
	words := strings.FieldsFunc(s, func(r rune) bool { return r == '-' || r == '_' })
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// fallbackDisplayName derives a human-readable name from a Bedrock model ID
// when the API did not return a ModelName. Takes the last segment after "." or
// "/" and title-cases it, stripping trailing ":N" version suffixes.
func fallbackDisplayName(modelID string) string {
	slug := modelID
	if i := strings.LastIndexAny(slug, "./"); i >= 0 {
		slug = slug[i+1:]
	}
	if i := strings.LastIndex(slug, ":"); i >= 0 {
		slug = slug[:i]
	}
	return titleCaseSlug(slug)
}

// stripRegionPrefix removes any known cross-region prefix from a model ID,
// returning the base FM ID.
// e.g. "apac.anthropic.claude-3-haiku-20240307-v1:0" → "anthropic.claude-3-haiku-20240307-v1:0"
func stripRegionPrefix(id string) string {
	for _, pfx := range knownRegionPrefixes {
		if strings.HasPrefix(id, pfx) {
			return id[len(pfx):]
		}
	}
	return id
}

// aliases generates clean, stable alias IDs for a canonical Bedrock model ID.
// The returned slice never contains the canonical ID itself.
//
// Logic:
//  1. Strip any region prefix ("apac.", "au.", "global.", "us.", …) → base ID
//     e.g. "apac.anthropic.claude-3-haiku-20240307-v1:0" → "anthropic.claude-3-haiku-20240307-v1:0"
//  2. Strip the provider namespace ("anthropic.", "amazon.", etc.) → short slug
//     e.g. "anthropic.claude-3-haiku-20240307-v1:0" → "claude-3-haiku-20240307-v1:0"
//  3. Strip trailing version suffix ":N"
//     e.g. "claude-3-haiku-20240307-v1:0" → "claude-3-haiku-20240307-v1"
func aliases(canonicalID string) []string {
	var results []string
	seen := map[string]bool{canonicalID: true}

	add := func(s string) {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			results = append(results, s)
			seen[s] = true
		}
	}

	// Step 1: strip region prefix.
	baseID := stripRegionPrefix(canonicalID)
	add(baseID)

	// Step 2: strip provider namespace.
	shortID := baseID
	for _, ns := range knownNamespacePrefixes {
		if strings.HasPrefix(baseID, ns) {
			shortID = baseID[len(ns):]
			break
		}
	}
	add(shortID)

	// Step 3: strip trailing ":N" version suffix.
	if i := strings.LastIndex(shortID, ":"); i >= 0 {
		add(shortID[:i])
	}

	return results
}

// buildProviderModel constructs a ProviderModel for the given canonical Bedrock
// model ID. name is the human-readable name returned by the API; if empty,
// a fallback is derived from the model ID.
func buildProviderModel(canonicalID, name string) inferencegw.ProviderModel {
	if name == "" {
		name = fallbackDisplayName(canonicalID)
	}
	return inferencegw.ProviderModel{
		Provider:    inferencegw.ProviderBedrock,
		ModelID:     canonicalID,
		DisplayName: name,
		Aliases:     aliases(canonicalID),
	}
}
