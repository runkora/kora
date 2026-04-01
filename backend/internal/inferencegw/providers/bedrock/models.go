package bedrock

import (
	"slices"
	"strings"

	bedrockSDKtypes "github.com/aws/aws-sdk-go-v2/service/bedrock/types"

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
// Bedrock model IDs (e.g. "anthropic.claude-..." -> strip "anthropic.").
var knownNamespacePrefixes = []string{
	"anthropic.", "amazon.", "meta.", "mistral.", "cohere.",
	"ai21.", "deepseek.", "writer.", "google.", "nvidia.",
	"qwen.", "openai.", "moonshotai.", "moonshot.", "minimax.",
	"zai.", "twelvelabs.", "stability.",
}

// titleCaseSlug converts a hyphen/underscore-separated slug to Title Case.
// e.g. "nova-pro-v1" -> "Nova Pro V1".
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
// e.g. "apac.anthropic.claude-3-haiku-20240307-v1:0" -> "anthropic.claude-3-haiku-20240307-v1:0"
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
//  1. Strip any region prefix ("apac.", "au.", "global.", "us.", ...) -> base ID
//     e.g. "apac.anthropic.claude-3-haiku-20240307-v1:0" -> "anthropic.claude-3-haiku-20240307-v1:0"
//  2. Strip the provider namespace ("anthropic.", "amazon.", etc.) -> short slug
//     e.g. "anthropic.claude-3-haiku-20240307-v1:0" -> "claude-3-haiku-20240307-v1:0"
//  3. Strip trailing version suffix ":N"
//     e.g. "claude-3-haiku-20240307-v1:0" -> "claude-3-haiku-20240307-v1"
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

	// Step 3: strip trailing ":0" version suffix only.
	// We only strip ":0" (the initial revision) because higher version numbers
	// (":1", ":2", ...) identify distinct, non-backward-compatible model revisions --
	// e.g. amazon.nova-reel-v1:0 and amazon.nova-reel-v1:1 are separate FM
	// versions that would otherwise both collapse to the same alias "nova-reel-v1".
	if i := strings.LastIndex(shortID, ":"); i >= 0 && shortID[i+1:] == "0" {
		add(shortID[:i])
	}

	return results
}

// mapBedrockModality converts a Bedrock SDK ModelModality enum value to our
// internal Modality type.
//
// NOTE: The Bedrock SDK enum only defines TEXT, IMAGE, EMBEDDING. VIDEO is
// returned by the API (e.g. amazon.nova-reel) but has not been added to the
// typed enum yet, so we match it by raw string value.
func mapBedrockModality(m bedrockSDKtypes.ModelModality) (inferencegw.Modality, bool) {
	switch m {
	case bedrockSDKtypes.ModelModalityText:
		return inferencegw.ModalityText, true
	case bedrockSDKtypes.ModelModalityImage:
		return inferencegw.ModalityImage, true
	case bedrockSDKtypes.ModelModalityEmbedding:
		return inferencegw.ModalityEmbedding, true
	case "VIDEO":
		// Not yet in the typed enum — matched by raw string value.
		return inferencegw.ModalityVideo, true
	default:
		return "", false
	}
}

// convertModalities maps a slice of Bedrock SDK modalities to our internal type,
// ignoring any unknown values the SDK may add in future.
func convertModalities(src []bedrockSDKtypes.ModelModality) []inferencegw.Modality {
	if len(src) == 0 {
		return nil
	}
	out := make([]inferencegw.Modality, 0, len(src))
	for _, m := range src {
		if mapped, ok := mapBedrockModality(m); ok {
			out = append(out, mapped)
		}
	}
	return out
}

