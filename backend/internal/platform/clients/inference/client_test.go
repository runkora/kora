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

	"github.com/runkora/kora/backend/internal/platform/logging"
)

type capture struct {
	method, path, auth string
	body               []byte
}

func newTestServer(t *testing.T, status int, body string) (*httptest.Server, *capture) {
	t.Helper()
	cap := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.method = r.Method
		cap.path = r.URL.Path
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
		{http.StatusNotFound, ErrUnexpectedStatus},
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
