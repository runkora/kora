package inferencegw

import "strings"

// familyRules map model id substrings to vendor families. The first match wins.
var familyRules = []struct{ contains, family string }{
	{"claude", "anthropic"},
	{"gpt", "openai"},
	{"dall-e", "openai"},
	{"dalle", "openai"},
	{"whisper", "openai"},
	{"text-embedding", "openai"},
	{"llama", "meta"},
	{"mistral", "mistral"},
	{"mixtral", "mistral"},
	{"gemini", "google"},
	{"gemma", "google"},
	{"deepseek", "deepseek"},
	{"qwen", "qwen"},
	{"cohere", "cohere"},
	{"command", "cohere"},
	{"phi-", "microsoft"},
	{"nova", "amazon"},
	{"titan", "amazon"},
}

// InferFamily names the vendor of a model known only by its id. An explicit
// vendor prefix ("anthropic/claude-x", as OpenRouter lists them) wins over
// substring rules. It returns "unknown" when nothing matches.
func InferFamily(modelID string) string {
	id := strings.ToLower(modelID)
	if vendor, _, ok := strings.Cut(id, "/"); ok && vendor != "" {
		return vendor
	}
	if len(id) >= 2 && id[0] == 'o' && id[1] >= '1' && id[1] <= '9' {
		return "openai"
	}
	for _, r := range familyRules {
		if strings.Contains(id, r.contains) {
			return r.family
		}
	}
	return "unknown"
}
