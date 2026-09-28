package egressgw

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/clear-route/orbit/internal/credentials"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/token"
)

// Span status descriptions are package-level sentinels: raw error text can
// carry hostnames and upstream detail, which must not land in a span string.
var (
	errResolveConnection = errors.New("resolve connection")
	errMintCredential    = errors.New("mint credential")
	errOpenConnector     = errors.New("open connector")
)

// policyChecker is the single method the broker needs from the policy
// evaluator: does this caller hold the scope the classified call requires.
type policyChecker interface {
	Check(ctx context.Context, input policy.CheckInput) (policy.Result, error)
}

// Broker is the orchestrator that turns one verified inbound request into a
// minted, scoped credential ready for the proxy to inject. It is the package's
// pure-logic core, with no HTTP handling, no TLS and no upstream calls, so
// every authorization and minting path is unit-testable in isolation.
//
// The Broker collaborates with four injected dependencies:
//   - policyChecker: authorizes "connection:<name>:<action>" against the
//     caller's claims. Fail-closed: any evaluator error denies the egress.
//   - connLookup: resolves the connection serving the request's destination
//     Host, scoped to the caller's tenant.
//   - AdapterRegistry: dispatches to the adapter that classifies the request
//     and mints/resolves the credential for the connection's adapter type.
//   - passthrough: serves hosts with no connection, injecting nothing.
type Broker struct {
	policy      policyChecker
	conns       connLookup
	adapters    *AdapterRegistry
	passthrough credentials.Adapter
	logger      logging.Logger
}

// NewBroker constructs a Broker. None of the dependencies are optional; passing
// a nil for any of them yields a Broker that panics on first use.
//
// pass serves hosts with no registered connection. It is held directly rather
// than in adapters so that a Connection CR cannot name it: reaching a public
// host is granted by the egress whitelist, never by registering a connection.
func NewBroker(
	pol policyChecker,
	conns connLookup,
	adapters *AdapterRegistry,
	pass credentials.Adapter,
	logger logging.Logger,
) *Broker {
	return &Broker{policy: pol, conns: conns, adapters: adapters, passthrough: pass, logger: logger}
}

