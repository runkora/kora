package agents

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWriteSSE(t *testing.T) {
	at := time.Date(2026, 9, 29, 14, 23, 17, 0, time.UTC)
	tests := []struct {
		name    string
		seq     int64
		payload string
		want    string
	}{
		{name: "durable", seq: 7, payload: `{"text": "hi"}`, want: "id: 7\nevent: e\ndata: {\"at\":\"2026-09-29T14:23:17Z\",\"payload\":{\"text\": \"hi\"}}\n\n"},
		{name: "streaming-only has no id", payload: `{"delta":"x"}`, want: "event: e\ndata: {\"at\":\"2026-09-29T14:23:17Z\",\"payload\":{\"delta\":\"x\"}}\n\n"},
		{name: "no payload", payload: ``, want: "event: e\ndata: {\"at\":\"2026-09-29T14:23:17Z\",\"payload\":{}}\n\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b strings.Builder
			writeSSE(&b, tt.seq, "e", at, json.RawMessage(tt.payload))
			if b.String() != tt.want {
				t.Errorf("writeSSE = %q, want %q", b.String(), tt.want)
			}
		})
	}
}

func TestActivityEventTypes_Persistence(t *testing.T) {
	for _, typ := range []string{"api_retry", "sandbox_status", "turn_stopped"} {
		if !durableEventType(typ) {
			t.Errorf("%s should be recorded in run_events", typ)
		}
		if replayOnlyEventType(typ) {
			t.Errorf("%s should reach live subscribers", typ)
		}
	}
	if durableEventType("tool_progress") {
		t.Error("tool_progress is a heartbeat and must not be recorded")
	}
}

type sseEnvelope struct {
	At      time.Time       `json:"at"`
	Payload json.RawMessage `json:"payload"`
}

// sseData returns the data lines of an SSE body, decoded as envelopes.
func sseData(t *testing.T, body string) []sseEnvelope {
	t.Helper()
	var out []sseEnvelope
	for line := range strings.SplitSeq(body, "\n") {
		d, ok := strings.CutPrefix(line, "data: ")
		if !ok {
			continue
		}
		var env sseEnvelope
		if err := json.Unmarshal([]byte(d), &env); err != nil {
			t.Fatalf("data %q: %v", d, err)
		}
		out = append(out, env)
	}
	return out
}

// Replay and live events both carry the server's time, and a replayed event
// carries the time it was recorded, not the time of the reload.
func TestHandleStreamEvents_EnvelopeCarriesServerTime(t *testing.T) {
	c, repo, _, _ := newTestController(t)
	seedRun(t, repo, Run{ID: "run-ts", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateIdle})
	recorded := time.Date(2026, 9, 29, 1, 2, 3, 0, time.UTC)
	if _, err := repo.AppendRunEvent(context.Background(), RunEvent{RunID: "run-ts", EventType: "turn_stopped", Payload: []byte(`{"reason":"user"}`), CreatedAt: recorded}); err != nil {
		t.Fatalf("AppendRunEvent: %v", err)
	}

	ctx, cancel := context.WithTimeout(withTenantCtx(context.Background(), defaultTestTenant), 200*time.Millisecond)
	defer cancel()
	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = c.runs.PublishEvent(context.Background(), "run-ts", PushedEvent{Type: "tool_progress", Payload: []byte(`{"elapsed_seconds":5}`)})
	}()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/run-ts/events", nil).WithContext(ctx)
	req.SetPathValue("id", "run-ts")
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	c.handleStreamEvents(w, req)

	got := sseData(t, w.Body.String())
	if len(got) != 2 {
		t.Fatalf("got %d events, want replayed + live", len(got))
	}
	if !got[0].At.Equal(recorded) || string(got[0].Payload) != `{"reason":"user"}` {
		t.Errorf("replayed = %s at %v, want the stored payload at %v", got[0].Payload, got[0].At, recorded)
	}
	if time.Since(got[1].At) > time.Minute {
		t.Errorf("live event at %v, want the publish time", got[1].At)
	}
}

// SendMessage activates an idle run before forwarding the message, and the
// session-manager reports the same transition when the turn starts.
func TestTransitionRunState_IdleToActiveIsIdempotent(t *testing.T) {
	tests := []struct {
		name    string
		state   RunState
		from    RunState
		to      RunState
		wantErr bool
	}{
		{name: "already active", state: RunStateActive, from: RunStateIdle, to: RunStateActive},
		{name: "idle", state: RunStateIdle, from: RunStateIdle, to: RunStateActive},
		{name: "terminal run still conflicts", state: RunStateCompleted, from: RunStateIdle, to: RunStateActive, wantErr: true},
		{name: "other transitions still conflict", state: RunStateIdle, from: RunStateActive, to: RunStateIdle, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, repo, _, _ := newTestController(t)
			seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: "at-1", State: tt.state})
			err := c.runs.TransitionRunState(context.Background(), "run-1", tt.from, tt.to)
			if (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
