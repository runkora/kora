package korajson

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/logging"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
)

// TestEmptyTwoxxBodyIsSuccessAck covers endpoints that return a 2xx with no body
// (e.g. send-message, interrupt): the client must treat that as a success ack,
// not fail to decode an empty body as JSON.
func TestEmptyTwoxxBodyIsSuccessAck(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusAccepted, http.StatusNoContent} {
		backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
		}))
		got, err := NewClient(logging.NewLogger()).Post(context.Background(), Caller{Bearer: "tok"}, backend.URL, "", map[string]string{"x": "y"})
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

// The caller's bearer and delegation token travel on the request. An absent
// delegation sends no header.
func TestSendAttachesCaller(t *testing.T) {
	cases := []struct {
		name           string
		caller         Caller
		wantAuth       string
		wantDelegation string
	}{
		{"delegated", Caller{Bearer: "tok", Delegation: "del"}, "Bearer tok", "del"},
		{"autonomous", Caller{Bearer: "tok"}, "Bearer tok", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotAuth string
			var gotDelegation []string
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotAuth, gotDelegation = r.Header.Get("Authorization"), r.Header.Values(mw.DelegationTokenHeader)
				w.WriteHeader(http.StatusNoContent)
			}))
			defer backend.Close()

			if _, err := NewClient(logging.NewLogger()).Get(context.Background(), tc.caller, backend.URL, "", nil); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotAuth != tc.wantAuth {
				t.Errorf("Authorization = %q, want %q", gotAuth, tc.wantAuth)
			}
			if got := strings.Join(gotDelegation, ","); got != tc.wantDelegation {
				t.Errorf("delegation = %q, want %q", got, tc.wantDelegation)
			}
		})
	}
}

func TestStatusSentinels(t *testing.T) {
	cases := []struct {
		status   int
		body     string
		want     error
		notWant  error
		wantText string
	}{
		{http.StatusUnauthorized, `{"code":"unauthorized"}`, ErrUnauthorized, ErrForbidden, ""},
		{http.StatusForbidden, `{"code":"forbidden","message":"runs can only be started by a person"}`, ErrForbidden, ErrUnauthorized, "runs can only be started by a person"},
		{http.StatusForbidden, `not json`, ErrForbidden, ErrUnauthorized, ""},
		{http.StatusInternalServerError, `{}`, ErrUpstream, ErrForbidden, ""},
	}
	for _, tc := range cases {
		backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		_, err := NewClient(logging.NewLogger()).Get(context.Background(), Caller{Bearer: "t"}, backend.URL, "", nil)
		backend.Close()
		if !errors.Is(err, tc.want) || errors.Is(err, tc.notWant) {
			t.Errorf("status %d: err = %v, want %v and not %v", tc.status, err, tc.want, tc.notWant)
		}
		var forbidden *ForbiddenError
		if errors.As(err, &forbidden) && forbidden.Message != tc.wantText {
			t.Errorf("status %d: message = %q, want %q", tc.status, forbidden.Message, tc.wantText)
		}
	}
}