// Authorize is the broker's one entry point. It validates the request, looks
// up the connection serving its Host within the caller's tenant, asks the
// matching adapter to classify the request into a policy action, evaluates
// policy, mints the credential, and returns a Decision the proxy applies.
//
// Errors are sentinels (ErrMissingHost, ErrForbidden, …) so the proxy can map
// them to the right HTTP status. The broker never logs the minted credential
// value, only its expiry and metadata.
func (b *Broker) Authorize(ctx context.Context, claims *token.PlatformClaims, req EgressRequest) (Decision, error) {
	ctx, span := tracer.Start(ctx, "egress.authorize", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	if claims == nil {
		span.SetStatus(codes.Error, ErrUnauthenticated.Error())
		return Decision{}, ErrUnauthenticated
	}
	tenantID := claims.Orbit.TenantID
	if tenantID == "" {
		return Decision{}, ErrUnauthenticated
	}
	if req.Host == "" {
		return Decision{}, ErrMissingHost
	}

	conn, registered, err := b.resolveConnection(ctx, tenantID, req.Host)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, errResolveConnection.Error())
		return Decision{}, err
	}
	span.SetAttributes(
		attribute.String("orbit.connection", conn.Name),
		attribute.String("orbit.adapter", string(conn.Adapter)),
		attribute.Bool("orbit.connection.registered", registered),
	)
	if conn.TenantID != tenantID {
		return Decision{}, ErrTenantMismatch
	}

	// The adapter knows the upstream's API shape; the broker stays
	// adapter-agnostic about both classification and minting. Open decodes the
	// connection's configuration once, so nothing below this point sees it raw.
	adapter := b.passthrough
	if registered {
		known, ok := b.adapters.Lookup(conn.Adapter)
		if !ok {
			return Decision{}, fmt.Errorf("%w: %q", credentials.ErrUnsupportedAdapter, conn.Adapter)
		}
		adapter = known
	}
	connector, err := adapter.Open(*conn)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, errOpenConnector.Error())
		// A connection Orbit cannot read is an operator error, not a caller
		// one. It still denies, but reporting it as forbidden sends whoever
		// reads the audit log hunting a grant that was never the problem.
		return Decision{}, err
	}

	cls, err := b.classify(ctx, connector, conn, req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrForbidden.Error())
		return Decision{}, deny(DenialUnclassified, req.Host, conn, credentials.Classification{}, err)
	}
	span.SetAttributes(
		attribute.String("orbit.action", cls.Action),
		attribute.String("orbit.qualifier", cls.Qualifier),
	)

	// A passthrough host has no connection scope to check against. Its
	// reachability was settled by the egress posture at the caller's OPA
	// sidecar, which the agent cannot bypass.
	action, also := cls.Action, []string(nil)
	if registered {
		authorized, perr := b.evaluatePolicy(ctx, claims, conn, cls, req.Host, tenantID)
		if perr != nil {
			span.SetAttributes(attribute.Bool("orbit.allowed", false))
			span.RecordError(perr)
			span.SetStatus(codes.Error, ErrForbidden.Error())
			return Decision{}, perr
		}
		action, also = authorized[0], authorized[1:]
	}
	span.SetAttributes(
		attribute.Bool("orbit.allowed", true),
		attribute.String("orbit.action", action),
		attribute.StringSlice("orbit.also_authorized", also),
	)

	// Mint.
	identity := credentials.CallerIdentity{
		TenantID:       tenantID,
		PrincipalID:    claims.Subject,
		RunID:          claims.Orbit.Claims["run_id"],
		Action:         action,
		Qualifier:      cls.Qualifier,
		AlsoAuthorized: also,
	}
	cred, err := connector.Mint(ctx, identity)
	if err != nil {
		// The upstream refusing to issue a credential for this scope (e.g. the
		// GitHub App installation lacks the permission Orbit authorized) is a
		// forbidden, not an internal error. The App is the outer ceiling.
		span.RecordError(err)
		span.SetStatus(codes.Error, errMintCredential.Error())
		if errors.Is(err, credentials.ErrCredentialScopeRejected) {
			b.logger.Info(ctx, "egress denied: upstream refused credential scope",
				logging.Field{Key: "connection", Value: conn.Name},
				logging.Field{Key: "action", Value: action},
				logging.Field{Key: "qualifier", Value: cls.Qualifier},
				logging.Field{Key: "tenant_id", Value: tenantID},
			)
			refused := credentials.Classification{Action: action, Qualifier: cls.Qualifier}
			return Decision{}, deny(DenialCredentialRefused, req.Host, conn, refused, err)
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
		logging.Field{Key: "action", Value: action},
		logging.Field{Key: "qualifier", Value: cls.Qualifier},
		logging.Field{Key: "registered", Value: registered},
	)

	return Decision{Connection: conn, Action: action, Qualifier: cls.Qualifier, Credential: cred}, nil
}

