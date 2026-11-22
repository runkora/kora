package identityproxy

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

func TestTokenReady(t *testing.T) {
	status := http.StatusServiceUnavailable
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tokenz" {
			t.Errorf("path = %q, want /tokenz", r.URL.Path)
		}
		w.WriteHeader(status)
	}))
	defer srv.Close()
	c := NewClient(logging.NewLoggerWithOutput(io.Discard), srv.URL)

	if err := c.TokenReady(t.Context()); !errors.Is(err, ErrNoToken) {
		t.Errorf("TokenReady with no token = %v, want ErrNoToken", err)
	}
	status = http.StatusOK
	if err := c.TokenReady(t.Context()); err != nil {
		t.Errorf("TokenReady with a token = %v, want nil", err)
	}
}
