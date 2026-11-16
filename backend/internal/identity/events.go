package identity

import (
	"context"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/runkora/kora/backend/internal/platform/events"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// newSessionEntity leaves out the display name, which the envelope's koraprincipalname carries.
func newSessionEntity(s *Session) events.SessionPayload {
	return events.SessionPayload{
		ID: s.ID, PrincipalID: s.PrincipalID, PrincipalType: string(s.PrincipalType),
		IdentitySource: string(s.IdentitySource), CreatedAt: s.CreatedAt, ExpiresAt: s.ExpiresAt,
	}
}

func newPATEntity(p *PAT) events.PATPayload {
	return events.PATPayload{
		ID: p.ID, UserID: p.UserID, Name: p.Name, Scopes: p.Scopes, GrantID: p.GrantID,
		ExpiresAt: p.ExpiresAt, CreatedAt: p.CreatedAt, RevokedAt: p.RevokedAt,
	}
}

func patEvent(typ events.Type, p *PAT, changed []string) events.Event {
	return events.Event{Type: typ, Tenant: p.TenantID, Subject: "pats/" + p.ID, Entity: newPATEntity(p), Changed: changed}
}

func delegationEvent(d *DelegationIssuance) events.Event {
	e := events.Event{
		Type: events.DelegationIssued, Tenant: d.TenantID, Subject: "delegations/" + d.RunID,
		Entity: events.DelegationPayload{
			RunID: d.RunID, AgentPrincipalID: d.AgentPrincipalID, DelegatorPrincipalID: d.DelegatorPrincipalID,
			DelegatorPrincipalType: string(d.DelegatorPrincipalType), CreatedAt: d.CreatedAt,
		},
	}
	// The envelope's run attribute must be a UUID. Every kora run id is one.
	if _, err := uuid.Parse(d.RunID); err == nil {
		e.RunID = d.RunID
	}
	return e
}

// actingAs attributes an event to the session's principal when no verified
// token is in the context, as on a sign-in.
func actingAs(ctx context.Context, s *Session) context.Context {
	if claims, ok := token.ClaimsFromContext(ctx); ok && claims != nil {
		return ctx
	}
	return attributedTo(ctx, s.PrincipalID, s.PrincipalType, s.TenantID, s.DisplayName)
}

// attributedTo records an event as caused by the principal. A delegation is
// the delegator's act even when a platform service brokers the mint.
func attributedTo(ctx context.Context, principalID string, typ PrincipalType, tenantID, displayName string) context.Context {
	return token.WithClaims(ctx, &token.PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: principalID},
		Kora:             token.KoraClaims{PrincipalType: typ, TenantID: tenantID, DisplayName: displayName},
	})
}
