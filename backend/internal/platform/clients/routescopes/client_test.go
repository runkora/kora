package routescopes

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

func TestRegister_SendsBodyAndBearer(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody registrationRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	token := func(context.Context) (string, error) { return "self-token", nil }
	c := NewClient(srv.URL, token, logging.NewLoggerWithOutput(io.Discard))

	if err := c.RegisterWithPrune(t.Context(), "tool-gateway", testRoutes, testScopes, false); err != nil {
		t.Fatalf("RegisterWithPrune: %v", err)
	}
	if gotPath != "/api/v1/policy/routes/register" {
		t.Errorf("path = %q, want /api/v1/policy/routes/register", gotPath)
	}
	if gotAuth != "Bearer self-token" {
		t.Errorf("Authorization = %q, want Bearer self-token", gotAuth)
	}
	if gotBody.Service != "tool-gateway" || !gotBody.SkipPrune || len(gotBody.Scopes) != 1 {
		t.Errorf("body = %+v, want service=tool-gateway, skip_prune, one scope", gotBody)
	}
}

func TestRegister_TokenErrorSendsNothing(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	errNoToken := errors.New("identity unavailable")
	token := func(context.Context) (string, error) { return "", errNoToken }
	c := NewClient(srv.URL, token, logging.NewLoggerWithOutput(io.Discard))

	if err := c.Register(t.Context(), "inference-gateway", testRoutes, nil); !errors.Is(err, errNoToken) {
		t.Fatalf("Register error = %v, want %v", err, errNoToken)
	}
	if called {
		t.Error("registration was sent without a token")
	}
}

func TestRegister_Non204IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	token := func(context.Context) (string, error) { return "self-token", nil }
	c := NewClient(srv.URL, token, logging.NewLoggerWithOutput(io.Discard))

	if err := c.Register(t.Context(), "orbit-agents", testRoutes, nil); !errors.Is(err, ErrRegistrationFailed) {
		t.Fatalf("Register error = %v, want %v", err, ErrRegistrationFailed)
	}
}
