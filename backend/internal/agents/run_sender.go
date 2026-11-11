package agents

import (
	"context"

	"github.com/runkora/kora/backend/internal/platform/events"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// MessageSender is the person or agent who sent a message to a run.
// See docs/adr/2026-11-11-run-page-record.md, Decision 1.
type MessageSender struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Name string `json:"name"`
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
		return &MessageSender{ID: claims.Subject, Type: events.PrincipalTypeOf(claims), Name: events.PrincipalNameOf(claims)}
	}
	return nil
}
