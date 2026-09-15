package skills

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
)

type stubTokenSource struct {
	token string
	err   error
}

func (s stubTokenSource) Token(_ context.Context, _, _ string) (string, error) {
	return s.token, s.err
}

func TestNewClient_SetsBaseURLs(t *testing.T) {
	c := NewClient(logging.NewLoggerWithOutput(io.Discard), stubTokenSource{token: "test-token"})

	if c.apiBaseURL != DefaultAPIBaseURL {
		t.Errorf("apiBaseURL = %q, want %q", c.apiBaseURL, DefaultAPIBaseURL)
	}
	if c.rawBaseURL != DefaultRawBaseURL {
		t.Errorf("rawBaseURL = %q, want %q", c.rawBaseURL, DefaultRawBaseURL)
	}
}

func TestGetTree_ReturnsEntries(t *testing.T) {
	var gotPath, gotAuth, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		_ = json.NewEncoder(w).Encode(Tree{
			SHA: "abc123",
			Entries: []TreeEntry{
				{Path: "skills/react-expert/SKILL.md", Type: "blob", SHA: "s1", Size: 42},
				{Path: "skills/react-expert/resources/examples.md", Type: "blob", SHA: "s2", Size: 10},
			},
			Truncated: false,
		})
	}))
	defer srv.Close()

	c := NewClient(logging.NewLoggerWithOutput(io.Discard), stubTokenSource{token: "test-token"})
	c.apiBaseURL = srv.URL

	tree, err := c.GetTree(context.Background(), "platform-engineering", "skills", "main")
	if err != nil {
		t.Fatalf("GetTree: %v", err)
	}
	if gotPath != "/repos/platform-engineering/skills/git/trees/main" {
		t.Errorf("path = %q, want /repos/platform-engineering/skills/git/trees/main", gotPath)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("auth = %q, want Bearer test-token", gotAuth)
	}
	if gotAccept != acceptHeader {
		t.Errorf("accept = %q, want %q", gotAccept, acceptHeader)
	}
	if len(tree.Entries) != 2 {
		t.Fatalf("len(tree.Entries) = %d, want 2", len(tree.Entries))
	}
	if tree.Truncated {
		t.Error("Truncated = true, want false")
	}
}

func TestGetTree_TokenSourceError(t *testing.T) {
	c := NewClient(logging.NewLoggerWithOutput(io.Discard), stubTokenSource{err: errors.New("mint failed")})

	_, err := c.GetTree(context.Background(), "org", "repo", "main")
	if err == nil {
		t.Fatal("expected an error when tokenSource fails")
	}
}

func TestGetTree_RepositoryNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewClient(logging.NewLoggerWithOutput(io.Discard), stubTokenSource{token: "test-token"})
	c.apiBaseURL = srv.URL

	_, err := c.GetTree(context.Background(), "org", "repo", "main")
	if !errors.Is(err, ErrRepositoryNotFound) {
		t.Errorf("err = %v, want ErrRepositoryNotFound", err)
	}
}

func TestGetTree_UnexpectedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewClient(logging.NewLoggerWithOutput(io.Discard), stubTokenSource{token: "test-token"})
	c.apiBaseURL = srv.URL

	_, err := c.GetTree(context.Background(), "org", "repo", "main")
	if !errors.Is(err, ErrUnexpectedStatus) {
		t.Errorf("err = %v, want ErrUnexpectedStatus", err)
	}
}

func TestGetFile_ReturnsBytes(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("---\nname: React Expert\n---\nBody"))
	}))
	defer srv.Close()

	c := NewClient(logging.NewLoggerWithOutput(io.Discard), stubTokenSource{token: "test-token"})
	c.rawBaseURL = srv.URL

	content, err := c.GetFile(context.Background(), "platform-engineering", "skills", "main", "skills/react-expert/SKILL.md")
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	wantPath := "/platform-engineering/skills/main/skills/react-expert/SKILL.md"
	if gotPath != wantPath {
		t.Errorf("path = %q, want %q", gotPath, wantPath)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("auth = %q, want Bearer test-token", gotAuth)
	}
	if string(content) != "---\nname: React Expert\n---\nBody" {
		t.Errorf("content = %q, unexpected", string(content))
	}
}

func TestGetFile_FileNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewClient(logging.NewLoggerWithOutput(io.Discard), stubTokenSource{token: "test-token"})
	c.rawBaseURL = srv.URL

	_, err := c.GetFile(context.Background(), "org", "repo", "main", "missing.md")
	if !errors.Is(err, ErrFileNotFound) {
		t.Errorf("err = %v, want ErrFileNotFound", err)
	}
}

func TestGetFile_TokenSourceError(t *testing.T) {
	c := NewClient(logging.NewLoggerWithOutput(io.Discard), stubTokenSource{err: errors.New("mint failed")})

	_, err := c.GetFile(context.Background(), "org", "repo", "main", "path.md")
	if !errors.Is(err, ErrTokenSource) {
		t.Errorf("err = %v, want ErrTokenSource", err)
	}
}

