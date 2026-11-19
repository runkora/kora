package agents

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"

	"github.com/runkora/kora/backend/internal/platform/clients/sessionmanager"
)

// agentConfig is the effective agent config a run starts with, in canonical form for agentConfigDigest.
// Fields are in key order and every list is sorted, so equal configs encode to equal bytes.
type agentConfig struct {
	Brief        *agentConfigBrief  `json:"brief"`
	Connectors   []Connector        `json:"connectors"`
	Environments []AgentEnvironment `json:"environments"`
	Framework    string             `json:"framework"`
	GrantScopes  []string           `json:"grant_scopes"`
	Guardrails   map[string]int     `json:"guardrails"`
	MCPTools     []string           `json:"mcp_tools"`
	Model        string             `json:"model"`
	Skills       []string           `json:"skills"`
	SystemPrompt string             `json:"system_prompt"`
}

// agentConfigBrief holds what the runtime-environment brief is rendered from, except the run's workspace dir.
type agentConfigBrief struct {
	ConnectorHosts map[string][]string `json:"connector_hosts"`
	Connectors     []Connector         `json:"connectors"`
	Hosts          []string            `json:"hosts"`
	Open           bool                `json:"open"`
	Product        string              `json:"product"`
	SandboxMount   bool                `json:"sandbox_mount"`
}

// agentConfigDigest is the sha256 hex of the config a run of at starts with (docs/adr/2026-11-19-graders.md).
// It hashes the brief's inputs and not the rendered brief, which names the run's workspace dir.
// A nil access means the run starts without the brief. Guardrails are the ones the session request sends.
// Skills stay empty until skills attach to agents.
func agentConfigDigest(at AgentType, access *EffectiveAccess, guardrails sessionmanager.RunGuardrails, sandboxMount bool, product string) string {
	envs := append([]AgentEnvironment{}, at.Environments...)
	slices.SortStableFunc(envs, func(a, b AgentEnvironment) int { return strings.Compare(a.EnvironmentID, b.EnvironmentID) })
	cfg := agentConfig{
		Connectors:   canonicalConnectors(at.Connectors),
		Environments: envs,
		Framework:    string(at.Framework),
		GrantScopes:  sortedStrings(at.EffectiveGrantScopes()),
		// A map encodes with sorted keys.
		Guardrails: map[string]int{
			"repeated_call_pause":            guardrails.RepeatedCallPause,
			"repeated_call_warn":             guardrails.RepeatedCallWarn,
			"sandbox_provision_attempts":     guardrails.SandboxProvisionAttempts,
			"sandbox_provision_backoff_secs": guardrails.SandboxProvisionBackoffSecs,
			"sandbox_provision_budget_secs":  guardrails.SandboxProvisionBudgetSecs,
			"wait_rearm_pause":               guardrails.WaitRearmPause,
			"wait_rearm_warn":                guardrails.WaitRearmWarn,
		},
		MCPTools:     sortedStrings(at.MCPTools),
		Skills:       []string{},
		SystemPrompt: at.SystemPrompt,
	}
	if at.ModelID != nil {
		cfg.Model = *at.ModelID
	}
	if access != nil {
		hosts := make(map[string][]string, len(access.ConnectorEgress))
		for _, ce := range access.ConnectorEgress {
			hosts[ce.Connection] = sortedStrings(ce.Hosts)
		}
		cfg.Brief = &agentConfigBrief{
			ConnectorHosts: hosts,
			Connectors:     canonicalConnectors(access.Connectors),
			Hosts:          sortedStrings(access.Hosts),
			Open:           access.Open,
			Product:        product,
			SandboxMount:   sandboxMount,
		}
	}
	// Marshal cannot fail: the config holds only strings, bools, slices and string-keyed maps.
	b, _ := json.Marshal(cfg)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// sortedStrings returns a sorted copy of s, never nil.
func sortedStrings(s []string) []string {
	out := append([]string{}, s...)
	slices.Sort(out)
	return out
}

// canonicalConnectors returns the connectors sorted by connection, with sorted actions and resources.
func canonicalConnectors(cs []Connector) []Connector {
	out := make([]Connector, 0, len(cs))
	for _, c := range cs {
		out = append(out, Connector{Connection: c.Connection, Actions: sortedStrings(c.Actions), Resources: sortedStrings(c.Resources)})
	}
	slices.SortStableFunc(out, func(a, b Connector) int { return strings.Compare(a.Connection, b.Connection) })
	return out
}
