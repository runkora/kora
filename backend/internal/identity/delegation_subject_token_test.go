package identity

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/token"
)

// The RFC 8693 subject_token path: a platform service (orbit-agents) brokers
// the mint because its identity-proxy overwrites the Authorization header on
// egress — the runner's own platform token travels in the body and the
// delegator is derived from IT after full verification (ADR Decision 3).

func signSubjectToken(t *testing.T, svc *Service, p *Principal, jti string) string {
	t.Helper()
	tok, err := svc.signToken(p, jti, "sess-subject", "", time.Now(), time.Hour)
	if err != nil {
		t.Fatalf("signToken: %v", err)
	}
	return tok
}

func TestHandleCreateDelegation_SubjectTokenBrokeredMint(t *testing.T) {
	c, svc, delegations, _ := newDelegationTestController(t)

	subject := signSubjectToken(t, svc, &Principal{
		ID:             "user-runner",
		Type:           PrincipalTypeHuman,
		TenantID:       "t1",
		IdentitySource: IdentitySourceFederated,
	}, "subject-jti-1")

	// Transport principal is the SYSTEM service token the identity-proxy
	// stamps on orbit-agents egress.
	w := postJSONWithClaims(t, c.handleCreateDelegation, systemClaims("orbit-agents"), CreateDelegationRequest{
		AgentPrincipalID: testAgentSpiffeID,
		RunID:            "run-subject-1",
		SubjectToken:     subject,
	})

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	if len(delegations.byJTI) != 1 {
		t.Fatalf("expected 1 issuance, got %d", len(delegations.byJTI))
	}
	for _, iss := range delegations.byJTI {
		if iss.DelegatorPrincipalID != "user-runner" {
			t.Errorf("delegator = %q, want the subject token's person %q", iss.DelegatorPrincipalID, "user-runner")
		}
		if iss.DelegatorPrincipalType != token.PrincipalTypeHuman {
			t.Errorf("delegator type = %q, want HUMAN", iss.DelegatorPrincipalType)
		}
		if iss.TenantID != "t1" {
			t.Errorf("tenant = %q, want the subject token's tenant t1", iss.TenantID)
		}
	}
}

func TestHandleCreateDelegation_SubjectTokenRefusals(t *testing.T) {
	c, svc, delegations, blocklist := newDelegationTestController(t)

	agentSubject := signSubjectToken(t, svc, &Principal{
		ID:             testAgentSpiffeID,
		Type:           PrincipalTypeAgent,
		TenantID:       "t1",
		IdentitySource: IdentitySourceAttested,
	}, "subject-jti-agent")
	blockedSubject := signSubjectToken(t, svc, &Principal{
		ID:             "user-blocked",
		Type:           PrincipalTypeHuman,
		TenantID:       "t1",
		IdentitySource: IdentitySourceFederated,
	}, "subject-jti-blocked")
	if err := blocklist.BlockJTI(context.Background(), "subject-jti-blocked", time.Hour); err != nil {
		t.Fatalf("BlockJTI: %v", err)
	}

	tests := []struct {
		name         string
		subjectToken string
		wantStatus   int
		wantBody     string
	}{
		{"garbage subject token", "not-a-jwt", http.StatusUnauthorized, "subject_token"},
		{"agent-typed subject token", agentSubject, http.StatusForbidden, "HUMAN or PAT"},
		{"revoked subject token", blockedSubject, http.StatusUnauthorized, "subject_token"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := postJSONWithClaims(t, c.handleCreateDelegation, systemClaims("orbit-agents"), CreateDelegationRequest{
				AgentPrincipalID: testAgentSpiffeID,
				RunID:            "run-" + strings.ReplaceAll(tc.name, " ", "-"),
				SubjectToken:     tc.subjectToken,
			})
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", w.Code, tc.wantStatus, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), tc.wantBody) {
				t.Errorf("body should mention %q; got: %s", tc.wantBody, w.Body.String())
			}
		})
	}
	if len(delegations.byJTI) != 0 {
		t.Errorf("no issuance should be persisted, found %d", len(delegations.byJTI))
	}
}

// A blocklist backend error on the subject-token path fails open: the mint goes
// ahead, as every other blocklist lookup does while Redis is unavailable.
func TestHandleCreateDelegation_SubjectTokenFailsOpenOnBlocklistError(t *testing.T) {
	c, svc, delegations, blocklist := newDelegationTestController(t)

	subject := signSubjectToken(t, svc, &Principal{
		ID:             "user-runner",
		Type:           PrincipalTypeHuman,
		TenantID:       "t1",
		IdentitySource: IdentitySourceFederated,
	}, "subject-jti-blockerr")
	blocklist.isBlockedErr = errors.New("redis unavailable")

	w := postJSONWithClaims(t, c.handleCreateDelegation, systemClaims("orbit-agents"), CreateDelegationRequest{
		AgentPrincipalID: testAgentSpiffeID,
		RunID:            "run-blockerr",
		SubjectToken:     subject,
	})

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	if len(delegations.byJTI) != 1 {
		t.Errorf("one issuance should be persisted when the blocklist is unavailable, found %d", len(delegations.byJTI))
	}
}

// An AGENT transport can never courier a subject token — a delegated run
// cannot start another delegated agent by replaying its runner's token.
func TestHandleCreateDelegation_AgentTransportRefusedEvenWithSubjectToken(t *testing.T) {
	c, svc, delegations, _ := newDelegationTestController(t)

	subject := signSubjectToken(t, svc, &Principal{
		ID:             "user-runner",
		Type:           PrincipalTypeHuman,
		TenantID:       "t1",
		IdentitySource: IdentitySourceFederated,
	}, "subject-jti-2")

	w := postJSONWithClaims(t, c.handleCreateDelegation, agentClaims("t1", testAgentSpiffeID), CreateDelegationRequest{
		AgentPrincipalID: testAgentSpiffeID,
		RunID:            "run-agent-transport",
		SubjectToken:     subject,
	})

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body: %s", w.Code, w.Body.String())
	}
	if len(delegations.byJTI) != 0 {
		t.Errorf("no issuance should be persisted, found %d", len(delegations.byJTI))
	}
}
