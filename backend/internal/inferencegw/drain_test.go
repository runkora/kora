package inferencegw

import (
	"context"
	"errors"
	"testing"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

// ─── drainOpenAIStream ───────────────────────────────────────────────────────

func TestDrainOpenAIStream_CompletesNormally(t *testing.T) {
	t.Parallel()
	upstream := make(chan OpenAIStreamResult, 2)
	out := make(chan OpenAIStreamResult, 2)

	upstream <- OpenAIStreamResult{Chunk: &openai.ChatCompletionChunk{}}
	upstream <- OpenAIStreamResult{Chunk: &openai.ChatCompletionChunk{}}
	close(upstream)

	_, _, _, _, err := drainOpenAIStream(context.Background(), upstream, out)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 forwarded chunks, got %d", len(out))
	}
}

func TestDrainOpenAIStream_PropagatesUpstreamError(t *testing.T) {
	t.Parallel()
	upstream := make(chan OpenAIStreamResult, 1)
	out := make(chan OpenAIStreamResult, 1)

	sentinel := errors.New("provider boom")
	upstream <- OpenAIStreamResult{Err: sentinel}
	close(upstream)

	_, _, _, _, err := drainOpenAIStream(context.Background(), upstream, out)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected upstream error, got %v", err)
	}
}

func TestDrainOpenAIStream_ClientDisconnect(t *testing.T) {
	t.Parallel()
	upstream := make(chan OpenAIStreamResult) // unbuffered, never closes
	out := make(chan OpenAIStreamResult, 1)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before drain starts — outer select fires immediately

	_, _, _, _, err := drainOpenAIStream(ctx, upstream, out)
	if !errors.Is(err, errClientDisconnected) {
		t.Fatalf("expected errClientDisconnected, got %v", err)
	}
}

// ─── drainAnthropicStream ────────────────────────────────────────────────────

func TestDrainAnthropicStream_CompletesNormally(t *testing.T) {
	t.Parallel()
	upstream := make(chan AnthropicStreamEvent, 2)
	out := make(chan AnthropicStreamEvent, 2)

	upstream <- AnthropicStreamEvent{}
	upstream <- AnthropicStreamEvent{}
	close(upstream)

	_, err := drainAnthropicStream(context.Background(), upstream, out)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 forwarded events, got %d", len(out))
	}
}

func TestDrainAnthropicStream_PropagatesUpstreamError(t *testing.T) {
	t.Parallel()
	upstream := make(chan AnthropicStreamEvent, 1)
	out := make(chan AnthropicStreamEvent, 1)

	sentinel := errors.New("provider boom")
	upstream <- AnthropicStreamEvent{Err: sentinel}
	close(upstream)

	_, err := drainAnthropicStream(context.Background(), upstream, out)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected upstream error, got %v", err)
	}
}

func TestDrainAnthropicStream_ClientDisconnect(t *testing.T) {
	t.Parallel()
	upstream := make(chan AnthropicStreamEvent) // unbuffered, never closes
	out := make(chan AnthropicStreamEvent, 1)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before drain starts — outer select fires immediately

	_, err := drainAnthropicStream(ctx, upstream, out)
	if !errors.Is(err, errClientDisconnected) {
		t.Fatalf("expected errClientDisconnected, got %v", err)
	}
}

// ─── drainOpenAIStream cache token extraction ────────────────────────────────

