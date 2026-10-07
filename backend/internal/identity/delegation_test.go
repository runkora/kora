package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"

	"github.com/runkora/kora/backend/internal/platform/logging"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// ── test fixtures ─────────────────────────────────────────────────────────────

const (
	testAgentSpiffeID   = "spiffe://orbit.test/tenant/t1/agent/reviewer"
	testOtherAgentID    = "spiffe://orbit.test/tenant/t1/agent/deployer"
	testSandboxSpiffeID = testAgentSpiffeID + "/sandbox/run-1"
)

// ── mockDelegationRepository ──────────────────────────────────────────────────

type mockDelegationRepository struct {
	mu        sync.Mutex
	byJTI     map[string]*DelegationIssuance
	createErr error
}

func newMockDelegationRepo() *mockDelegationRepository {
	return &mockDelegationRepository{byJTI: make(map[string]*DelegationIssuance)}
}

func (m *mockDelegationRepository) CreateIssuance(_ context.Context, d *DelegationIssuance) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.byJTI {
		if existing.RunID == d.RunID && existing.RevokedAt == nil {
			return ErrDelegationRunActive
		}
	}
	cp := *d
	m.byJTI[d.JTI] = &cp
	return nil
}

func (m *mockDelegationRepository) GetActiveByRunID(_ context.Context, runID string) (*DelegationIssuance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range m.byJTI {
		if d.RunID == runID && d.RevokedAt == nil {
			cp := *d
			return &cp, nil
		}
	}
	return nil, ErrDelegationNotFound
}

func (m *mockDelegationRepository) GetByJTI(_ context.Context, jti string) (*DelegationIssuance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d, ok := m.byJTI[jti]; ok {
		cp := *d
		return &cp, nil
	}
	return nil, ErrDelegationNotFound
}

func (m *mockDelegationRepository) Revoke(_ context.Context, jti string, revokedAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.byJTI[jti]
	if !ok || d.RevokedAt != nil {
		return ErrDelegationNotFound
	}
	d.RevokedAt = &revokedAt
	return nil
}

func (m *mockDelegationRepository) RevokeByPAT(_ context.Context, patID string, revokedAt time.Time) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var jtis []string
	for _, d := range m.byJTI {
		if d.DelegatorPATID != nil && *d.DelegatorPATID == patID && d.RevokedAt == nil {
			d.RevokedAt = &revokedAt
			jtis = append(jtis, d.JTI)
		}
	}
	return jtis, nil
}

func (m *mockDelegationRepository) ListActiveByDelegator(_ context.Context, tenantID, delegatorPrincipalID string) ([]DelegationIssuance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []DelegationIssuance
	for _, d := range m.byJTI {
		if d.TenantID == tenantID && d.DelegatorPrincipalID == delegatorPrincipalID && d.RevokedAt == nil {
			out = append(out, *d)
		}
	}
	return out, nil
}

// ── construction helpers ──────────────────────────────────────────────────────

// newDelegationTestService builds a *Service wired with a mock delegation
// repository and blocklist, mirroring newTestService.
func newDelegationTestService(t *testing.T) (*Service, *mockDelegationRepository, *mockJTIBlocklistRepository) {
	t.Helper()
	t.Setenv("ORBIT_ALLOW_EPHEMERAL_KEY", "true")
	t.Setenv("ORBIT_TOKEN_SIGNING_KEY_FILE", "")
	t.Setenv("ORBIT_TOKEN_SIGNING_KEY", "")

	cfg := &Config{
		Token: TokenConfig{
			AccessTokenTTLMinutes:     60,
			RefreshTokenTTLDays:       30,
			PATDefaultTTLDays:         90,
			PATMaxTTLDays:             365,
			Audience:                  "orbit-platform",
			DelegationTokenTTLMinutes: 30,
		},
	}
	delegations := newMockDelegationRepo()
	blocklist := newMockBlocklistRepo()
	svc, err := NewService(
		context.Background(),
		cfg,
		newMockSessionRepo(),
		newMockRefreshTokenRepo(),
		blocklist,
		newMockAuthCodeRepo(),
		newMockClientRepo(),
		newMockPATRepo(),
		nil,
		nil,
		delegations,
		&mockPolicyClient{},
		&mockPolicyEvaluator{},
		&XFCCPrincipalExtractor{},
		logging.NewLoggerWithOutput(io.Discard),
		nil,
	)
	if err != nil {
		t.Fatalf("newDelegationTestService: %v", err)
	}
	return svc, delegations, blocklist
}

