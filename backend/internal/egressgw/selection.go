package egressgw

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/clear-route/orbit/internal/credentials"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/token"
)

// ownerScoped is a connector bound to one upstream account. Connectors that
// implement it can be told apart when several connections serve one host.
type ownerScoped interface {
	Owner() string
	RequestOwners(req *http.Request) credentials.RequestOwners
	NodeOwners(ctx context.Context, ids []string) (map[string]string, error)
}

// errNodeLookup is the span status of a failed node id owner lookup.
var errNodeLookup = errors.New("node owner lookup")

// selectConnection chooses among the connections serving one host: by the
// owners the request names, else by the one candidate the caller holds, else by
// the owners its node ids resolve to. A request it cannot resolve returns an
// unresolved-connection Denial. See
// docs/adr/2026-10-15-same-host-connection-selection.md.
func (b *Broker) selectConnection(ctx context.Context, claims *token.PlatformClaims, req EgressRequest, candidates []*credentials.Connection) (*credentials.Connection, error) {
	owners := make([]string, len(candidates))
	scoped := map[*credentials.Connection]ownerScoped{}
	var target credentials.RequestOwners
	for i, c := range candidates {
		s, ok := b.openOwnerScoped(c)
		if !ok {
			continue
		}
		scoped[c], owners[i] = s, s.Owner()
		if target.Subject == "" {
			target = s.RequestOwners(req.HTTPRequest)
		}
	}
	if target.Subject == "" {
		target.Subject = "request path"
	}

	if len(target.Owners) > 0 {
		return selectByOwners(req.Host, candidates, owners, target.Owners, target.Subject)
	}

	held, err := b.heldCandidates(ctx, claims, candidates)
	if err != nil {
		b.logger.Error(ctx, "reading caller scopes failed",
			logging.Field{Key: "host", Value: req.Host},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return nil, &Denial{Reason: DenialPolicyUnavailable, Host: req.Host, cause: err}
	}
	switch {
	case len(held) == 1:
		return held[0], nil
	case len(held) > 1 && len(target.NodeIDs) > 0:
		resolved, err := b.resolveNodeOwners(ctx, claims, req.Host, held, scoped, target.NodeIDs)
		if err != nil {
			return nil, unresolved(req.Host, candidates, true, "the node id lookup failed")
		}
		if len(resolved) > 0 {
			return selectByOwners(req.Host, candidates, owners, resolved, "node ids in the "+target.Subject)
		}
		return nil, unresolved(req.Host, candidates, true, "the "+target.Subject+" names no owner, and none of its node ids resolved to one")
	}
	return nil, unresolved(req.Host, candidates, len(held) > 1, "the "+target.Subject+" names no owner")
}

// selectByOwners returns the one candidate every named owner maps to. An owner
// no candidate serves leaves the request unresolved. Owners that span
// candidates deny it, since one request carries one token.
func selectByOwners(host string, candidates []*credentials.Connection, owners, named []string, subject string) (*credentials.Connection, error) {
	var selected *credentials.Connection
	for _, name := range named {
		i := slices.Index(owners, name)
		if i < 0 {
			return nil, unresolved(host, candidates, false, fmt.Sprintf("owner %s from the %s is served by no connection", name, subject))
		}
		if selected != nil && selected != candidates[i] {
			return nil, unresolved(host, candidates, true, fmt.Sprintf("owners %s from the %s are served by different connections", strings.Join(named, ", "), subject))
		}
		selected = candidates[i]
	}
	return selected, nil
}

// resolveNodeOwners returns the distinct owners of the node ids that resolve,
// trying each held candidate's lookup in turn for the ids still unresolved. Ids
// no held candidate can see are ignored. Results are cached per tenant and host.
func (b *Broker) resolveNodeOwners(ctx context.Context, claims *token.PlatformClaims, host string, held []*credentials.Connection, scoped map[*credentials.Connection]ownerScoped, ids []string) ([]string, error) {
	tenantID := claims.Orbit.TenantID
	found := map[string]bool{}
	var pending []string
	for _, id := range ids {
		if owner, ok := b.nodeOwners.get(tenantID, host, id); ok {
			found[owner] = true
		} else {
			pending = append(pending, id)
		}
	}
	for _, c := range held {
		s, ok := scoped[c]
		if len(pending) == 0 || !ok {
			continue
		}
		resolved, err := b.lookupNodeOwners(ctx, claims, c, s, pending)
		if err != nil {
			return nil, err
		}
		pending = slices.DeleteFunc(pending, func(id string) bool {
			owner, ok := resolved[id]
			if ok {
				found[owner] = true
				b.nodeOwners.put(tenantID, host, id, owner)
			}
			return ok
		})
	}
	return slices.Sorted(maps.Keys(found)), nil
}

// lookupNodeOwners runs one candidate's node id lookup under its own span and
// logs it with the run attribution. Its result never reaches the agent.
func (b *Broker) lookupNodeOwners(ctx context.Context, claims *token.PlatformClaims, c *credentials.Connection, s ownerScoped, ids []string) (map[string]string, error) {
	ctx, span := tracer.Start(ctx, "egress.node_owner_lookup", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()
	span.SetAttributes(attribute.String("orbit.connection", c.Name), attribute.Int("orbit.node_ids", len(ids)))

	resolved, err := s.NodeOwners(ctx, ids)
	fields := []logging.Field{
		{Key: "connection", Value: c.Name},
		{Key: "node_ids", Value: len(ids)},
		{Key: "resolved", Value: len(resolved)},
		{Key: "tenant_id", Value: claims.Orbit.TenantID},
		{Key: "principal_id", Value: claims.Subject},
		{Key: "run_id", Value: claims.Orbit.Claims["run_id"]},
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, errNodeLookup.Error())
		b.logger.Warn(ctx, "node id owner lookup failed", append(fields, logging.Field{Key: "error", Value: err.Error()})...)
		return nil, err
	}
	b.logger.Info(ctx, "node id owner lookup", fields...)
	return resolved, nil
}

// openOwnerScoped opens c and returns its connector when it reports owners. A
// connection that fails to open is skipped here and fails if selected.
func (b *Broker) openOwnerScoped(c *credentials.Connection) (ownerScoped, bool) {
	adapter, ok := b.adapters.Lookup(c.Adapter)
	if !ok {
		return nil, false
	}
	connector, err := adapter.Open(*c)
	if err != nil {
		return nil, false
	}
	scoped, ok := connector.(ownerScoped)
	return scoped, ok
}

// heldCandidates returns the candidates the caller holds any scope on. A
// two-part connection:<verb> scope covers every connection, so it holds them all.
func (b *Broker) heldCandidates(ctx context.Context, claims *token.PlatformClaims, candidates []*credentials.Connection) ([]*credentials.Connection, error) {
	scopes, err := b.policy.Scopes(ctx, policy.ScopesInput{Claims: claims})
	if err != nil {
		return nil, fmt.Errorf("read caller scopes: %w", err)
	}
	for _, s := range scopes {
		if parts := strings.Split(s, ":"); len(parts) == 2 && parts[0] == resourceType {
			return candidates, nil
		}
	}
	var held []*credentials.Connection
	for _, c := range candidates {
		prefix := resourceType + ":" + c.Name + ":"
		for _, s := range scopes {
			if strings.HasPrefix(s, prefix) {
				held = append(held, c)
				break
			}
		}
	}
	return held, nil
}

// unresolved builds the denial for a request no single candidate serves. part
// states what could not be tied to an owner.
func unresolved(host string, candidates []*credentials.Connection, ambiguous bool, part string) *Denial {
	names := make([]string, len(candidates))
	for i, c := range candidates {
		names[i] = c.Name
	}
	return &Denial{Reason: DenialConnectionUnresolved, Host: host, Candidates: names, Unresolved: part, ambiguous: ambiguous, adapter: candidates[0].Adapter}
}

// unresolvedConnection handles a request selectConnection could not resolve. No
// credential is minted. An ambiguous request or a failed scope read is denied.
// Any other is served by the anonymous read fallback when it qualifies.
func (b *Broker) unresolvedConnection(ctx context.Context, span trace.Span, claims *token.PlatformClaims, req EgressRequest, d *Denial) (Decision, error) {
	if d.ambiguous || d.Reason != DenialConnectionUnresolved {
		span.SetStatus(codes.Error, ErrForbidden.Error())
		b.logDenial(ctx, claims.Orbit.TenantID, req, d)
		return Decision{}, d
	}
	conn := &credentials.Connection{
		TenantID: claims.Orbit.TenantID,
		Name:     passthroughConnectionName,
		Adapter:  d.adapter,
		BaseHost: req.Host,
	}
	return b.anonymousReadOrDeny(ctx, span, claims, conn, req, d)
}
