package egressgw

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/clear-route/orbit/internal/credentials"
	platformconns "github.com/clear-route/orbit/internal/platform/connections"
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
			// No credential-injecting connection is registered for this host.
			// Reachability was already authorized by the egress posture at the
			// caller's OPA sidecar (egress:<host>:connect) — which the agent
			// cannot bypass — so default to PASSTHROUGH: re-originate to the
			// host with NO credential injected and NO connection-scope check.
			// Connections are a credential OVERLAY, not the reachability gate;
			// a host that just needs to be reached only needs the egress
			// whitelist, never a duplicate connection registration.
			return b.passthroughDecision(ctx, tenantID, claims, req.Host), nil
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

	cls, err := b.classify(ctx, adapter, conn, req, tenantID)
	if err != nil {
		return Decision{}, err
	}

	if perr := b.evaluatePolicy(ctx, claims, conn, cls, tenantID); perr != nil {
		return Decision{}, perr
	}

	// Mint.
	identity := credentials.CallerIdentity{
		TenantID:    tenantID,
		PrincipalID: claims.Subject,
		RunID:       claims.Orbit.Claims["run_id"],
		Action:      cls.Action,
		Qualifier:   cls.Qualifier,
	}
	cred, err := adapter.Resolve(ctx, *conn, identity)
	if err != nil {
		// The upstream refusing to issue a credential for this scope (e.g. the
		// GitHub App installation lacks the permission Orbit authorized) is a
		// forbidden, not an internal error — the App is the outer ceiling.
		if errors.Is(err, credentials.ErrCredentialScopeRejected) {
			b.logger.Info(ctx, "egress denied: upstream refused credential scope",
				logging.Field{Key: "connection", Value: conn.Name},
				logging.Field{Key: "action", Value: cls.Action},
				logging.Field{Key: "qualifier", Value: cls.Qualifier},
				logging.Field{Key: "tenant_id", Value: tenantID},
			)
			return Decision{}, fmt.Errorf("%w: %w", ErrForbidden, err)
		}
		return Decision{}, fmt.Errorf("mint credential: %w", err)
	}

	b.logger.Info(ctx, "egress authorized",
		logging.Field{Key: "connection", Value: conn.Name},
		logging.Field{Key: "adapter", Value: string(conn.Adapter)},
		logging.Field{Key: "tenant_id", Value: tenantID},
		logging.Field{Key: "principal_id", Value: claims.Subject},
		logging.Field{Key: "run_id", Value: identity.RunID},
		logging.Field{Key: "host", Value: req.Host},
		logging.Field{Key: "action", Value: cls.Action},
		logging.Field{Key: "qualifier", Value: cls.Qualifier},
	)

	return Decision{Connection: conn, Action: cls.Action, Qualifier: cls.Qualifier, Credential: cred}, nil
}