// classify asks the connector to map the request to its policy action and the
// resource it names. Which resources an agent may reach is settled by its
// grant: an attachment narrowed to a repository compiles to a qualified scope,
// and policy evaluation below rejects anything the grant does not cover. See
// docs/adr/2026-07-22-connection-scope-qualifiers.md.
func (b *Broker) classify(ctx context.Context, connector credentials.Connector, conn *credentials.Connection, req EgressRequest) (credentials.Classification, error) {
	cls, err := connector.Classify(req.HTTPRequest)
	if err != nil {
		// An adapter that cannot classify the request shape is a deny.
		// The deny is recorded with the unsupported reason so audit captures it.
		b.logger.Info(ctx, "egress denied: adapter could not classify action",
			logging.Field{Key: "connection", Value: conn.Name},
			logging.Field{Key: "host", Value: req.Host},
			logging.Field{Key: "method", Value: req.Method},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return credentials.Classification{}, err
	}

	return cls, nil
}

// evaluatePolicy checks the classified call against the caller's grants and
// returns the actions the grant covers: the classified action first when held,
// then each held alternate. The required scope is
// connection:<name>:<category>[:<qualifier>]:<verb>. The action fragment's
// category joins the path so unqualified and parent grants cover by prefix and
// qualified grants must match. Single-word actions ("use") keep their legacy
// two-segment shape.
//
// The call is denied only when the grant covers none of them. An evaluator
// error on the classified action fails closed; one on an alternate drops that
// alternate.
func (b *Broker) evaluatePolicy(ctx context.Context, claims *token.PlatformClaims, conn *credentials.Connection, cls credentials.Classification, host, tenantID string) ([]string, error) {
	primary, err := b.checkAction(ctx, claims, conn, cls.Action, cls.Qualifier)
	if err != nil {
		b.logger.Error(ctx, "policy evaluation failed",
			logging.Field{Key: "connection", Value: conn.Name},
			logging.Field{Key: "action", Value: cls.Action},
			logging.Field{Key: "qualifier", Value: cls.Qualifier},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return nil, deny(DenialPolicyUnavailable, host, conn, cls, err)
	}

	var authorized []string
	if primary.Allow {
		authorized = append(authorized, cls.Action)
	}
	for _, alt := range cls.Alternates {
		decision, err := b.checkAction(ctx, claims, conn, alt, cls.Qualifier)
		if err != nil {
			// Skipping an alternate only narrows the credential, so an
			// alternate that cannot be evaluated is treated as not held.
			b.logger.Warn(ctx, "policy evaluation failed for alternate action",
				logging.Field{Key: "connection", Value: conn.Name},
				logging.Field{Key: "action", Value: alt},
				logging.Field{Key: "qualifier", Value: cls.Qualifier},
				logging.Field{Key: "error", Value: err.Error()},
			)
			continue
		}
		if decision.Allow {
			authorized = append(authorized, alt)
		}
	}

	if len(authorized) == 0 {
		b.logger.Info(ctx, "egress denied by policy",
			logging.Field{Key: "connection", Value: conn.Name},
			logging.Field{Key: "action", Value: cls.Action},
			logging.Field{Key: "qualifier", Value: cls.Qualifier},
			logging.Field{Key: "tenant_id", Value: tenantID},
			logging.Field{Key: "reason", Value: primary.Reason},
		)
		return nil, deny(DenialNotGranted, host, conn, cls, nil)
	}
	return authorized, nil
}

// checkAction asks the policy service whether the caller's grant covers one
// action on this connection and qualifier.
func (b *Broker) checkAction(ctx context.Context, claims *token.PlatformClaims, conn *credentials.Connection, action, qualifier string) (policy.Result, error) {
	segments, verb := scopePath(conn, action, qualifier)
	return b.policy.Check(ctx, policy.CheckInput{
		Claims:       claims,
		ResourceType: resourceType,
		PathSegments: segments,
		Action:       verb,
	})
}

// scopePath returns the policy path segments and verb for one action on a
// connection: connection:<name>:<category>[:<qualifier>]:<verb>.
func scopePath(conn *credentials.Connection, action, qualifier string) (segments []string, verb string) {
	category, verb := splitActionFragment(action)
	segments = []string{conn.Name}
	if category != "" {
		segments = append(segments, category)
	}
	if qualifier != "" {
		segments = append(segments, qualifier)
	}
	return segments, verb
}

// deny builds the caller-facing Denial for a refused call.
func deny(reason DenialReason, host string, conn *credentials.Connection, cls credentials.Classification, cause error) *Denial {
	d := &Denial{Reason: reason, Host: host, Connection: conn.Name, Action: cls.Action, Qualifier: cls.Qualifier, cause: cause}
	if reason == DenialNotGranted {
		segments, verb := scopePath(conn, cls.Action, cls.Qualifier)
		d.RequiredScope = policy.DeriveRequiredScope(resourceType, segments, verb)
	}
	return d
}

// splitActionFragment splits an adapter action fragment ("issues:write") at its
// last colon into the category and verb the qualified scope shape needs. A
// single-word fragment ("use") has no category and keeps the two-segment shape.
func splitActionFragment(action string) (category, verb string) {
	if i := strings.LastIndex(action, ":"); i > 0 {
		return action[:i], action[i+1:]
	}
	return "", action
}

// passthroughConnectionName is the connection name reported for a host with no
// registered connection. It is not a registrable connection: no CR can claim
// the name, because the broker synthesizes it only on a lookup miss.
const passthroughConnectionName = "passthrough"

// resolveConnection finds the connection serving host within the caller's
// tenant. Cross-tenant access is impossible because GetByHost is tenant-scoped:
// a connection in another tenant matching the same host is indistinguishable
// from "not found" here.
//
// registered is false when no connection serves the host. The broker then
// synthesizes one for the passthrough adapter, which injects nothing.
// Connections are a credential overlay, not the reachability gate, so a host
// that only needs to be reached needs the egress whitelist and nothing else.
func (b *Broker) resolveConnection(ctx context.Context, tenantID, host string) (*credentials.Connection, bool, error) {
	conn, err := b.conns.GetByHost(ctx, tenantID, host)
	switch {
	case err == nil:
		return conn, true, nil
	case errors.Is(err, credentials.ErrConnectionNotFound):
		return &credentials.Connection{
			TenantID: tenantID,
			Name:     passthroughConnectionName,
			Adapter:  credentials.AdapterPassthrough,
			BaseHost: host,
		}, false, nil
	default:
		return nil, false, fmt.Errorf("resolve connection: %w", err)
	}
}
