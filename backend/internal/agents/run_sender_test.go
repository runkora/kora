package agents

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/golang-jwt/jwt/v5"

	"github.com/runkora/kora/backend/internal/platform/token"
)

func TestMessageSender(t *testing.T) {
	tests := []struct {
		name   string
		claims *token.PlatformClaims
		want   *MessageSender
	}{
		{
			name: "a person carries their display name",
			claims: &token.PlatformClaims{RegisteredClaims: jwt.RegisteredClaims{Subject: "user-1"},
				Kora: token.KoraClaims{PrincipalType: token.PrincipalTypeHuman, DisplayName: "Dev User"}},
			want: &MessageSender{ID: "user-1", Type: "USER", Name: "Dev User"},
		},
		{
			name: "a person without a name falls back to their email",
			claims: &token.PlatformClaims{RegisteredClaims: jwt.RegisteredClaims{Subject: "user-1"},
				Kora: token.KoraClaims{PrincipalType: token.PrincipalTypePAT, Claims: map[string]string{"email": "dev@example.com"}}},
			want: &MessageSender{ID: "user-1", Type: "USER", Name: "dev@example.com"},
		},
		{
			name: "an agent is a sender",
			claims: &token.PlatformClaims{RegisteredClaims: jwt.RegisteredClaims{Subject: "spiffe://kora/tenant/t/agent/coder"},
				Kora: token.KoraClaims{PrincipalType: token.PrincipalTypeAgent, DisplayName: "coder"}},
			want: &MessageSender{ID: "spiffe://kora/tenant/t/agent/coder", Type: "AGENT", Name: "coder"},
		},
		{
			name: "a trigger is not a sender",
			claims: &token.PlatformClaims{RegisteredClaims: jwt.RegisteredClaims{Subject: "trigger:t1"},
				Kora: token.KoraClaims{PrincipalType: token.PrincipalTypeService}},
		},
		{name: "no claims, no sender"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.claims != nil {
				ctx = token.WithClaims(ctx, tt.claims)
			}
			if got := messageSender(ctx); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("messageSender = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestSendMessage_RecordsSender(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot", MaxConcurrentRunsPerPod: 5})
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateActive})

	ctx := withPrincipalCtx(context.Background(), "t-1", token.PrincipalTypeHuman, "user-1")
	if err := svc.run.SendMessage(ctx, "run-1", SendMessageRequest{Content: "hi"}); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	want := &MessageSender{ID: "user-1", Type: "USER", Name: "user-1"}
	if len(repo.messages) != 1 || !reflect.DeepEqual(repo.messages[0].Sender, want) {
		t.Errorf("stored messages = %+v, want one from %+v", repo.messages, want)
	}
}

func TestStartRun_FirstMessageSender(t *testing.T) {
	tests := []struct {
		name       string
		principal  token.PrincipalType
		subject    string
		req        StartRunRequest
		wantSender *MessageSender
	}{
		{
			name: "a person's run names its starter", principal: token.PrincipalTypeHuman, subject: "user-1",
			req:        StartRunRequest{InitialMessage: "fix the build"},
			wantSender: &MessageSender{ID: "user-1", Type: "USER", Name: "user-1"},
		},
		{
			name: "a trigger's run has no sender", principal: token.PrincipalTypeService, subject: "trigger:trig-1",
			req: StartRunRequest{InitialMessage: "fix the build", TriggerID: "trig-1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, repo, _, _ := newTestService(t)
			at := seedAutonomousAgentType(t, repo, "at-1", "t1", "reviewer")
			tt.req.AgentTypeID = at.ID
			ctx := withPrincipalCtx(context.Background(), "t1", tt.principal, tt.subject)
			run, err := startAndSchedule(ctx, f.run, tt.req)
			if err != nil {
				t.Fatalf("startAndSchedule: %v", err)
			}
			events, err := repo.ListRunEvents(ctx, run.ID, 0)
			if err != nil {
				t.Fatalf("ListRunEvents: %v", err)
			}
			var got []humanMessagePayload
			for _, e := range events {
				if e.EventType == eventHumanMessage {
					var p humanMessagePayload
					if err := json.Unmarshal(e.Payload, &p); err != nil {
						t.Fatalf("decode %s: %v", e.Payload, err)
					}
					got = append(got, p)
				}
			}
			want := []humanMessagePayload{{Content: "fix the build", Sender: tt.wantSender}}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("human messages = %+v, want %+v", got, want)
			}
			if events[0].EventType != eventHumanMessage {
				t.Errorf("first event = %s, want the first message", events[0].EventType)
			}
		})
	}
}
