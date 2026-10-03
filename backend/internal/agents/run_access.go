package agents

import (
	"context"
	"errors"
	"fmt"

	"github.com/clear-route/orbit/internal/platform/token"
)

// Delegated-run steer and visibility constraints
// (docs/adr/2026-07-23-delegated-agent-identity.md, Decision 6).
//
// A run of a DELEGATED agent belongs to the person who started it: it executes
// with the starter's authority under the starter's name. On top of the
// scope-based authz in authz.go, the service layer enforces:
//
//   - Visibility: run get / list / events / outcomes and terminate are limited
//     to the starter, unless the caller holds agents:delegated:read.
//   - Steer: sending messages and interrupting are limited to the starter, for
//     every caller, because steering acts with the starter's authority.
//
// Autonomous agents are untouched by every rule here. No role is evaluated.

// ErrDelegatedRunSteer is returned when someone other than the starter, a
// holder of agents:delegated:read included, tries to steer (message or interrupt) a delegated
// run. Messages execute with the starter's authority under the starter's name,
// so nobody else may direct the work.
var ErrDelegatedRunSteer = errors.New("delegated identity model: only the person who started this run can steer it (messages execute with the starter's authority)")

// RunVisibility scopes repository run reads for the delegated identity model.
// The filter is applied in the query itself, never by trimming pages in
// memory, so counts and ordering stay correct under any future pagination.
type RunVisibility struct {
	// SeeAll returns every run unfiltered. Set when the caller holds
	// agents:delegated:read.
	SeeAll bool
	// Subject is the caller's verified token subject. When SeeAll is false,
	// runs of delegated agents are returned only where created_by = Subject.
	// Empty matches no delegated run (fail closed); autonomous-agent runs are
	// never filtered.
	Subject string
}

// runVisibility decides, once per request, whether the caller sees every
// delegated run. It is the agents:delegated:read check, not a role test.
func (s *RunService) runVisibility(ctx context.Context) (RunVisibility, error) {
	allowed, err := s.authz.canReadDelegatedRuns(ctx)
	if err != nil {
		return RunVisibility{}, err
	}
	return RunVisibility{SeeAll: allowed, Subject: callerSubject(ctx)}, nil
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

// isRunStarter reports whether the caller's verified subject is the run's
// recorded starter. An empty subject or empty created_by never matches.
func isRunStarter(ctx context.Context, run *Run) bool {
	sub := callerSubject(ctx)
	return sub != "" && run.CreatedBy == sub
}

// checkDelegatedRunVisibility enforces run-ownership privacy on a single run:
// for a delegated agent, only the starter may read it unless seeAll. Returns
// ErrNotFound on deny so existence is not leaked (the same mapping
// loadRunForAction applies to scope denials). No-op for autonomous agents.
func checkDelegatedRunVisibility(ctx context.Context, at *AgentType, run *Run, seeAll bool) error {
	if at.IdentityMode.OrDefault() != IdentityModeDelegated || seeAll || isRunStarter(ctx, run) {
		return nil
	}
	return ErrNotFound
}

// appendVisibility adds the delegated-run rule to a run query whose alias for
// runs is r and for agent_types is at, appending the caller's subject to args.
// Shared by ListRuns and CountRunOutcomes so the rule lives in one place. An
// empty subject matches no delegated run (fail closed).
func appendVisibility(query string, args []any, vis RunVisibility) (string, []any) {
	if vis.SeeAll {
		return query, args
	}
	args = append(args, vis.Subject)
	n := len(args)
	return query + fmt.Sprintf(" AND (at.identity_mode <> 'delegated' OR ($%[1]d <> '' AND r.created_by = $%[1]d))", n), args
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
