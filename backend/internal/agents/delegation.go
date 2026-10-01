package agents

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	identityclient "github.com/clear-route/orbit/internal/platform/clients/identity"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/token"
)

// Delegated-run transport support (docs/adr/2026-07-23-delegated-agent-identity.md,
// Decision 3 + Decision 6): StartRun on a delegated agent type mints a
// delegation issuance from the CALLER's own bearer token, and every terminal
// run state revokes it — the delegation ends with the run.

// ErrDelegatedRunPrincipal is returned when a delegated agent's run is started
// by a principal that cannot delegate. Only HUMAN and PAT principals may start
// a delegated run (a PAT counts as the person); AGENT, SERVICE, SYSTEM, and
// trigger-initiated starts are refused, which structurally blocks a delegated
// run from starting another delegated agent.
var ErrDelegatedRunPrincipal = errors.New("delegated identity model: only HUMAN or PAT principals may start a run of a delegated agent")

// ErrDelegationUnavailable is returned when a delegated run cannot start
// because the delegation issuer is not wired or the caller's bearer token is
// not available to forward. Fail closed: a delegated run never starts without
// a minted delegation.
var ErrDelegationUnavailable = errors.New("delegated identity model: delegation issuer unavailable, refusing to start run without a delegation")

// Static span-status sentinels (dynamic detail travels via span.RecordError).
var (
	errDelegatedRunRefused  = errors.New("delegated run start refused")
	errDelegationMintFailed = errors.New("delegation mint failed")
)

// delegationIssuer is the subset of the platform identity client used to mint
// and revoke run delegations. *identityclient.Client satisfies it; tests
// inject a fake.
type delegationIssuer interface {
	CreateDelegation(ctx context.Context, bearer string, req identityclient.CreateDelegationRequest) (*identityclient.DelegationIssuance, error)
	RevokeDelegation(ctx context.Context, bearer, jti string) error
}

// ServiceTokenSource returns a bearer token for orbit-agents' own service
// identity — the same exchanged-platform-token pattern the policy client uses
// (cmd/orbit-agents/main.go wires it to identityclient.Client.Exchange).
type ServiceTokenSource func(ctx context.Context) (string, error)

// DelegationBroker mints delegation issuances at run start (forwarding the
// caller's bearer) and revokes them on terminal run state (authenticating as
// orbit-agents' own SERVICE principal).
type DelegationBroker struct {
	issuer      delegationIssuer
	tokenSource ServiceTokenSource
	trustDomain string
	logger      logging.Logger
}

// NewDelegationBroker wires a broker. trustDomain is the SPIFFE trust domain
// used to derive agent principal IDs (mirrors K8sProvisioner.agentSpiffeID).
func NewDelegationBroker(issuer delegationIssuer, tokenSource ServiceTokenSource, trustDomain string, logger logging.Logger) *DelegationBroker {
	return &DelegationBroker{
		issuer:      issuer,
		tokenSource: tokenSource,
		trustDomain: trustDomain,
		logger:      logger,
	}
}

// AgentPrincipalID returns the SPIFFE principal ID for an agent type's pods —
// the delegation's act.sub. Mirrors K8sProvisioner.agentSpiffeID so the minted
// issuance names the same principal the pods attest as.
func (b *DelegationBroker) AgentPrincipalID(namespace, slug string) string {
	return fmt.Sprintf("spiffe://%s/tenant/%s/agent/%s", b.trustDomain, namespace, slug)
}

// Mint creates a delegation issuance for runID. The caller's own bearer token
// travels as the RFC 8693 subject_token in the request body; the Authorization
// header carries orbit-agents' own token as transport auth. Identity verifies the subject token and derives the delegator from it,
// so it never mints delegated authority the runner does not hold (ADR
// Decision 3). Returns the issuance JTI.
func (b *DelegationBroker) Mint(ctx context.Context, callerBearer, agentPrincipalID, runID string) (string, error) {
	transport, err := b.tokenSource(ctx)
	if err != nil {
		return "", fmt.Errorf("mint delegation for run %s: service token: %w", runID, err)
	}
	issuance, err := b.issuer.CreateDelegation(ctx, transport, identityclient.CreateDelegationRequest{
		AgentPrincipalID: agentPrincipalID,
		RunID:            runID,
		SubjectToken:     callerBearer,
	})
	if err != nil {
		return "", fmt.Errorf("mint delegation for run %s: %w", runID, err)
	}
	return issuance.JTI, nil
}

