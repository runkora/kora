package policy

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/token"
)

// Delegation binding checks (docs/adr/2026-07-23-delegated-agent-identity.md,
// Decision 4): a delegation token presented outside the run or agent it was
// minted for is denied before any bundle evaluation.

func bindingAgentClaims(sub, tokenRunID string) *token.PlatformClaims {
	var extra map[string]string
	if tokenRunID != "" {
		extra = map[string]string{"run_id": tokenRunID}
	}
	return &token.PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: sub},
		Orbit: token.OrbitClaims{
			TenantID:      "t1",
			PrincipalType: token.PrincipalTypeAgent,
			Claims:        extra,
		},
	}
}

func TestCheckDelegationBinding(t *testing.T) {
	const agent = "spiffe://orbit.test/tenant/t1/agent/coder"
	tests := []struct {
		name       string
		claims     *token.PlatformClaims
		delegation *DelegationClaims
		runID      string // path-asserted run (CheckInput.RunID)
		wantReason string
	}{
		{
			name:       "no delegation — no binding check",
			claims:     bindingAgentClaims(agent, ""),
			delegation: nil,
			runID:      "run-x",
			wantReason: "",
		},
		{
			name:       "act.sub mismatch",
			claims:     bindingAgentClaims(agent, ""),
			delegation: &DelegationClaims{Sub: "user-1", ActSub: "spiffe://orbit.test/tenant/t1/agent/other", RunID: "run-a"},
			wantReason: ReasonDelegationActSubMismatch,
		},
		{
			name:       "path mode — asserted run matches",
			claims:     bindingAgentClaims(agent, ""),
			delegation: &DelegationClaims{Sub: "user-1", ActSub: agent, RunID: "run-a"},
			runID:      "run-a",
			wantReason: "",
		},
		{
			name:       "path mode — asserted run differs",
			claims:     bindingAgentClaims(agent, ""),
			delegation: &DelegationClaims{Sub: "user-1", ActSub: agent, RunID: "run-a"},
			runID:      "run-b",
			wantReason: ReasonDelegationRunMismatch,
		},
		{
			name:       "sandbox mode — platform token run matches",
			claims:     bindingAgentClaims(agent, "run-a"),
			delegation: &DelegationClaims{Sub: "user-1", ActSub: agent, RunID: "run-a"},
			wantReason: "",
		},
		{
			name:       "sandbox mode — platform token run differs",
			claims:     bindingAgentClaims(agent, "run-b"),
			delegation: &DelegationClaims{Sub: "user-1", ActSub: agent, RunID: "run-a"},
			wantReason: ReasonDelegationRunMismatch,
		},
		{
			name: "sandbox token beats a matching path assertion",
			// The attested sandbox run_id (run-b) contradicts the delegation
			// even though the asserted path agrees with it: attested wins.
			claims:     bindingAgentClaims(agent, "run-b"),
			delegation: &DelegationClaims{Sub: "user-1", ActSub: agent, RunID: "run-a"},
			runID:      "run-a",
			wantReason: ReasonDelegationRunMismatch,
		},
		{
			name:       "no run identity anywhere — binding not checkable, defer to policy",
			claims:     bindingAgentClaims(agent, ""),
			delegation: &DelegationClaims{Sub: "user-1", ActSub: agent, RunID: "run-a"},
			wantReason: "",
		},
		{
			name:       "delegation without run_id (forwards compat) — no run binding",
			claims:     bindingAgentClaims(agent, "run-b"),
			delegation: &DelegationClaims{Sub: "user-1", ActSub: agent},
			runID:      "run-c",
			wantReason: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := checkDelegationBinding(CheckInput{
				Claims:     tc.claims,
				Delegation: tc.delegation,
				RunID:      tc.runID,
			})
			if got != tc.wantReason {
				t.Errorf("checkDelegationBinding = %q, want %q", got, tc.wantReason)
			}
		})
	}
}

// TestEngineCheck_DelegationRunMismatch_DeniesBeforeBundle proves the engine
// denies a mismatched binding without any bundle available — the denial can
// never depend on policy data being loaded.
func TestEngineCheck_DelegationRunMismatch_DeniesBeforeBundle(t *testing.T) {
	e := newEngine("http://127.0.0.1:1/bundles", time.Hour, logging.NewLoggerWithOutput(io.Discard))
	const agent = "spiffe://orbit.test/tenant/t1/agent/coder"

	tests := []struct {
		name       string
		claims     *token.PlatformClaims
		runID      string
		wantReason string
	}{
		{"path mode", bindingAgentClaims(agent, ""), "run-b", ReasonDelegationRunMismatch},
		{"sandbox mode", bindingAgentClaims(agent, "run-b"), "", ReasonDelegationRunMismatch},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := e.Check(context.Background(), CheckInput{
				Claims:       tc.claims,
				Delegation:   &DelegationClaims{Sub: "user-1", ActSub: agent, RunID: "run-a"},
				ResourceType: "inference",
				Action:       "invoke",
				RunID:        tc.runID,
			})
			if err != nil {
				t.Fatalf("Check: %v (binding denial must not require a bundle)", err)
			}
			if res.Allow {
				t.Fatal("expected deny")
			}
			if res.Reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", res.Reason, tc.wantReason)
			}
		})
	}
}

// TestBuildOPAInput_DelegatorFields verifies the Layer-2 input carries the
// verified delegator identity AND principal type — the rego delegator-grant
// leg matches grants on both, so omitting the type fails the leg closed
// (delegated-agent-identity ADR, Decision 5).
func TestBuildOPAInput_DelegatorFields(t *testing.T) {
	claims := bindingAgentClaims("spiffe://orbit.test/tenant/t1/agent/coder", "")

	in := buildOPAInput(claims, &DelegationClaims{
		Sub:                    "user-1",
		DelegatorPrincipalType: "HUMAN",
	}, "inference", nil, "invoke", nil)

	if !in.DelegationPresent {
		t.Error("DelegationPresent must be true")
	}
	if in.DelegatorID != "user-1" {
		t.Errorf("DelegatorID = %q, want user-1", in.DelegatorID)
	}
	if in.DelegatorPrincipalType != "HUMAN" {
		t.Errorf("DelegatorPrincipalType = %q, want HUMAN", in.DelegatorPrincipalType)
	}

	// No delegation → no delegator fields.
	plain := buildOPAInput(claims, nil, "inference", nil, "invoke", nil)
	if plain.DelegationPresent || plain.DelegatorID != "" || plain.DelegatorPrincipalType != "" {
		t.Errorf("non-delegated input must carry no delegator fields, got %+v", plain)
	}
}

// TestEngineCheck_ActSubMismatch_DeniesBeforeBundle pins the pre-existing
// act.sub ↔ sub rejection to the same before-bundle position.
func TestEngineCheck_ActSubMismatch_DeniesBeforeBundle(t *testing.T) {
	e := newEngine("http://127.0.0.1:1/bundles", time.Hour, logging.NewLoggerWithOutput(io.Discard))
	res, err := e.Check(context.Background(), CheckInput{
		Claims:       bindingAgentClaims("spiffe://orbit.test/tenant/t1/agent/coder", ""),
		Delegation:   &DelegationClaims{Sub: "user-1", ActSub: "spiffe://orbit.test/tenant/t1/agent/other"},
		ResourceType: "inference",
		Action:       "invoke",
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if res.Allow || res.Reason != ReasonDelegationActSubMismatch {
		t.Errorf("got (%v, %q), want deny with %q", res.Allow, res.Reason, ReasonDelegationActSubMismatch)
	}
}
