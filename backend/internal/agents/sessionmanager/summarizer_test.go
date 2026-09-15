package sessionmanager

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/clear-route/orbit/internal/platform/logging"
)

func TestSummarizer_CallsMessagesEndpointWithModel(t *testing.T) {
	var gotPath, gotModel, gotUserMsg string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		var req messagesRequest
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &req)
		gotModel = req.Model
		if len(req.Messages) > 0 {
			gotUserMsg = req.Messages[0].Content
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"content":[{"type":"text","text":"Summarize the auth refactor"}]}`)) //nolint:errcheck
	}))
	defer srv.Close()

	s := newSummarizer(logging.NewLogger())
	got, err := s.Summarize(context.Background(), srv.URL+"/run/run-1", "anthropic/claude-sonnet-4-6", "please refactor the auth module")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "Summarize the auth refactor" {
		t.Errorf("summary = %q, want %q", got, "Summarize the auth refactor")
	}
	if gotPath != "/run/run-1/v1/messages" {
		t.Errorf("path = %q, want /run/run-1/v1/messages", gotPath)
	}
	if gotModel != "anthropic/claude-sonnet-4-6" {
		t.Errorf("model = %q, want the session's model", gotModel)
	}
	if !strings.Contains(gotUserMsg, "please refactor the auth module") {
		t.Errorf("user message should embed the initial message as data to label, got %q", gotUserMsg)
	}
}

// Newer Anthropic models (claude-sonnet-5 on Bedrock) reject any request that
// sets temperature with a 400, so the summary request must not send it at all.
func TestSummarizer_OmitsTemperature(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"content":[{"type":"text","text":"Label"}]}`)) //nolint:errcheck
	}))
	defer srv.Close()

	s := newSummarizer(logging.NewLogger())
	if _, err := s.Summarize(context.Background(), srv.URL, "anthropic/claude-sonnet-5", "x"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body == nil {
		t.Fatal("request body was not captured")
	}
	if _, ok := body["temperature"]; ok {
		t.Errorf("request sent temperature=%v; it must be omitted", body["temperature"])
	}
}

func TestSummarizer_TruncatesToMaxChars(t *testing.T) {
	long := strings.Repeat("a", 200)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(messagesResponse{Content: []struct { //nolint:errcheck,errchkjson
			Text string `json:"text"`
		}{{Text: long}}})
	}))
	defer srv.Close()

	s := newSummarizer(logging.NewLogger())
	got, err := s.Summarize(context.Background(), srv.URL, "m", "x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if utf8.RuneCountInString(got) != summaryMaxChars {
		t.Errorf("len = %d runes, want %d", utf8.RuneCountInString(got), summaryMaxChars)
	}
}

func TestTruncateSummary_MultibyteUnicodeAtBoundary(t *testing.T) {
	// Fill to two runes short of the cap, then push past it with 4-byte emoji so
	// the cut lands mid-emoji if the logic were byte-based instead of rune-based.
	in := strings.Repeat("a", summaryMaxChars-2) + "🚀🔥🌟"
	got := truncateSummary(in)

	if !utf8.ValidString(got) {
		t.Fatalf("truncated string is not valid UTF-8: %q", got)
	}
	if n := utf8.RuneCountInString(got); n != summaryMaxChars {
		t.Errorf("rune count = %d, want %d", n, summaryMaxChars)
	}
	if !strings.HasSuffix(got, "🚀🔥") {
		t.Errorf("expected the cut to land on a rune boundary keeping 🚀🔥, got %q", got)
	}
}

func TestTruncateSummary_TrimsWhitespace(t *testing.T) {
	if got := truncateSummary("  \t Build the thing \n "); got != "Build the thing" {
		t.Errorf("truncateSummary = %q, want trimmed %q", got, "Build the thing")
	}
}

func TestSummarizer_ErrorOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"code":"policy_denied"}`)) //nolint:errcheck
	}))
	defer srv.Close()

	s := newSummarizer(logging.NewLogger())
	if _, err := s.Summarize(context.Background(), srv.URL, "m", "x"); err == nil {
		t.Fatal("expected error for 403 response, got nil")
	}
}

func TestSummarizer_ErrorOnEmptyContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"content":[]}`)) //nolint:errcheck
	}))
	defer srv.Close()

	s := newSummarizer(logging.NewLogger())
	if _, err := s.Summarize(context.Background(), srv.URL, "m", "x"); err == nil {
		t.Fatal("expected error for empty content, got nil")
	}
}
