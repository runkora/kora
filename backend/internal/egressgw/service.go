package egressgw

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
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

// policyChecker is what the broker needs from the policy evaluator: does this
// caller hold the scope the classified call requires, and which scopes does it
// hold. Scopes only chooses among same-host connections and never authorizes.
type policyChecker interface {
	Check(ctx context.Context, input policy.CheckInput) (policy.Result, error)
	Scopes(ctx context.Context, input policy.ScopesInput) ([]string, error)
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

	conn, registered, err := b.resolveConnection(ctx, claims, req)
	var unresolved *Denial
	if errors.As(err, &unresolved) {
		span.RecordError(err)
		return b.unresolvedConnection(ctx, span, claims, req, unresolved)
	}
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
		// A connection kora cannot read is an operator error, not a caller
		// one. It still denies, but reporting it as forbidden sends whoever
		// reads the audit log hunting a grant that was never the problem.
		return Decision{}, err
	}

	cls, err := b.classify(connector, req)
	if err != nil {
		span.RecordError(err)
		return b.anonymousReadOrDeny(ctx, span, claims, conn, req, deny(DenialUnclassified, req.Host, conn, credentials.Classification{}, err))
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
		authorized, perr := b.evaluatePolicy(ctx, claims, conn, cls, req)
		if perr != nil {
			return b.policyDenied(ctx, span, claims, conn, req, perr)
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
		// GitHub App installation lacks the permission kora authorized) is a
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
		logging.Field{Key: "also_authorized", Value: strings.Join(also, ",")},
		logging.Field{Key: "qualifier", Value: cls.Qualifier},
		logging.Field{Key: "registered", Value: registered},
	)

	return Decision{Connection: conn, Action: action, Qualifier: cls.Qualifier, Credential: cred}, nil
}

// policyDenied handles a policy refusal. A denial the grant could explain may
// still be served by the anonymous fallback; a failed evaluation never is.
func (b *Broker) policyDenied(ctx context.Context, span trace.Span, claims *token.PlatformClaims, conn *credentials.Connection, req EgressRequest, perr error) (Decision, error) {
	span.SetAttributes(attribute.Bool("orbit.allowed", false))
	span.RecordError(perr)
	var denial *Denial
	if errors.As(perr, &denial) && denial.Reason != DenialPolicyUnavailable {
		return b.anonymousReadOrDeny(ctx, span, claims, conn, req, denial)
	}
	span.SetStatus(codes.Error, ErrForbidden.Error())
	// An evaluator error is logged where it happens. An operational result
	// such as no policy for the tenant is logged here.
	if denial != nil && denial.decision != "" {
		b.logDenial(ctx, claims.Orbit.TenantID, req, denial)
	}
	return Decision{}, perr
}

// anonymousReadAction is the action recorded for a read served by the
// anonymous fallback.
const anonymousReadAction = "anonymous_read"

// anonymousReadOrDeny serves a GitHub read the connection's grant does not
// authorize as an anonymous passthrough, when the caller's own egress posture
// reaches the host. Anything else returns denial. Connector-derived egress
// (egress:<host>:connector) never qualifies. See
// docs/adr/2026-06-06-jit-credential-brokering-egress-gateway.md, Amendment 5.
func (b *Broker) anonymousReadOrDeny(ctx context.Context, span trace.Span, claims *token.PlatformClaims, conn *credentials.Connection, req EgressRequest, denial *Denial) (Decision, error) {
	if conn.Adapter != credentials.AdapterGitHubApp || !isRead(req) || !b.ownEgressAllows(ctx, claims, req.Host) {
		span.SetStatus(codes.Error, ErrForbidden.Error())
		b.logDenial(ctx, claims.Orbit.TenantID, req, denial)
		return Decision{}, denial
	}

	span.SetAttributes(
		attribute.Bool("orbit.allowed", true),
		attribute.String("orbit.action", anonymousReadAction),
	)
	b.logger.Info(ctx, "egress authorized: anonymous read",
		logging.Field{Key: "decision", Value: anonymousReadAction},
		logging.Field{Key: "connection", Value: conn.Name},
		logging.Field{Key: "denial_reason", Value: string(denial.Reason)},
		logging.Field{Key: "tenant_id", Value: claims.Orbit.TenantID},
		logging.Field{Key: "principal_id", Value: claims.Subject},
		logging.Field{Key: "run_id", Value: claims.Orbit.Claims["run_id"]},
		logging.Field{Key: "host", Value: req.Host},
		logging.Field{Key: "method", Value: req.Method},
		logging.Field{Key: "path", Value: requestPath(req)},
		logging.Field{Key: "credential", Value: false},
	)
	// The connection is kept for its upstream CA trust. With no credential the
	// proxy injects nothing and strips the agent's Authorization header.
	return Decision{Connection: conn, Action: anonymousReadAction}, nil
}

