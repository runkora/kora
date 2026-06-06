package egressgw

import (
	"context"
	"errors"
	"fmt"

	"github.com/clear-route/orbit/internal/credentials"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/token"
)

// Broker is the orchestrator that turns one verified inbound request into a
// minted, scoped credential ready for the proxy to inject. It is the package's
// pure-logic core — no HTTP handling, no TLS, no upstream calls — so every
// authorization and minting path is unit-testable in isolation.
//
// The Broker collaborates with three injected dependencies:
//   - policy.Evaluator: authorizes "connection:<name>:<action>" against the
//     caller's claims. Fail-closed: any evaluator error denies the egress.
//   - credentials.ConnectionRepository: resolves the connection serving the
//     request's destination Host, scoped to the caller's tenant.
//   - AdapterRegistry: dispatches to the adapter that classifies the request
//     and mints/resolves the credential for the connection's adapter type.
type Broker struct {
	policy   policy.Evaluator
	conns    credentials.ConnectionRepository
	adapters *AdapterRegistry
	logger   logging.Logger
}

// NewBroker constructs a Broker. None of the dependencies are optional —
// passing a nil for any of them yields a Broker that panics on first use.
func NewBroker(
	pol policy.Evaluator,
	conns credentials.ConnectionRepository,
	adapters *AdapterRegistry,
	logger logging.Logger,
) *Broker {
	return &Broker{policy: pol, conns: conns, adapters: adapters, logger: logger}
}

// Authorize is the broker's one entry point. It validates the request, looks
// up the connection serving its Host within the caller's tenant, asks the
// matching adapter to classify the request into a policy action, evaluates
// policy, mints the credential, and returns a Decision the proxy applies.
//
// Errors are sentinels (ErrMissingHost, ErrForbidden, …) so the proxy can map
// them to the right HTTP status. The broker never logs the minted credential
// value — only its expiry and metadata.
func (b *Broker) Authorize(ctx context.Context, claims *token.PlatformClaims, req EgressRequest) (Decision, error) {
	if claims == nil {
		return Decision{}, ErrUnauthenticated
	}
	tenantID := claims.Orbit.TenantID
	if tenantID == "" {
		return Decision{}, ErrUnauthenticated
	}
	if req.Host == "" {
		return Decision{}, ErrMissingHost
	}

	// Resolve the connection by Host in the caller's tenant. Cross-tenant
	// access is impossible because GetByHost is tenant-scoped — a connection
	// in another tenant matching the same host looks identical to "not found"
	// from this caller's perspective.
	conn, err := b.conns.GetByHost(ctx, tenantID, req.Host)
	if err != nil {
		if errors.Is(err, credentials.ErrConnectionNotFound) {
			return Decision{}, fmt.Errorf("%w: host %q in tenant %q", credentials.ErrConnectionNotFound, req.Host, tenantID)
		}
		return Decision{}, fmt.Errorf("resolve connection: %w", err)
	}
	if conn.TenantID != tenantID {
		return Decision{}, ErrTenantMismatch
	}

	// Dispatch to the adapter for classification + minting. The adapter knows
	// the upstream's API shape — broker stays adapter-agnostic.
	adapter, ok := b.adapters.Lookup(conn.Adapter)
	if !ok {
		return Decision{}, fmt.Errorf("%w: %q", credentials.ErrUnsupportedAdapter, conn.Adapter)
	}

	action, err := adapter.ClassifyAction(req.HTTPRequest)
	if err != nil {
		// An adapter that can't classify the request shape — treat as a deny.
		// The deny is recorded with the unsupported reason so audit captures it.
		b.logger.Info(ctx, "egress denied: adapter could not classify action",
			logging.Field{Key: "connection", Value: conn.Name},
			logging.Field{Key: "host", Value: req.Host},
			logging.Field{Key: "method", Value: req.Method},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return Decision{}, fmt.Errorf("%w: %w", ErrForbidden, err)
	}

	// Evaluate policy. The scope vocabulary is "connection:<name>:<action>";
	// the gateway encodes it as resourceType=connection, path=[name],
	// action=<action>. Fail-closed on evaluator error.
	decision, err := b.policy.Check(ctx, policy.CheckInput{
		Claims:       claims,
		ResourceType: resourceType,
		PathSegments: []string{conn.Name},
		Action:       action,
	})
	if err != nil {
		b.logger.Error(ctx, "policy evaluation failed",
			logging.Field{Key: "connection", Value: conn.Name},
			logging.Field{Key: "action", Value: action},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return Decision{}, fmt.Errorf("%w: %w", ErrForbidden, err)
	}
	if !decision.Allow {
		b.logger.Info(ctx, "egress denied by policy",
			logging.Field{Key: "connection", Value: conn.Name},
			logging.Field{Key: "action", Value: action},
			logging.Field{Key: "tenant_id", Value: tenantID},
			logging.Field{Key: "reason", Value: decision.Reason},
		)
		return Decision{}, ErrForbidden
	}

	// Mint.
	identity := credentials.CallerIdentity{
		TenantID:    tenantID,
		PrincipalID: claims.Subject,
		RunID:       claims.Orbit.Claims["run_id"],
		Action:      action,
	}
	cred, err := adapter.Resolve(ctx, *conn, identity)
	if err != nil {
		return Decision{}, fmt.Errorf("mint credential: %w", err)
	}

	b.logger.Info(ctx, "egress authorized",
		logging.Field{Key: "connection", Value: conn.Name},
		logging.Field{Key: "adapter", Value: string(conn.Adapter)},
		logging.Field{Key: "tenant_id", Value: tenantID},
		logging.Field{Key: "principal_id", Value: claims.Subject},
		logging.Field{Key: "run_id", Value: identity.RunID},
		logging.Field{Key: "host", Value: req.Host},
		logging.Field{Key: "action", Value: action},
	)

	return Decision{Connection: conn, Action: action, Credential: cred}, nil
}
