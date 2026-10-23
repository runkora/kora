package agents

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

func newTestClient(t *testing.T, status int, body string) (*Client, *capture) {
	t.Helper()
	cap := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.method = r.Method
		cap.path = r.URL.Path
		cap.query = r.URL.RawQuery
		cap.auth = r.Header.Get("Authorization")
		cap.delegation = r.Header.Get("X-Kora-Delegation-Token")
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
	method, path, query, auth, body string
	delegation                      string
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

// The trigger backlink has to reach kora-agents on the wire, under the field
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

	out, err := c.GetAgentType(context.Background(), Caller{Bearer: "tok-123"}, "at-1")
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

// A delegated caller's delegation travels with its bearer; an autonomous caller sends none.
func TestGetAgentType_ForwardsDelegation(t *testing.T) {
	for _, delegation := range []string{"del-1", ""} {
		c, cap := newTestClient(t, http.StatusOK, `{"id":"at-1"}`)
		if _, err := c.GetAgentType(context.Background(), Caller{Bearer: "tok", Delegation: delegation}, "at-1"); err != nil {
			t.Fatalf("GetAgentType: %v", err)
		}
		if cap.delegation != delegation {
			t.Errorf("delegation header = %q, want %q", cap.delegation, delegation)
		}
	}
}

// An empty bearer sends no Authorization header rather than a bare "Bearer ".
func TestGetAgentType_EmptyBearerSendsNoAuthorization(t *testing.T) {
	c, cap := newTestClient(t, http.StatusOK, `{"id":"at-1"}`)
	if _, err := c.GetAgentType(context.Background(), Caller{}, "at-1"); err != nil {
		t.Fatalf("GetAgentType: %v", err)
	}
	if cap.auth != "" {
		t.Errorf("auth header = %q, want none", cap.auth)
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
		_, err := c.GetAgentType(context.Background(), Caller{Bearer: "tok"}, "at-1")
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
		// count failures against the requester (kora-triggers' auto-pause) rely
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

func TestCanReadRun(t *testing.T) {
	cases := []struct {
		status int
		want   error
	}{
		{http.StatusOK, nil},
		{http.StatusForbidden, ErrForbidden},
		{http.StatusUnauthorized, ErrForbidden},
		{http.StatusNotFound, ErrRunNotFound},
		{http.StatusBadGateway, ErrUpstreamUnavailable},
		{http.StatusTeapot, ErrUnexpectedStatus},
	}
	for _, tc := range cases {
		var gotPath, gotAuth, gotDelegation string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath, gotAuth, gotDelegation = r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("X-Kora-Delegation-Token")
			w.WriteHeader(tc.status)
		}))
		err := NewClient(srv.URL, logging.NewLoggerWithOutput(io.Discard)).CanReadRun(context.Background(), "tok", "deleg", "run-1")
		srv.Close()
		if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
			t.Errorf("status %d: err = %v, want %v", tc.status, err, tc.want)
		}
		if gotPath != "/api/v1/runs/run-1" || gotAuth != "Bearer tok" || gotDelegation != "deleg" {
			t.Errorf("status %d: request %s with %q, want /api/v1/runs/run-1 with the caller's bearer", tc.status, gotPath, gotAuth)
		}
	}
}

func TestSendRunMessage_Success(t *testing.T) {
	c, cap := newTestClient(t, http.StatusAccepted, "")

	err := c.SendRunMessage(context.Background(), "tok-123", "run-1", SendRunMessageRequest{
		Content:     "new comment",
		TriggerID:   "trig-1",
		TriggerName: "PR comments",
		DeliveryID:  "deliv-1",
	})
	if err != nil {
		t.Fatalf("SendRunMessage: %v", err)
	}
	if cap.method != http.MethodPost || cap.path != "/api/v1/runs/run-1/messages" {
		t.Errorf("request = %s %s", cap.method, cap.path)
	}
	if cap.auth != "Bearer tok-123" {
		t.Errorf("auth header = %q, want Bearer tok-123", cap.auth)
	}
	want := `{"content":"new comment","trigger_id":"trig-1","trigger_name":"PR comments","delivery_id":"deliv-1"}`
	if strings.TrimSpace(cap.body) != want {
		t.Errorf("body = %s, want %s", cap.body, want)
	}
}

func TestSendRunMessage_StatusMapping(t *testing.T) {
	tests := []struct {
		status int
		want   error
	}{
		{http.StatusForbidden, ErrForbidden},
		{http.StatusUnauthorized, ErrForbidden},
		{http.StatusNotFound, ErrRunNotFound},
		{http.StatusConflict, ErrRunNotAcceptingMessages},
		{http.StatusInternalServerError, ErrUpstreamUnavailable},
		{http.StatusTeapot, ErrUnexpectedStatus},
	}
	for _, tt := range tests {
		c, _ := newTestClient(t, tt.status, `{"error":"x"}`)
		err := c.SendRunMessage(context.Background(), "tok", "run-1", SendRunMessageRequest{Content: "hi"})
		if !errors.Is(err, tt.want) {
			t.Errorf("status %d: got %v, want %v", tt.status, err, tt.want)
		}
	}
}

