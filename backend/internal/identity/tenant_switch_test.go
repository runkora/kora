package identity

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// switchRegistry holds three bound tenants sharing acme.com and one unbound
// tenant on the same issuer and client.
func switchRegistry(issuer string) string {
	return fmt.Sprintf(`version: "v1"
tenants:
  - id: eng
    displayName: Engineering
    state: active
    oidc: {issuerURL: %[1]q, clientID: test-client, emailDomains: [acme.com], claimBinding: {claim: groups, values: [eng]}}
  - id: sre
    displayName: SRE
    state: active
    oidc: {issuerURL: %[1]q, clientID: test-client, emailDomains: [acme.com], claimBinding: {claim: groups, values: [sre]}}
  - id: ops
    displayName: Ops
    state: active
    oidc: {issuerURL: %[1]q, clientID: test-client, emailDomains: [acme.com], claimBinding: {claim: groups, values: [ops]}}
  - id: plain
    displayName: Plain
    state: active
    oidc: {issuerURL: %[1]q, clientID: test-client, emailDomains: [plain.com]}
`, issuer)
}

var (
	tenantEng   = EligibleTenant{ID: "eng", DisplayName: "Engineering"}
	tenantSRE   = EligibleTenant{ID: "sre", DisplayName: "SRE"}
	tenantPlain = EligibleTenant{ID: "plain", DisplayName: "Plain"}
)

func groups(g ...string) jwt.MapClaims { return jwt.MapClaims{"groups": g} }

func TestExchangeIDToken_TenantHint(t *testing.T) {
	issuer, _, key := newSigningOIDCTestServerWithKey(t)
	none, one, several := groups("sales"), groups("sre"), groups("eng", "sre")
	noEmail := jwt.MapClaims{"email": nil, "groups": []string{"eng", "sre"}}
	tests := []struct {
		name        string
		requested   string
		hint        string
		claims      jwt.MapClaims
		wantErr     error
		wantTenant  string
		wantTenants []EligibleTenant
	}{
		{"no match, no hint", "eng", "", none, ErrClaimBindingMismatch, "", nil},
		{"no match, hint outside", "eng", "sre", none, ErrClaimBindingMismatch, "", nil},
		{"no match, unknown hint", "eng", "nope", none, ErrClaimBindingMismatch, "", nil},
		{"one match, no hint", "eng", "", one, nil, "sre", []EligibleTenant{tenantSRE}},
		{"one match, hint inside", "eng", "sre", one, nil, "sre", []EligibleTenant{tenantSRE}},
		{"one match, hint outside", "eng", "ops", one, nil, "sre", []EligibleTenant{tenantSRE}},
		{"one match, unknown hint", "eng", "nope", one, nil, "sre", []EligibleTenant{tenantSRE}},
		{"several, no hint", "eng", "", several, ErrMultipleTenantsMatch, "", []EligibleTenant{tenantEng, tenantSRE}},
		{"several, hint inside", "eng", "sre", several, nil, "sre", []EligibleTenant{tenantEng, tenantSRE}},
		{"several, hint outside", "eng", "ops", several, ErrMultipleTenantsMatch, "", []EligibleTenant{tenantEng, tenantSRE}},
		{"several, unknown hint", "eng", "nope", several, ErrMultipleTenantsMatch, "", []EligibleTenant{tenantEng, tenantSRE}},
		{"no email claim keeps the requested tenant", "eng", "sre", noEmail, nil, "eng", []EligibleTenant{tenantEng}},
		{"unbound tenant ignores the hint", "plain", "eng", jwt.MapClaims{"email": "alice@plain.com", "groups": []string{"eng"}}, nil, "plain", []EligibleTenant{tenantPlain}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, err := newTestServiceWithRegistry(t, switchRegistry(issuer))
			if err != nil {
				t.Fatalf("NewService: %v", err)
			}
			resp, err := svc.ExchangeIDToken(context.Background(), SessionRequest{
				TenantID:   tc.requested,
				TenantHint: tc.hint,
				IDToken:    issueIDTokenWithClaims(t, issuer, key, tc.claims),
			})
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				var tme *TenantMatchError
				if tc.wantTenants != nil && (!errors.As(err, &tme) || !reflect.DeepEqual(tme.Tenants, tc.wantTenants)) {
					t.Errorf("error tenants = %+v, want %+v", tme, tc.wantTenants)
				}
				return
			}
			if err != nil {
				t.Fatalf("ExchangeIDToken: %v", err)
			}
			if got := sessionTenant(t, resp); got != tc.wantTenant {
				t.Errorf("session tenant = %q, want %q", got, tc.wantTenant)
			}
			if !reflect.DeepEqual(resp.Tenants, tc.wantTenants) {
				t.Errorf("response tenants = %+v, want %+v", resp.Tenants, tc.wantTenants)
			}
		})
	}
}