// newDelegationTestServiceWithPAT wires both the delegation repo and the PAT
// repo so the PAT-revocation cascade can be exercised end to end.
func newDelegationTestServiceWithPAT(t *testing.T) (*Service, *mockDelegationRepository, *mockJTIBlocklistRepository, *mockPATRepository) {
	t.Helper()
	t.Setenv("ORBIT_ALLOW_EPHEMERAL_KEY", "true")
	t.Setenv("ORBIT_TOKEN_SIGNING_KEY_FILE", "")
	t.Setenv("ORBIT_TOKEN_SIGNING_KEY", "")

	cfg := &Config{
		Token: TokenConfig{
			AccessTokenTTLMinutes:     60,
			RefreshTokenTTLDays:       30,
			PATDefaultTTLDays:         90,
			PATMaxTTLDays:             365,
			Audience:                  "orbit-platform",
			DelegationTokenTTLMinutes: 30,
		},
	}
	delegations := newMockDelegationRepo()
	blocklist := newMockBlocklistRepo()
	pats := newMockPATRepo()
	svc, err := NewService(
		context.Background(),
		cfg,
		newMockSessionRepo(),
		newMockRefreshTokenRepo(),
		blocklist,
		newMockAuthCodeRepo(),
		newMockClientRepo(),
		pats,
		nil,
		nil,
		delegations,
		&mockPolicyClient{},
		&mockPolicyEvaluator{},
		&XFCCPrincipalExtractor{},
		logging.NewLoggerWithOutput(io.Discard),
		nil,
	)
	if err != nil {
		t.Fatalf("newDelegationTestServiceWithPAT: %v", err)
	}
	return svc, delegations, blocklist, pats
}

// TestRevokePAT_CascadesToMintedDelegations proves that revoking a PAT
// tombstones and blocklists the delegations it minted, and leaves delegations
// minted by other credentials untouched (ADR Decision 3).
func TestRevokePAT_CascadesToMintedDelegations(t *testing.T) {
	svc, delegations, blocklist, pats := newDelegationTestServiceWithPAT(t)
	ctx := context.Background()
	now := time.Now().UTC()

	patID := "pat-cascade"
	pats.mu.Lock()
	pats.pats[patID] = &PAT{
		ID: patID, TenantID: "t1", UserID: "user-1", Name: "cascade-pat",
		JTI: "pat-jti", ExpiresAt: now.Add(24 * time.Hour), CreatedAt: now,
	}
	pats.mu.Unlock()

	// One delegation minted by this PAT, one minted by another credential.
	patLineage := patID
	mine := &DelegationIssuance{
		JTI: "deleg-mine", RunID: "run-a", TenantID: "t1",
		DelegatorPrincipalID: "user-1", DelegatorPrincipalType: token.PrincipalTypeHuman,
		DelegatorPATID: &patLineage, AgentPrincipalID: testAgentSpiffeID, CreatedAt: now,
	}
	other := &DelegationIssuance{
		JTI: "deleg-other", RunID: "run-b", TenantID: "t1",
		DelegatorPrincipalID: "user-1", DelegatorPrincipalType: token.PrincipalTypeHuman,
		AgentPrincipalID: testAgentSpiffeID, CreatedAt: now,
	}
	if err := delegations.CreateIssuance(ctx, mine); err != nil {
		t.Fatalf("seed mine: %v", err)
	}
	if err := delegations.CreateIssuance(ctx, other); err != nil {
		t.Fatalf("seed other: %v", err)
	}

	if err := svc.RevokePAT(ctx, "t1", "user-1", patID); err != nil {
		t.Fatalf("RevokePAT: %v", err)
	}

	// The PAT-minted delegation is tombstoned and its JTI blocklisted.
	if got, _ := delegations.GetByJTI(ctx, "deleg-mine"); got.RevokedAt == nil {
		t.Error("expected PAT-minted delegation to be tombstoned")
	}
	if blocked, _ := blocklist.IsBlocked(ctx, "deleg-mine"); !blocked {
		t.Error("expected PAT-minted delegation JTI to be blocklisted")
	}
	// The delegation minted by another credential is untouched.
	if got, _ := delegations.GetByJTI(ctx, "deleg-other"); got.RevokedAt != nil {
		t.Error("expected non-PAT delegation to survive PAT revocation")
	}
	if blocked, _ := blocklist.IsBlocked(ctx, "deleg-other"); blocked {
		t.Error("non-PAT delegation JTI must not be blocklisted")
	}
}

