package sessionmanager

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── Notify ────────────────────────────────────────────────────────────────────

func TestHTTPNotifier_Notify_2xxSucceeds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	n := NewHTTPNotifier(srv.URL)
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

	n := NewHTTPNotifier(srv.URL)
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

	n := NewHTTPNotifier(srv.URL)
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

	n := NewHTTPNotifier(srv.URL)
	if err := n.CompleteRun(context.Background(), "run-1", false); err != nil {
		t.Errorf("unexpected error for 204: %v", err)
	}
}

func TestHTTPNotifier_CompleteRun_Non2xxReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte("state conflict")) //nolint:errcheck
	}))
	defer srv.Close()

	n := NewHTTPNotifier(srv.URL)
	err := n.CompleteRun(context.Background(), "run-1", false)
	if err == nil {
		t.Fatal("expected error for 409 response, got nil")
	}
	if !strings.Contains(err.Error(), "409") {
		t.Errorf("error should mention status code, got: %v", err)
	}
}

// ── ProvisionSandbox ──────────────────────────────────────────────────────────

func TestHTTPNotifier_ProvisionSandbox_OKReturnsEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"endpoint":"10.0.0.1:50051"}`)) //nolint:errcheck
	}))
	defer srv.Close()

	n := NewHTTPNotifier(srv.URL)
	endpoint, err := n.ProvisionSandbox(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if endpoint != "10.0.0.1:50051" {
		t.Errorf("endpoint = %q, want 10.0.0.1:50051", endpoint)
	}
}

func TestHTTPNotifier_ProvisionSandbox_Non2xxReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("run not found")) //nolint:errcheck
	}))
	defer srv.Close()

	n := NewHTTPNotifier(srv.URL)
	_, err := n.ProvisionSandbox(context.Background(), "run-1")
	if err == nil {
		t.Fatal("expected error for 404 response, got nil")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error should mention status code, got: %v", err)
	}
}
