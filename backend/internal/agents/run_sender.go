package agents

import (
	"context"

	"github.com/runkora/kora/backend/internal/platform/clients/sessionmanager"
	"github.com/runkora/kora/backend/internal/platform/events"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// MessageSender is the person or agent who sent a message to a run.
// See docs/adr/2026-11-11-run-page-record.md, Decision 1.
type MessageSender struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Name string `json:"name"`
	// Email is the sender's email when the token has one. It reaches the agent in the message header.
	Email string `json:"email,omitempty"`
}

// messageSender is the sender the request's claims name: a person or an agent.
// A trigger and the platform are not senders.
func messageSender(ctx context.Context) *MessageSender {
	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		return nil
	}
	switch claims.Kora.PrincipalType {
	case token.PrincipalTypeHuman, token.PrincipalTypePAT, token.PrincipalTypeAgent:
		return &MessageSender{ID: claims.Subject, Type: events.PrincipalTypeOf(claims), Name: events.PrincipalNameOf(claims), Email: claims.Kora.Claims["email"]}
	}
	return nil
}

// agentFrom is the sender the agent is told about for a message from a person or an agent.
func (m *MessageSender) agentFrom() *sessionmanager.From {
	if m == nil {
		return nil
	}
	kind := "person"
	if m.Type == events.PrincipalAgent {
		kind = "agent"
	}
	return &sessionmanager.From{Kind: kind, ID: m.ID, Name: m.Name, Email: m.Email}
}

// agentFrom is the sender the agent is told about for a message from a trigger.
// An empty trigger still marks the message as automated.
func (t *StartedByTrigger) agentFrom() *sessionmanager.From {
	if t == nil {
		return nil
	}
	return &sessionmanager.From{Kind: "trigger", ID: t.TriggerID, Name: t.TriggerName, DeliveryID: t.DeliveryID}
}