// logDenial records a refused call once, after the fallback has declined it.
// The query string and headers stay out of the log: either can carry tokens.
func (b *Broker) logDenial(ctx context.Context, tenantID string, req EgressRequest, d *Denial) {
	if d.Reason == DenialConnectionUnresolved {
		b.logger.Info(ctx, "egress denied: connection unresolved",
			logging.Field{Key: "host", Value: req.Host},
			logging.Field{Key: "method", Value: req.Method},
			logging.Field{Key: "path", Value: requestPath(req)},
			logging.Field{Key: "candidates", Value: strings.Join(d.Candidates, ",")},
			logging.Field{Key: "tenant_id", Value: tenantID},
		)
		return
	}
	if d.Reason == DenialUnclassified {
		cause := ""
		if d.cause != nil {
			cause = d.cause.Error()
		}
		b.logger.Info(ctx, "egress denied: adapter could not classify action",
			logging.Field{Key: "connection", Value: d.Connection},
			logging.Field{Key: "host", Value: req.Host},
			logging.Field{Key: "method", Value: req.Method},
			logging.Field{Key: "path", Value: requestPath(req)},
			logging.Field{Key: "error", Value: cause},
		)
		return
	}
	b.logger.Info(ctx, "egress denied by policy",
		logging.Field{Key: "connection", Value: d.Connection},
		logging.Field{Key: "host", Value: req.Host},
		logging.Field{Key: "method", Value: req.Method},
		logging.Field{Key: "path", Value: requestPath(req)},
		logging.Field{Key: "action", Value: d.Action},
		logging.Field{Key: "qualifier", Value: d.Qualifier},
		logging.Field{Key: "tenant_id", Value: tenantID},
		logging.Field{Key: "reason", Value: d.decision},
	)
}