// inferModalities derives input/output modalities from a model ID string when
// the API did not return modality data (e.g. for inference profiles whose base
// FM was not present in ListFoundationModels). This is a best-effort fallback
// based on well-known ID patterns.
func inferModalities(id string) (input, output []inferencegw.Modality) {
	lower := strings.ToLower(id)
	switch {
	case strings.Contains(lower, "embed"):
		return []inferencegw.Modality{inferencegw.ModalityText},
			[]inferencegw.Modality{inferencegw.ModalityEmbedding}
	case strings.Contains(lower, "nova-reel"):
		// Nova Reel is a video generation model: text+image in, video out.
		return []inferencegw.Modality{inferencegw.ModalityText, inferencegw.ModalityImage},
			[]inferencegw.Modality{inferencegw.ModalityVideo}
	case strings.Contains(lower, "titan-image") ||
		strings.Contains(lower, "nova-canvas") ||
		strings.Contains(lower, "stable-image") ||
		strings.Contains(lower, "stable-diffusion"):
		return []inferencegw.Modality{inferencegw.ModalityText, inferencegw.ModalityImage},
			[]inferencegw.Modality{inferencegw.ModalityImage}
	case strings.Contains(lower, "rerank"):
		// Rerankers accept text pairs and score them; no text generation output.
		return []inferencegw.Modality{inferencegw.ModalityText},
			[]inferencegw.Modality{inferencegw.ModalityText}
	default:
		return []inferencegw.Modality{inferencegw.ModalityText},
			[]inferencegw.Modality{inferencegw.ModalityText}
	}
}

// computeDialects returns the gateway wire protocols supported for a model.
// Rules:
//   - No text in output -> no chat dialect (embedding, image-gen, reranker).
//   - Text output + Anthropic model ID -> ["openai", "anthropic"].
//   - Text output + non-Anthropic model ID -> ["openai"].
func computeDialects(canonicalID string, outputMods []inferencegw.Modality) []inferencegw.APIDialect {
	hasTextOutput := slices.Contains(outputMods, inferencegw.ModalityText)
	if !hasTextOutput {
		return nil
	}
	if isAnthropicModel(canonicalID) {
		return []inferencegw.APIDialect{inferencegw.DialectOpenAI, inferencegw.DialectAnthropic}
	}
	return []inferencegw.APIDialect{inferencegw.DialectOpenAI}
}

// computeCapabilities derives the endpoint families a model supports from its
// output modalities and model ID. The rules mirror the three-layer taxonomy
// described in ADR-0026:
//
//   - text output + non-reranker  → CapabilityChat
//   - embedding output            → CapabilityEmbedding
//   - image output                → CapabilityImageGeneration
//   - video output                → CapabilityVideoGeneration
//   - "rerank" in model ID        → CapabilityRerank (overrides text-output chat)
func computeCapabilities(canonicalID string, outputMods []inferencegw.Modality) []inferencegw.ModelCapability {
	lower := strings.ToLower(canonicalID)
	if strings.Contains(lower, "rerank") {
		return []inferencegw.ModelCapability{inferencegw.CapabilityRerank}
	}
	var caps []inferencegw.ModelCapability
	for _, m := range outputMods {
		switch m {
		case inferencegw.ModalityText:
			caps = append(caps, inferencegw.CapabilityChat)
		case inferencegw.ModalityEmbedding:
			caps = append(caps, inferencegw.CapabilityEmbedding)
		case inferencegw.ModalityImage:
			caps = append(caps, inferencegw.CapabilityImageGeneration)
		case inferencegw.ModalityVideo:
			caps = append(caps, inferencegw.CapabilityVideoGeneration)
		}
	}
	return caps
}

// buildProviderModel constructs a ProviderModel for the given canonical Bedrock
// model ID. name is the human-readable name returned by the API; if empty,
// a fallback is derived from the model ID. inputMods/outputMods come from the
// API response; if nil, they are inferred from the model ID.
func buildProviderModel(canonicalID, name string, inputMods, outputMods []inferencegw.Modality) inferencegw.ProviderModel {
	if name == "" {
		name = fallbackDisplayName(canonicalID)
	}
	// Fall back to pattern inference when modality data is absent OR empty.
	// An empty (non-nil) slice can result when the API returns modality values
	// that are not yet in the SDK typed enum (e.g. "VIDEO" for nova-reel was
	// dropped by convertModalities, producing []Modality{} instead of nil).
	if len(inputMods) == 0 || len(outputMods) == 0 {
		inputMods, outputMods = inferModalities(canonicalID)
	}
	return inferencegw.ProviderModel{
		Provider:         inferencegw.ProviderBedrock,
		ModelID:          canonicalID,
		DisplayName:      name,
		Aliases:          aliases(canonicalID),
		InputModalities:  inputMods,
		OutputModalities: outputMods,
		Dialects:         computeDialects(canonicalID, outputMods),
		Capabilities:     computeCapabilities(canonicalID, outputMods),
	}
}
