package sessionmanager

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// Pod IPs are reused, so no call may ride a connection opened by an earlier
// call: it could lead to the previous holder of the IP.
func TestClient_OpensNewConnectionPerCall(t *testing.T) {
	var conns atomic.Int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			conns.Add(1)
		}
	}
	srv.Start()
	t.Cleanup(srv.Close)

	c := NewClient(logging.NewLoggerWithOutput(io.Discard), staticToken("tok"))
	const calls = 5
	for range calls {
		if err := c.SendMessage(context.Background(), Pod{URL: srv.URL}, "run-1", "hello", 0, false, nil); err != nil {
			t.Fatalf("SendMessage: %v", err)
		}
	}
	if got := conns.Load(); got != calls {
		t.Fatalf("connections = %d, want %d", got, calls)
	}
}
