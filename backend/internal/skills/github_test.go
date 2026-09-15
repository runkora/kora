package skills

import (
	"context"
	"io"
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
