package convert

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/openai/openai-go/v3/responses"
)

// joinToolName is the Anthropic tool name for a function in a Responses
// namespace, or the function's own name outside one.
func joinToolName(namespace, name string) string {
	if namespace == "" {
		return name
	}
	joined := namespace + "__" + name
	// Anthropic caps tool names at 64 characters. A longer name keeps its start
	// and a hash of the whole, and ToolNames maps it back.
	if len(joined) > maxAnthropicToolName {
		sum := sha256.Sum256([]byte(joined))
		joined = joined[:maxAnthropicToolName-9] + "_" + hex.EncodeToString(sum[:4])
	}
	return joined
}

const maxAnthropicToolName = 64

// ToolNames maps the Anthropic name of each namespaced function back to its
// Responses namespace and name, which a function_call must carry.
type ToolNames map[string][2]string

// ResponsesToolNames builds the ToolNames for a request's tools.
func ResponsesToolNames(tools []responses.ToolUnionParam) ToolNames {
	names := ToolNames{}
	for _, t := range tools {
		if t.OfNamespace == nil {
			continue
		}
		for _, inner := range t.OfNamespace.Tools {
			if fn := inner.OfFunction; fn != nil {
				names[joinToolName(t.OfNamespace.Name, fn.Name)] = [2]string{t.OfNamespace.Name, fn.Name}
			}
		}
	}
	return names
}

// split returns the namespace and name for an Anthropic tool name.
func (n ToolNames) split(anthropicName string) (namespace, name string) {
	if v, ok := n[anthropicName]; ok {
		return v[0], v[1]
	}
	return "", anthropicName
}
