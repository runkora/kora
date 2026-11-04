package convert

import (
	"context"
	"errors"
	"io"

	anthropic "github.com/anthropics/anthropic-sdk-go"

	"github.com/runkora/kora/backend/internal/inferencegw"
)

// AnthropicStream is the server-sent event stream the Anthropic SDK returns for
// a streaming messages call.
type AnthropicStream interface {
	Next() bool
	Current() anthropic.MessageStreamEventUnion
	Err() error
	Close() error
}

// AccumulateAnthropicStream reads s to the end, reporting acceptance via
// inferencegw.NotifyAccepted on the first event, closes it and returns the
// assembled message with the number of events read. Errors are mapped for provider.
func AccumulateAnthropicStream(ctx context.Context, s AnthropicStream, provider inferencegw.ProviderName) (*anthropic.Message, int, error) {
	defer func() { _ = s.Close() }()
	var msg anthropic.Message
	events := 0
	for s.Next() {
		if events == 0 {
			// First event: the upstream accepted the request.
			inferencegw.NotifyAccepted(ctx)
		}
		events++
		if err := msg.Accumulate(s.Current()); err != nil {
			return nil, events, MapAnthropicError(provider, err)
		}
	}
	if err := s.Err(); err != nil && !errors.Is(err, io.EOF) {
		return nil, events, MapAnthropicError(provider, err)
	}
	return &msg, events, nil
}

// AnthropicStreamToOpenAIChunks relays s as OpenAI chat completion chunks on a
// channel, then a mapped error if the stream failed, and closes the channel.
// done, when set, runs at the end with the event count and the unmapped error.
func AnthropicStreamToOpenAIChunks(s AnthropicStream, provider inferencegw.ProviderName, done func(events int, err error)) <-chan inferencegw.OpenAIStreamResult {
	ch := make(chan inferencegw.OpenAIStreamResult, 32)
	go func() {
		defer close(ch)
		defer func() { _ = s.Close() }()
		state := NewAnthropicStreamState()
		events := 0
		for s.Next() {
			events++
			event := s.Current()
			if chunk, ok := AnthropicEventToOpenAIChunk(event, state); ok {
				res := inferencegw.OpenAIStreamResult{Chunk: chunk}
				if event.Type == "message_delta" {
					res.CacheCreationTokens = state.CacheCreationTokens
				}
				ch <- res
			}
		}
		err := s.Err()
		if errors.Is(err, io.EOF) {
			err = nil
		}
		if err != nil {
			ch <- inferencegw.OpenAIStreamResult{Err: MapAnthropicError(provider, err)}
		}
		if done != nil {
			done(events, err)
		}
	}()
	return ch
}
