package orbitjson

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
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

// TestPostWithTimeoutOutlastsDefault covers a slow call that opts into a longer
// limit, such as MCP runs.start waiting on a rolling pod.
func TestPostWithTimeoutOutlastsDefault(t *testing.T) {
	defaultTimeout = 50 * time.Millisecond
	t.Cleanup(func() { defaultTimeout = 30 * time.Second })

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`{"id":"run-1"}`))
	}))
	t.Cleanup(backend.Close)
	c := NewClient(logging.NewLogger())

	if _, err := c.Post(context.Background(), "tok", backend.URL, nil); err == nil {
		t.Fatal("Post: want timeout error")
	}
	got, err := c.PostWithTimeout(context.Background(), "tok", backend.URL, nil, StartRunTimeout)
	if err != nil {
		t.Fatalf("PostWithTimeout: %v", err)
	}
	if string(got) != `{"id":"run-1"}` {
		t.Errorf("got %s", got)
	}
}
