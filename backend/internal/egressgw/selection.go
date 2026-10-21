package egressgw

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/runkora/kora/backend/internal/credentials"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// ownerScoped is a connector bound to one upstream account. Connectors that
// implement it can be told apart when several connections serve one host.
type ownerScoped interface {
	Owner() string
	RequestOwners(req *http.Request) credentials.RequestOwners
	NodeOwners(ctx context.Context, ids, categories []string) (map[string]string, error)
	RepoOwners(ctx context.Context, ids, categories []string) (map[string]string, error)
}

// idLookup resolves one kind of id, node or repository, to owners.
type idLookup struct {
	kind    string
	subject string
	resolve func(s ownerScoped, ctx context.Context, ids, categories []string) (map[string]string, error)
}

var (
	nodeLookup = idLookup{kind: "node", subject: "node ids", resolve: ownerScoped.NodeOwners}
	repoLookup = idLookup{kind: "repository", subject: "repository ids", resolve: ownerScoped.RepoOwners}
)

// idsToResolve returns the ids the request names for an owner lookup, node ids
// before repository ids.
func idsToResolve(target credentials.RequestOwners) ([]string, idLookup) {
	if len(target.NodeIDs) > 0 {
		return target.NodeIDs, nodeLookup
	}
	return target.RepoIDs, repoLookup
}

// MaxNodeIDs is the most node ids a document may name for the lookup to select a
// connection. It matches the most ids one lookup call resolves.
const MaxNodeIDs = 100

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

	var unserved string
	if len(target.Owners) > 0 {
		conn, name, err := selectByOwners(req.Host, candidates, owners, target.Owners, target.Subject)
		if name == "" {
			return conn, err
		}
		unserved = name
	}

	held, scopes, err := b.heldCandidates(ctx, claims, candidates)
	if err != nil {
		b.logger.Error(ctx, "reading caller scopes failed",
			logging.Field{Key: "host", Value: req.Host},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return nil, &Denial{Reason: DenialPolicyUnavailable, Host: req.Host, cause: err}
	}
	ids, lookup := idsToResolve(target)
	switch {
	case len(held) == 1:
		// The held token reaches only public data of an owner it does not serve.
		return held[0], nil
	case unserved != "":
		return nil, unresolved(req.Host, candidates, false, fmt.Sprintf("owner %s from the %s is served by no connection", unserved, target.Subject))
	case len(held) > 1 && len(ids) > MaxNodeIDs:
		return nil, unresolved(req.Host, candidates, true, fmt.Sprintf("the %s names more than %d %s", target.Subject, MaxNodeIDs, lookup.subject))
	case len(held) > 1 && len(ids) > 0:
		return b.selectByIDs(ctx, claims, req.Host, candidates, held, scoped, scopes, target, ids, lookup)
	}
	return nil, unresolved(req.Host, candidates, len(held) > 1, "the "+target.Subject+" names no owner")
}

// selectByOwners returns the one candidate every named owner maps to, or the
// first named owner no candidate serves. Owners that span candidates deny the
// request, since one request carries one token.
func selectByOwners(host string, candidates []*credentials.Connection, owners, named []string, subject string) (*credentials.Connection, string, error) {
	var selected *credentials.Connection
	for _, name := range named {
		i := slices.Index(owners, name)
		if i < 0 {
			return nil, name, nil
		}
		if selected != nil && selected != candidates[i] {
			return nil, "", unresolved(host, candidates, true, fmt.Sprintf("owners %s from the %s are served by different connections", strings.Join(named, ", "), subject))
		}
		selected = candidates[i]
	}
	return selected, "", nil
}

// selectByIDs selects among the held candidates by the owners the request's
// node or repository ids resolve to. Its denials never name a resolved owner, to
// keep objects outside the caller's connections from leaking through them.
func (b *Broker) selectByIDs(ctx context.Context, claims *token.PlatformClaims, host string, candidates, held []*credentials.Connection, scoped map[*credentials.Connection]ownerScoped, scopes []string, target credentials.RequestOwners, ids []string, lookup idLookup) (*credentials.Connection, error) {
	resolved, err := b.resolveOwners(ctx, claims, host, held, scoped, scopes, ids, lookup)
	if err != nil {
		return nil, unresolved(host, candidates, true, "the "+lookup.kind+" id lookup failed")
	}
	if len(resolved) == 0 {
		return nil, unresolved(host, candidates, true, "the "+target.Subject+" names no owner, and none of its "+lookup.subject+" resolved to one")
	}
	var selected *credentials.Connection
	for _, owner := range resolved {
		i := slices.IndexFunc(held, func(c *credentials.Connection) bool { return scoped[c] != nil && scoped[c].Owner() == owner })
		if i < 0 {
			return nil, unresolved(host, candidates, true, "the "+lookup.subject+" in the "+target.Subject+" resolve to an account none of the agent's connections serves")
		}
		if selected != nil && selected != held[i] {
			return nil, unresolved(host, candidates, true, "the "+lookup.subject+" in the "+target.Subject+" resolve to accounts served by different connections")
		}
		selected = held[i]
	}
	return selected, nil
}