func newDelegationTestController(t *testing.T) (*Controller, *Service, *mockDelegationRepository, *mockJTIBlocklistRepository) {
	t.Helper()
	svc, delegations, blocklist := newDelegationTestService(t)
	c := NewController(svc, nil, nil, "", logging.NewLoggerWithOutput(io.Discard))
	return c, svc, delegations, blocklist
}

// serviceClaims returns PlatformClaims for a SERVICE principal.
func serviceClaims(tenantID, principalID string) *token.PlatformClaims {
	c := humanClaims(tenantID, principalID)
	c.Kora.PrincipalType = token.PrincipalTypeService
	return c
}

// agentClaims returns PlatformClaims for an AGENT principal.
func agentClaims(tenantID, principalID string) *token.PlatformClaims {
	c := humanClaims(tenantID, principalID)
	c.Kora.PrincipalType = token.PrincipalTypeAgent
	return c
}

// systemClaims returns PlatformClaims for a SYSTEM principal (no tenant).
func systemClaims(principalID string) *token.PlatformClaims {
	c := humanClaims("", principalID)
	c.Kora.PrincipalType = token.PrincipalTypeSystem
	return c
}

// mintTestDelegation mints an issuance as user for run and returns it.
func mintTestDelegation(t *testing.T, svc *Service, claims *token.PlatformClaims, runID string) *DelegationIssuance {
	t.Helper()
	issuance, err := svc.MintDelegation(context.Background(), claims, CreateDelegationRequest{
		AgentPrincipalID: testAgentSpiffeID,
		RunID:            runID,
	})
	if err != nil {
		t.Fatalf("MintDelegation: %v", err)
	}
	return issuance
}

// getDelegationForRun invokes the run-retrieval handler with an XFCC header.
func getDelegationForRun(t *testing.T, c *Controller, runID, xfccURI string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/delegation/run/"+runID, nil)
	req.SetPathValue("run_id", runID)
	if xfccURI != "" {
		req.Header.Set("x-forwarded-client-cert", "Hash=abc;URI="+xfccURI)
	}
	w := httptest.NewRecorder()
	c.handleGetDelegationForRun(w, req)
	return w
}

// deleteDelegation invokes the revoke handler with claims in context.
func deleteDelegation(t *testing.T, c *Controller, claims *token.PlatformClaims, jti string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/delegation/"+jti, nil)
	req.SetPathValue("jti", jti)
	req = req.WithContext(token.WithClaims(req.Context(), claims))
	w := httptest.NewRecorder()
	c.handleRevokeDelegation(w, req)
	return w
}

// newDelegationVerifier serves the service's JWKS over httptest and returns a
// *token.Verifier configured exactly like a consuming service's.
func newDelegationVerifier(t *testing.T, svc *Service) *token.Verifier {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/keys", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(svc.JWKSJSON())
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return token.NewVerifier(context.Background(), srv.URL+"/keys", token.DefaultIssuer, "orbit-platform")
}

// ── mint ──────────────────────────────────────────────────────────────────────

func TestHandleCreateDelegation_RefusesNonPersonPrincipals(t *testing.T) {
	tests := []struct {
		name   string
		claims *token.PlatformClaims
	}{
		{"agent", agentClaims("t1", testAgentSpiffeID)},
		{"service", serviceClaims("t1", "webhook-source-1")},
		{"system", systemClaims("orbit-identity-service")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, _, delegations, _ := newDelegationTestController(t)

			w := postJSONWithClaims(t, c.handleCreateDelegation, tc.claims, CreateDelegationRequest{
				AgentPrincipalID: testAgentSpiffeID,
				RunID:            "run-1",
			})

			if w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403; body: %s", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), "HUMAN or PAT") {
				t.Errorf("error message should name the HUMAN/PAT constraint; got: %s", w.Body.String())
			}
			if len(delegations.byJTI) != 0 {
				t.Errorf("no issuance should be persisted, found %d", len(delegations.byJTI))
			}
		})
	}
}

