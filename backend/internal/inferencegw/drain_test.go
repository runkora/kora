package inferencegw

import (
	"context"
	"errors"
	"testing"

	openai "github.com/openai/openai-go"
)

// ─── drainOpenAIStream ───────────────────────────────────────────────────────

func TestDrainOpenAIStream_CompletesNormally(t *testing.T) {
	t.Parallel()
	upstream := make(chan OpenAIStreamResult, 2)
	out := make(chan OpenAIStreamResult, 2)

	upstream <- OpenAIStreamResult{Chunk: &openai.ChatCompletionChunk{}}
	upstream <- OpenAIStreamResult{Chunk: &openai.ChatCompletionChunk{}}
	close(upstream)

	_, _, _, err := drainOpenAIStream(context.Background(), upstream, out)
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

	_, _, _, err := drainOpenAIStream(context.Background(), upstream, out)
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

	_, _, _, err := drainOpenAIStream(ctx, upstream, out)
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
