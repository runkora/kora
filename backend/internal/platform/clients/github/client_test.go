package github

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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

		got, err := NewClient(server.URL, logging.NewLogger()).GetRawContent(context.Background(), "test-token", "org", "repo", "main", "skills/skill-name/SKILL.md", 1024)
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
		wantNotFound    bool
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
			wantNotFound:    true,
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

			_, err := NewClient(server.URL, logging.NewLogger()).GetRawContent(context.Background(), "test-token", "org", "repo", "main", "SKILL.md", 1024)
			if !errors.Is(err, ErrUnexpectedStatus) {
				t.Fatalf("GetRawContent error = %v, want wrapped ErrUnexpectedStatus", err)
			}
			if got := errors.Is(err, ErrUnavailable); got != tt.wantUnavailable {
				t.Errorf("errors.Is(err, ErrUnavailable) = %v, want %v; err = %v", got, tt.wantUnavailable, err)
			}
			if got := errors.Is(err, ErrNotFound); got != tt.wantNotFound {
				t.Errorf("errors.Is(err, ErrNotFound) = %v, want %v; err = %v", got, tt.wantNotFound, err)
			}
		})
	}

	t.Run("a file over the limit is too large", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("12345"))
		}))
		defer server.Close()
		client := NewClient(server.URL, logging.NewLogger())
		if _, err := client.GetRawContent(context.Background(), "t", "org", "repo", "main", "f", 4); !errors.Is(err, ErrTooLarge) {
			t.Errorf("GetRawContent over the limit = %v, want ErrTooLarge", err)
		}
		if got, err := client.GetRawContent(context.Background(), "t", "org", "repo", "main", "f", 5); err != nil || string(got) != "12345" {
			t.Errorf("GetRawContent at the limit = %q, %v", got, err)
		}
	})

	t.Run("network failure is unavailable", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		server.Close() // every call to the closed server fails with a network error

		_, err := NewClient(server.URL, logging.NewLogger()).GetRawContent(context.Background(), "test-token", "org", "repo", "main", "SKILL.md", 1024)
		if !errors.Is(err, ErrUnavailable) {
			t.Errorf("GetRawContent error = %v, want wrapped ErrUnavailable", err)
		}
	})

	t.Run("canceled call is not unavailable", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		defer server.Close()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := NewClient(server.URL, logging.NewLogger()).GetRawContent(ctx, "test-token", "org", "repo", "main", "SKILL.md", 1024)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("GetRawContent error = %v, want context.Canceled", err)
		}
		if errors.Is(err, ErrUnavailable) {
			t.Errorf("GetRawContent error = %v, want no ErrUnavailable for a canceled call", err)
		}
	})
}

func TestGetTree(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/org/repo/git/trees/abc" || r.URL.Query().Get("recursive") != "1" {
			t.Errorf("request = %s", r.URL)
		}
		switch r.Header.Get("Authorization") {
		case "Bearer truncated":
			_, _ = w.Write([]byte(`{"tree":[],"truncated":true}`))
		default:
			_, _ = w.Write([]byte(`{"tree":[{"path":"a/SKILL.md","type":"blob","size":12},{"path":"a","type":"tree"}]}`))
		}
	}))
	defer server.Close()
	client := NewClient(server.URL, logging.NewLogger())

	got, err := client.GetTree(context.Background(), "t", "org", "repo", "abc")
	if err != nil {
		t.Fatalf("GetTree: %v", err)
	}
	if len(got) != 2 || got[0] != (TreeEntry{Path: "a/SKILL.md", Type: "blob", Size: 12}) {
		t.Errorf("GetTree = %+v", got)
	}
	if _, err := client.GetTree(context.Background(), "truncated", "org", "repo", "abc"); !errors.Is(err, ErrTreeTruncated) {
		t.Errorf("truncated tree = %v, want ErrTreeTruncated", err)
	}
}

func TestGetTree_OverTheByteCap(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tree":[{"path":"` + strings.Repeat("a", maxTreeBytes) + `","type":"blob"}]}`))
	}))
	defer server.Close()
	if _, err := NewClient(server.URL, logging.NewLogger()).GetTree(context.Background(), "t", "org", "repo", "abc"); !errors.Is(err, ErrTreeTruncated) {
		t.Errorf("GetTree over the cap = %v, want ErrTreeTruncated", err)
	}
}

func TestResolveRef(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	tests := []struct {
		name    string
		status  int
		body    string
		want    string
		wantErr error
	}{
		{name: "returns the SHA", status: http.StatusOK, body: sha + "\n", want: sha},
		{name: "unknown repository", status: http.StatusNotFound, wantErr: ErrNotFound},
		{name: "unknown ref", status: http.StatusUnprocessableEntity, wantErr: ErrNotFound},
		{name: "not a SHA", status: http.StatusOK, body: "<html>", wantErr: ErrBadCommit},
		{name: "outage", status: http.StatusBadGateway, wantErr: ErrUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.EscapedPath() != "/repos/org/repo/commits/release%2F1" || r.Header.Get("Accept") != "application/vnd.github.sha" {
					t.Errorf("request = %s Accept %q", r.URL.EscapedPath(), r.Header.Get("Accept"))
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			got, err := NewClient(server.URL, logging.NewLogger()).ResolveRef(context.Background(), "t", "org", "repo", "release/1")
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("ResolveRef error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Errorf("ResolveRef = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
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

func TestRateLimitIsDistinguished(t *testing.T) {
	for _, tc := range []struct {
		status      int
		header      string
		rateLimited bool
	}{
		{http.StatusTooManyRequests, "", true},
		{http.StatusForbidden, "0", true},
		{http.StatusServiceUnavailable, "", false},
		{http.StatusForbidden, "retry", true},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			switch tc.header {
			case "":
			case "retry":
				w.Header().Set("Retry-After", "30")
			default:
				w.Header().Set("X-RateLimit-Remaining", tc.header)
			}
			w.WriteHeader(tc.status)
		}))
		_, err := NewClient(server.URL, logging.NewLogger()).GetRawContent(context.Background(), "t", "o", "r", "main", "f", 10)
		server.Close()
		if !errors.Is(err, ErrUnavailable) || errors.Is(err, ErrRateLimited) != tc.rateLimited {
			t.Errorf("status %d: err = %v, want unavailable with rateLimited %v", tc.status, err, tc.rateLimited)
		}
	}
}

func TestRefusedCredentialIsUnauthorized(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
		_, err := NewClient(server.URL, logging.NewLogger()).ResolveRef(context.Background(), "t", "o", "r", "main")
		server.Close()
		if !errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrUnavailable) {
			t.Errorf("status %d = %v, want ErrUnauthorized only", status, err)
		}
	}
}