func TestHandleCreateDelegation_PersistsRecordAndReturnsNoToken(t *testing.T) {
	tests := []struct {
		name      string
		claims    *token.PlatformClaims
		wantType  token.PrincipalType
		wantPATID string
	}{
		{"human", humanClaims("t1", "user-1"), token.PrincipalTypeHuman, ""},
		// The delegator is the PERSON, not the PAT credential: a PAT-minted
		// delegation normalises to the issuing user's subject with type HUMAN
		// (so the rego delegator-grant lookup, keyed HUMAN grants by sub, can
		// match), while the PAT lineage is preserved for audit/revocation.
		{"pat normalises to human person and records lineage", func() *token.PlatformClaims {
			c := patTokenClaims("t1", "user-1")
			c.Kora.PatID = "pat-uuid-1"
			return c
		}(), token.PrincipalTypeHuman, "pat-uuid-1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, _, delegations, _ := newDelegationTestController(t)

			w := postJSONWithClaims(t, c.handleCreateDelegation, tc.claims, CreateDelegationRequest{
				AgentPrincipalID: testAgentSpiffeID,
				RunID:            "run-1",
			})

			if w.Code != http.StatusCreated {
				t.Fatalf("status = %d, want 201; body: %s", w.Code, w.Body.String())
			}

			// The response must carry the record, never a token.
			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("unmarshal response: %v", err)
			}
			for key := range body {
				if strings.Contains(key, "token") {
					t.Errorf("mint response must not carry a token field, found %q", key)
				}
			}
			if body["run_id"] != "run-1" || body["agent_principal_id"] != testAgentSpiffeID {
				t.Errorf("unexpected record body: %v", body)
			}
			// Tenant comes from claims, never the body.
			if body["tenant_id"] != "t1" {
				t.Errorf("tenant_id = %v, want t1 (from verified claims)", body["tenant_id"])
			}

			jti, _ := body["jti"].(string)
			stored, err := delegations.GetByJTI(context.Background(), jti)
			if err != nil {
				t.Fatalf("issuance not persisted: %v", err)
			}
			if stored.DelegatorPrincipalID != "user-1" || stored.DelegatorPrincipalType != tc.wantType {
				t.Errorf("delegator = %s/%s, want user-1/%s", stored.DelegatorPrincipalID, stored.DelegatorPrincipalType, tc.wantType)
			}
			if tc.wantPATID == "" && stored.DelegatorPATID != nil {
				t.Errorf("DelegatorPATID = %v, want nil", *stored.DelegatorPATID)
			}
			if tc.wantPATID != "" && (stored.DelegatorPATID == nil || *stored.DelegatorPATID != tc.wantPATID) {
				t.Errorf("DelegatorPATID = %v, want %q", stored.DelegatorPATID, tc.wantPATID)
			}
		})
	}
}

func TestHandleCreateDelegation_DuplicateActiveRunConflicts(t *testing.T) {
	c, svc, _, _ := newDelegationTestController(t)
	mintTestDelegation(t, svc, humanClaims("t1", "user-1"), "run-1")

	w := postJSONWithClaims(t, c.handleCreateDelegation, humanClaims("t1", "user-2"), CreateDelegationRequest{
		AgentPrincipalID: testAgentSpiffeID,
		RunID:            "run-1",
	})

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleCreateDelegation_ValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		req  CreateDelegationRequest
	}{
		{"missing run_id", CreateDelegationRequest{AgentPrincipalID: testAgentSpiffeID}},
		{"missing agent_principal_id", CreateDelegationRequest{RunID: "run-1"}},
		{"non-spiffe agent id", CreateDelegationRequest{AgentPrincipalID: "reviewer", RunID: "run-1"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, _, _, _ := newDelegationTestController(t)
			w := postJSONWithClaims(t, c.handleCreateDelegation, humanClaims("t1", "user-1"), tc.req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
			}
		})
	}
}

