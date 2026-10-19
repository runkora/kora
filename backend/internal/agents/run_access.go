package agents

import (
	"context"
	"errors"
	"fmt"

	"github.com/runkora/kora/backend/internal/platform/token"
)

// A delegated run belongs to its starter (docs/adr/2026-07-23-delegated-agent-identity.md,
// Decision 6). Visibility and terminate are starter-only unless the caller holds
// agents:delegated:read, and steering is starter-only for everyone. No role is read.

// ErrDelegatedRunSteer is returned when anyone but the starter tries to steer a
// delegated run, because messages execute with the starter's authority.
var ErrDelegatedRunSteer = errors.New("delegated identity model: only the person who started this run can steer it (messages execute with the starter's authority)")

// RunVisibility scopes repository run reads for the delegated identity model.
// The filter is applied in the query itself, never by trimming pages in
// memory, so counts and ordering stay correct under any future pagination.
type RunVisibility struct {
	// SeeAll returns every run unfiltered. Set for holders of agents:delegated:read.
	SeeAll bool
	// Subject is the caller's verified token subject. When SeeAll is false,
	// runs of delegated agents are returned only where created_by = Subject.
	// Empty matches no delegated run (fail closed); autonomous-agent runs are
	// never filtered.
	Subject string
}

// runVisibility checks agents:delegated:read once per request.
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

// checkDelegatedRunVisibility limits a delegated run to its starter unless seeAll.
// It returns ErrNotFound on deny so existence is not leaked.
func checkDelegatedRunVisibility(ctx context.Context, at *AgentType, run *Run, seeAll bool) error {
	if at.IdentityMode.OrDefault() != IdentityModeDelegated || seeAll || isRunStarter(ctx, run) {
		return nil
	}
	return ErrNotFound
}

// appendVisibility adds the delegated-run rule to a run query (runs as r,
// agent_types as at). An empty subject matches no delegated run.
func appendVisibility(query string, args []any, vis RunVisibility) (string, []any) {
	if vis.SeeAll {
		return query, args
	}
	args = append(args, vis.Subject)
	n := len(args)
	return query + fmt.Sprintf(" AND (at.identity_mode <> 'delegated' OR ($%[1]d <> '' AND r.created_by = $%[1]d))", n), args
}

// checkDelegatedRunSteer limits messaging and interrupting a delegated run to its starter.
func checkDelegatedRunSteer(ctx context.Context, at *AgentType, run *Run) error {
	if at.IdentityMode.OrDefault() != IdentityModeDelegated {
		return nil
	}
	if isRunStarter(ctx, run) {
		return nil
	}
	return ErrDelegatedRunSteer
}
