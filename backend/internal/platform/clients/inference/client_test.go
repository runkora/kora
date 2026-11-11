package inference

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/clients/httpclient"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

type capture struct {
	method, path, query, auth string
	body                      []byte
}

func newTestServer(t *testing.T, status int, body string) (*httptest.Server, *capture) {
	t.Helper()
	cap := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.method = r.Method
		cap.path = r.URL.Path
		cap.query = r.URL.RawQuery
		cap.auth = r.Header.Get("Authorization")
		cap.body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, cap
}

func routingRequest() SystemOneRequest {
	return SystemOneRequest{
		Model: "liquid/d1",
		State: map[string]string{"message": "a new comment on PR 1427"},
		Questions: map[string]Question{
			"agent": {
				Type:         QuestionTypeChoice,
				Instructions: "Which agent should handle the message?",
				Criteria:     map[string]string{"a1": "Code reviewer", "a2": "Coder"},
			},
		},
	}
}

func TestSystemOne_Success(t *testing.T) {
	srv, cap := newTestServer(t, http.StatusOK, `{"model":"liquid/d1","answers":{"agent":{"type":"choice","choice":"a1","confidence":0.82,"probabilities":{"a1":0.91,"a2":0.09}}},"usage":{"input_tokens":392,"output_tokens":65}}`)
	c := NewClient(srv.URL, logging.NewLoggerWithOutput(io.Discard))

	out, err := c.SystemOne(context.Background(), "tok-123", routingRequest())
	if err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	if cap.method != http.MethodPost || cap.path != "/v1/systemone" {
		t.Errorf("request = %s %s, want POST /v1/systemone", cap.method, cap.path)
	}
	if cap.auth != "Bearer tok-123" {
		t.Errorf("auth header = %q, want Bearer tok-123", cap.auth)
	}
	var sent map[string]any
	if err := json.Unmarshal(cap.body, &sent); err != nil {
		t.Fatalf("request body is not JSON: %v", err)
	}
	q := sent["questions"].(map[string]any)["agent"].(map[string]any)
	if sent["model"] != "liquid/d1" || sent["state"].(map[string]any)["message"] == nil || q["type"] != "choice" || q["criteria"].(map[string]any)["a2"] != "Coder" {
		t.Errorf("request body = %s", cap.body)
	}

	a, ok := out.Answers["agent"]
	if !ok {
		t.Fatalf("answers = %+v, want an agent answer", out.Answers)
	}
	if a.Choice != "a1" || a.Probabilities["a1"] != 0.91 || a.Probabilities["a2"] != 0.09 || a.Confidence != 0.82 {
		t.Errorf("answer = %+v", a)
	}
	if out.Model != "liquid/d1" || out.Usage.InputTokens != 392 || out.Usage.OutputTokens != 65 {
		t.Errorf("response = %+v", out)
	}
}

func TestSystemOne_StatusMapping(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   error
	}{
		{http.StatusBadRequest, ErrInvalidRequest},
		{http.StatusUnprocessableEntity, ErrInvalidRequest},
		{http.StatusUnauthorized, ErrForbidden},
		{http.StatusForbidden, ErrForbidden},
		{http.StatusTooManyRequests, ErrRateLimited},
		{http.StatusInternalServerError, ErrUpstreamUnavailable},
		{http.StatusServiceUnavailable, ErrUpstreamUnavailable},
		{529, ErrUpstreamUnavailable},
		{http.StatusNotFound, ErrModelNotFound},
		{http.StatusTeapot, ErrUnexpectedStatus},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			srv, _ := newTestServer(t, tc.status, `{"code":"x","message":"refused"}`)
			c := NewClient(srv.URL, logging.NewLoggerWithOutput(io.Discard))
			_, err := c.SystemOne(context.Background(), "tok", routingRequest())
			if !errors.Is(err, tc.want) {
				t.Errorf("status %d: err = %v, want %v", tc.status, err, tc.want)
			}
		})
	}
}

func TestSystemOne_TimeoutIsUpstreamUnavailable(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	c := newClient(srv.URL, logging.NewLoggerWithOutput(io.Discard), 50*time.Millisecond)
	_, err := c.SystemOne(context.Background(), "tok", routingRequest())
	if !errors.Is(err, ErrUpstreamUnavailable) {
		t.Errorf("err = %v, want ErrUpstreamUnavailable", err)
	}
}

func TestSystemOne_BadBaseURLIsNotUpstreamUnavailable(t *testing.T) {
	c := NewClient("http://bad host", logging.NewLoggerWithOutput(io.Discard))
	_, err := c.SystemOne(context.Background(), "tok", routingRequest())
	if !errors.Is(err, httpclient.ErrBuildRequest) || errors.Is(err, ErrUpstreamUnavailable) {
		t.Errorf("err = %v, want ErrBuildRequest only", err)
	}
}

func TestRunBudget(t *testing.T) {
	const runID = "6f1c2a1e-6a59-4c55-9a43-0f1d3c1b2e7a"
	t.Run("the reached ceiling", func(t *testing.T) {
		srv, cap := newTestServer(t, http.StatusOK, `{"scope":"delegator","owner":"user-1","owner_name":"Dev User","window":"monthly","limit_usd":5,"resets_at":"2026-12-01T00:00:00Z"}`)
		c := NewClient(srv.URL, logging.NewLoggerWithOutput(io.Discard))
		got, err := c.RunBudget(context.Background(), "svc-tok", "t1", runID)
		if err != nil {
			t.Fatalf("RunBudget: %v", err)
		}
		if cap.method != http.MethodGet || cap.path != "/v1/budgets/runs/"+runID || cap.query != "tenant_id=t1" || cap.auth != "Bearer svc-tok" {
			t.Errorf("request = %s %s?%s %q, want the run's tenant in the query", cap.method, cap.path, cap.query, cap.auth)
		}
		want := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
		if got.Scope != "delegator" || got.OwnerName != "Dev User" || got.Window != "monthly" || got.LimitUSD != 5 || got.ResetsAt == nil || !got.ResetsAt.Equal(want) {
			t.Errorf("budget = %+v", got)
		}
	})
	for _, tt := range []struct {
		status int
		body   string
		want   error
	}{
		{http.StatusNotFound, `{"code":"budget_not_reached","message":"y"}`, ErrBudgetNotReached},
		{http.StatusNotFound, `{"code":"run_usage_not_found","message":"y"}`, ErrUnexpectedStatus},
		{http.StatusNotFound, `404 page not found`, ErrUnexpectedStatus},
		{http.StatusForbidden, `{"code":"x","message":"y"}`, ErrForbidden},
		{http.StatusInternalServerError, `{"code":"x","message":"y"}`, ErrUpstreamUnavailable},
		{http.StatusTeapot, `{"code":"x","message":"y"}`, ErrUnexpectedStatus},
	} {
		srv, _ := newTestServer(t, tt.status, tt.body)
		c := NewClient(srv.URL, logging.NewLoggerWithOutput(io.Discard))
		if _, err := c.RunBudget(context.Background(), "svc-tok", "t1", runID); !errors.Is(err, tt.want) {
			t.Errorf("status %d %s: err = %v, want %v", tt.status, tt.body, err, tt.want)
		}
	}
}