// resolveOwners returns the distinct owners of the ids that resolve,
// trying each held candidate in turn for the ids still unresolved. A lookup sees
// only the categories the caller holds unqualified on that connection, and a
// cached owner counts only for the same connection and categories. Ids no held
// candidate can see are ignored.
func (b *Broker) resolveOwners(ctx context.Context, claims *token.PlatformClaims, host string, held []*credentials.Connection, scoped map[*credentials.Connection]ownerScoped, scopes, ids []string, lookup idLookup) ([]string, error) {
	tenantID := claims.Kora.TenantID
	found := map[string]bool{}
	pending := slices.Clone(ids)
	for _, c := range held {
		s, ok := scoped[c]
		categories := lookupCategories(scopes, c.Name)
		if len(pending) == 0 || !ok || len(categories) == 0 {
			continue
		}
		cacheConn := lookup.kind + "|" + c.Name + "|" + strings.Join(categories, ",")
		resolved := map[string]string{}
		var misses []string
		for _, id := range pending {
			owner, hit := b.nodeOwners.get(tenantID, host, cacheConn, id)
			switch {
			case !hit:
				misses = append(misses, id)
			case owner != "":
				resolved[id] = owner
			}
		}
		if len(misses) > 0 {
			looked, err := b.lookupOwners(ctx, claims, c, s, misses, categories, lookup)
			if err != nil {
				return nil, err
			}
			// An id this token cannot see is cached as "" to spare repeat lookups.
			for _, id := range misses {
				resolved[id] = looked[id]
				b.nodeOwners.put(tenantID, host, cacheConn, id, looked[id])
			}
		}
		pending = slices.DeleteFunc(pending, func(id string) bool {
			owner := resolved[id]
			if owner != "" {
				found[owner] = true
			}
			return owner != ""
		})
	}
	return slices.Sorted(maps.Keys(found)), nil
}

// lookupOwners runs one candidate's id lookup under its own span and
// logs it with the run attribution. Its result never reaches the agent.
func (b *Broker) lookupOwners(ctx context.Context, claims *token.PlatformClaims, c *credentials.Connection, s ownerScoped, ids, categories []string, lookup idLookup) (map[string]string, error) {
	ctx, span := tracer.Start(ctx, "egress."+lookup.kind+"_owner_lookup", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()
	span.SetAttributes(attribute.String("orbit.connection", c.Name), attribute.Int("orbit."+lookup.kind+"_ids", len(ids)))

	resolved, err := lookup.resolve(s, ctx, ids, categories)
	fields := []logging.Field{
		{Key: "connection", Value: c.Name},
		{Key: lookup.kind + "_ids", Value: len(ids)},
		{Key: "resolved", Value: len(resolved)},
		{Key: "tenant_id", Value: claims.Kora.TenantID},
		{Key: "principal_id", Value: claims.Subject},
		{Key: "run_id", Value: claims.Kora.Claims["run_id"]},
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, lookup.kind+" owner lookup")
		b.logger.Warn(ctx, lookup.kind+" id owner lookup failed", append(fields, logging.Field{Key: "error", Value: err.Error()})...)
		return nil, err
	}
	b.logger.Info(ctx, lookup.kind+" id owner lookup", fields...)
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
func (b *Broker) heldCandidates(ctx context.Context, claims *token.PlatformClaims, candidates []*credentials.Connection) ([]*credentials.Connection, []string, error) {
	scopes, err := b.policy.Scopes(ctx, policy.ScopesInput{Claims: claims})
	if err != nil {
		return nil, nil, fmt.Errorf("read caller scopes: %w", err)
	}
	for _, s := range scopes {
		if parts := strings.Split(s, ":"); len(parts) == 2 && parts[0] == resourceType {
			return candidates, scopes, nil
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
	return held, scopes, nil
}

// lookupCategories returns the categories the caller holds without a repository
// qualifier on a connection, the only ones a node id lookup may read. A
// two-part connection:<verb> scope holds every category, as the repo umbrella.
func lookupCategories(scopes []string, connection string) []string {
	held := map[string]bool{}
	for _, s := range scopes {
		parts := strings.Split(s, ":")
		switch {
		case parts[0] != resourceType:
		case len(parts) == 2:
			held["repo"] = true
		case len(parts) == 4 && parts[1] == connection:
			held[parts[2]] = true
		}
	}
	return slices.Sorted(maps.Keys(held))
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
		b.logDenial(ctx, claims, req, d)
		return Decision{}, d
	}
	conn := &credentials.Connection{
		TenantID: claims.Kora.TenantID,
		Name:     passthroughConnectionName,
		Adapter:  d.adapter,
		BaseHost: req.Host,
	}
	return b.anonymousReadOrDeny(ctx, span, claims, conn, req, d)
}
