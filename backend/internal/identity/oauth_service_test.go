package identity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/token"
)

func TestPKCES256_RFC7636Vector(t *testing.T) {
	// RFC 7636 Appendix B.
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	want := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if got := pkceS256(verifier); got != want {
		t.Errorf("pkceS256 = %q, want %q", got, want)
	}
}

func TestIsLoopbackRedirect(t *testing.T) {
	for _, u := range []string{"http://127.0.0.1:8080/cb", "http://localhost:1234/cb", "https://localhost/cb"} {
		if !isLoopbackRedirect(u) {
			t.Errorf("expected loopback allowed: %s", u)
		}
	}
	for _, u := range []string{"https://evil.com/cb", "http://example.com/cb", "not a url", "ftp://localhost/cb", ""} {
		if isLoopbackRedirect(u) {
			t.Errorf("expected non-loopback rejected: %s", u)
		}
	}
}

func newOAuthTestService(t *testing.T) *Service {
	return newTestService(t, newMockSessionRepo(), newMockRefreshTokenRepo(), newMockBlocklistRepo(), newMockPATRepo())
}

const (
	testVerifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	testRedirect = "http://127.0.0.1:9000/callback"
)

// registerTestClient registers a public client with the test redirect and
// returns its generated client_id.
func registerTestClient(t *testing.T, svc *Service) string {
	t.Helper()
	c, err := svc.RegisterClient(context.Background(), ClientRegistrationRequest{
		RedirectURIs: []string{testRedirect}, ClientName: "test", TokenEndpointAuthMethod: "none",
	})
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}
	return c.ClientID
}

func mkParams(clientID string) AuthorizeParams {
	return AuthorizeParams{ClientID: clientID, RedirectURI: testRedirect, CodeChallenge: pkceS256(testVerifier), CodeChallengeMethod: "S256"}
}

