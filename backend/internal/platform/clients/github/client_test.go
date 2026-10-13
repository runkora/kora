package github

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
)

func TestGetRawContent(t *testing.T) {
	t.Run("returns the file content", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/repos/org/repo/contents/skills/skill-name/SKILL.md" || r.URL.Query().Get("ref") != "main" {
				t.Errorf("request = %s, want the SKILL.md at ref main", r.URL)
			}
			if r.Header.Get("Authorization") != "Bearer test-token" {
				t.Errorf("Authorization = %q, want the bearer token", r.Header.Get("Authorization"))
			}
			_, _ = w.Write([]byte("file content"))
		}))
		defer server.Close()

		got, err := NewClient(server.URL, logging.NewLogger()).GetRawContent(context.Background(), "test-token", "org", "repo", "main", "skills/skill-name/SKILL.md")
		if err != nil {
			t.Fatalf("GetRawContent: %v", err)
		}
		if string(got) != "file content" {
			t.Errorf("GetRawContent = %q, want %q", got, "file content")
		}
	})

	tests := []struct {
		name            string
		status          int
		header          map[string]string
		body            string
		wantUnavailable bool
	}{
		{name: "500 is unavailable", status: http.StatusInternalServerError, wantUnavailable: true},
		{name: "503 is unavailable", status: http.StatusServiceUnavailable, wantUnavailable: true},
		{name: "429 is unavailable", status: http.StatusTooManyRequests, wantUnavailable: true},
		{
			name:            "403 with x-ratelimit-remaining 0 is unavailable",
			status:          http.StatusForbidden,
			header:          map[string]string{"X-RateLimit-Remaining": "0"},
			wantUnavailable: true,
		},
		{
			name:            "403 with retry-after is unavailable",
			status:          http.StatusForbidden,
			header:          map[string]string{"Retry-After": "60"},
			wantUnavailable: true,
		},
		{
			name:            "403 with a secondary rate limit message is unavailable",
			status:          http.StatusForbidden,
			body:            `{"message":"You have exceeded a secondary rate limit. Please wait a few minutes before you try again."}`,
			wantUnavailable: true,
		},
		{
			name:            "403 without a rate-limit signal is not unavailable",
			status:          http.StatusForbidden,
			header:          map[string]string{"X-RateLimit-Remaining": "4999"},
			body:            `{"message":"Resource not accessible by integration"}`,
			wantUnavailable: false,
		},
		{
			name:            "404 is not unavailable",
			status:          http.StatusNotFound,
			body:            `{"message":"Not Found"}`,
			wantUnavailable: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				for key, value := range tt.header {
					w.Header().Set(key, value)
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			_, err := NewClient(server.URL, logging.NewLogger()).GetRawContent(context.Background(), "test-token", "org", "repo", "main", "SKILL.md")
			if !errors.Is(err, ErrUnexpectedStatus) {
				t.Fatalf("GetRawContent error = %v, want wrapped ErrUnexpectedStatus", err)
			}
			if got := errors.Is(err, ErrUnavailable); got != tt.wantUnavailable {
				t.Errorf("errors.Is(err, ErrUnavailable) = %v, want %v; err = %v", got, tt.wantUnavailable, err)
			}
		})
	}

	t.Run("network failure is unavailable", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		server.Close() // every call to the closed server fails with a network error

		_, err := NewClient(server.URL, logging.NewLogger()).GetRawContent(context.Background(), "test-token", "org", "repo", "main", "SKILL.md")
		if !errors.Is(err, ErrUnavailable) {
			t.Errorf("GetRawContent error = %v, want wrapped ErrUnavailable", err)
		}
	})

	t.Run("canceled call is not unavailable", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		defer server.Close()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := NewClient(server.URL, logging.NewLogger()).GetRawContent(ctx, "test-token", "org", "repo", "main", "SKILL.md")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("GetRawContent error = %v, want context.Canceled", err)
		}
		if errors.Is(err, ErrUnavailable) {
			t.Errorf("GetRawContent error = %v, want no ErrUnavailable for a canceled call", err)
		}
	})
}

func TestGetTreeUnavailable(t *testing.T) {
	tests := []struct {
		name            string
		status          int
		wantUnavailable bool
	}{
		{name: "503 is unavailable", status: http.StatusServiceUnavailable, wantUnavailable: true},
		{name: "404 is not unavailable", status: http.StatusNotFound, wantUnavailable: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
			}))
			defer server.Close()

			_, err := NewClient(server.URL, logging.NewLogger()).GetTree(context.Background(), "test-token", "org", "repo", "main")
			if !errors.Is(err, ErrUnexpectedStatus) {
				t.Fatalf("GetTree error = %v, want wrapped ErrUnexpectedStatus", err)
			}
			if got := errors.Is(err, ErrUnavailable); got != tt.wantUnavailable {
				t.Errorf("errors.Is(err, ErrUnavailable) = %v, want %v; err = %v", got, tt.wantUnavailable, err)
			}
		})
	}
}
