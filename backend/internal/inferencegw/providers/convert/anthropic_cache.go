package convert

import (
	anthropic "github.com/anthropics/anthropic-sdk-go"
)

// ephemeralBreakpoint is the marker Anthropic reads as "cache everything up to
// here". The 5m TTL is Anthropic's default and is written out explicitly
// because the SDK omits a zero-valued cache_control from the request.
func ephemeralBreakpoint() anthropic.CacheControlEphemeralParam {
	return anthropic.CacheControlEphemeralParam{TTL: anthropic.CacheControlEphemeralTTLTTL5m}
}

// applyPromptCaching marks the cacheable prefix of a bridged request. The
// Responses and OpenAI dialects have no field for cache_control, so without
// this every Claude call through them pays full input rate on tokens the
// provider would otherwise serve from cache at a tenth of the price.
//
// Three breakpoints, well inside Anthropic's limit of four and independent of
// conversation length: the tool definitions and system prompt are the stable
// head of the prompt, and the final message closes the prefix that the next
// call will match against. Anthropic ignores a breakpoint whose prefix is
// below the model's cacheable minimum, so short requests are unaffected.
func applyPromptCaching(p *anthropic.MessageNewParams) {
	if n := len(p.Tools); n > 0 {
		if cc := p.Tools[n-1].GetCacheControl(); cc != nil {
			*cc = ephemeralBreakpoint()
		}
	}
	if n := len(p.System); n > 0 {
		p.System[n-1].CacheControl = ephemeralBreakpoint()
	}
	if n := len(p.Messages); n > 0 {
		blocks := p.Messages[n-1].Content
		if b := len(blocks); b > 0 {
			if cc := blocks[b-1].GetCacheControl(); cc != nil {
				*cc = ephemeralBreakpoint()
			}
		}
	}
}
