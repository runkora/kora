package identity

import (
	"context"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/runkora/kora/backend/internal/platform/events"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// sessionEntity is the session an event carries. It leaves out the token id
// and the identity provider claims.
type sessionEntity struct {
	ID             string         `json:"id"`
	PrincipalID    string         `json:"principal_id"`
	PrincipalType  PrincipalType  `json:"principal_type"`
	DisplayName    string         `json:"display_name,omitempty"`
	IdentitySource IdentitySource `json:"identity_source"`
	CreatedAt      time.Time      `json:"created_at"`
	ExpiresAt      time.Time      `json:"expires_at"`
}

func newSessionEntity(s *Session) sessionEntity {
	return sessionEntity{
		ID: s.ID, PrincipalID: s.PrincipalID, PrincipalType: s.PrincipalType, DisplayName: s.DisplayName,
		IdentitySource: s.IdentitySource, CreatedAt: s.CreatedAt, ExpiresAt: s.ExpiresAt,
	}
}

// tenantSwitchEntity is one side of a switch. Each tenant records its own
// side, and neither names the other tenant.
type tenantSwitchEntity struct {
	SessionID   string `json:"session_id"`
	PrincipalID string `json:"principal_id"`
	// Direction is "out" in the tenant the person left and "in" in the one they joined.
	Direction string `json:"direction"`
}

// patEntity is the personal access token an event carries. It leaves out the
// token id and the token hash.
type patEntity struct {
	ID        string     `json:"id"`
	UserID    string     `json:"user_id"`
	Name      string     `json:"name"`
	Scopes    []string   `json:"scopes"`
	GrantID   *string    `json:"grant_id,omitempty"`
	ExpiresAt time.Time  `json:"expires_at"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

func newPATEntity(p *PAT) patEntity {
	return patEntity{
		ID: p.ID, UserID: p.UserID, Name: p.Name, Scopes: p.Scopes, GrantID: p.GrantID,
		ExpiresAt: p.ExpiresAt, CreatedAt: p.CreatedAt, RevokedAt: p.RevokedAt,
	}
}

func patEvent(typ events.Type, p *PAT, changed []string) events.Event {
	return events.Event{Type: typ, Tenant: p.TenantID, Subject: "pats/" + p.ID, Entity: newPATEntity(p), Changed: changed}
}

// delegationEntity is the delegation an event carries. It leaves out the
// token id of the delegation token.
type delegationEntity struct {
	RunID                  string        `json:"run_id"`
	AgentPrincipalID       string        `json:"agent_principal_id"`
	DelegatorPrincipalID   string        `json:"delegator_principal_id"`
	DelegatorPrincipalType PrincipalType `json:"delegator_principal_type"`
	CreatedAt              time.Time     `json:"created_at"`
}

func delegationEvent(d *DelegationIssuance) events.Event {
	e := events.Event{
		Type: events.DelegationIssued, Tenant: d.TenantID, Subject: "delegations/" + d.RunID,
		Entity: delegationEntity{
			RunID: d.RunID, AgentPrincipalID: d.AgentPrincipalID, DelegatorPrincipalID: d.DelegatorPrincipalID,
			DelegatorPrincipalType: d.DelegatorPrincipalType, CreatedAt: d.CreatedAt,
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
	return attributedTo(ctx, s.PrincipalID, s.PrincipalType, s.TenantID)
}

// attributedTo records an event as caused by the principal. A delegation is
// the delegator's act even when a platform service brokers the mint.
func attributedTo(ctx context.Context, principalID string, typ PrincipalType, tenantID string) context.Context {
	return token.WithClaims(ctx, &token.PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: principalID},
		Kora:             token.KoraClaims{PrincipalType: typ, TenantID: tenantID},
	})
}
