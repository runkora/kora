package sessionmanager

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
		call   func(c *Client, url string) error
	}{
		{
			name:   "CreateSession",
			status: http.StatusCreated,
			call: func(c *Client, url string) error {
				return c.CreateSession(context.Background(), url, CreateSessionRequest{RunID: "run-1"})
			},
		},
		{
			name:   "SendMessage",
			status: http.StatusAccepted,
			call: func(c *Client, url string) error {
				return c.SendMessage(context.Background(), url, "run-1", "hello", 0, false)
			},
		},
		{
			name:   "InterruptSession",
			status: http.StatusAccepted,
			call: func(c *Client, url string) error {
				return c.InterruptSession(context.Background(), url, "run-1")
			},
		},
		{
			name:   "CloseSession",
			status: http.StatusNoContent,
			call: func(c *Client, url string) error {
				return c.CloseSession(context.Background(), url, "run-1")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, got := captureAuth(t, tc.status)
			c := NewClient(logger, staticToken("tok-123"))

			if err := tc.call(c, srv.URL); err != nil {
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

	err := c.CreateSession(context.Background(), srv.URL, CreateSessionRequest{RunID: "run-1"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want it to wrap %v", err, sentinel)
	}
	if *got != "" {
		t.Errorf("request was sent despite the token failure (Authorization %q)", *got)
	}
}

// A nil TokenFunc is the local-dev escape hatch and must not panic.

func TestPodURL_TargetsTheSessionManagerPort(t *testing.T) {
	got := PodURL("coder", 0, "kora-tenant-acme")
	want := "http://coder-0.coder.kora-tenant-acme.svc.cluster.local:8090"
	if got != want {
		t.Errorf("PodURL = %q, want %q", got, want)
	}
	if !strings.HasSuffix(got, "8090") {
		t.Errorf("PodURL must target the port the AuthorizationPolicy restricts")
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

			if err := c.SendMessage(context.Background(), srv.URL, "run-1", "hi", tc.secs, tc.person); err != nil {
				t.Fatalf("SendMessage: %v", err)
			}
			if got := strings.TrimSpace(string(body)); got != tc.want {
				t.Errorf("body = %s, want %s", got, tc.want)
			}
		})
	}
}
