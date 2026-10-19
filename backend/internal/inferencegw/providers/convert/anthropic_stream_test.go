package convert

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"

	"github.com/runkora/kora/backend/internal/inferencegw"
)

// fakeStream replays events, then reports err.
type fakeStream struct {
	events []anthropic.MessageStreamEventUnion
	i      int
	err    error
	closed bool
}

func (f *fakeStream) Next() bool { f.i++; return f.i <= len(f.events) }
func (f *fakeStream) Current() anthropic.MessageStreamEventUnion {
	return f.events[f.i-1]
}
func (f *fakeStream) Err() error   { return f.err }
func (f *fakeStream) Close() error { f.closed = true; return nil }

func streamOf(t *testing.T, err error, raw ...string) *fakeStream {
	t.Helper()
	s := &fakeStream{err: err}
	for _, r := range raw {
		var ev anthropic.MessageStreamEventUnion
		if e := json.Unmarshal([]byte(r), &ev); e != nil {
			t.Fatal(e)
		}
		s.events = append(s.events, ev)
	}
	return s
}

func textTurn(t *testing.T, err error) *fakeStream {
	return streamOf(t, err,
		`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-x","content":[],"usage":{"input_tokens":12,"output_tokens":1,"cache_creation_input_tokens":5}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}`,
		`{"type":"message_stop"}`,
	)
}

func TestAccumulateAnthropicStreamAssemblesTheMessage(t *testing.T) {
	s := textTurn(t, io.EOF)
	msg, events, err := AccumulateAnthropicStream(s, "compatible:x")
	if err != nil {
		t.Fatal(err)
	}
	if events != 6 || !s.closed {
		t.Errorf("events = %d, closed = %v", events, s.closed)
	}
	if msg.StopReason != anthropic.StopReasonEndTurn || msg.Usage.InputTokens != 12 || msg.Usage.OutputTokens != 7 || msg.Usage.CacheCreationInputTokens != 5 {
		t.Errorf("message = %+v", msg)
	}
	if len(msg.Content) != 1 || msg.Content[0].Text != "hello" {
		t.Errorf("content = %+v", msg.Content)
	}
}

func TestAccumulateAnthropicStreamMapsAStreamError(t *testing.T) {
	s := textTurn(t, errors.New("connection reset"))
	_, _, err := AccumulateAnthropicStream(s, "compatible:x")
	var ce *inferencegw.ConnectorError
	if !errors.As(err, &ce) || ce.Provider != "compatible:x" {
		t.Errorf("err = %v, want a ConnectorError for compatible:x", err)
	}
	if !s.closed {
		t.Error("stream not closed")
	}
}

func TestAccumulateAnthropicStreamMapsAnOutOfOrderEvent(t *testing.T) {
	s := streamOf(t, nil, `{"type":"content_block_delta","index":3,"delta":{"type":"text_delta","text":"x"}}`)
	if _, _, err := AccumulateAnthropicStream(s, "compatible:x"); err == nil {
		t.Error("want an error for a delta with no content block")
	}
}

func TestAnthropicStreamToOpenAIChunksRelaysChunksAndUsage(t *testing.T) {
	var doneEvents int
	var doneErr error
	ch := AnthropicStreamToOpenAIChunks(textTurn(t, nil), "compatible:x", func(n int, err error) { doneEvents, doneErr = n, err })

	var text strings.Builder
	var finish string
	var cacheCreation int64
	var usage int64
	for res := range ch {
		if res.Err != nil {
			t.Fatal(res.Err)
		}
		for _, c := range res.Chunk.Choices {
			text.WriteString(c.Delta.Content)
			if c.FinishReason != "" {
				finish = c.FinishReason
			}
		}
		if res.Chunk.Usage.TotalTokens > 0 {
			usage = res.Chunk.Usage.CompletionTokens
		}
		if res.CacheCreationTokens > 0 {
			cacheCreation = res.CacheCreationTokens
		}
	}
	if text.String() != "hello" || finish != "stop" || usage != 7 || cacheCreation != 5 {
		t.Errorf("text %q, finish %q, completion tokens %d, cache creation %d", text.String(), finish, usage, cacheCreation)
	}
	if doneEvents != 6 || doneErr != nil {
		t.Errorf("done(%d, %v), want 6 events and no error", doneEvents, doneErr)
	}
}

func TestAnthropicStreamToOpenAIChunksEndsWithTheMappedError(t *testing.T) {
	var doneErr error
	ch := AnthropicStreamToOpenAIChunks(textTurn(t, errors.New("boom")), "bedrock:us", func(_ int, err error) { doneErr = err })

	var last inferencegw.OpenAIStreamResult
	for res := range ch {
		last = res
	}
	var ce *inferencegw.ConnectorError
	if !errors.As(last.Err, &ce) || ce.Provider != "bedrock:us" {
		t.Errorf("last result = %+v, want a mapped error", last)
	}
	if doneErr == nil || doneErr.Error() != "boom" {
		t.Errorf("done err = %v, want the unmapped error", doneErr)
	}
}

func TestAnthropicStreamToOpenAIChunksTreatsEOFAsTheEnd(t *testing.T) {
	for res := range AnthropicStreamToOpenAIChunks(textTurn(t, io.EOF), "compatible:x", nil) {
		if res.Err != nil {
			t.Fatalf("got error %v for EOF", res.Err)
		}
	}
}
