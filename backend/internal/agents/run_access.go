package agents

import (
	"context"
	"errors"
	"strings"

	"github.com/clear-route/orbit/internal/platform/token"
)

// Delegated-run steer and visibility constraints
// (docs/adr/2026-07-23-delegated-agent-identity.md, Decision 6).
//
// A run of a DELEGATED agent belongs to the person who started it: it executes
// with the starter's authority under the starter's name. The service layer
// therefore enforces, on top of the scope-based authz in authz.go:
//
//   - Visibility: run get / list / events are limited to the starter, unless
//     the caller holds a reserved admin role (governance read).
//   - Steer: sending messages and interrupting are limited to the starter —
//     admins included, because steering acts with the starter's authority.
//   - Terminate: starter and admins (governance control).
//
// Autonomous agents are untouched by every rule here.

// rolePlatformAdmin mirrors bundle.PlatformAdminRole: the reserved role,
// compiled into every bundle and not redefinable per tenant.
const rolePlatformAdmin = "platform-admin"

// ErrDelegatedRunSteer is returned when someone other than the starter — a
// tenant admin included — tries to steer (message or interrupt) a delegated
// run. Messages execute with the starter's authority under the starter's name,
// so nobody else may direct the work.
var ErrDelegatedRunSteer = errors.New("delegated identity model: only the person who started this run can steer it (messages execute with the starter's authority)")

// RunVisibility scopes repository run reads for the delegated identity model.
// The filter is applied in the query itself, never by trimming pages in
// memory, so counts and ordering stay correct under any future pagination.
type RunVisibility struct {
	// SeeAll returns every run unfiltered. Set for tenant admins (governance
	// read over the whole tenant).
	SeeAll bool
	// Subject is the caller's verified token subject. When SeeAll is false,
	// runs of delegated agents are returned only where created_by = Subject.
	// Empty matches no delegated run (fail closed); autonomous-agent runs are
	// never filtered.
	Subject string
}

// runVisibilityFromContext derives the caller's run visibility from verified
// claims: admins see all, everyone else sees delegated runs they started.
func runVisibilityFromContext(ctx context.Context) RunVisibility {
	if isPlatformAdmin(ctx) {
		return RunVisibility{SeeAll: true}
	}
	return RunVisibility{Subject: callerSubject(ctx)}
}

// callerSubject returns the verified platform-token subject, or "" when the
// context carries no claims. For PAT principals the subject is the issuing
// person's OIDC subject (the PAT counts as the person), so a run started with
// a PAT and read with an interactive session — or vice versa — matches.
func callerSubject(ctx context.Context) string {
	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		return ""
	}
	return claims.Subject
}

// isPlatformAdmin reports whether the verified claims carry a reserved admin
// role. Roles travel as the comma-separated "roles" claim the identity service
// stamps from the IdP — the same extraction the policy engine (extractRoles)
// and policy-service API use. PAT tokens carry no roles claim and therefore
// never count as admin here (fail closed to the non-admin rules).
func isPlatformAdmin(ctx context.Context) bool {
	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil || claims.Orbit.Claims == nil {
		return false
	}
	for role := range strings.SplitSeq(claims.Orbit.Claims["roles"], ",") {
		if strings.TrimSpace(role) == rolePlatformAdmin {
			return true
		}
	}
	return false
}

// isRunStarter reports whether the caller's verified subject is the run's
// recorded starter. An empty subject or empty created_by never matches.
func isRunStarter(ctx context.Context, run *Run) bool {
	sub := callerSubject(ctx)
	return sub != "" && run.CreatedBy == sub
}

// checkDelegatedRunVisibility enforces run-ownership privacy on a single run:
// for a delegated agent, only the starter and tenant admins may read it.
// Returns ErrNotFound on deny so existence is not leaked (the same mapping
// loadRunForAction applies to scope denials). No-op for autonomous agents.
func checkDelegatedRunVisibility(ctx context.Context, at *AgentType, run *Run) error {
	if at.IdentityMode.OrDefault() != IdentityModeDelegated {
		return nil
	}
	if isRunStarter(ctx, run) || isPlatformAdmin(ctx) {
		return nil
	}
	return ErrNotFound
}

// checkDelegatedRunSteer enforces the steer constraint: only the starter may
// send messages to or interrupt a delegated run. Admins are refused too —
// with the explicit reason rather than a 404, since visibility already admits
// them. No-op for autonomous agents.
func checkDelegatedRunSteer(ctx context.Context, at *AgentType, run *Run) error {
	if at.IdentityMode.OrDefault() != IdentityModeDelegated {
		return nil
	}
	if isRunStarter(ctx, run) {
		return nil
	}
	return ErrDelegatedRunSteer
}