// eventLog records repository writes and the switch log entry in call order.
type eventLog struct {
	mu     sync.Mutex
	events []string
}

func (l *eventLog) add(e string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, e)
}

func (l *eventLog) take() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := l.events
	l.events = nil
	return out
}

// Write records the switch log entry from the service logger.
func (l *eventLog) Write(p []byte) (int, error) {
	if strings.Contains(string(p), `"tenant switched"`) {
		l.add("log " + string(p))
	}
	return len(p), nil
}

type recordingSessions struct {
	*mockSessionRepository
	log *eventLog
}

func (r recordingSessions) CreateSession(ctx context.Context, s *Session) error {
	r.log.add("create session " + s.TenantID)
	return r.mockSessionRepository.CreateSession(ctx, s)
}

func (r recordingSessions) RevokeSession(ctx context.Context, id string, at time.Time) error {
	r.log.add("revoke session " + id)
	return r.mockSessionRepository.RevokeSession(ctx, id, at)
}

type recordingBlocklist struct {
	*mockJTIBlocklistRepository
	log *eventLog
}

func (r recordingBlocklist) BlockJTI(ctx context.Context, jti string, ttl time.Duration) error {
	r.log.add("block jti " + jti)
	return r.mockJTIBlocklistRepository.BlockJTI(ctx, jti, ttl)
}

type recordingRefreshTokens struct {
	*mockRefreshTokenRepository
	log *eventLog
}

func (r recordingRefreshTokens) CreateRefreshToken(ctx context.Context, rt *RefreshToken) error {
	r.log.add("create refresh token")
	return r.mockRefreshTokenRepository.CreateRefreshToken(ctx, rt)
}

func (r recordingRefreshTokens) RevokeRefreshToken(ctx context.Context, id string, at time.Time) error {
	r.log.add("revoke refresh token " + id)
	return r.mockRefreshTokenRepository.RevokeRefreshToken(ctx, id, at)
}

func (r recordingRefreshTokens) RevokeAllForSession(ctx context.Context, sessionID string, at time.Time) error {
	r.log.add("revoke refresh tokens " + sessionID)
	return r.mockRefreshTokenRepository.RevokeAllForSession(ctx, sessionID, at)
}

type switchFixture struct {
	svc      *Service
	sessions *mockSessionRepository
	refresh  *mockRefreshTokenRepository
	log      *eventLog
	issuer   string
	key      *rsa.PrivateKey
}

func newSwitchFixture(t *testing.T) *switchFixture {
	t.Helper()
	issuer, _, key := newSigningOIDCTestServerWithKey(t)
	f := &switchFixture{sessions: newMockSessionRepo(), refresh: newMockRefreshTokenRepo(), log: &eventLog{}, issuer: issuer, key: key}
	svc, err := newTestServiceWithRegistryRepos(t, switchRegistry(issuer),
		recordingSessions{f.sessions, f.log}, recordingRefreshTokens{f.refresh, f.log}, f.log)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	svc.blocklist = recordingBlocklist{newMockBlocklistRepo(), f.log}
	f.svc = svc
	return f
}