func TestOAuth_RoundTrip_MintsHumanToken(t *testing.T) {
	svc := newOAuthTestService(t)
	ctx := context.Background()
	clientID := registerTestClient(t, svc)
	human := AuthCode{
		Subject: "user-sub-1", TenantID: "acme", DisplayName: "jane@acme.com",
		Claims: map[string]string{"roles": "platform-admin"},
	}

	code, err := svc.CreateAuthCode(ctx, human, mkParams(clientID))
	if err != nil {
		t.Fatalf("CreateAuthCode: %v", err)
	}
	resp, err := svc.ExchangeAuthCode(ctx, TokenRequest{GrantType: "authorization_code", Code: code, CodeVerifier: testVerifier, RedirectURI: testRedirect, ClientID: clientID})
	if err != nil {
		t.Fatalf("ExchangeAuthCode: %v", err)
	}
	if resp.TokenType != "Bearer" || resp.AccessToken == "" {
		t.Fatalf("bad token response: %+v", resp)
	}
	parts := strings.Split(resp.AccessToken, ".")
	if len(parts) != 3 {
		t.Fatalf("not a JWT: %q", resp.AccessToken)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var claims struct {
		Sub  string `json:"sub"`
		Kora struct {
			PrincipalType string            `json:"principal_type"`
			TenantID      string            `json:"tenant_id"`
			Claims        map[string]string `json:"claims"`
		} `json:"kora"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	if claims.Sub != "user-sub-1" || claims.Kora.PrincipalType != "HUMAN" || claims.Kora.TenantID != "acme" {
		t.Errorf("wrong claims: %+v", claims)
	}
	if claims.Kora.Claims["mcp_client_id"] != clientID {
		t.Errorf("wrong mcp_client_id: %+v", claims.Kora.Claims)
	}
	// The human's roles must survive into the MCP token (acts-as-you) — else
	// role-derived grants are lost and tools/list comes back empty.
	if claims.Kora.Claims["roles"] != "platform-admin" {
		t.Errorf("roles not preserved into MCP token: %+v", claims.Kora.Claims)
	}
}

func TestOAuth_CodeIsSingleUse(t *testing.T) {
	svc := newOAuthTestService(t)
	ctx := context.Background()
	clientID := registerTestClient(t, svc)
	code, _ := svc.CreateAuthCode(ctx, AuthCode{Subject: "u", TenantID: "t"}, mkParams(clientID))
	req := TokenRequest{GrantType: "authorization_code", Code: code, CodeVerifier: testVerifier, RedirectURI: testRedirect, ClientID: clientID}
	if _, err := svc.ExchangeAuthCode(ctx, req); err != nil {
		t.Fatalf("first exchange should succeed: %v", err)
	}
	if _, err := svc.ExchangeAuthCode(ctx, req); !errors.Is(err, ErrInvalidOAuthGrant) {
		t.Errorf("second exchange should be invalid_grant, got %v", err)
	}
}

func TestOAuth_RefreshToken_RotatesAndPreservesClaims(t *testing.T) {
	svc := newOAuthTestService(t)
	ctx := context.Background()
	clientID := registerTestClient(t, svc)

	code, err := svc.CreateAuthCode(ctx, AuthCode{
		Subject: "user-sub-1", TenantID: "acme", DisplayName: "jane@acme.com",
		Claims: map[string]string{"roles": "platform-admin"},
	}, mkParams(clientID))
	if err != nil {
		t.Fatalf("CreateAuthCode: %v", err)
	}
	first, err := svc.ExchangeAuthCode(ctx, TokenRequest{GrantType: "authorization_code", Code: code, CodeVerifier: testVerifier, RedirectURI: testRedirect, ClientID: clientID})
	if err != nil {
		t.Fatalf("ExchangeAuthCode: %v", err)
	}
	// The exchange must now issue a refresh token so the client can refresh silently.
	if first.RefreshToken == "" || first.RefreshTokenExpiresIn <= 0 {
		t.Fatalf("exchange did not issue a refresh token: %+v", first)
	}

	// Refresh → a fresh token pair with a *rotated* refresh token.
	second, err := svc.RefreshOAuthToken(ctx, first.RefreshToken)
	if err != nil {
		t.Fatalf("RefreshOAuthToken: %v", err)
	}
	if second.AccessToken == "" || second.RefreshToken == "" {
		t.Fatalf("refresh did not return a token pair: %+v", second)
	}
	if second.RefreshToken == first.RefreshToken {
		t.Error("refresh token was not rotated — same token returned")
	}

	// The used refresh token must be revoked (one-time use / rotation).
	if _, err := svc.RefreshOAuthToken(ctx, first.RefreshToken); !errors.Is(err, ErrInvalidOAuthGrant) {
		t.Errorf("reusing a rotated refresh token should be invalid_grant, got %v", err)
	}

	// The human's roles + mcp_client_id must survive the refresh (acts-as-you).
	var claims struct {
		Kora struct {
			Claims map[string]string `json:"claims"`
		} `json:"kora"`
	}
	parts := strings.Split(second.AccessToken, ".")
	raw, _ := base64.RawURLEncoding.DecodeString(parts[1])
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatalf("decode refreshed claims: %v", err)
	}
	if claims.Kora.Claims["roles"] != "platform-admin" {
		t.Errorf("roles not preserved across refresh: %+v", claims.Kora.Claims)
	}
	if claims.Kora.Claims["mcp_client_id"] != clientID {
		t.Errorf("mcp_client_id not preserved across refresh: %+v", claims.Kora.Claims)
	}

	// An empty / unknown refresh token is invalid_grant, never a 500.
	if _, err := svc.RefreshOAuthToken(ctx, ""); !errors.Is(err, ErrInvalidOAuthRequest) {
		t.Errorf("empty refresh token should be invalid_request, got %v", err)
	}
	if _, err := svc.RefreshOAuthToken(ctx, "nope"); !errors.Is(err, ErrInvalidOAuthGrant) {
		t.Errorf("unknown refresh token should be invalid_grant, got %v", err)
	}
}

func TestOAuth_WrongVerifier_Denied(t *testing.T) {
	svc := newOAuthTestService(t)
	ctx := context.Background()
	clientID := registerTestClient(t, svc)
	code, _ := svc.CreateAuthCode(ctx, AuthCode{Subject: "u", TenantID: "t"}, mkParams(clientID))
	_, err := svc.ExchangeAuthCode(ctx, TokenRequest{GrantType: "authorization_code", Code: code, CodeVerifier: "wrong-verifier", RedirectURI: testRedirect, ClientID: clientID})
	if !errors.Is(err, ErrInvalidOAuthGrant) {
		t.Errorf("wrong verifier should be invalid_grant, got %v", err)
	}
}

func TestOAuth_UnregisteredClient_Denied(t *testing.T) {
	svc := newOAuthTestService(t)
	ctx := context.Background()
	// No registration → unknown client_id must be rejected.
	_, err := svc.CreateAuthCode(ctx, AuthCode{Subject: "u"}, mkParams("never-registered"))
	if !errors.Is(err, ErrInvalidOAuthRequest) {
		t.Errorf("unregistered client should be invalid_request, got %v", err)
	}
}

func TestOAuth_UnregisteredRedirect_Denied(t *testing.T) {
	svc := newOAuthTestService(t)
	ctx := context.Background()
	clientID := registerTestClient(t, svc) // registers testRedirect only
	p := mkParams(clientID)
	p.RedirectURI = "http://127.0.0.1:8888/other" // loopback but not registered
	if _, err := svc.CreateAuthCode(ctx, AuthCode{Subject: "u"}, p); !errors.Is(err, ErrInvalidOAuthRequest) {
		t.Errorf("unregistered redirect should be invalid_request, got %v", err)
	}
}

func TestOAuth_RegisterClient_Validation(t *testing.T) {
	svc := newOAuthTestService(t)
	ctx := context.Background()
	// no redirect_uris
	if _, err := svc.RegisterClient(ctx, ClientRegistrationRequest{}); !errors.Is(err, ErrInvalidClientMetadata) {
		t.Errorf("no redirect_uris should be invalid_client_metadata, got %v", err)
	}
	// non-loopback plain HTTP redirect — rejected (unencrypted code exfiltration risk)
	if _, err := svc.RegisterClient(ctx, ClientRegistrationRequest{RedirectURIs: []string{"http://evil.com/cb"}}); !errors.Is(err, ErrInvalidClientMetadata) {
		t.Errorf("non-loopback http redirect should be invalid_client_metadata, got %v", err)
	}
	// confidential client (secret auth) rejected — public only
	if _, err := svc.RegisterClient(ctx, ClientRegistrationRequest{RedirectURIs: []string{testRedirect}, TokenEndpointAuthMethod: "client_secret_post"}); !errors.Is(err, ErrInvalidClientMetadata) {
		t.Errorf("confidential client should be rejected, got %v", err)
	}
	// valid loopback
	c, err := svc.RegisterClient(ctx, ClientRegistrationRequest{RedirectURIs: []string{testRedirect}})
	if err != nil || c.ClientID == "" {
		t.Errorf("valid loopback registration failed: %v / %+v", err, c)
	}
	// valid non-loopback HTTPS — remote MCP clients like Claude Desktop
	c2, err := svc.RegisterClient(ctx, ClientRegistrationRequest{RedirectURIs: []string{"https://claude.ai/api/mcp/callback"}, ClientName: "Claude Desktop"})
	if err != nil || c2.ClientID == "" {
		t.Errorf("valid https registration failed: %v / %+v", err, c2)
	}
}

func TestOAuth_DCRDisabled(t *testing.T) {
	svc := newOAuthTestService(t)
	svc.cfg.OAuth.RequirePreRegistration = true
	if _, err := svc.RegisterClient(context.Background(), ClientRegistrationRequest{RedirectURIs: []string{testRedirect}}); !errors.Is(err, ErrDCRDisabled) {
		t.Errorf("disabled DCR should return ErrDCRDisabled, got %v", err)
	}
}

// ── Pre-registered (static) client redirect handling ──────────────────────────

const testHostedRedirect = "https://app.example.com/api/auth/callback/kora"

// seedStaticClient stores a client with a non-loopback redirect URI directly in
// the service's client repository, bypassing RegisterClient's DCR validation.
// This exercises the CreateAuthCode authorize gate for a client whose redirect
// URI DCR would refuse; it is not a substitute for the mounted-source path,
// which is covered by the StaticClientSource tests in oauth_static_clients_test.go.
func seedStaticClient(t *testing.T, svc *Service, clientID string, redirectURIs ...string) {
	t.Helper()
	if err := svc.clients.Store(context.Background(), RegisteredClient{
		ClientID: clientID, ClientName: "Hosted App", RedirectURIs: redirectURIs,
	}, time.Hour); err != nil {
		t.Fatalf("seed static client: %v", err)
	}
}

func TestOAuth_StaticClient_NonLoopbackRedirectAllowed(t *testing.T) {
	svc := newOAuthTestService(t)
	seedStaticClient(t, svc, "example-app", testHostedRedirect)

	p := AuthorizeParams{
		ClientID: "example-app", RedirectURI: testHostedRedirect,
		CodeChallenge: pkceS256(testVerifier), CodeChallengeMethod: "S256",
	}
	code, err := svc.CreateAuthCode(context.Background(), AuthCode{Subject: "u", TenantID: "acme"}, p)
	if err != nil {
		t.Fatalf("non-loopback redirect for a registered client should be allowed: %v", err)
	}
	if code == "" {
		t.Fatal("expected a non-empty auth code")
	}
}

func TestOAuth_StaticClient_UnregisteredRedirectDenied(t *testing.T) {
	svc := newOAuthTestService(t)
	seedStaticClient(t, svc, "example-app", testHostedRedirect)

	p := AuthorizeParams{
		ClientID: "example-app", RedirectURI: "https://evil.example.com/callback",
		CodeChallenge: pkceS256(testVerifier), CodeChallengeMethod: "S256",
	}
	if _, err := svc.CreateAuthCode(context.Background(), AuthCode{Subject: "u"}, p); !errors.Is(err, ErrInvalidOAuthRequest) {
		t.Errorf("a redirect_uri not registered for the client must be rejected, got %v", err)
	}
}

func TestOAuth_DCR_NonLoopbackHTTPRejected(t *testing.T) {
	svc := newOAuthTestService(t)
	// Non-loopback plain HTTP is rejected — it would send the authorization code
	// over an unencrypted channel to a remote host (code exfiltration).
	if _, err := svc.RegisterClient(context.Background(), ClientRegistrationRequest{
		RedirectURIs: []string{"http://remote.example.com/cb"}, TokenEndpointAuthMethod: "none",
	}); !errors.Is(err, ErrInvalidClientMetadata) {
		t.Errorf("DCR with a non-loopback http redirect must be ErrInvalidClientMetadata, got %v", err)
	}
}

func TestOAuth_DCR_NonLoopbackHTTPSAllowed(t *testing.T) {
	svc := newOAuthTestService(t)
	// Non-loopback HTTPS is now accepted for remote MCP clients (e.g. Claude Desktop).
	c, err := svc.RegisterClient(context.Background(), ClientRegistrationRequest{
		RedirectURIs: []string{testHostedRedirect}, TokenEndpointAuthMethod: "none",
	})
	if err != nil || c.ClientID == "" {
		t.Errorf("DCR with a non-loopback https redirect must succeed, got err=%v client=%+v", err, c)
	}
	if !c.AllowsRedirect(testHostedRedirect) {
		t.Errorf("registered client must allow its own redirect URI")
	}
}

func TestIsValidDCRRedirectURI(t *testing.T) {
	allowed := []string{
		"http://127.0.0.1:8080/cb",
		"http://localhost:1234/cb",
		"https://localhost/cb",
		"https://claude.ai/api/mcp/callback",
		"https://app.example.com/auth/callback",
	}
	for _, u := range allowed {
		if !isValidDCRRedirectURI(u) {
			t.Errorf("expected DCR redirect allowed: %s", u)
		}
	}
	rejected := []string{
		"http://evil.com/cb",
		"http://remote.example.com/cb",
		"not a url",
		"ftp://localhost/cb",
		"",
		"://bad",
	}
	for _, u := range rejected {
		if isValidDCRRedirectURI(u) {
			t.Errorf("expected DCR redirect rejected: %s", u)
		}
	}
}

// Only a human session may create an authorization code. Any other caller would
// be minted a HUMAN token for its own subject.
func TestHandleCreateOAuthCode_HumanOnly(t *testing.T) {
	svc := newOAuthTestService(t)
	c := NewController(svc, nil, nil, "", logging.NewLoggerWithOutput(io.Discard))
	clientID := registerTestClient(t, svc)
	p := mkParams(clientID)
	body := fmt.Sprintf(`{"client_id":%q,"redirect_uri":%q,"code_challenge":%q,"code_challenge_method":"S256"}`, p.ClientID, p.RedirectURI, p.CodeChallenge)

	cases := []struct {
		principal  token.PrincipalType
		wantStatus int
	}{
		{token.PrincipalTypeHuman, http.StatusOK},
		{token.PrincipalTypeAgent, http.StatusForbidden},
		{token.PrincipalTypeService, http.StatusForbidden},
		{token.PrincipalTypePAT, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(string(tc.principal), func(t *testing.T) {
			claims := &token.PlatformClaims{}
			claims.Subject = "sub-1"
			claims.Kora.TenantID = "acme"
			claims.Kora.PrincipalType = tc.principal
			req := httptest.NewRequest(http.MethodPost, "/oauth/code", strings.NewReader(body))
			req = req.WithContext(token.WithClaims(req.Context(), claims))
			w := httptest.NewRecorder()
			c.handleCreateOAuthCode(w, req)
			if w.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d (body: %s)", w.Code, tc.wantStatus, w.Body)
			}
		})
	}
}
