package korajson

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// TestEmptyTwoxxBodyIsSuccessAck covers endpoints that return a 2xx with no body
// (e.g. send-message, interrupt): the client must treat that as a success ack,
// not fail to decode an empty body as JSON.
func TestEmptyTwoxxBodyIsSuccessAck(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusAccepted, http.StatusNoContent} {
		backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
		}))
		got, err := NewClient(logging.NewLogger()).Post(context.Background(), "tok", backend.URL, map[string]string{"x": "y"})
		backend.Close()
		if err != nil {
			t.Errorf("status %d: unexpected error: %v", status, err)
			continue
		}
		if string(got) != `{"status":"ok"}` {
			t.Errorf("status %d: got %s, want {\"status\":\"ok\"}", status, got)
		}
	}
}
