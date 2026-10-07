package compatible

import (
	"slices"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"

	"github.com/clear-route/orbit/internal/inferencegw"
)

// foldTrailingSystem moves the text of the system messages that end a request
// into the user turn before them. Some hosts of non-Anthropic models continue a
// trailing system message as reasoning and never start the assistant turn.
// Requests for Anthropic models are returned unchanged.
func foldTrailingSystem(params anthropic.MessageNewParams) anthropic.MessageNewParams {
	if isAnthropicModel(params.Model) {
		return params
	}
	msgs := params.Messages
	end := len(msgs)
	for end > 0 && msgs[end-1].Role == anthropic.MessageParamRoleSystem {
		end--
	}
	if end == len(msgs) || end == 0 || msgs[end-1].Role != anthropic.MessageParamRoleUser {
		return params
	}
	user := msgs[end-1]
	user.Content = slices.Clone(user.Content)
	for _, m := range msgs[end:] {
		for _, b := range m.Content {
			if b.OfText != nil {
				user.Content = append(user.Content, b)
			}
		}
	}
	params.Messages = append(slices.Clone(msgs[:end-1]), user)
	return params
}

// isAnthropicModel also matches a Claude id behind another vendor prefix, as
// proxies list them (e.g. "bedrock/anthropic.claude-x").
func isAnthropicModel(id string) bool {
	return inferencegw.InferFamily(id) == "anthropic" || strings.Contains(strings.ToLower(id), "claude")
}
