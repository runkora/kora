package agents

import (
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/api/resource"
)

// ErrNoStartingRunAllowance means no starting-run allowance is configured for an agent's framework.
var ErrNoStartingRunAllowance = errors.New("no starting-run allowance for the agent's framework")

// StartingRunAllowances is the memory a run is assumed to take between its start
// and the next memory reading, by framework (docs/adr/2026-11-09-demand-driven-agent-scaling.md).
type StartingRunAllowances map[AgentFramework]resource.Quantity

// NewStartingRunAllowances keys the chart's claudeCode, codex and openCode values by the framework each names.
func NewStartingRunAllowances(claudeCode, codex, openCode resource.Quantity) StartingRunAllowances {
	return StartingRunAllowances{AgentFrameworkClaudeCode: claudeCode, AgentFrameworkCodex: codex, AgentFrameworkOpenCode: openCode}
}

// For returns the allowance for framework. Empty is claude-code, as the session-manager treats it.
func (a StartingRunAllowances) For(framework AgentFramework) (resource.Quantity, error) {
	if framework == "" {
		framework = AgentFrameworkClaudeCode
	}
	q, ok := a[framework]
	if !ok {
		return resource.Quantity{}, fmt.Errorf("%w: %q", ErrNoStartingRunAllowance, framework)
	}
	return q, nil
}
