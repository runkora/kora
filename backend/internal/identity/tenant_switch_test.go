package identity

import (
	"bytes"
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

var (
	switchTenants = map[string]string{"eng": `groups:"eng"`, "sre": `groups:"sre"`, "ops": `groups:"ops"`}
	engAndSRE     = eligible("eng", "sre")
)

func groups(g ...string) jwt.MapClaims { return jwt.MapClaims{"groups": g} }

// lockedBuffer captures the service log, which the registry watcher can also write.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// switchFixture is a service over the package mocks with its log captured.
type switchFixture struct {
	svc       *Service
	sessions  *mockSessionRepository
	refresh   *mockRefreshTokenRepository
	blocklist *mockJTIBlocklistRepository
	log       *lockedBuffer
	issuer    string
	key       *rsa.PrivateKey
}

func newSwitchFixture(t *testing.T) *switchFixture {
	t.Helper()
	issuer, _, key := newSigningOIDCTestServerWithKey(t)
	log := &lockedBuffer{}
	svc, err := newTestServiceWithRegistryLog(t, sharedRegistry(issuer, switchTenants), log)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	f := &switchFixture{
		svc:       svc,
		sessions:  svc.sessions.(*mockSessionRepository),
		refresh:   svc.refreshTokens.(*mockRefreshTokenRepository),
		blocklist: svc.blocklist.(*mockJTIBlocklistRepository),
		log:       log,
		issuer:    issuer,
		key:       key,
	}
	return f
}

// signIn creates a session in eng for sub.
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

// assertNothingAfterCommit fails when a switch that did not commit blocked a
// token or wrote the switch log entry.
func (f *switchFixture) assertNothingAfterCommit(t *testing.T) {
	t.Helper()
	if len(f.blocklist.blocked) != 0 {
		t.Errorf("blocklist written: %v", f.blocklist.blocked)
	}
	if strings.Contains(f.log.String(), `"tenant switched"`) {
		t.Error("switch log entry written")
	}
}

func TestExchangeIDToken_Switch(t *testing.T) {
	const blockFailure = "failed to block the replaced access token"
	tests := []struct {
		name        string
		inject      func(f *switchFixture)
		wantErr     error
		wantAttempt bool
	}{
		{"switches", func(*switchFixture) {}, nil, true},
		{"blocklist failure still switches", func(f *switchFixture) { f.blocklist.blockErr = errors.New("redis down") }, nil, true},
		{"transaction fails", func(f *switchFixture) { f.sessions.switchErr = errors.New("tx failed") }, errors.New("tx failed"), true},
		{"refresh token consumed at commit", func(f *switchFixture) { f.sessions.switchErr = fmt.Errorf("switch: %w", ErrRefreshTokenNotFound) }, ErrTenantSwitchDenied, true},
		{"session revoked at commit", func(f *switchFixture) { f.sessions.switchErr = fmt.Errorf("switch: %w", ErrSessionNotFound) }, ErrTenantSwitchDenied, true},
		{"storage error before the switch", func(f *switchFixture) { f.sessions.getByIDErr = errors.New("db down") }, errors.New("db down"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newSwitchFixture(t)
			old := f.signIn(t, "user-1")
			oldRT := f.refreshRow(t, old.RefreshToken)
			oldSession, _ := f.sessions.GetSessionByID(context.Background(), oldRT.SessionID)
			tc.inject(f)

			resp, err := f.switchTo(t, "sre", old.RefreshToken, groups("eng", "sre"))
			if (f.sessions.switched != nil) != tc.wantAttempt {
				t.Fatalf("SwitchSession called = %v, want %v", f.sessions.switched != nil, tc.wantAttempt)
			}
			if tc.wantErr != nil {
				switch {
				case errors.Is(tc.wantErr, ErrTenantSwitchDenied):
					var tme *TenantMatchError
					if !errors.As(err, &tme) || !errors.Is(err, ErrTenantSwitchDenied) || !reflect.DeepEqual(tme.Tenants, engAndSRE) {
						t.Fatalf("err = %v, want ErrTenantSwitchDenied with the eligible set", err)
					}
				case err == nil || errors.Is(err, ErrTenantSwitchDenied):
					t.Fatalf("err = %v, want the storage error", err)
				}
				f.assertNothingAfterCommit(t)
				return
			}
			if err != nil {
				t.Fatalf("switch: %v", err)
			}

			call := f.sessions.switched
			if call.replaced.ID != oldRT.ID || call.next.TenantID != "sre" || call.nextRefresh.SessionID != call.next.ID {
				t.Errorf("SwitchSession(next %s/%s, refresh for %s, replaced %s), want next in sre with its refresh token replacing %s",
					call.next.ID, call.next.TenantID, call.nextRefresh.SessionID, call.replaced.ID, oldRT.ID)
			}
			if got := sessionTenant(t, resp); got != "sre" {
				t.Errorf("session tenant = %q, want sre", got)
			}
			if !reflect.DeepEqual(resp.Tenants, engAndSRE) {
				t.Errorf("response tenants = %+v, want %+v", resp.Tenants, engAndSRE)
			}

			logged := f.log.String()
			switchedAt := strings.Index(logged, `"tenant switched"`)
			if switchedAt < 0 {
				t.Fatalf("no switch log entry in %s", logged)
			}
			line := logged[strings.LastIndex(logged[:switchedAt], "\n")+1:]
			line, _, _ = strings.Cut(line, "\n")
			for _, field := range []string{`"principal_id":"user-1"`, `"from_tenant_id":"eng"`, `"to_tenant_id":"sre"`} {
				if !strings.Contains(line, field) {
					t.Errorf("switch log entry lacks %s", field)
				}
			}
			if f.blocklist.blockErr != nil {
				if at := strings.Index(logged, blockFailure); at < 0 || at > switchedAt {
					t.Errorf("the blocklist failure is not logged before the switch entry: %s", logged)
				}
			} else if !f.blocklist.blocked[oldSession.JTI] {
				t.Error("replaced access token is not blocked")
			}
		})
	}
}

func TestExchangeIDToken_SwitchDenied(t *testing.T) {
	both := groups("eng", "sre")
	tests := []struct {
		name        string
		hint        string
		claims      jwt.MapClaims
		replaces    func(t *testing.T, f *switchFixture, own string) string
		wantTenants []EligibleTenant
	}{
		{"hint outside the matches", "ops", both, ownToken, engAndSRE},
		{"one match other than the hint", "eng", groups("sre"), ownToken, eligible("sre")},
		{"no match", "sre", groups("sales"), ownToken, eligible()},
		{"unknown hint", "nope", both, ownToken, engAndSRE},
		{"no hint", "", both, ownToken, engAndSRE},
		{"unknown refresh token", "sre", both, func(*testing.T, *switchFixture, string) string { return "not-a-token" }, engAndSRE},
		{"revoked refresh token", "sre", both, func(t *testing.T, f *switchFixture, own string) string {
			now := time.Now()
			f.refreshRow(t, own).RevokedAt = &now
			return own
		}, engAndSRE},
		{"expired refresh token", "sre", both, func(t *testing.T, f *switchFixture, own string) string {
			f.refreshRow(t, own).ExpiresAt = time.Now().Add(-time.Minute)
			return own
		}, engAndSRE},
		{"revoked session", "sre", both, func(t *testing.T, f *switchFixture, own string) string {
			s, _ := f.sessions.GetSessionByID(context.Background(), f.refreshRow(t, own).SessionID)
			now := time.Now()
			s.RevokedAt = &now
			return own
		}, engAndSRE},
		{"session on another issuer", "sre", both, func(t *testing.T, f *switchFixture, own string) string {
			s, _ := f.sessions.GetSessionByID(context.Background(), f.refreshRow(t, own).SessionID)
			s.Claims["issuer"] = "https://other.example.com"
			return own
		}, engAndSRE},
		{"session of another subject", "sre", both, func(t *testing.T, f *switchFixture, _ string) string {
			return f.signIn(t, "user-2").RefreshToken
		}, engAndSRE},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newSwitchFixture(t)
			own := f.signIn(t, "user-1")
			replaces := tc.replaces(t, f, own.RefreshToken)
			sessions, refreshTokens := len(f.sessions.sessions), len(f.refresh.byHash)

			_, err := f.switchTo(t, tc.hint, replaces, tc.claims)
			if !errors.Is(err, ErrTenantSwitchDenied) {
				t.Fatalf("err = %v, want ErrTenantSwitchDenied", err)
			}
			var tme *TenantMatchError
			if !errors.As(err, &tme) || !reflect.DeepEqual(tme.Tenants, tc.wantTenants) {
				t.Errorf("denied tenants = %+v, want %+v", tme, tc.wantTenants)
			}
			if f.sessions.switched != nil || len(f.sessions.sessions) != sessions || len(f.refresh.byHash) != refreshTokens {
				t.Errorf("a denied switch wrote: switch %v, sessions %d to %d, refresh tokens %d to %d",
					f.sessions.switched != nil, sessions, len(f.sessions.sessions), refreshTokens, len(f.refresh.byHash))
			}
			f.assertNothingAfterCommit(t)
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
		map[string]any{"id": "eng", "display_name": "eng"},
		map[string]any{"id": "sre", "display_name": "sre"},
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
	svc, err := newTestServiceWithRegistry(t, sharedRegistry(issuer, switchTenants))
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

	if _, err := svc.PollDeviceAuth(ctx, SessionRequest{TenantID: "eng", DeviceCode: "dc"}); !errors.Is(err, ErrMultipleTenantsMatch) {
		t.Fatalf("no hint: err = %v, want ErrMultipleTenantsMatch", err)
	}
	resp, err := svc.PollDeviceAuth(ctx, SessionRequest{TenantID: "sre", TenantHint: "sre", DeviceCode: "dc"})
	if err != nil {
		t.Fatalf("hint sre: %v", err)
	}
	if got := sessionTenant(t, resp); got != "sre" {
		t.Errorf("session tenant = %q, want sre", got)
	}
	if !reflect.DeepEqual(resp.Tenants, engAndSRE) {
		t.Errorf("response tenants = %+v, want %+v", resp.Tenants, engAndSRE)
	}
}