func TestDrainOpenAIStream_ExtractsCacheReadTokens(t *testing.T) {
	t.Parallel()
	upstream := make(chan OpenAIStreamResult, 2)
	out := make(chan OpenAIStreamResult, 2)

	// Providers that support prompt caching send cached tokens in the usage chunk.
	// Use two chunks to verify additive accumulation across chunks.
	upstream <- OpenAIStreamResult{Chunk: &openai.ChatCompletionChunk{
		Usage: openai.CompletionUsage{
			PromptTokens:        100,
			PromptTokensDetails: openai.CompletionUsagePromptTokensDetails{CachedTokens: 40},
		},
	}}
	upstream <- OpenAIStreamResult{Chunk: &openai.ChatCompletionChunk{
		Usage: openai.CompletionUsage{
			PromptTokensDetails: openai.CompletionUsagePromptTokensDetails{CachedTokens: 10},
		},
	}}
	close(upstream)

	_, _, _, cacheRead, err := drainOpenAIStream(context.Background(), upstream, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cacheRead != 50 {
		t.Errorf("cacheReadTokens = %d, want 50 (40+10 from two chunks)", cacheRead)
	}
}

// ─── drainResponsesStream ────────────────────────────────────────────────────

func TestDrainResponsesStream_CompletesNormally(t *testing.T) {
	t.Parallel()
	upstream := make(chan ResponsesStreamResult, 3)
	out := make(chan ResponsesStreamResult, 3)

	upstream <- ResponsesStreamResult{Event: responses.ResponseStreamEventUnion{Type: "response.created"}}
	upstream <- ResponsesStreamResult{Event: responses.ResponseStreamEventUnion{Type: "response.output_text.delta"}}
	upstream <- ResponsesStreamResult{Event: responses.ResponseStreamEventUnion{
		Type: "response.completed",
		Response: responses.Response{
			Usage: responses.ResponseUsage{InputTokens: 10, OutputTokens: 4, TotalTokens: 14},
		},
	}}
	close(upstream)

	inputTokens, outputTokens, _, err := drainResponsesStream(context.Background(), upstream, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inputTokens != 10 {
		t.Errorf("inputTokens = %d, want 10", inputTokens)
	}
	if outputTokens != 4 {
		t.Errorf("outputTokens = %d, want 4", outputTokens)
	}
	if len(out) != 3 {
		t.Errorf("forwarded %d events, want 3", len(out))
	}
}

func TestDrainResponsesStream_ExtractsCacheReadTokens(t *testing.T) {
	t.Parallel()
	upstream := make(chan ResponsesStreamResult, 1)
	out := make(chan ResponsesStreamResult, 1)

	upstream <- ResponsesStreamResult{Event: responses.ResponseStreamEventUnion{
		Type: "response.completed",
		Response: responses.Response{
			Usage: responses.ResponseUsage{
				InputTokens:  100,
				OutputTokens: 20,
				InputTokensDetails: responses.ResponseUsageInputTokensDetails{
					CachedTokens: 35,
				},
			},
		},
	}}
	close(upstream)

	_, _, cacheRead, err := drainResponsesStream(context.Background(), upstream, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cacheRead != 35 {
		t.Errorf("cacheReadTokens = %d, want 35", cacheRead)
	}
}

func TestDrainResponsesStream_IncompleteExtractsTokens(t *testing.T) {
	t.Parallel()
	upstream := make(chan ResponsesStreamResult, 1)
	out := make(chan ResponsesStreamResult, 1)

	// response.incomplete is the terminal event when the model hits max_output_tokens
	// via a native provider (Azure, Vertex, Mantle). It carries the same Usage
	// structure as response.completed and must be billed correctly.
	upstream <- ResponsesStreamResult{Event: responses.ResponseStreamEventUnion{
		Type: "response.incomplete",
		Response: responses.Response{
			Usage: responses.ResponseUsage{
				InputTokens:  80,
				OutputTokens: 40,
			},
		},
	}}
	close(upstream)

	inputTokens, outputTokens, _, err := drainResponsesStream(context.Background(), upstream, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inputTokens != 80 {
		t.Errorf("inputTokens = %d, want 80", inputTokens)
	}
	if outputTokens != 40 {
		t.Errorf("outputTokens = %d, want 40", outputTokens)
	}
}

func TestDrainResponsesStream_PropagatesUpstreamError(t *testing.T) {
	t.Parallel()
	upstream := make(chan ResponsesStreamResult, 1)
	out := make(chan ResponsesStreamResult, 1)

	sentinel := errors.New("provider error")
	upstream <- ResponsesStreamResult{Err: sentinel}
	close(upstream)

	_, _, _, err := drainResponsesStream(context.Background(), upstream, out)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected upstream error, got %v", err)
	}
}

func TestDrainResponsesStream_ResponseFailed(t *testing.T) {
	t.Parallel()
	upstream := make(chan ResponsesStreamResult, 1)
	out := make(chan ResponsesStreamResult, 2)

	upstream <- ResponsesStreamResult{Event: responses.ResponseStreamEventUnion{Type: "response.failed"}}
	close(upstream)

	_, _, _, err := drainResponsesStream(context.Background(), upstream, out)
	if !errors.Is(err, ErrProviderError) {
		t.Fatalf("expected ErrProviderError for response.failed, got %v", err)
	}
}

func TestDrainResponsesStream_ClientDisconnect(t *testing.T) {
	t.Parallel()
	upstream := make(chan ResponsesStreamResult) // unbuffered, never closes
	out := make(chan ResponsesStreamResult, 1)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, _, err := drainResponsesStream(ctx, upstream, out)
	if !errors.Is(err, errClientDisconnected) {
		t.Fatalf("expected errClientDisconnected, got %v", err)
	}
}