// RevokeBestEffort tombstones a delegation issuance, authenticating as
// orbit-agents' own service identity. Failures are logged and swallowed —
// revocation must never wedge run termination; the JTI blocklist is defense in
// depth and the fail-closed rego marker is the real guarantee.
func (b *DelegationBroker) RevokeBestEffort(ctx context.Context, jti, runID string) {
	// Detach from request cancellation: terminal-state cleanup must complete
	// even when the initiating request has gone away.
	ctx = context.WithoutCancel(ctx)
	bearer, err := b.tokenSource(ctx)
	if err != nil {
		b.logger.Warn(ctx, "delegation revoke skipped: service token unavailable",
			logging.Field{Key: "jti", Value: jti},
			logging.Field{Key: "run_id", Value: runID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}
	if err := b.issuer.RevokeDelegation(ctx, bearer, jti); err != nil {
		b.logger.Warn(ctx, "delegation revoke failed (continuing — fail-closed rego is the guarantee)",
			logging.Field{Key: "jti", Value: jti},
			logging.Field{Key: "run_id", Value: runID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}
	b.logger.Info(ctx, "delegation revoked on terminal run state",
		logging.Field{Key: "jti", Value: jti},
		logging.Field{Key: "run_id", Value: runID},
	)
}

// ── Caller bearer passthrough ─────────────────────────────────────────────────
//
// The delegation mint forwards the CALLER's bearer token. Mirroring the
// platform MCP kit's caller-token passthrough (internal/platform/mcp), the
// transport layer lifts the raw Authorization bearer into the request context
// and the service reads it back when minting. The token is already verified by
// AuthMiddleware before any handler runs; this carries the raw string only.

type callerBearerContextKey struct{}

// WithCallerBearer stores the caller's raw bearer token in ctx.
func WithCallerBearer(ctx context.Context, bearer string) context.Context {
	if bearer == "" {
		return ctx
	}
	return context.WithValue(ctx, callerBearerContextKey{}, bearer)
}

// callerBearerFromContext returns the caller's raw bearer token, or "".
func callerBearerFromContext(ctx context.Context) string {
	bearer, _ := ctx.Value(callerBearerContextKey{}).(string)
	return bearer
}

// requireDelegatorPrincipal enforces the ADR Decision 6 start gate: a delegated
// agent's run may only be started by a HUMAN or PAT principal.
func requireDelegatorPrincipal(ctx context.Context) error {
	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		return ErrDelegatedRunPrincipal
	}
	switch claims.Orbit.PrincipalType {
	case token.PrincipalTypeHuman, token.PrincipalTypePAT:
		return nil
	default:
		return fmt.Errorf("%w (principal type %s)", ErrDelegatedRunPrincipal, claims.Orbit.PrincipalType)
	}
}

// ── RunService delegation hooks ───────────────────────────────────────────────

// authorizeDelegatedStart applies the ADR Decision 6 start gate when the agent
// type is delegated: only HUMAN or PAT principals may start the run. A no-op
// for autonomous agents.
func (s *RunService) authorizeDelegatedStart(ctx context.Context, span trace.Span, delegated bool) error {
	if !delegated {
		return nil
	}
	if err := requireDelegatorPrincipal(ctx); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, errDelegatedRunRefused.Error())
		return err
	}
	return nil
}

// mintRunDelegation mints and persists the delegation issuance for a delegated
// run at start. Fail closed: a missing broker or missing caller bearer refuses
// the start rather than degrading to an autonomous run. Errors are recorded on
// span with the static errDelegationMintFailed status.
func (s *RunService) mintRunDelegation(ctx context.Context, span trace.Span, runID string, at AgentType, namespace string) (string, error) {
	jti, err := s.mintAndPersistDelegation(ctx, runID, at, namespace)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, errDelegationMintFailed.Error())
		return "", err
	}
	s.logger.Info(ctx, "delegation minted for run",
		logging.Field{Key: "run_id", Value: runID},
		logging.Field{Key: "jti", Value: jti},
		logging.Field{Key: "agent_type", Value: at.Slug},
	)
	return jti, nil
}

func (s *RunService) mintAndPersistDelegation(ctx context.Context, runID string, at AgentType, namespace string) (string, error) {
	if s.delegation == nil {
		return "", ErrDelegationUnavailable
	}
	bearer := callerBearerFromContext(ctx)
	if bearer == "" {
		return "", fmt.Errorf("%w: caller bearer token not available to forward", ErrDelegationUnavailable)
	}
	jti, err := s.delegation.Mint(ctx, bearer, s.delegation.AgentPrincipalID(namespace, at.Slug), runID)
	if err != nil {
		return "", err
	}
	if err := s.repo.SetRunDelegationJTI(ctx, runID, jti); err != nil {
		// The issuance exists but the run row does not record it — revoke rather
		// than leaving an unrevokable delegation live (the deferred start-failure
		// cleanup also revokes, but only via mintedJTI which the caller sets from
		// our return value; here we have not returned yet).
		s.delegation.RevokeBestEffort(ctx, jti, runID)
		return "", fmt.Errorf("persist delegation jti: %w", err)
	}
	return jti, nil
}

// cleanupFailedStart is StartRun's deferred failure path: fail the created run
// row synchronously (rather than waiting for the stale-run reaper) and revoke
// any delegation minted before the failure. WithoutCancel so cleanup runs even
// when the request context was canceled.
func (s *RunService) cleanupFailedStart(ctx context.Context, runID, mintedJTI string) {
	ctx = context.WithoutCancel(ctx)
	if runID != "" {
		if ferr := s.repo.MarkRunFailed(ctx, runID); ferr != nil {
			s.logger.Warn(ctx, "failed to mark run failed after start error",
				logging.Field{Key: "run_id", Value: runID},
				logging.Field{Key: "error", Value: ferr.Error()},
			)
		}
	}
	if mintedJTI != "" && s.delegation != nil {
		s.delegation.RevokeBestEffort(ctx, mintedJTI, runID)
	}
}

// revokeRunDelegation tombstones a run's delegation issuance on terminal state.
// Best-effort by design: failures log and continue — revocation must never
// wedge run termination.
func (s *RunService) revokeRunDelegation(ctx context.Context, run *Run) {
	if run == nil || run.DelegationJTI == nil || *run.DelegationJTI == "" {
		return
	}
	if s.delegation == nil {
		s.logger.Warn(ctx, "run has a delegation but no broker is wired; skipping revoke",
			logging.Field{Key: "run_id", Value: run.ID},
			logging.Field{Key: "jti", Value: *run.DelegationJTI},
		)
		return
	}
	s.delegation.RevokeBestEffort(ctx, *run.DelegationJTI, run.ID)
}