// signIn creates a session in eng for sub and returns it, clearing the event log.
func (f *switchFixture) signIn(t *testing.T, sub string) *SessionResponse {
	t.Helper()
	resp, err := f.svc.ExchangeIDToken(context.Background(), SessionRequest{
		TenantID:   "eng",
		TenantHint: "eng",
		IDToken:    issueIDTokenWithClaims(t, f.issuer, f.key, jwt.MapClaims{"sub": sub, "groups": []string{"eng", "sre"}}),
	})
	if err != nil {
		t.Fatalf("sign in: %v", err)
	}
	f.log.take()
	return resp
}

func (f *switchFixture) switchTo(t *testing.T, hint, replaces string, claims jwt.MapClaims) (*SessionResponse, error) {
	t.Helper()
	return f.svc.ExchangeIDToken(context.Background(), SessionRequest{
		TenantID:             "eng",
		TenantHint:           hint,
		ReplacesRefreshToken: replaces,
		IDToken:              issueIDTokenWithClaims(t, f.issuer, f.key, claims),
	})
}

func (f *switchFixture) refreshRow(t *testing.T, raw string) *RefreshToken {
	t.Helper()
	rt, err := f.refresh.GetRefreshTokenByHash(context.Background(), hashToken(raw))
	if err != nil {
		t.Fatalf("refresh token row: %v", err)
	}
	return rt
}

func TestExchangeIDToken_SwitchSucceeds(t *testing.T) {
	f := newSwitchFixture(t)
	old := f.signIn(t, "user-1")
	oldRT := f.refreshRow(t, old.RefreshToken)

	resp, err := f.switchTo(t, "sre", old.RefreshToken, jwt.MapClaims{"groups": []string{"eng", "sre"}})
	if err != nil {
		t.Fatalf("switch: %v", err)
	}
	if got := sessionTenant(t, resp); got != "sre" {
		t.Errorf("session tenant = %q, want sre", got)
	}
	if want := []EligibleTenant{tenantEng, tenantSRE}; !reflect.DeepEqual(resp.Tenants, want) {
		t.Errorf("response tenants = %+v, want %+v", resp.Tenants, want)
	}

	events := f.log.take()
	oldSession, _ := f.sessions.GetSessionByID(context.Background(), oldRT.SessionID)
	wantPrefix := []string{
		"create session sre",
		"create refresh token",
		"block jti " + oldSession.JTI,
		"revoke refresh tokens " + oldRT.SessionID,
		"revoke session " + oldRT.SessionID,
	}
	if len(events) != 6 || !reflect.DeepEqual(events[:5], wantPrefix) {
		t.Fatalf("events = %q, want %q then the switch log entry", events, wantPrefix)
	}
	for _, field := range []string{`"principal_id":"user-1"`, `"from_tenant_id":"eng"`, `"to_tenant_id":"sre"`} {
		if !strings.Contains(events[5], field) {
			t.Errorf("switch log entry %s lacks %s", events[5], field)
		}
	}

	if _, err := f.svc.RefreshSession(context.Background(), SessionRequest{RefreshToken: old.RefreshToken}); !errors.Is(err, ErrRefreshTokenRevoked) {
		t.Errorf("refresh with the replaced token: err = %v, want ErrRefreshTokenRevoked", err)
	}
	if oldSession.RevokedAt == nil {
		t.Error("replaced session is not revoked")
	}
	if blocked, _ := f.svc.blocklist.IsBlocked(context.Background(), oldSession.JTI); !blocked {
		t.Error("replaced access token is not blocked")
	}
}

