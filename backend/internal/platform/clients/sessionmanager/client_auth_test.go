package sessionmanager

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// captureAuth stands in for a session-manager pod, recording the Authorization
// header it was sent.
func captureAuth(t *testing.T, status int) (*httptest.Server, *string) {
	t.Helper()
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func staticToken(tok string) TokenFunc {
	return func(context.Context) (string, error) { return tok, nil }
}

// Every session-control call must carry the caller's platform token: the
// session-manager now authenticates them and checks the principal is
// kora-agents.
func TestClient_SendsBearerOnEverySessionCall(t *testing.T) {
	logger := logging.NewLoggerWithOutput(io.Discard)

	tests := []struct {
		name   string
		status int
		call   func(c *Client, pod Pod) error
	}{
		{
			name:   "CreateSession",
			status: http.StatusCreated,
			call: func(c *Client, pod Pod) error {
				return c.CreateSession(context.Background(), pod, CreateSessionRequest{RunID: "run-1"})
			},
		},
		{
			name:   "SendMessage",
			status: http.StatusAccepted,
			call: func(c *Client, pod Pod) error {
				return c.SendMessage(context.Background(), pod, "run-1", "hello", 0, false, nil)
			},
		},
		{
			name:   "InterruptSession",
			status: http.StatusAccepted,
			call: func(c *Client, pod Pod) error {
				return c.InterruptSession(context.Background(), pod, "run-1")
			},
		},
		{
			name:   "CloseSession",
			status: http.StatusNoContent,
			call: func(c *Client, pod Pod) error {
				return c.CloseSession(context.Background(), pod, "run-1")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, got := captureAuth(t, tc.status)
			c := NewClient(logger, staticToken("tok-123"))

			if err := tc.call(c, Pod{URL: srv.URL}); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if *got != "Bearer tok-123" {
				t.Errorf("Authorization = %q, want %q", *got, "Bearer tok-123")
			}
		})
	}
}

// A token that cannot be obtained must fail the call rather than send an
// unauthenticated request that the pod would reject with a confusing 401.
func TestClient_TokenErrorFailsTheCall(t *testing.T) {
	srv, got := captureAuth(t, http.StatusCreated)
	sentinel := errors.New("svid exchange unavailable")
	c := NewClient(logging.NewLoggerWithOutput(io.Discard), func(context.Context) (string, error) {
		return "", sentinel
	})

	err := c.CreateSession(context.Background(), Pod{URL: srv.URL}, CreateSessionRequest{RunID: "run-1"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want it to wrap %v", err, sentinel)
	}
	if *got != "" {
		t.Errorf("request was sent despite the token failure (Authorization %q)", *got)
	}
}

// A nil TokenFunc is the local-dev escape hatch and must not panic.

func TestPodAt_TargetsThePodIPWithTheAgentServiceAsHost(t *testing.T) {
	got := PodAt("10.42.1.7", "coder", "kora-tenant-acme")
	want := Pod{URL: "http://10.42.1.7:8090", Host: "coder.kora-tenant-acme.svc.cluster.local:8090"}
	if got != want {
		t.Errorf("PodAt = %+v, want %+v", got, want)
	}
	if !strings.HasSuffix(got.URL, "8090") {
		t.Errorf("PodAt must target the port the AuthorizationPolicy restricts")
	}
}

// Every call carries the agent Service as Host header. The header routes the
// call to the Envoy cluster that pins the agent SPIFFE ID.
func TestClient_SendsTheAgentServiceAsHost(t *testing.T) {
	var host string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host = r.Host
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := NewClient(logging.NewLoggerWithOutput(io.Discard), staticToken("tok"))
	// The second call runs under a parent span, the path that opens a client span.
	traced := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{1}, SpanID: trace.SpanID{1}, TraceFlags: trace.FlagsSampled,
	}))
	for _, ctx := range []context.Context{context.Background(), traced} {
		host = ""
		if err := c.CloseSession(ctx, Pod{URL: srv.URL, Host: "coder.kora-tenant-acme.svc.cluster.local:8090"}, "run-1"); err != nil {
			t.Fatal(err)
		}
		if host != "coder.kora-tenant-acme.svc.cluster.local:8090" {
			t.Errorf("Host = %q, want the agent Service", host)
		}
	}
}

// The idle timeout rides on the message only when there is one to change.
func TestClient_SendMessageIdleTimeout(t *testing.T) {
	tests := []struct {
		name   string
		secs   int
		person bool
		want   string
	}{
		{name: "omitted when zero", secs: 0, want: `{"content":"hi"}`},
		{name: "sent when set", secs: 1200, want: `{"content":"hi","idle_timeout_secs":1200}`},
		{name: "from a person", person: true, want: `{"content":"hi","from_person":true}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var body []byte
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ = io.ReadAll(r.Body)
				w.WriteHeader(http.StatusAccepted)
			}))
			t.Cleanup(srv.Close)
			c := NewClient(logging.NewLoggerWithOutput(io.Discard), staticToken("tok"))

			if err := c.SendMessage(context.Background(), Pod{URL: srv.URL}, "run-1", "hi", tc.secs, tc.person, nil); err != nil {
				t.Fatalf("SendMessage: %v", err)
			}
			if got := strings.TrimSpace(string(body)); got != tc.want {
				t.Errorf("body = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestClient_Memory(t *testing.T) {
	var auth, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, path = r.Header.Get("Authorization"), r.URL.Path
		_, _ = io.WriteString(w, `{"working_set_bytes":350000000,"limit_bytes":1073741824}`)
	}))
	t.Cleanup(srv.Close)

	got, err := NewClient(logging.NewLoggerWithOutput(io.Discard), staticToken("tok-123")).Memory(context.Background(), Pod{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if want := (Memory{WorkingSetBytes: 350000000, LimitBytes: 1073741824}); got != want {
		t.Errorf("Memory = %+v, want %+v", got, want)
	}
	if auth != "Bearer tok-123" || path != "/memory" {
		t.Errorf("request = %s with Authorization %q, want /memory with the bearer", path, auth)
	}
}

func TestClient_Bound(t *testing.T) {
	tests := []struct {
		status    int
		want      bool
		wantError bool
	}{
		{status: http.StatusOK, want: true},
		{status: http.StatusServiceUnavailable, want: false},
		{status: http.StatusForbidden, wantError: true},
	}
	for _, tc := range tests {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			var auth, path string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				auth, path = r.Header.Get("Authorization"), r.URL.Path
				w.WriteHeader(tc.status)
			}))
			t.Cleanup(srv.Close)

			got, err := NewClient(logging.NewLoggerWithOutput(io.Discard), staticToken("tok-123")).Bound(context.Background(), Pod{URL: srv.URL})
			if (err != nil) != tc.wantError || got != tc.want {
				t.Fatalf("Bound = %v, %v; want %v with error %v", got, err, tc.want, tc.wantError)
			}
			if auth != "Bearer tok-123" || path != "/health/bound" {
				t.Errorf("request = %s with Authorization %q, want /health/bound with the bearer", path, auth)
			}
		})
	}
}