func TestGetFile_UnexpectedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewClient(logging.NewLoggerWithOutput(io.Discard), stubTokenSource{token: "test-token"})
	c.rawBaseURL = srv.URL

	_, err := c.GetFile(context.Background(), "org", "repo", "main", "path.md")
	if !errors.Is(err, ErrUnexpectedStatus) {
		t.Errorf("err = %v, want ErrUnexpectedStatus", err)
	}
}

func TestGetFile_InvalidPath(t *testing.T) {
	cases := []string{
		"../config.json",
		"skills/react-expert/../../../etc/passwd",
		"//etc/passwd",
		"/etc/passwd",
	}
	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			// No server needed: an invalid path must be rejected before any
			// network call, so tokenSource.Token must never even be called.
			c := NewClient(logging.NewLoggerWithOutput(io.Discard), stubTokenSource{
				err: errors.New("Token should not be called for an invalid path"),
			})

			_, err := c.GetFile(context.Background(), "org", "repo", "main", path)
			if !errors.Is(err, ErrInvalidPath) {
				t.Errorf("err = %v, want ErrInvalidPath", err)
			}
		})
	}
}

func TestGetFile_AllowsDoubleDotSubstringInFilename(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte("content"))
	}))
	defer srv.Close()

	c := NewClient(logging.NewLoggerWithOutput(io.Discard), stubTokenSource{token: "test-token"})
	c.rawBaseURL = srv.URL

	_, err := c.GetFile(context.Background(), "org", "repo", "main", "resources/v1..2/notes.md")
	if err != nil {
		t.Fatalf("GetFile: %v, want no error for a filename that merely contains \"..\" as a substring", err)
	}
	wantPath := "/org/repo/main/resources/v1..2/notes.md"
	if gotPath != wantPath {
		t.Errorf("path = %q, want %q", gotPath, wantPath)
	}
}

func TestGetFile_EmptyPathRejected(t *testing.T) {
	c := NewClient(logging.NewLoggerWithOutput(io.Discard), stubTokenSource{
		err: errors.New("Token should not be called for an invalid path"),
	})

	_, err := c.GetFile(context.Background(), "org", "repo", "main", "")
	if !errors.Is(err, ErrInvalidPath) {
		t.Errorf("err = %v, want ErrInvalidPath", err)
	}
}

func TestGetFile_InvalidOrgRepoBranch(t *testing.T) {
	cases := []struct {
		name           string
		org, repo, ref string
	}{
		{"empty org", "", "repo", "main"},
		{"empty repo", "org", "", "main"},
		{"empty branch", "org", "repo", ""},
		{"org with traversal segment", "..", "repo", "main"},
		{"repo with traversal segment", "org", "..", "main"},
		{"branch with traversal segment", "org", "repo", ".."},
		{"org with leading slash", "/org", "repo", "main"},
		{"branch with double slash", "org", "repo", "main//x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := NewClient(logging.NewLoggerWithOutput(io.Discard), stubTokenSource{
				err: errors.New("Token should not be called for an invalid org/repo/branch"),
			})

			_, err := c.GetFile(context.Background(), tc.org, tc.repo, tc.ref, "path.md")
			if !errors.Is(err, ErrInvalidPath) {
				t.Errorf("err = %v, want ErrInvalidPath", err)
			}
		})
	}
}

func TestGetTree_InvalidOrgRepoBranch(t *testing.T) {
	cases := []struct {
		name           string
		org, repo, ref string
	}{
		{"empty org", "", "repo", "main"},
		{"empty repo", "org", "", "main"},
		{"empty branch", "org", "repo", ""},
		{"org with traversal segment", "..", "repo", "main"},
		{"repo with traversal segment", "org", "..", "main"},
		{"branch with traversal segment", "org", "repo", ".."},
		{"org with leading slash", "/org", "repo", "main"},
		{"branch with double slash", "org", "repo", "main//x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := NewClient(logging.NewLoggerWithOutput(io.Discard), stubTokenSource{
				err: errors.New("Token should not be called for an invalid org/repo/branch"),
			})

			_, err := c.GetTree(context.Background(), tc.org, tc.repo, tc.ref)
			if !errors.Is(err, ErrInvalidPath) {
				t.Errorf("err = %v, want ErrInvalidPath", err)
			}
		})
	}
}

func TestGetFile_ReadBodyError(t *testing.T) {
	// Declare a Content-Length longer than what's actually written, then
	// return immediately. The client reads based on Content-Length, so it
	// hits an unexpected-EOF partway through — simulating a dropped
	// connection or truncated stream, without needing a real network fault.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("short"))
	}))
	defer srv.Close()

	c := NewClient(logging.NewLoggerWithOutput(io.Discard), stubTokenSource{token: "test-token"})
	c.rawBaseURL = srv.URL

	_, err := c.GetFile(context.Background(), "org", "repo", "main", "path.md")
	if !errors.Is(err, ErrReadBody) {
		t.Errorf("err = %v, want ErrReadBody", err)
	}
}