func TestExchangeIDToken_SwitchStorageErrorIsNotDenied(t *testing.T) {
	f := newSwitchFixture(t)
	own := f.signIn(t, "user-1")
	f.sessions.getByIDErr = errors.New("db down")

	_, err := f.switchTo(t, "sre", own.RefreshToken, groups("eng", "sre"))
	if err == nil || errors.Is(err, ErrTenantSwitchDenied) {
		t.Fatalf("err = %v, want a storage error", err)
	}
	if events := f.log.take(); len(events) != 0 {
		t.Errorf("a failed switch wrote %q", events)
	}
}

func TestExchangeIDToken_SwitchDenied(t *testing.T) {
	both := jwt.MapClaims{"groups": []string{"eng", "sre"}}
	tests := []struct {
		name        string
		hint        string
		claims      jwt.MapClaims
		replaces    func(t *testing.T, f *switchFixture, own string) string
		wantTenants []EligibleTenant
	}{
		{"hint outside the matches", "ops", both, ownToken, []EligibleTenant{tenantEng, tenantSRE}},
		{"one match other than the hint", "eng", groups("sre"), ownToken, []EligibleTenant{tenantSRE}},
		{"no match", "sre", groups("sales"), ownToken, []EligibleTenant{}},
		{"unknown hint", "nope", both, ownToken, []EligibleTenant{tenantEng, tenantSRE}},
		{"no hint", "", both, ownToken, []EligibleTenant{tenantEng, tenantSRE}},
		{"unknown refresh token", "sre", both, func(*testing.T, *switchFixture, string) string { return "not-a-token" }, []EligibleTenant{tenantEng, tenantSRE}},
		{"revoked refresh token", "sre", both, func(t *testing.T, f *switchFixture, own string) string {
			now := time.Now()
			f.refreshRow(t, own).RevokedAt = &now
			return own
		}, []EligibleTenant{tenantEng, tenantSRE}},
		{"expired refresh token", "sre", both, func(t *testing.T, f *switchFixture, own string) string {
			f.refreshRow(t, own).ExpiresAt = time.Now().Add(-time.Minute)
			return own
		}, []EligibleTenant{tenantEng, tenantSRE}},
		{"revoked session", "sre", both, func(t *testing.T, f *switchFixture, own string) string {
			s, _ := f.sessions.GetSessionByID(context.Background(), f.refreshRow(t, own).SessionID)
			now := time.Now()
			s.RevokedAt = &now
			return own
		}, []EligibleTenant{tenantEng, tenantSRE}},
		{"session on another issuer", "sre", both, func(t *testing.T, f *switchFixture, own string) string {
			s, _ := f.sessions.GetSessionByID(context.Background(), f.refreshRow(t, own).SessionID)
			s.Claims["issuer"] = "https://other.example.com"
			return own
		}, []EligibleTenant{tenantEng, tenantSRE}},
		{"session of another subject", "sre", both, func(t *testing.T, f *switchFixture, _ string) string {
			other := f.signIn(t, "user-2")
			return other.RefreshToken
		}, []EligibleTenant{tenantEng, tenantSRE}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newSwitchFixture(t)
			own := f.signIn(t, "user-1")
			replaces := tc.replaces(t, f, own.RefreshToken)
			f.log.take()

			_, err := f.switchTo(t, tc.hint, replaces, tc.claims)
			if !errors.Is(err, ErrTenantSwitchDenied) {
				t.Fatalf("err = %v, want ErrTenantSwitchDenied", err)
			}
			var tme *TenantMatchError
			if !errors.As(err, &tme) || !reflect.DeepEqual(tme.Tenants, tc.wantTenants) {
				t.Errorf("denied tenants = %+v, want %+v", tme, tc.wantTenants)
			}
			if events := f.log.take(); len(events) != 0 {
				t.Errorf("a denied switch wrote %q", events)
			}
		})
	}
}

func ownToken(_ *testing.T, _ *switchFixture, own string) string { return own }

