package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/token"
)

func validRunEvent() *RunEventSummary {
	return &RunEventSummary{
		Actor:  RunEventActor{Name: "octocat", Kind: "person", URL: "https://github.com/octocat"},
		Verb:   "opened",
		Object: RunEventObject{Label: "PR #6", URL: "https://github.com/o/r/pull/6"},
		Quote:  "Please review",
		Preview: &RunEventPreview{
			Title:  "Median: sort a copy",
			Facts:  []string{"o/r #6", "fix → main"},
			Status: &RunEventStatus{Label: "Open", Tone: "ok"},
		},
	}
}

func TestAuthorizeRunEvent(t *testing.T) {
	service := withPrincipalCtx(context.Background(), "t-1", token.PrincipalTypeService, "trigger:trig-1")
	tests := []struct {
		name    string
		ctx     context.Context
		edit    func(e *RunEventSummary)
		nilSum  bool
		wantErr error
	}{
		{name: "a service principal records a valid summary", ctx: service},
		{name: "no summary is always allowed", ctx: context.Background(), nilSum: true},
		{name: "a person may not record one", ctx: withPrincipalCtx(context.Background(), "t-1", token.PrincipalTypeHuman, "user-1"), wantErr: ErrRunEventRefused},
		{name: "unknown actor kind", ctx: service, edit: func(e *RunEventSummary) { e.Actor.Kind = "robot" }, wantErr: ErrRunEventInvalid},
		{name: "missing verb", ctx: service, edit: func(e *RunEventSummary) { e.Verb = "" }, wantErr: ErrRunEventInvalid},
		{name: "a string over 300 characters", ctx: service, edit: func(e *RunEventSummary) { e.Preview.Title = strings.Repeat("é", 301) }, wantErr: ErrRunEventInvalid},
		{name: "a fact over 300 characters", ctx: service, edit: func(e *RunEventSummary) { e.Preview.Facts[0] = strings.Repeat("x", 301) }, wantErr: ErrRunEventInvalid},
		{name: "300 characters is allowed", ctx: service, edit: func(e *RunEventSummary) { e.Verb = strings.Repeat("é", 300) }},
		{name: "a quote over 2000 characters", ctx: service, edit: func(e *RunEventSummary) { e.Quote = strings.Repeat("x", 2001) }, wantErr: ErrRunEventInvalid},
		{name: "a quote of 2000 characters is allowed", ctx: service, edit: func(e *RunEventSummary) { e.Quote = strings.Repeat("x", 2000) }},
		{name: "more than 6 facts", ctx: service, edit: func(e *RunEventSummary) { e.Preview.Facts = make([]string, 7) }, wantErr: ErrRunEventInvalid},
		{name: "unknown status tone", ctx: service, edit: func(e *RunEventSummary) { e.Preview.Status.Tone = "warn" }, wantErr: ErrRunEventInvalid},
		{name: "a javascript link", ctx: service, edit: func(e *RunEventSummary) { e.Object.URL = "javascript:alert(1)" }, wantErr: ErrRunEventInvalid},
		{name: "a relative link", ctx: service, edit: func(e *RunEventSummary) { e.Actor.URL = "/users/octocat" }, wantErr: ErrRunEventInvalid},
		{name: "an http link is allowed", ctx: service, edit: func(e *RunEventSummary) { e.Object.URL = "http://prometheus:9090/graph" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := validRunEvent()
			if tt.edit != nil {
				tt.edit(e)
			}
			if tt.nilSum {
				e = nil
			}
			if err := authorizeRunEvent(tt.ctx, e); !errors.Is(err, tt.wantErr) {
				t.Errorf("authorizeRunEvent = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// The summary lands on the first human_message of a trigger-started run and on each trigger_message.
func TestRunEvent_StoredOnMessageEvents(t *testing.T) {
	f, repo, _, _ := newTestService(t)
	at := seedAutonomousAgentType(t, repo, "at-1", "t1", "reviewer")
	ctx := withPrincipalCtx(context.Background(), "t1", token.PrincipalTypeService, "trigger-source:github")
	event := validRunEvent()
	run, err := startAndSchedule(ctx, f.run, StartRunRequest{AgentTypeID: at.ID, InitialMessage: "review", TriggerID: "trig-1", Event: event})
	if err != nil {
		t.Fatalf("startAndSchedule: %v", err)
	}
	joined := validRunEvent()
	joined.Verb = "commented on"
	if err := f.run.SendMessage(ctx, run.ID, SendMessageRequest{Content: "again", TriggerID: "trig-1", Event: joined}); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	events, err := repo.ListRunEvents(ctx, run.ID, 0)
	if err != nil {
		t.Fatalf("ListRunEvents: %v", err)
	}
	got := map[string]*RunEventSummary{}
	for _, e := range events {
		if e.EventType != eventHumanMessage && e.EventType != eventTriggerMessage {
			continue
		}
		var p struct {
			Event *RunEventSummary `json:"event"`
		}
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			t.Fatalf("decode %s: %v", e.Payload, err)
		}
		got[e.EventType] = p.Event
	}
	want := map[string]*RunEventSummary{eventHumanMessage: event, eventTriggerMessage: joined}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("message events carry %+v, want %+v", got, want)
	}
}

func TestStartRun_RefusesEventFromPerson(t *testing.T) {
	f, repo, _, _ := newTestService(t)
	at := seedAutonomousAgentType(t, repo, "at-1", "t1", "reviewer")
	ctx := withPrincipalCtx(context.Background(), "t1", token.PrincipalTypeHuman, "user-1")
	if _, err := f.run.StartRun(ctx, StartRunRequest{AgentTypeID: at.ID, Event: validRunEvent()}); !errors.Is(err, ErrRunEventRefused) {
		t.Errorf("StartRun = %v, want ErrRunEventRefused", err)
	}
}

// A person's summary is a 403 and a malformed service summary a 400, on start and on join.
func TestRunEvent_HandlerStatus(t *testing.T) {
	bad := validRunEvent()
	bad.Preview.Facts = make([]string, 7)
	tests := []struct {
		name      string
		principal token.PrincipalType
		subject   string
		event     *RunEventSummary
		want      int
	}{
		{"a person", token.PrincipalTypeHuman, "user-1", validRunEvent(), http.StatusForbidden},
		{"a malformed summary", token.PrincipalTypeService, "trigger:trig-1", bad, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, repo, _, _ := newTestController(t)
			at := seedAutonomousAgentType(t, repo, "at-1", "t1", "reviewer")
			seedRun(t, repo, Run{ID: "run-1", TenantID: "t1", AgentTypeID: at.ID, State: RunStateActive})
			ctx := withPrincipalCtx(context.Background(), "t1", tt.principal, tt.subject)

			start, _ := json.Marshal(StartRunRequest{AgentTypeID: at.ID, InitialMessage: "go", Event: tt.event})
			req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewReader(start)).WithContext(ctx)
			w := httptest.NewRecorder()
			c.handleStartRun(w, req)
			if w.Code != tt.want {
				t.Errorf("start status = %d, want %d (body: %s)", w.Code, tt.want, w.Body)
			}

			msg, _ := json.Marshal(SendMessageRequest{Content: "more", Event: tt.event})
			req = httptest.NewRequest(http.MethodPost, "/api/v1/runs/run-1/messages", bytes.NewReader(msg)).WithContext(ctx)
			req.SetPathValue("id", "run-1")
			w = httptest.NewRecorder()
			c.handleSendMessage(w, req)
			if w.Code != tt.want {
				t.Errorf("message status = %d, want %d (body: %s)", w.Code, tt.want, w.Body)
			}
		})
	}
}
