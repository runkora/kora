package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/policyservice/bundle"
	"github.com/runkora/kora/backend/internal/policyservice/vocabulary"
)

// coherenceRule names a scope that does nothing on its own: matches picks the
// scope, companion derives the scope it depends on.
type coherenceRule struct {
	matches   func(parts []string) bool
	companion func(parts []string) string
	// declaredOnly limits the rule to companions in the vocabulary, for
	// adapters where the companion exists for some connections only.
	declaredOnly bool
}

// coherenceRules are the known dependency gaps a grant or role write is linted
// for. Gaps produce warnings, never errors: another grant or role may fill them.
var coherenceRules = []coherenceRule{
	{ // A model or provider scope is useless without the endpoint gate.
		matches:   func(p []string) bool { return p[0] == "inference" && last(p) == "use" },
		companion: fixed("inference:invoke"),
	},
	{ // An MCP tool or server scope is useless without the tool-gateway gate.
		matches:   func(p []string) bool { return p[0] == "mcp" && last(p) == "use" },
		companion: fixed("mcp:invoke"),
	},
	{ // GitHub clients probe metadata before any category call.
		matches: func(p []string) bool { return p[0] == "connection" && len(p) >= 4 && p[2] != "metadata" },
		companion: func(p []string) string {
			return "connection:" + p[1] + ":metadata:read"
		},
		declaredOnly: true,
	},
	{ // Starting or reading an agent's runs needs the agent's definition.
		matches: func(p []string) bool {
			return len(p) == 4 && p[0] == "agents" && p[1] == "runs" && p[2] != "delegated" && p[2] != "autonomous"
		},
		companion: func(p []string) string { return "agents:types:" + p[2] + ":read" },
	},
	{ // Editing agent definitions needs reading them.
		matches:   func(p []string) bool { return len(p) >= 3 && p[0] == "agents" && p[1] == "types" && last(p) == "write" },
		companion: fixed("agents:types:read"),
	},
	{ // Editing triggers needs reading them.
		matches:   func(p []string) bool { return strings.Join(p, ":") == "triggers:write" },
		companion: fixed("triggers:read"),
	},
}

func last(p []string) string { return p[len(p)-1] }

func fixed(scope string) func([]string) string { return func([]string) string { return scope } }

// coherenceWarnings returns one warning per missing companion scope. Scopes are
// expanded with their vocabulary requires first, so a companion conferred by a
// requires edge counts as held.
func coherenceWarnings(scopes []string, vocab []*vocabulary.ScopeEntry) []string {
	held := bundle.ExpandRequires(scopes, vocab)
	declared := make(map[string]struct{}, len(vocab))
	for _, e := range vocab {
		declared[e.Scope] = struct{}{}
	}

	var warnings []string
	reported := map[string]struct{}{}
	for _, scope := range scopes {
		parts := strings.Split(scope, ":")
		if len(parts) < 2 {
			continue
		}
		for _, rule := range coherenceRules {
			if !rule.matches(parts) {
				continue
			}
			companion := rule.companion(parts)
			if _, done := reported[companion]; done || policy.AnyScopeCovers(held, companion) {
				continue
			}
			if _, ok := declared[companion]; rule.declaredOnly && !ok {
				continue
			}
			reported[companion] = struct{}{}
			warnings = append(warnings, fmt.Sprintf("%s needs %s, which is not included", scope, companion))
		}
	}
	return warnings
}

// LintScopes returns coherence warnings for a grant or role write. A
// vocabulary read failure yields no warnings: the lint never blocks a write.
func (s *Service) LintScopes(ctx context.Context, tenantID string, scopes []string) []string {
	vocab, err := s.vocabRepo.List(ctx, tenantID, "")
	if err != nil {
		s.logger.Warn(ctx, "scope coherence lint skipped", logging.Field{Key: "error", Value: err.Error()})
		return nil
	}
	return coherenceWarnings(scopes, vocab)
}
