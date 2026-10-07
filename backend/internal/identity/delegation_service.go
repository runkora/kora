package identity

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// Delegation issuer (docs/adr/2026-07-23-delegated-agent-identity.md, Decision 3).
//
// The identity service persists a delegation ISSUANCE record, never a signed
// token: retrieval re-signs a fresh JWT from the live record, so the JTI stays
// stable across re-signs (it identifies the delegation for revocation) while
// exp is always fresh. A tombstoned issuance yields nothing on retrieval —
// fail closed.

// defaultDelegationTokenTTL applies when the config value is absent or invalid.
const defaultDelegationTokenTTL = time.Hour

// delegationActClaim is the RFC 8693 nested actor claim: the agent acting on
// the delegator's behalf.
type delegationActClaim struct {
	Sub string `json:"sub"`
}

// delegationTokenClaims is the signed shape of a delegation token. It carries
// NO scope claim, ever: kora tokens identify; authority lives in grants and
// is evaluated live (ADR Decision 3). The consumer half is
// policy.ParseDelegationToken in internal/platform/policy/delegation.go.
type delegationTokenClaims struct {
	jwt.RegisteredClaims
	Act                    delegationActClaim `json:"act"`
	RunID                  string             `json:"run_id"`
	DelegatorPrincipalType string             `json:"delegator_principal_type"`
	TenantID               string             `json:"tenant_id"`
}

// delegationTokenTTL returns the configured delegation token lifetime.
func (s *Service) delegationTokenTTL() time.Duration {
	if s.cfg.Token.DelegationTokenTTLMinutes > 0 {
		return time.Duration(s.cfg.Token.DelegationTokenTTLMinutes) * time.Minute
	}
	return defaultDelegationTokenTTL
}

// MintDelegation creates and persists a delegation issuance for a run.
//
// Only HUMAN and PAT principals may delegate (PATs count as the person); AGENT,
// SERVICE, and SYSTEM callers are refused with ErrDelegationPrincipalType,
// which structurally blocks a delegated run from starting another delegated
// agent. Tenant and delegator identity come exclusively from the caller's
// verified claims. The response is the record — the token never leaves the
// identity service at mint time.
func (s *Service) MintDelegation(ctx context.Context, claims *PlatformClaims, req CreateDelegationRequest) (*DelegationIssuance, error) {
	ctx, span := s.tracer.Start(ctx, "identity.MintDelegation",
		trace.WithAttributes(
			attribute.String("principal.type", string(claims.Kora.PrincipalType)),
			attribute.String("tenant_id", claims.Kora.TenantID),
			attribute.String("run_id", req.RunID),
		),
	)
	defer span.End()

	// An AGENT can never mint or courier a delegation, with or without a
	// subject token — this structurally blocks a delegated run from starting
	// another delegated agent.
	if claims.Kora.PrincipalType == token.PrincipalTypeAgent {
		span.RecordError(ErrDelegationPrincipalType)
		span.SetStatus(codes.Error, ErrDelegationPrincipalType.Error())
		return nil, ErrDelegationPrincipalType
	}

	// Resolve the DELEGATOR. Directly authenticated HUMAN/PAT callers delegate
	// as themselves. A platform service brokering a run start (orbit-agents)
	// authenticates with its own token and carries the runner's token as the
	// RFC 8693 subject_token in the body; the delegator is derived from that
	// token after full verification (signature, expiry, JTI blocklist).
	// Possession of a person's valid platform token already confers that
	// person's full authority, so accepting it here delegates nothing the
	// bearer could not do — escalation stays structurally impossible.
	delegator := claims
	if req.SubjectToken != "" {
		subject, err := s.parseToken(req.SubjectToken)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, ErrDelegationSubjectToken.Error())
			return nil, ErrDelegationSubjectToken
		}
		// A blocklist backend error fails open, as every other blocklist lookup
		// does, so a Redis outage does not stop delegated runs from starting.
		blocked, blErr := s.blocklist.IsBlocked(ctx, subject.ID)
		if blErr != nil {
			span.RecordError(blErr)
			s.logger.Error(ctx, "subject token blocklist check failed, minting anyway",
				logging.Field{Key: "jti", Value: subject.ID},
				logging.Field{Key: "error", Value: blErr.Error()})
		}
		if blocked {
			span.RecordError(ErrDelegationSubjectToken)
			span.SetStatus(codes.Error, ErrDelegationSubjectToken.Error())
			return nil, ErrDelegationSubjectToken
		}
		delegator = subject
	}

	switch delegator.Kora.PrincipalType {
	case token.PrincipalTypeHuman, token.PrincipalTypePAT:
		// The person, directly or via a PAT acting as the person.
	default:
		span.RecordError(ErrDelegationPrincipalType)
		span.SetStatus(codes.Error, ErrDelegationPrincipalType.Error())
		return nil, ErrDelegationPrincipalType
	}

	// The DELEGATOR is always the person, never the credential. A PAT's
	// platform claims carry the issuing user's OIDC subject as sub, so the
	// issuance (and every token re-signed from it) records the person:
	// delegator_principal_id = the user's subject, delegator_principal_type =
	// HUMAN. Without this normalisation the rego delegator-grant lookup — which
	// keys HUMAN grants by sub — could never match a PAT-minted delegation
	// (docs/adr/2026-07-23-delegated-agent-identity.md, Decision 3: PATs count
	// as the person). PAT lineage is preserved separately in DelegatorPATID for
	// audit and revocation.
	issuance := &DelegationIssuance{
		JTI:                    uuid.NewString(),
		RunID:                  req.RunID,
		TenantID:               delegator.Kora.TenantID,
		DelegatorPrincipalID:   delegator.Subject,
		DelegatorPrincipalType: token.PrincipalTypeHuman,
		AgentPrincipalID:       req.AgentPrincipalID,
		CreatedAt:              time.Now().UTC(),
	}
	// PAT lineage: record which PAT minted the delegation so revoking the PAT
	// can tombstone its issuances.
	if delegator.Kora.PrincipalType == token.PrincipalTypePAT && delegator.Kora.PatID != "" {
		patID := delegator.Kora.PatID
		issuance.DelegatorPATID = &patID
	}

	if err := s.delegations.CreateIssuance(ctx, issuance); err != nil {
		if errors.Is(err, ErrDelegationRunActive) {
			span.RecordError(err)
			span.SetStatus(codes.Error, ErrDelegationRunActive.Error())
			return nil, ErrDelegationRunActive
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "delegation issuance persistence failed")
		return nil, fmt.Errorf("persist delegation issuance: %w", err)
	}

	span.SetAttributes(attribute.String("jti", issuance.JTI))
	span.SetStatus(codes.Ok, "delegation issuance created")
	s.metrics.recordDelegationIssued(ctx)
	return issuance, nil
}