func TestListRuns_Query(t *testing.T) {
	tests := []struct {
		name      string
		q         ListRunsQuery
		wantQuery string
	}{
		{name: "no filter", q: ListRunsQuery{}, wantQuery: ""},
		{
			name:      "every filter",
			q:         ListRunsQuery{AgentTypeIDs: []string{"at-1", "at-2"}, States: []string{"active", "idle"}, StartedByTrigger: true, Limit: 20},
			wantQuery: "agent_type_id=at-1&agent_type_id=at-2&limit=20&started_by=trigger&state=active&state=idle",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, cap := newTestClient(t, http.StatusOK, `{"runs":[{"id":"run-1","agent_type_id":"at-1","state":"idle","summary":"Review PR 12","created_at":"2026-10-19T01:02:03Z","updated_at":"2026-10-19T01:05:00Z","started_by":{"trigger_id":"trig-1","trigger_name":"PR comments"}}],"next_cursor":null,"limit":20}`)
			runs, err := c.ListRuns(context.Background(), "tok-123", tt.q)
			if err != nil {
				t.Fatalf("ListRuns: %v", err)
			}
			if cap.method != http.MethodGet || cap.path != "/api/v1/runs" || cap.query != tt.wantQuery {
				t.Errorf("request = %s %s?%s, want GET /api/v1/runs?%s", cap.method, cap.path, cap.query, tt.wantQuery)
			}
			if cap.auth != "Bearer tok-123" {
				t.Errorf("auth header = %q, want Bearer tok-123", cap.auth)
			}
			if len(runs) != 1 || runs[0].ID != "run-1" || runs[0].State != "idle" || runs[0].Summary == nil ||
				runs[0].StartedBy == nil || runs[0].StartedBy.TriggerName != "PR comments" {
				t.Errorf("runs = %+v", runs)
			}
		})
	}
}

func TestListRuns_StatusMapping(t *testing.T) {
	tests := []struct {
		status int
		want   error
	}{
		{http.StatusForbidden, ErrForbidden},
		{http.StatusUnauthorized, ErrForbidden},
		{http.StatusBadRequest, ErrUnexpectedStatus},
		{http.StatusServiceUnavailable, ErrUpstreamUnavailable},
	}
	for _, tt := range tests {
		c, _ := newTestClient(t, tt.status, `{"error":"x"}`)
		if _, err := c.ListRuns(context.Background(), "tok", ListRunsQuery{}); !errors.Is(err, tt.want) {
			t.Errorf("status %d: got %v, want %v", tt.status, err, tt.want)
		}
	}
}

func TestSendRunMessage_ConflictReason(t *testing.T) {
	for body, want := range map[string]error{
		`{"code":"conflict","message":"run is starting","details":{"reason":"run_starting"}}`:             ErrRunStarting,
		`{"code":"conflict","message":"run is not accepting messages","details":{"reason":"run_closed"}}`: ErrRunNotAcceptingMessages,
		`not json`: ErrRunNotAcceptingMessages,
	} {
		c, _ := newTestClient(t, http.StatusConflict, body)
		err := c.SendRunMessage(context.Background(), "tok", "run-1", SendRunMessageRequest{Content: "hi"})
		if !errors.Is(err, want) || !strings.Contains(err.Error(), "status 409") {
			t.Errorf("body %s: got %v, want %v", body, err, want)
		}
	}
}

func TestListAgentTypes(t *testing.T) {
	c, cap := newTestClient(t, http.StatusOK, `{"agent_types":[{"id":"at-1","tenant_id":"t1","slug":"coder","name":"Coder","description":"Implements changes.","identity_mode":"autonomous","can_run":true}]}`)
	types, err := c.ListAgentTypes(context.Background(), Caller{Bearer: "tok-123", Delegation: "deleg"})
	if err != nil {
		t.Fatalf("ListAgentTypes: %v", err)
	}
	if cap.method != http.MethodGet || cap.path != "/api/v1/agent-types" || cap.auth != "Bearer tok-123" || cap.delegation != "deleg" {
		t.Errorf("request = %s %s (auth %q, delegation %q)", cap.method, cap.path, cap.auth, cap.delegation)
	}
	if len(types) != 1 || types[0].Name != "Coder" || types[0].Description != "Implements changes." || types[0].Slug != "coder" || !types[0].CanRun {
		t.Errorf("types = %+v", types)
	}
}

func TestListAgentTypes_StatusMapping(t *testing.T) {
	tests := []struct {
		status int
		want   error
	}{
		{http.StatusForbidden, ErrForbidden},
		{http.StatusUnauthorized, ErrForbidden},
		{http.StatusBadRequest, ErrUnexpectedStatus},
		{http.StatusServiceUnavailable, ErrUpstreamUnavailable},
	}
	for _, tt := range tests {
		c, _ := newTestClient(t, tt.status, `{"error":"x"}`)
		if _, err := c.ListAgentTypes(context.Background(), Caller{Bearer: "tok"}); !errors.Is(err, tt.want) {
			t.Errorf("status %d: got %v, want %v", tt.status, err, tt.want)
		}
	}
}

func TestListAgentTypesByIDs(t *testing.T) {
	c, cap := newTestClient(t, http.StatusOK, `{"agent_types":[{"id":"at-1","slug":"coder","name":"Coder","mcp_tools":["mcp:github:issue_read:use"],"effective_connectors":[{"connection":"gh","actions":["contents:read"]}]}]}`)
	types, err := c.ListAgentTypesByIDs(context.Background(), Caller{Bearer: "tok-123"}, []string{"at-1", "at-2"})
	if err != nil {
		t.Fatalf("ListAgentTypesByIDs: %v", err)
	}
	if cap.path != "/api/v1/agent-types" || cap.query != "ids=at-1%2Cat-2&include=effective_access" || cap.auth != "Bearer tok-123" {
		t.Errorf("request = %s ?%s (auth %q)", cap.path, cap.query, cap.auth)
	}
	if len(types) != 1 || len(types[0].MCPTools) != 1 || len(types[0].EffectiveConnectors) != 1 || types[0].EffectiveConnectors[0].Connection != "gh" {
		t.Errorf("types = %+v", types)
	}
}
