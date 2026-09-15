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