// RevokeDelegation tombstones a delegation issuance and blocklists its JTI so
// already-released tokens are rejected at enforcement before they expire.
//
// Allowed callers: the delegator themselves (sub match within the same tenant)
// or a SERVICE / SYSTEM principal — orbit-agents revokes on terminal run state
// (transport phase). Everyone else gets ErrDelegationRevokeForbidden.
// Revocation is idempotent: revoking an already-tombstoned issuance re-asserts
// the JTI blocklist entry and succeeds.
func (s *Service) RevokeDelegation(ctx context.Context, claims *PlatformClaims, jti string) error {
	ctx, span := s.tracer.Start(ctx, "identity.RevokeDelegation",
		trace.WithAttributes(
			attribute.String("jti", jti),
			attribute.String("principal.type", string(claims.Kora.PrincipalType)),
		),
	)
	defer span.End()

	issuance, err := s.delegations.GetByJTI(ctx, jti)
	if err != nil {
		if errors.Is(err, ErrDelegationNotFound) {
			span.RecordError(err)
			span.SetStatus(codes.Error, ErrDelegationNotFound.Error())
			return ErrDelegationNotFound
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "delegation issuance lookup failed")
		return fmt.Errorf("get delegation issuance: %w", err)
	}

	switch claims.Kora.PrincipalType {
	case token.PrincipalTypeService, token.PrincipalTypeSystem:
		// Platform components (orbit-agents on terminal run state).
	case token.PrincipalTypeHuman, token.PrincipalTypePAT:
		if claims.Subject != issuance.DelegatorPrincipalID || claims.Kora.TenantID != issuance.TenantID {
			span.RecordError(ErrDelegationRevokeForbidden)
			span.SetStatus(codes.Error, ErrDelegationRevokeForbidden.Error())
			return ErrDelegationRevokeForbidden
		}
	default:
		span.RecordError(ErrDelegationRevokeForbidden)
		span.SetStatus(codes.Error, ErrDelegationRevokeForbidden.Error())
		return ErrDelegationRevokeForbidden
	}

	if issuance.RevokedAt == nil {
		if err := s.delegations.Revoke(ctx, jti, time.Now().UTC()); err != nil && !errors.Is(err, ErrDelegationNotFound) {
			// ErrDelegationNotFound here means a concurrent revocation won —
			// the tombstone exists, which is the desired end state.
			span.RecordError(err)
			span.SetStatus(codes.Error, "delegation issuance tombstone failed")
			return fmt.Errorf("tombstone delegation issuance: %w", err)
		}
	}

	// Blocklist the JTI for the delegation token TTL — the maximum remaining
	// lifetime of any re-signed token carrying it. The tombstone stops new
	// re-signs; the blocklist kills tokens already released.
	if err := s.blocklist.BlockJTI(ctx, jti, s.delegationTokenTTL()); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "jti blocklist operation failed")
		return fmt.Errorf("blocklist delegation jti: %w", err)
	}

	span.SetStatus(codes.Ok, "delegation revoked")
	s.metrics.recordDelegationRevoked(ctx)
	return nil
}

