package agents

import (
	"context"
	"errors"
	"fmt"

	mw "github.com/runkora/kora/backend/internal/platform/middleware"
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

// inheritedChain is the chain of the run the agent caller acts for, one step
// deeper, so a run an agent starts or messages stays in its loop guards. It is
// empty for any other caller and for the target run itself.
func inheritedChain(ctx context.Context, runs runGetter, targetRunID string) (runChain, error) {
	id := mw.ActingRunID(ctx)
	if id == "" || id == targetRunID {
		return runChain{}, nil
	}
	claims, _ := token.ClaimsFromContext(ctx)
	chain, err := agentRunChain(ctx, runs, id, tenantFromContext(ctx), agentSlugFromClaims(claims))
	if err != nil || chain.depth == 0 && len(chain.triggers) == 0 {
		return runChain{}, err
	}
	chain.depth++
	return chain, nil
}

// agentRunChain is the chain of a run of agent in tenant. It is empty for a
// missing run, a run of another agent or tenant, and a run in no trigger chain.
func agentRunChain(ctx context.Context, runs runGetter, runID, tenant, agent string) (runChain, error) {
	run, err := runs.GetRun(ctx, runID)
	if errors.Is(err, ErrNotFound) {
		return runChain{}, nil
	}
	if err != nil {
		return runChain{}, fmt.Errorf("read the acting run: %w", err)
	}
	if run.TenantID != tenant || run.AgentSlug != agent {
		return runChain{}, nil
	}
	return runChain{causedBy: run.CausedBy, depth: run.ChainDepth, triggers: run.TriggerIDs}, nil
}
