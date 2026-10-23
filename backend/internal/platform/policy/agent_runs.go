package policy

import (
	"context"
	"fmt"

	"github.com/runkora/kora/backend/internal/platform/token"
)

// AgentRunAllowed reports whether the caller in ctx may perform action ("read" or
// "write") on runs of the agent with this slug. It checks agents:runs:<slug>:<action>.
// A delegated agent's write also passes on agents:runs:delegated:write, which is
// not a prefix cover of any per-agent scope and is therefore checked by name.
// A context without verified claims is denied. delegated must come from the stored agent type.
// kora-agents uses it for every run check. kora-triggers can call it for a trigger bind check.
func AgentRunAllowed(ctx context.Context, eval Evaluator, slug string, delegated bool, action string) (bool, error) {
	allowed, err := checkAgentRuns(ctx, eval, slug, action)
	if err != nil || allowed || !delegated || action != "write" {
		return allowed, err
	}
	return checkAgentRuns(ctx, eval, "delegated", action)
}

func checkAgentRuns(ctx context.Context, eval Evaluator, segment, action string) (bool, error) {
	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		return false, nil
	}
	r, err := eval.Check(ctx, CheckInput{
		Claims:       claims,
		Delegation:   DelegationFromContext(ctx),
		ResourceType: "agents",
		PathSegments: []string{"runs", segment},
		Action:       action,
	})
	if err != nil {
		return false, fmt.Errorf("authz check agent runs: %w", err)
	}
	return r.Allow, nil
}