// DelegationTokenForRun releases a freshly re-signed delegation token for a
// run to a mesh-attested caller.
//
// Authentication is identical to POST /identity/exchange: the caller's SPIFFE
// identity comes from the Envoy-injected XFCC header, and the token is
// released only when that identity resolves to the SAME agent principal the
// issuance names as act.sub. Per-run sandbox SVIDs
// (…/agent/{slug}/sandbox/{run-id}) normalise to the agent principal in
// principalFromSpiffeID, so both shared-pod and sandbox identity-proxies
// qualify. No active issuance → ErrDelegationNotFound, which the caller treats
// as "autonomous, none".
func (s *Service) DelegationTokenForRun(ctx context.Context, r *http.Request, runID string) (*DelegationTokenResponse, error) {
	ctx, span := s.tracer.Start(ctx, "identity.DelegationTokenForRun",
		trace.WithAttributes(attribute.String("run_id", runID)),
	)
	defer span.End()

	principal, err := s.extractor.Extract(r)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "principal extraction failed")
		return nil, fmt.Errorf("extract principal: %w", err)
	}
	span.SetAttributes(attribute.String("principal.id", principal.ID))

	issuance, err := s.delegations.GetActiveByRunID(ctx, runID)
	if err != nil {
		if errors.Is(err, ErrDelegationNotFound) {
			span.SetStatus(codes.Ok, "no active delegation for run")
			return nil, ErrDelegationNotFound
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "delegation issuance lookup failed")
		return nil, fmt.Errorf("get delegation issuance for run: %w", err)
	}

	// Release only to the agent the delegation names. A mismatch is a tamper
	// signal (ADR Monitoring): some other workload asserted this run.
	if principal.ID != issuance.AgentPrincipalID {
		span.RecordError(ErrDelegationAgentMismatch)
		span.SetStatus(codes.Error, ErrDelegationAgentMismatch.Error())
		return nil, ErrDelegationAgentMismatch
	}

	now := time.Now().UTC()
	ttl := s.delegationTokenTTL()
	signed, err := s.signDelegationToken(issuance, now, ttl)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "delegation token signing failed")
		return nil, fmt.Errorf("sign delegation token: %w", err)
	}

	span.SetAttributes(attribute.String("jti", issuance.JTI))
	span.SetStatus(codes.Ok, "delegation token released")
	return &DelegationTokenResponse{
		DelegationToken: signed,
		ExpiresIn:       int(ttl.Seconds()),
		RunID:           issuance.RunID,
	}, nil
}

// ListDelegations returns the caller's active delegation issuances — the
// delegator view. PAT callers list the issuances of the person they act as
// (sub carries the user id for both HUMAN and PAT tokens).
func (s *Service) ListDelegations(ctx context.Context, claims *PlatformClaims) ([]DelegationIssuance, error) {
	issuances, err := s.delegations.ListActiveByDelegator(ctx, claims.Kora.TenantID, claims.Subject)
	if err != nil {
		return nil, fmt.Errorf("list delegation issuances: %w", err)
	}
	return issuances, nil
}

// delegationVerifier verifies the delegation tokens this service signs, with its
// own public key. Identity holds the key, so it does not fetch its own JWKS.
func (s *Service) delegationVerifier() *token.Verifier {
	return token.NewStaticVerifier(&s.signingKey.PublicKey, token.DefaultIssuer, s.cfg.Token.Audience)
}

// signDelegationToken signs an RS256 delegation JWT from a live issuance
// record. iss/aud/signing key are identical to platform tokens so the existing
// consumer stack (policy.ParseDelegationToken) verifies it.
// The JTI is the issuance's — STABLE across re-signs — while iat/exp are fresh.
func (s *Service) signDelegationToken(d *DelegationIssuance, now time.Time, ttl time.Duration) (string, error) {
	claims := delegationTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        d.JTI,
			Subject:   d.DelegatorPrincipalID,
			Audience:  jwt.ClaimStrings{s.cfg.Token.Audience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			Issuer:    token.DefaultIssuer,
		},
		Act:                    delegationActClaim{Sub: d.AgentPrincipalID},
		RunID:                  d.RunID,
		DelegatorPrincipalType: string(d.DelegatorPrincipalType),
		TenantID:               d.TenantID,
	}
	t := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	t.Header["kid"] = keyID(&s.signingKey.PublicKey)
	return t.SignedString(s.signingKey)
}
