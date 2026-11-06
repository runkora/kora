package agents

import (
	"context"
	"errors"
	"fmt"

	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// runChain is the event chain a run start or message carries onto its target run
// (docs/adr/2026-11-06-event-spine.md, section 7).
type runChain struct {
	causedBy string
	depth    int
	triggers []string
}

// runGetter reads the run an agent acts for.
type runGetter interface {
	GetRun(ctx context.Context, id string) (*Run, error)
}

// attestedRunID is the run an AGENT caller acts for when the platform attests
// it: the run of a per-run sandbox identity, or the run its delegation is bound to.
func attestedRunID(ctx context.Context) string {
	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil || claims.Kora.PrincipalType != token.PrincipalTypeAgent {
		return ""
	}
	if id := claims.Kora.Claims["run_id"]; id != "" {
		return id
	}
	if d := policy.DelegationFromContext(ctx); d != nil {
		return d.RunID
	}
	return ""
}

// requestChain is the chain a trigger reports, checked by authorizeChain, or
// else the one the caller inherits. from is the authorized trigger, nil for anyone else.
func requestChain(ctx context.Context, runs runGetter, from *StartedByTrigger, causedBy string, depth int, targetRunID string) (runChain, error) {
	if err := authorizeChain(from, causedBy, depth); err != nil {
		return runChain{}, err
	}
	if causedBy != "" || depth != 0 {
		return runChain{causedBy: causedBy, depth: depth}, nil
	}
	return inheritedChain(ctx, runs, targetRunID)
}

// inheritedChain is the chain of the attested run the caller acts for, one step
// deeper, so a run an agent starts or messages stays in its loop guards. It is
// empty for any other caller, for the target run itself, for a run that is not
// the calling agent's in its tenant, and for a run in no trigger chain.
func inheritedChain(ctx context.Context, runs runGetter, targetRunID string) (runChain, error) {
	id := attestedRunID(ctx)
	if id == "" || id == targetRunID {
		return runChain{}, nil
	}
	parent, err := runs.GetRun(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return runChain{}, nil
	}
	if err != nil {
		return runChain{}, fmt.Errorf("read the calling run: %w", err)
	}
	claims, _ := token.ClaimsFromContext(ctx)
	// A run outside a trigger chain passes no chain on.
	if parent.TenantID != tenantFromContext(ctx) || parent.AgentSlug != agentSlugFromClaims(claims) ||
		(parent.ChainDepth == 0 && len(parent.TriggerIDs) == 0) {
		return runChain{}, nil
	}
	return runChain{causedBy: parent.CausedBy, depth: parent.ChainDepth + 1, triggers: parent.TriggerIDs}, nil
}