// ownEgressAllows asks policy whether the caller's own posture (open mode, or
// the host in its whitelist or an environment) reaches host. An evaluation
// error counts as no.
func (b *Broker) ownEgressAllows(ctx context.Context, claims *token.PlatformClaims, host string) bool {
	res, err := b.policy.Check(ctx, policy.CheckInput{
		Claims:       claims,
		ResourceType: "egress",
		PathSegments: []string{hostWithoutPort(host)},
		Action:       "connect",
	})
	if err != nil {
		b.logger.Warn(ctx, "egress posture check failed",
			logging.Field{Key: "host", Value: host},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return false
	}
	return res.Allow
}

// isRead reports whether the request only reads: GET, HEAD, or a git fetch
// (git-upload-pack). Push discovery (service=git-receive-pack) is a write.
func isRead(req EgressRequest) bool {
	path := requestPath(req)
	switch req.Method {
	case http.MethodGet, http.MethodHead:
		return req.URL == nil || req.URL.Query().Get("service") != "git-receive-pack"
	case http.MethodPost:
		return strings.HasSuffix(path, "/git-upload-pack")
	default:
		return false
	}
}

// hostWithoutPort lowercases host and drops any port, the form egress scopes use.
func hostWithoutPort(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.ToLower(host)
}

// classify asks the connector to map the request to its policy action and the
// resource it names. Which resources an agent may reach is settled by its
// grant: an attachment narrowed to a repository compiles to a qualified scope,
// and policy evaluation below rejects anything the grant does not cover. See
// docs/adr/2026-07-22-connection-scope-qualifiers.md.
func (b *Broker) classify(connector credentials.Connector, req EgressRequest) (credentials.Classification, error) {
	// An adapter that cannot classify the request shape is a deny, logged by
	// logDenial unless the anonymous fallback serves the request.
	return connector.Classify(req.HTTPRequest)
}

// evaluatePolicy checks the classified call against the caller's grants and
// returns the actions the grant covers: the classified action first when held,
// then each held alternate. The required scope is
// connection:<name>:<category>[:<qualifier>]:<verb>. The action fragment's
// category joins the path so unqualified and parent grants cover by prefix and
// qualified grants must match. Single-word actions ("use") keep their legacy
// two-segment shape.
//
// The call is denied when the grant covers none of them, or misses any of the
// classification's AlsoRequired actions, which are appended when all are held.
// An evaluator error on the classified action or a required one fails closed;
// one on an alternate drops that alternate.
func (b *Broker) evaluatePolicy(ctx context.Context, claims *token.PlatformClaims, conn *credentials.Connection, cls credentials.Classification, req EgressRequest) ([]string, error) {
	primary, err := b.checkAction(ctx, claims, conn, cls.Action, cls.Qualifier)
	if err != nil {
		b.logger.Error(ctx, "policy evaluation failed",
			logging.Field{Key: "connection", Value: conn.Name},
			logging.Field{Key: "action", Value: cls.Action},
			logging.Field{Key: "qualifier", Value: cls.Qualifier},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return nil, deny(DenialPolicyUnavailable, req.Host, conn, cls, err)
	}

	var authorized []string
	reasons := []string{primary.Reason}
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
		reasons = append(reasons, decision.Reason)
	}

	if len(authorized) == 0 {
		reason, label := policyDenialReason(reasons)
		d := deny(reason, req.Host, conn, cls, nil)
		d.PolicyReason, d.decision = label, primary.Reason
		return nil, d
	}

	for _, action := range cls.AlsoRequired {
		required := credentials.Classification{Action: action, Qualifier: cls.Qualifier}
		decision, err := b.checkAction(ctx, claims, conn, action, cls.Qualifier)
		if err != nil {
			b.logger.Error(ctx, "policy evaluation failed",
				logging.Field{Key: "connection", Value: conn.Name},
				logging.Field{Key: "action", Value: action},
				logging.Field{Key: "qualifier", Value: cls.Qualifier},
				logging.Field{Key: "error", Value: err.Error()},
			)
			return nil, deny(DenialPolicyUnavailable, req.Host, conn, required, err)
		}
		if !decision.Allow {
			reason, label := policyDenialReason([]string{decision.Reason})
			d := deny(reason, req.Host, conn, required, nil)
			d.PolicyReason, d.decision = label, decision.Reason
			return nil, d
		}
		authorized = append(authorized, action)
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

// requestPath is the request path for deny logs. The query string and headers
// are left out: either can carry tokens.
func requestPath(req EgressRequest) string {
	if req.URL == nil {
		return ""
	}
	return req.URL.Path
}

// noMatchingGrant is the policy bundle's default deny label: no grant covers the call.
const noMatchingGrant = "deny:no_matching_grant"

// grantNotAppliedLabels are the bundle's labels for a grant that is held but did
// not apply to this call, where granting again would not help.
var grantNotAppliedLabels = map[string]bool{
	"deny:delegation_required": true,
	"deny:no_delegation_scope": true,
	"deny:no_delegator_grant":  true,
	"deny:no_agent_grant":      true,
}

// policyDenialReason picks the denial reason from the primary and alternate
// decisions. A held grant that did not apply wins, since only it explains the
// call; a missing grant comes next; anything else is an operational failure.
func policyDenialReason(reasons []string) (DenialReason, string) {
	for _, r := range reasons {
		if grantNotAppliedLabels[r] {
			return DenialGrantNotApplied, r
		}
	}
	for _, r := range reasons {
		if r == "" || r == noMatchingGrant {
			return DenialNotGranted, ""
		}
	}
	return DenialPolicyUnavailable, ""
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

// resolveConnection finds the connection serving the request's host within the
// caller's tenant, choosing among several with selectConnection. Cross-tenant
// access is impossible because ListByHost is tenant-scoped:
// a connection in another tenant matching the same host is indistinguishable
// from "not found" here.
//
// registered is false when no connection serves the host. The broker then
// synthesizes one for the passthrough adapter, which injects nothing.
// Connections are a credential overlay, not the reachability gate, so a host
// that only needs to be reached needs the egress whitelist and nothing else.
func (b *Broker) resolveConnection(ctx context.Context, claims *token.PlatformClaims, req EgressRequest) (*credentials.Connection, bool, error) {
	tenantID, host := claims.Orbit.TenantID, req.Host
	candidates, err := b.conns.ListByHost(ctx, tenantID, host)
	switch {
	case err == nil && len(candidates) == 1:
		return candidates[0], true, nil
	case err == nil:
		conn, selectErr := b.selectConnection(ctx, claims, req, candidates)
		return conn, selectErr == nil, selectErr
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