// ── run retrieval ─────────────────────────────────────────────────────────────

func TestHandleGetDelegationForRun_ReleasesVerifiableToken(t *testing.T) {
	tests := []struct {
		name    string
		xfccURI string
	}{
		{"shared pod agent SVID", testAgentSpiffeID},
		// Per-run sandbox SVIDs normalise to the agent principal in
		// principalFromSpiffeID, so the sandbox identity-proxy also qualifies.
		{"per-run sandbox SVID", testSandboxSpiffeID},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, svc, _, _ := newDelegationTestController(t)
			issuance := mintTestDelegation(t, svc, humanClaims("t1", "user-1"), "run-1")

			w := getDelegationForRun(t, c, "run-1", tc.xfccURI)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
			}

			var resp DelegationTokenResponse
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("unmarshal response: %v", err)
			}
			if resp.DelegationToken == "" {
				t.Fatal("response missing delegation_token")
			}
			if resp.RunID != "run-1" {
				t.Errorf("run_id = %q, want run-1", resp.RunID)
			}

			// The token must verify with the EXISTING consumer stack.
			verifier := newDelegationVerifier(t, svc)
			claims, err := policy.ParseDelegationToken(context.Background(), resp.DelegationToken, verifier)
			if err != nil {
				t.Fatalf("ParseDelegationToken rejected the minted token: %v", err)
			}
			if claims.Sub != "user-1" {
				t.Errorf("sub = %q, want user-1", claims.Sub)
			}
			if claims.ActSub != testAgentSpiffeID {
				t.Errorf("act.sub = %q, want %q", claims.ActSub, testAgentSpiffeID)
			}
			if claims.RunID != "run-1" {
				t.Errorf("run_id = %q, want run-1", claims.RunID)
			}
			if claims.DelegatorPrincipalType != string(token.PrincipalTypeHuman) {
				t.Errorf("delegator_principal_type = %q, want HUMAN", claims.DelegatorPrincipalType)
			}
			if claims.TenantID != "t1" {
				t.Errorf("tenant_id = %q, want t1", claims.TenantID)
			}
			if claims.JTI != issuance.JTI {
				t.Errorf("jti = %q, want issuance jti %q", claims.JTI, issuance.JTI)
			}
			// NO scope claim, ever (ADR Decision 3).
			if len(claims.Scopes) != 0 {
				t.Errorf("delegation token must carry no scopes, got %v", claims.Scopes)
			}
		})
	}
}

// A PAT-minted delegation releases a token naming the PERSON as the delegator:
// sub = the issuing user's OIDC subject and delegator_principal_type = HUMAN —
// the same shape a session-minted delegation produces, so the rego delegator
// leg (keyed HUMAN grants by sub) evaluates identically for both.
func TestHandleGetDelegationForRun_PATMintedTokenNamesThePerson(t *testing.T) {
	c, svc, _, _ := newDelegationTestController(t)
	patClaims := patTokenClaims("t1", "user-1")
	patClaims.Kora.PatID = "pat-uuid-1"
	mintTestDelegation(t, svc, patClaims, "run-1")

	w := getDelegationForRun(t, c, "run-1", testAgentSpiffeID)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp DelegationTokenResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	verifier := newDelegationVerifier(t, svc)
	claims, err := policy.ParseDelegationToken(context.Background(), resp.DelegationToken, verifier)
	if err != nil {
		t.Fatalf("ParseDelegationToken: %v", err)
	}
	if claims.Sub != "user-1" {
		t.Errorf("sub = %q, want the person user-1, never the PAT id", claims.Sub)
	}
	if claims.DelegatorPrincipalType != string(token.PrincipalTypeHuman) {
		t.Errorf("delegator_principal_type = %q, want HUMAN for a PAT-minted delegation", claims.DelegatorPrincipalType)
	}
}

