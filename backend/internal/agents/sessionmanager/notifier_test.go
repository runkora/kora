package sessionmanager

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// ── Notify ────────────────────────────────────────────────────────────────────

func TestHTTPNotifier_Notify_2xxSucceeds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	n := NewHTTPNotifier(srv.URL, logging.NewLoggerWithOutput(io.Discard))
	if err := n.Notify(context.Background(), "run-1", Event{Type: EventTypeTextDelta}); err != nil {
		t.Errorf("unexpected error for 204: %v", err)
	}
}

func TestHTTPNotifier_Notify_Non2xxReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("something went wrong")) //nolint:errcheck
	}))
	defer srv.Close()

	n := NewHTTPNotifier(srv.URL, logging.NewLoggerWithOutput(io.Discard))
	err := n.Notify(context.Background(), "run-1", Event{Type: EventTypeTextDelta})
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status code, got: %v", err)
	}
	if !strings.Contains(err.Error(), "something went wrong") {
		t.Errorf("error should include response body, got: %v", err)
	}
}

func TestHTTPNotifier_Notify_4xxReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("run not found")) //nolint:errcheck
	}))
	defer srv.Close()

	n := NewHTTPNotifier(srv.URL, logging.NewLoggerWithOutput(io.Discard))
	err := n.Notify(context.Background(), "run-1", Event{Type: EventTypeTextDelta})
	if err == nil {
		t.Fatal("expected error for 404 response, got nil")
	}
}

// ── CompleteRun ───────────────────────────────────────────────────────────────

func TestHTTPNotifier_CompleteRun_2xxSucceeds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	n := NewHTTPNotifier(srv.URL, logging.NewLoggerWithOutput(io.Discard))
	if err := n.CompleteRun(context.Background(), "run-1", "active", false); err != nil {
		t.Errorf("unexpected error for 204: %v", err)
	}
}

func TestHTTPNotifier_CompleteRun_Non2xxReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte("state conflict")) //nolint:errcheck
	}))
	defer srv.Close()

	n := NewHTTPNotifier(srv.URL, logging.NewLoggerWithOutput(io.Discard))
	err := n.CompleteRun(context.Background(), "run-1", "active", false)
	if err == nil {
		t.Fatal("expected error for 409 response, got nil")
	}
	if !strings.Contains(err.Error(), "409") {
		t.Errorf("error should mention status code, got: %v", err)
	}
}

// ── SetSummary ────────────────────────────────────────────────────────────────

func TestHTTPNotifier_SetSummary_PostsSummaryToRunURL(t *testing.T) {
	var gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	n := NewHTTPNotifier(srv.URL, logging.NewLoggerWithOutput(io.Discard))
	if err := n.SetSummary(context.Background(), "run-1", "Refactor the auth module"); err != nil {
		t.Fatalf("unexpected error for 204: %v", err)
	}
	if gotPath != "/internal/runs/run-1/summary" {
		t.Errorf("path = %q, want /internal/runs/run-1/summary", gotPath)
	}
	if !strings.Contains(gotBody, "Refactor the auth module") {
		t.Errorf("body should carry the summary, got: %q", gotBody)
	}
}

func TestHTTPNotifier_SetSummary_Non2xxReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("boom")) //nolint:errcheck
	}))
	defer srv.Close()

	n := NewHTTPNotifier(srv.URL, logging.NewLoggerWithOutput(io.Discard))
	err := n.SetSummary(context.Background(), "run-1", "x")
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status code, got: %v", err)
	}
}

// ── ProvisionSandbox ──────────────────────────────────────────────────────────

func TestHTTPNotifier_ProvisionSandbox_OKReturnsEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"endpoint":"10.0.0.1:50051","exec_token":"tok-1"}`)) //nolint:errcheck
	}))
	defer srv.Close()

	n := NewHTTPNotifier(srv.URL, logging.NewLoggerWithOutput(io.Discard))
	sandbox, err := n.ProvisionSandbox(context.Background(), "run-1", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sandbox.Endpoint != "10.0.0.1:50051" {
		t.Errorf("endpoint = %q, want 10.0.0.1:50051", sandbox.Endpoint)
	}
	if sandbox.ExecToken != "tok-1" {
		t.Errorf("exec token = %q, want tok-1", sandbox.ExecToken)
	}
}

func TestHTTPNotifier_ProvisionSandbox_Non2xxReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("run not found")) //nolint:errcheck
	}))
	defer srv.Close()

	n := NewHTTPNotifier(srv.URL, logging.NewLoggerWithOutput(io.Discard))
	_, err := n.ProvisionSandbox(context.Background(), "run-1", false)
	if err == nil {
		t.Fatal("expected error for 404 response, got nil")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error should mention status code, got: %v", err)
	}
}

// The ahead flag reaches orbit-agents only as a query parameter. A dropped or
// malformed one turns every pre-provision into an on-demand request, which is
// silent and brings back the empty-kubeconfig race.
func TestHTTPNotifier_ProvisionSandbox_AheadQuery(t *testing.T) {
	for _, tc := range []struct {
		name  string
		ahead bool
		want  string
	}{
		{name: "pre-provision", ahead: true, want: "true"},
		{name: "on demand", ahead: false, want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotQuery, gotPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotQuery = r.URL.Query().Get("ahead")
				gotPath = r.URL.Path
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"endpoint":"10.0.0.5:50051","exec_token":"tok-1"}`)) //nolint:errcheck
			}))
			defer srv.Close()

			n := NewHTTPNotifier(srv.URL, logging.NewLoggerWithOutput(io.Discard))
			if _, err := n.ProvisionSandbox(context.Background(), "run-1", tc.ahead); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotQuery != tc.want {
				t.Errorf("ahead query = %q, want %q", gotQuery, tc.want)
			}
			if gotPath != "/internal/runs/run-1/provision-sandbox" {
				t.Errorf("path = %q", gotPath)
			}
		})
	}
}
