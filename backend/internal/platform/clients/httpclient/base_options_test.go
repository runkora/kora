package httpclient_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/clients/httpclient"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// countingServer returns a server and a counter of connections it accepted.
func countingServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var conns atomic.Int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			conns.Add(1)
		}
	}
	srv.Start()
	t.Cleanup(srv.Close)
	return srv, &conns
}

func TestNew_WithDisableKeepAlives_OpensConnectionPerCall(t *testing.T) {
	tests := []struct {
		name      string
		opts      []httpclient.Option
		wantConns int32
	}{
		{name: "default reuses one connection", wantConns: 1},
		{name: "disable keep-alives opens one per call", opts: []httpclient.Option{httpclient.WithDisableKeepAlives()}, wantConns: 10},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, conns := countingServer(t)
			base := httpclient.New("test", logging.NewLoggerWithOutput(io.Discard), 0, tc.opts...)
			for range 10 {
				_, span, resp, err := base.Do(context.Background(), httpclient.Request{
					Method: http.MethodGet, BaseURL: srv.URL, Path: "/x",
				})
				span.End()
				if err != nil {
					t.Fatalf("Do: %v", err)
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
			}
			if got := conns.Load(); got != tc.wantConns {
				t.Fatalf("connections = %d, want %d", got, tc.wantConns)
			}
		})
	}
}
