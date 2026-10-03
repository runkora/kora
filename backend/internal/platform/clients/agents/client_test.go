package agents

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
)

func newTestClient(t *testing.T, status int, body string) (*Client, *capture) {
	t.Helper()
	cap := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.method = r.Method
		cap.path = r.URL.Path
		cap.auth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		cap.body = string(raw)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if body != "" {
			_, _ = w.Write([]byte(body))
		}
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, logging.NewLoggerWithOutput(io.Discard)), cap
}

type capture struct {
	method, path, auth, body string
}

func TestStartRun_Success(t *testing.T) {
	c, cap := newTestClient(t, http.StatusCreated, `{"id":"run-1","state":"active"}`)

	out, err := c.StartRun(context.Background(), "tok-123", StartRunRequest{
		AgentTypeID:    "at-1",
		InitialMessage: "do the thing",
	})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if out.ID != "run-1" || out.State != "active" {
		t.Errorf("response = %+v", out)
	}
	if cap.method != http.MethodPost || cap.path != "/api/v1/runs" {
		t.Errorf("request = %s %s", cap.method, cap.path)
	}
	if cap.auth != "Bearer tok-123" {
		t.Errorf("auth header = %q, want Bearer tok-123", cap.auth)
	}
	if !strings.Contains(cap.body, `"agent_type_id":"at-1"`) {
		t.Errorf("body missing agent_type_id: %s", cap.body)
	}
}

// The trigger backlink has to reach orbit-agents on the wire, under the field
// names the server decodes.
func TestStartRun_SendsTriggerAttribution(t *testing.T) {
	c, cap := newTestClient(t, http.StatusCreated, `{"id":"run-1","state":"active"}`)

	if _, err := c.StartRun(context.Background(), "tok-123", StartRunRequest{
		AgentTypeID: "at-1",
		TriggerID:   "trig-1",
		TriggerName: "Nightly review",
		DeliveryID:  "deliv-1",
	}); err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	for _, want := range []string{
		`"trigger_id":"trig-1"`,
		`"trigger_name":"Nightly review"`,
		`"delivery_id":"deliv-1"`,
	} {
		if !strings.Contains(cap.body, want) {
			t.Errorf("body missing %s: %s", want, cap.body)
		}
	}
}

// A run nobody attributed sends no attribution at all, rather than empty
// strings the server would have to interpret.
func TestStartRun_OmitsAbsentAttribution(t *testing.T) {
	c, cap := newTestClient(t, http.StatusCreated, `{"id":"run-1","state":"active"}`)

	if _, err := c.StartRun(context.Background(), "tok-123", StartRunRequest{AgentTypeID: "at-1"}); err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	for _, unwanted := range []string{"trigger_id", "trigger_name", "delivery_id"} {
		if strings.Contains(cap.body, unwanted) {
			t.Errorf("body should omit %s: %s", unwanted, cap.body)
		}
	}
}

func TestGetAgentType_Success(t *testing.T) {
	c, cap := newTestClient(t, http.StatusOK, `{"id":"at-1","tenant_id":"t-1","slug":"code-reviewer"}`)

	out, err := c.GetAgentType(context.Background(), "tok-123", "at-1")
	if err != nil {
		t.Fatalf("GetAgentType: %v", err)
	}
	if out.ID != "at-1" || out.TenantID != "t-1" || out.Slug != "code-reviewer" {
		t.Errorf("response = %+v", out)
	}
	if cap.method != http.MethodGet || cap.path != "/api/v1/agent-types/at-1" {
		t.Errorf("request = %s %s", cap.method, cap.path)
	}
	if cap.auth != "Bearer tok-123" {
		t.Errorf("auth header = %q, want Bearer tok-123", cap.auth)
	}
}

func TestGetAgentType_StatusMapping(t *testing.T) {
	tests := []struct {
		status int
		want   error
	}{
		{http.StatusForbidden, ErrForbidden},
		{http.StatusUnauthorized, ErrForbidden},
		{http.StatusNotFound, ErrAgentTypeNotFound},
		{http.StatusInternalServerError, ErrUnexpectedStatus},
	}
	for _, tt := range tests {
		c, _ := newTestClient(t, tt.status, `{"error":"x"}`)
		_, err := c.GetAgentType(context.Background(), "tok", "at-1")
		if !errors.Is(err, tt.want) {
			t.Errorf("status %d: got %v, want %v", tt.status, err, tt.want)
		}
	}
}

func TestStartRun_StatusMapping(t *testing.T) {
	tests := []struct {
		status int
		want   error
	}{
		{http.StatusForbidden, ErrForbidden},
		{http.StatusUnauthorized, ErrForbidden},
		{http.StatusNotFound, ErrAgentTypeNotFound},
		// 5xx is the service failing, not the request being wrong. Callers that
		// count failures against the requester (orbit-triggers' auto-pause) rely
		// on telling the two apart.
		{http.StatusInternalServerError, ErrUpstreamUnavailable},
		{http.StatusBadGateway, ErrUpstreamUnavailable},
		{http.StatusTeapot, ErrUnexpectedStatus},
	}
	for _, tt := range tests {
		c, _ := newTestClient(t, tt.status, `{"error":"x"}`)
		_, err := c.StartRun(context.Background(), "tok", StartRunRequest{AgentTypeID: "a"})
		if !errors.Is(err, tt.want) {
			t.Errorf("status %d: got %v, want %v", tt.status, err, tt.want)
		}
	}
}