func TestHandleGetDelegationForRun_RefusesDifferentAgent(t *testing.T) {
	c, svc, _, _ := newDelegationTestController(t)
	mintTestDelegation(t, svc, humanClaims("t1", "user-1"), "run-1")

	w := getDelegationForRun(t, c, "run-1", testOtherAgentID)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleGetDelegationForRun_NoIssuance404WithDistinctCode(t *testing.T) {
	c, _, _, _ := newDelegationTestController(t)

	w := getDelegationForRun(t, c, "run-unknown", testAgentSpiffeID)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
	// The identity-proxy keys "autonomous, none" off this code.
	if !strings.Contains(w.Body.String(), errCodeDelegationNotFound) {
		t.Errorf("body should carry code %q; got: %s", errCodeDelegationNotFound, w.Body.String())
	}
}

func TestHandleGetDelegationForRun_Tombstoned404(t *testing.T) {
	c, svc, _, _ := newDelegationTestController(t)
	issuance := mintTestDelegation(t, svc, humanClaims("t1", "user-1"), "run-1")

	if w := deleteDelegation(t, c, humanClaims("t1", "user-1"), issuance.JTI); w.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d, want 204; body: %s", w.Code, w.Body.String())
	}

	w := getDelegationForRun(t, c, "run-1", testAgentSpiffeID)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 after tombstone; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleGetDelegationForRun_MissingXFCC401(t *testing.T) {
	c, svc, _, _ := newDelegationTestController(t)
	mintTestDelegation(t, svc, humanClaims("t1", "user-1"), "run-1")

	w := getDelegationForRun(t, c, "run-1", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body: %s", w.Code, w.Body.String())
	}
}

func TestDelegationTokenForRun_ResignKeepsJTIStableWithFreshExp(t *testing.T) {
	c, svc, _, _ := newDelegationTestController(t)
	issuance := mintTestDelegation(t, svc, humanClaims("t1", "user-1"), "run-1")
	ttl := svc.delegationTokenTTL()

	parseUnverified := func(raw string) (jti string, exp time.Time) {
		t.Helper()
		var claims jwt.RegisteredClaims
		if _, _, err := jwt.NewParser().ParseUnverified(raw, &claims); err != nil {
			t.Fatalf("parse token: %v", err)
		}
		return claims.ID, claims.ExpiresAt.Time
	}

	first := getDelegationForRun(t, c, "run-1", testAgentSpiffeID)
	second := getDelegationForRun(t, c, "run-1", testAgentSpiffeID)
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("retrievals = %d, %d, want 200, 200", first.Code, second.Code)
	}

	var resp1, resp2 DelegationTokenResponse
	if err := json.Unmarshal(first.Body.Bytes(), &resp1); err != nil {
		t.Fatalf("unmarshal first: %v", err)
	}
	if err := json.Unmarshal(second.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("unmarshal second: %v", err)
	}

	jti1, exp1 := parseUnverified(resp1.DelegationToken)
	jti2, exp2 := parseUnverified(resp2.DelegationToken)

	// The JTI identifies the delegation for revocation — STABLE across re-signs.
	if jti1 != issuance.JTI || jti2 != issuance.JTI {
		t.Errorf("jti drifted across re-signs: %q, %q, want %q", jti1, jti2, issuance.JTI)
	}
	// exp is freshly computed on every re-sign.
	for i, exp := range []time.Time{exp1, exp2} {
		drift := time.Until(exp) - ttl
		if drift < -10*time.Second || drift > 10*time.Second {
			t.Errorf("retrieval %d: exp %v not ≈ now+%v", i+1, exp, ttl)
		}
	}
	if exp2.Before(exp1) {
		t.Errorf("second exp %v before first %v — exp must never regress", exp2, exp1)
	}
}

// ── revoke ────────────────────────────────────────────────────────────────────

