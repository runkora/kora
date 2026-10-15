package egressgw

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/clear-route/orbit/internal/credentials"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/token"
)

// ownerScoped is a connector bound to one upstream account. Connectors that
// implement it can be told apart when several connections serve one host.
type ownerScoped interface {
	Owner() string
	RequestOwner(req *http.Request) string
}

// selectConnection chooses among the connections serving one host: by the owner
// the request addresses, else by the one candidate the caller holds. A request it
// cannot resolve returns an unresolved-connection Denial. See
// docs/adr/2026-10-15-same-host-connection-selection.md.
func (b *Broker) selectConnection(ctx context.Context, claims *token.PlatformClaims, req EgressRequest, candidates []*credentials.Connection) (*credentials.Connection, error) {
	owners := make([]string, len(candidates))
	requestOwner := ""
	for i, c := range candidates {
		scoped, ok := b.ownerScoped(c)
		if !ok {
			continue
		}
		owners[i] = scoped.Owner()
		if requestOwner == "" {
			requestOwner = scoped.RequestOwner(req.HTTPRequest)
		}
	}

	if requestOwner != "" {
		for i, c := range candidates {
			if owners[i] == requestOwner {
				return c, nil
			}
		}
		return nil, unresolved(req.Host, candidates, false)
	}

	held, err := b.heldCandidates(ctx, claims, candidates)
	if err != nil {
		return nil, err
	}
	if len(held) == 1 {
		return held[0], nil
	}
	return nil, unresolved(req.Host, candidates, len(held) > 1)
}

// ownerScoped opens c and returns its connector when it reports owners. A
// connection that fails to open is skipped here and fails if selected.
func (b *Broker) ownerScoped(c *credentials.Connection) (ownerScoped, bool) {
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

// heldCandidates returns the candidates the caller holds any scope on.
func (b *Broker) heldCandidates(ctx context.Context, claims *token.PlatformClaims, candidates []*credentials.Connection) ([]*credentials.Connection, error) {
	scopes, err := b.policy.Scopes(ctx, policy.ScopesInput{Claims: claims})
	if err != nil {
		return nil, fmt.Errorf("read caller scopes: %w", err)
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

// unresolved builds the denial for a request no single candidate serves.
func unresolved(host string, candidates []*credentials.Connection, ambiguous bool) *Denial {
	names := make([]string, len(candidates))
	for i, c := range candidates {
		names[i] = c.Name
	}
	return &Denial{Reason: DenialConnectionUnresolved, Host: host, Candidates: names, ambiguous: ambiguous, adapter: candidates[0].Adapter}
}

// unresolvedConnection handles a request selectConnection could not resolve. No
// credential is minted. An ambiguous request is denied. Any other is served by
// the anonymous read fallback when it qualifies.
func (b *Broker) unresolvedConnection(ctx context.Context, span trace.Span, claims *token.PlatformClaims, req EgressRequest, d *Denial) (Decision, error) {
	if d.ambiguous {
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
