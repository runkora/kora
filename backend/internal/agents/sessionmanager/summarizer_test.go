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

	"github.com/runkora/kora/backend/internal/platform/logging"
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
		json.NewEncoder(w).Encode(messagesResponse{ //nolint:errcheck,errchkjson
			Content: []messagesContentBlock{{Type: "text", Text: long}},
		})
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

// A reasoning model returns a "thinking" block before the "text" block; the
// summary must come from the text block, not the thinking block.
func TestSummarizer_SkipsLeadingThinkingBlock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"content":[{"type":"thinking","thinking":"reasoning about the label..."},{"type":"text","text":"Audit the repo"}]}`)) //nolint:errcheck
	}))
	defer srv.Close()

	s := newSummarizer(logging.NewLogger())
	got, err := s.Summarize(context.Background(), srv.URL, "m", "x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "Audit the repo" {
		t.Errorf("summary = %q, want %q", got, "Audit the repo")
	}
}

// A reasoning model can spend its whole token budget thinking and return only
// a thinking block; that must still surface the empty-summary error, not a
// summary built from the thinking block's own content.
func TestSummarizer_ErrorOnThinkingBlockOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"content":[{"type":"thinking","thinking":"reasoning..."}]}`)) //nolint:errcheck
	}))
	defer srv.Close()

	s := newSummarizer(logging.NewLogger())
	if _, err := s.Summarize(context.Background(), srv.URL, "m", "x"); err == nil {
		t.Fatal("expected error when only a thinking block is returned, got nil")
	}
}

// thinkingServer rejects any thinking setting listed in rejects with a 400,
// records the thinking value of every request, and otherwise returns a label.
func thinkingServer(rejects map[string]bool, seen *[]string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		kind := "omitted"
		if th, ok := body["thinking"].(map[string]any); ok {
			kind, _ = th["type"].(string)
		}
		*seen = append(*seen, kind)
		w.Header().Set("Content-Type", "application/json")
		if rejects[kind] {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"message":"unsupported thinking"}`)) //nolint:errcheck
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"content":[{"type":"thinking","thinking":"hm"},{"type":"text","text":"Label"}]}`)) //nolint:errcheck
	}))
}

// Each thinking setting the summarizer can send must yield a summary, and a
// rejected setting must fall through to the next one.
func TestSummarizer_ThinkingSettings(t *testing.T) {
	tests := []struct {
		name    string
		rejects map[string]bool
		want    []string
	}{
		{"disabled accepted", nil, []string{"disabled"}},
		{"disabled rejected", map[string]bool{"disabled": true}, []string{"disabled", "between_tools"}},
		{"both rejected", map[string]bool{"disabled": true, "between_tools": true}, []string{"disabled", "between_tools", "omitted"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var seen []string
			srv := thinkingServer(tt.rejects, &seen)
			defer srv.Close()

			s := newSummarizer(logging.NewLogger())
			got, err := s.Summarize(context.Background(), srv.URL, "m", "x")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != "Label" {
				t.Errorf("summary = %q, want %q", got, "Label")
			}
			if strings.Join(seen, ",") != strings.Join(tt.want, ",") {
				t.Errorf("thinking settings sent = %v, want %v", seen, tt.want)
			}
		})
	}
}

// A 400 on every setting is an error, and other statuses are not retried.
func TestSummarizer_ThinkingRetryLimits(t *testing.T) {
	var seen []string
	srv := thinkingServer(map[string]bool{"disabled": true, "between_tools": true, "omitted": true}, &seen)
	defer srv.Close()
	s := newSummarizer(logging.NewLogger())
	if _, err := s.Summarize(context.Background(), srv.URL, "m", "x"); err == nil {
		t.Fatal("expected error when every setting is rejected")
	}
	if len(seen) != 3 {
		t.Errorf("requests = %d, want 3", len(seen))
	}

	calls := 0
	srv403 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv403.Close()
	if _, err := s.Summarize(context.Background(), srv403.URL, "m", "x"); err == nil {
		t.Fatal("expected error for 403")
	}
	if calls != 1 {
		t.Errorf("requests on 403 = %d, want 1", calls)
	}
}

// A thinking-enabled retry shares max_tokens with the thinking block, so it
// needs room for both or the response has no text block.
func TestSummarizer_ThinkingRetryHasRoomForText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			MaxTokens int `json:"max_tokens"`
			Thinking  *struct {
				Type string `json:"type"`
			} `json:"thinking"`
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		w.Header().Set("Content-Type", "application/json")
		if body.Thinking != nil && body.Thinking.Type == "disabled" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
		if body.MaxTokens < 500 {
			w.Write([]byte(`{"stop_reason":"max_tokens","content":[{"type":"thinking","thinking":"hm"}]}`)) //nolint:errcheck
			return
		}
		w.Write([]byte(`{"stop_reason":"end_turn","content":[{"type":"thinking","thinking":"hm"},{"type":"text","text":"Label"}]}`)) //nolint:errcheck
	}))
	defer srv.Close()

	s := newSummarizer(logging.NewLogger())
	got, err := s.Summarize(context.Background(), srv.URL, "m", "x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "Label" {
		t.Errorf("summary = %q, want %q", got, "Label")
	}
}