// classify asks the adapter to map the request to its policy action and
// qualifier, then enforces the connection's optional qualifier ceiling. A
// classification failure and an out-of-ceiling qualifier are distinct deny
// classes in the audit log. See
// docs/adr/2026-07-22-connection-scope-qualifiers.md (Decision C).
func (b *Broker) classify(ctx context.Context, adapter credentials.CredentialAdapter, conn *credentials.Connection, req EgressRequest, tenantID string) (credentials.Classification, error) {
	cls, err := adapter.ClassifyAction(req.HTTPRequest, *conn)
	if err != nil {
		// An adapter that can't classify the request shape — treat as a deny.
		// The deny is recorded with the unsupported reason so audit captures it.
		b.logger.Info(ctx, "egress denied: adapter could not classify action",
			logging.Field{Key: "connection", Value: conn.Name},
			logging.Field{Key: "host", Value: req.Host},
			logging.Field{Key: "method", Value: req.Method},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return credentials.Classification{}, fmt.Errorf("%w: %w", ErrForbidden, err)
	}

	if cls.Qualifier != "" {
		if qm := platformconns.Qualifier(string(conn.Adapter)); qm != nil {
			if allowed, declared := qualifierCeilingAllows(conn.Config, qm.ConfigKey, cls.Qualifier); declared && !allowed {
				b.logger.Info(ctx, "egress denied: qualifier outside connection ceiling",
					logging.Field{Key: "connection", Value: conn.Name},
					logging.Field{Key: "qualifier", Value: cls.Qualifier},
					logging.Field{Key: "action", Value: cls.Action},
					logging.Field{Key: "tenant_id", Value: tenantID},
				)
				return credentials.Classification{}, ErrForbidden
			}
		}
	}
	return cls, nil
}

// evaluatePolicy checks the classified call against the caller's grants. The
// required scope is connection:<name>:<category>[:<qualifier>]:<verb> — the
// action fragment's category joins the path so unqualified and parent grants
// cover by prefix and qualified grants must match. Single-word actions ("use")
// keep their legacy two-segment shape. Fail-closed on evaluator error.
func (b *Broker) evaluatePolicy(ctx context.Context, claims *token.PlatformClaims, conn *credentials.Connection, cls credentials.Classification, tenantID string) error {
	category, verb := splitActionFragment(cls.Action)
	segments := []string{conn.Name}
	if category != "" {
		segments = append(segments, category)
	}
	if cls.Qualifier != "" {
		segments = append(segments, cls.Qualifier)
	}
	decision, err := b.policy.Check(ctx, policy.CheckInput{
		Claims:       claims,
		ResourceType: resourceType,
		PathSegments: segments,
		Action:       verb,
	})
	if err != nil {
		b.logger.Error(ctx, "policy evaluation failed",
			logging.Field{Key: "connection", Value: conn.Name},
			logging.Field{Key: "action", Value: cls.Action},
			logging.Field{Key: "qualifier", Value: cls.Qualifier},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return fmt.Errorf("%w: %w", ErrForbidden, err)
	}
	if !decision.Allow {
		b.logger.Info(ctx, "egress denied by policy",
			logging.Field{Key: "connection", Value: conn.Name},
			logging.Field{Key: "action", Value: cls.Action},
			logging.Field{Key: "qualifier", Value: cls.Qualifier},
			logging.Field{Key: "tenant_id", Value: tenantID},
			logging.Field{Key: "reason", Value: decision.Reason},
		)
		return ErrForbidden
	}
	return nil
}

// splitActionFragment splits an adapter action fragment ("issues:write") at its
// last colon into the category and verb the qualified scope shape needs. A
// single-word fragment ("use") has no category — the legacy two-segment scope.
func splitActionFragment(action string) (category, verb string) {
	if i := strings.LastIndex(action, ":"); i > 0 {
		return action[:i], action[i+1:]
	}
	return "", action
}

// qualifierCeilingAllows reports whether the connection config declares a
// qualifier allowlist under key and, if so, whether qualifier is in it.
// declared=false (allow) when the config carries no non-empty list — the
// ceiling is optional.
func qualifierCeilingAllows(config json.RawMessage, key, qualifier string) (allowed, declared bool) {
	if len(config) == 0 || key == "" {
		return true, false
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(config, &raw); err != nil {
		return true, false
	}
	var list []string
	if err := json.Unmarshal(raw[key], &list); err != nil || len(list) == 0 {
		return true, false
	}
	for _, v := range list {
		if strings.EqualFold(strings.TrimSpace(v), qualifier) {
			return true, true
		}
	}
	return false, true
}

// passthroughDecision builds a no-credential Decision for a host that has no
// registered connection. The egress posture enforced at the caller's OPA
// sidecar (egress:<host>:connect, which the agent cannot bypass) is the sole
// reachability gate for such hosts, so there is no connection-scope check here
// and nothing is injected on the outbound leg — the gateway re-originates the
// request to the host unchanged. See the egress-gateway ADR (auto-passthrough:
// connections are a credential overlay, not the reachability gate).
func (b *Broker) passthroughDecision(ctx context.Context, tenantID string, claims *token.PlatformClaims, host string) Decision {
	b.logger.Info(ctx, "egress auto-passthrough (no connection; posture-authorized)",
		logging.Field{Key: "host", Value: host},
		logging.Field{Key: "tenant_id", Value: tenantID},
		logging.Field{Key: "principal_id", Value: claims.Subject},
		logging.Field{Key: "run_id", Value: claims.Orbit.Claims["run_id"]},
	)
	return Decision{
		Connection: &credentials.Connection{
			TenantID: tenantID,
			Name:     "passthrough",
			Adapter:  credentials.AdapterPassthrough,
			BaseHost: host,
		},
		Action:     "use",
		Credential: credentials.Credential{}, // inject nothing
	}
}