func TestHandleSession_TenantErrorBodies(t *testing.T) {
	f := newSwitchFixture(t)
	c := NewController(f.svc, nil, nil, "", logging.NewLoggerWithOutput(f.log))
	own := f.signIn(t, "user-1")
	both := []string{"eng", "sre"}
	tenants := []any{
		map[string]any{"id": "eng", "display_name": "Engineering"},
		map[string]any{"id": "sre", "display_name": "SRE"},
	}
	tests := []struct {
		name     string
		body     map[string]any
		wantCode int
		wantBody map[string]any
	}{
		{"several matches", map[string]any{"grant_type": "oidc_id_token", "tenant_id": "eng", "id_token": issueIDTokenWithClaims(t, f.issuer, f.key, jwt.MapClaims{"groups": both})},
			http.StatusConflict, map[string]any{"code": "multiple_tenants_match", "message": "the account matches several tenants", "details": map[string]any{"tenants": tenants}}},
		{"switch denied", map[string]any{"grant_type": "oidc_id_token", "tenant_id": "eng", "tenant_hint": "ops", "replaces_refresh_token": own.RefreshToken, "id_token": issueIDTokenWithClaims(t, f.issuer, f.key, jwt.MapClaims{"groups": both})},
			http.StatusForbidden, map[string]any{"code": "tenant_switch_denied", "message": "the account cannot switch to this tenant", "details": map[string]any{"tenants": tenants}}},
		{"switch denied with no match", map[string]any{"grant_type": "oidc_id_token", "tenant_id": "eng", "tenant_hint": "sre", "replaces_refresh_token": own.RefreshToken, "id_token": issueIDTokenWithClaims(t, f.issuer, f.key, jwt.MapClaims{"groups": []string{"sales"}})},
			http.StatusForbidden, map[string]any{"code": "tenant_switch_denied", "message": "the account cannot switch to this tenant", "details": map[string]any{"tenants": []any{}}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := postJSON(t, c.handleSession, tc.body)
			if w.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d: %s", w.Code, tc.wantCode, w.Body)
			}
			var got map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if !reflect.DeepEqual(got, tc.wantBody) {
				t.Errorf("body = %v, want %v", got, tc.wantBody)
			}
		})
	}
}

func TestPollDeviceAuth_TenantHint(t *testing.T) {
	issuer, _, key := newSigningOIDCTestServerWithKey(t)
	svc, err := newTestServiceWithRegistry(t, switchRegistry(issuer))
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	idToken := issueIDTokenWithClaims(t, issuer, key, groups("eng", "sre"))
	tokens := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"id_token": idToken})
	}))
	t.Cleanup(tokens.Close)
	// Tenants poll this server for the device token.
	for _, ts := range svc.oidcTenants {
		ts.meta.TokenEndpoint = tokens.URL
	}
	ctx := context.Background()

	_, err = svc.PollDeviceAuth(ctx, SessionRequest{TenantID: "eng", DeviceCode: "dc"})
	if !errors.Is(err, ErrMultipleTenantsMatch) {
		t.Fatalf("no hint: err = %v, want ErrMultipleTenantsMatch", err)
	}

	_, err = svc.PollDeviceAuth(ctx, SessionRequest{TenantID: "eng", TenantHint: "ops", DeviceCode: "dc"})
	if !errors.Is(err, ErrMultipleTenantsMatch) {
		t.Fatalf("hint outside: err = %v, want ErrMultipleTenantsMatch", err)
	}

	resp, err := svc.PollDeviceAuth(ctx, SessionRequest{TenantID: "sre", TenantHint: "sre", DeviceCode: "dc"})
	if err != nil {
		t.Fatalf("hint sre: %v", err)
	}
	if got := sessionTenant(t, resp); got != "sre" {
		t.Errorf("session tenant = %q, want sre", got)
	}
	if want := []EligibleTenant{tenantEng, tenantSRE}; !reflect.DeepEqual(resp.Tenants, want) {
		t.Errorf("response tenants = %+v, want %+v", resp.Tenants, want)
	}
}