func TestHandleRevokeDelegation_Authorization(t *testing.T) {
	tests := []struct {
		name       string
		claims     *token.PlatformClaims
		wantStatus int
	}{
		{"delegator revokes own", humanClaims("t1", "user-1"), http.StatusNoContent},
		{"another human refused", humanClaims("t1", "user-2"), http.StatusForbidden},
		{"same user other tenant refused", humanClaims("t2", "user-1"), http.StatusForbidden},
		{"agent refused", agentClaims("t1", testAgentSpiffeID), http.StatusForbidden},
		{"service allowed", serviceClaims("t1", "orbit-agents"), http.StatusNoContent},
		{"system allowed", systemClaims("orbit-identity-service"), http.StatusNoContent},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, svc, delegations, blocklist := newDelegationTestController(t)
			issuance := mintTestDelegation(t, svc, humanClaims("t1", "user-1"), "run-1")

			w := deleteDelegation(t, c, tc.claims, issuance.JTI)
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", w.Code, tc.wantStatus, w.Body.String())
			}

			stored, err := delegations.GetByJTI(context.Background(), issuance.JTI)
			if err != nil {
				t.Fatalf("GetByJTI: %v", err)
			}
			blocked, _ := blocklist.IsBlocked(context.Background(), issuance.JTI)

			if tc.wantStatus == http.StatusNoContent {
				if stored.RevokedAt == nil {
					t.Error("issuance should be tombstoned")
				}
				if !blocked {
					t.Error("jti should be blocklisted")
				}
			} else {
				if stored.RevokedAt != nil {
					t.Error("issuance should NOT be tombstoned on refused revoke")
				}
				if blocked {
					t.Error("jti should NOT be blocklisted on refused revoke")
				}
			}
		})
	}
}

func TestHandleRevokeDelegation_UnknownJTI404(t *testing.T) {
	c, _, _, _ := newDelegationTestController(t)
	w := deleteDelegation(t, c, humanClaims("t1", "user-1"), "no-such-jti")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}

// ── list ──────────────────────────────────────────────────────────────────────

func TestHandleListDelegations_DelegatorViewActiveOnly(t *testing.T) {
	c, svc, _, _ := newDelegationTestController(t)
	mine := mintTestDelegation(t, svc, humanClaims("t1", "user-1"), "run-1")
	revoked := mintTestDelegation(t, svc, humanClaims("t1", "user-1"), "run-2")
	mintTestDelegation(t, svc, humanClaims("t1", "user-2"), "run-3") // someone else's
	if w := deleteDelegation(t, c, humanClaims("t1", "user-1"), revoked.JTI); w.Code != http.StatusNoContent {
		t.Fatalf("revoke: %d", w.Code)
	}

	w := getWithClaims(t, c.handleListDelegations, humanClaims("t1", "user-1"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp ListDelegationsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Delegations) != 1 || resp.Delegations[0].JTI != mine.JTI {
		t.Errorf("delegations = %+v, want exactly the caller's one active issuance", resp.Delegations)
	}
}

// ── revoked JTI rejected on the consumer path ─────────────────────────────────

func TestDelegationMiddleware_RejectsRevokedJTI(t *testing.T) {
	svc, _, _ := newDelegationTestService(t)
	issuance := mintTestDelegation(t, svc, humanClaims("t1", "user-1"), "run-1")
	raw, err := svc.signDelegationToken(issuance, time.Now().UTC(), time.Hour)
	if err != nil {
		t.Fatalf("signDelegationToken: %v", err)
	}
	verifier := newDelegationVerifier(t, svc)

	tests := []struct {
		name        string
		blocked     bool
		wantStatus  int
		wantContext bool
	}{
		{"active delegation passes with claims in context", false, http.StatusOK, true},
		{"revoked delegation rejected", true, http.StatusUnauthorized, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			blocklist := newMockBlocklistRepo()
			if tc.blocked {
				if err := blocklist.BlockJTI(context.Background(), issuance.JTI, time.Hour); err != nil {
					t.Fatalf("BlockJTI: %v", err)
				}
			}

			var sawDelegation *policy.DelegationClaims
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sawDelegation = policy.DelegationFromContext(r.Context())
				w.WriteHeader(http.StatusOK)
			})
			handler := mw.MiddlewareChain(
				mw.DelegationMiddlewareWithBlocklist(logging.NewLoggerWithOutput(io.Discard), verifier, blocklist),
			)(next)

			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(nil))
			req.Header.Set("X-Orbit-Delegation-Token", raw)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", w.Code, tc.wantStatus, w.Body.String())
			}
			if tc.wantContext && (sawDelegation == nil || sawDelegation.JTI != issuance.JTI) {
				t.Errorf("delegation claims missing from context: %+v", sawDelegation)
			}
			if !tc.wantContext && sawDelegation != nil {
				t.Errorf("handler must not run for a revoked delegation")
			}
		})
	}
}
