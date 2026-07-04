package identity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
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

func mkParams() AuthorizeParams {
	return AuthorizeParams{ClientID: "cid", RedirectURI: testRedirect, CodeChallenge: pkceS256(testVerifier), CodeChallengeMethod: "S256"}
}

func TestOAuth_RoundTrip_MintsHumanToken(t *testing.T) {
	svc := newOAuthTestService(t)
	ctx := context.Background()
	human := AuthCode{Subject: "user-sub-1", TenantID: "acme", DisplayName: "jane@acme.com"}

	code, err := svc.CreateAuthCode(ctx, human, mkParams())
	if err != nil {
		t.Fatalf("CreateAuthCode: %v", err)
	}
	resp, err := svc.ExchangeAuthCode(ctx, TokenRequest{GrantType: "authorization_code", Code: code, CodeVerifier: testVerifier, RedirectURI: testRedirect, ClientID: "cid"})
	if err != nil {
		t.Fatalf("ExchangeAuthCode: %v", err)
	}
	if resp.TokenType != "Bearer" || resp.AccessToken == "" {
		t.Fatalf("bad token response: %+v", resp)
	}
	// Decode the JWT payload; it must be an aud=orbit-platform HUMAN token for the human.
	parts := strings.Split(resp.AccessToken, ".")
	if len(parts) != 3 {
		t.Fatalf("not a JWT: %q", resp.AccessToken)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var claims struct {
		Sub   string `json:"sub"`
		Orbit struct {
			PrincipalType string            `json:"principal_type"`
			TenantID      string            `json:"tenant_id"`
			Claims        map[string]string `json:"claims"`
		} `json:"orbit"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	if claims.Sub != "user-sub-1" || claims.Orbit.PrincipalType != "HUMAN" || claims.Orbit.TenantID != "acme" {
		t.Errorf("wrong claims: %+v", claims)
	}
	if claims.Orbit.Claims["mcp_client_id"] != "cid" {
		t.Errorf("missing mcp_client_id claim: %+v", claims.Orbit.Claims)
	}
}

func TestOAuth_CodeIsSingleUse(t *testing.T) {
	svc := newOAuthTestService(t)
	ctx := context.Background()
	code, _ := svc.CreateAuthCode(ctx, AuthCode{Subject: "u", TenantID: "t"}, mkParams())
	req := TokenRequest{GrantType: "authorization_code", Code: code, CodeVerifier: testVerifier, RedirectURI: testRedirect, ClientID: "cid"}
	if _, err := svc.ExchangeAuthCode(ctx, req); err != nil {
		t.Fatalf("first exchange should succeed: %v", err)
	}
	if _, err := svc.ExchangeAuthCode(ctx, req); !errors.Is(err, ErrInvalidOAuthGrant) {
		t.Errorf("second exchange should be invalid_grant, got %v", err)
	}
}

func TestOAuth_WrongVerifier_Denied(t *testing.T) {
	svc := newOAuthTestService(t)
	ctx := context.Background()
	code, _ := svc.CreateAuthCode(ctx, AuthCode{Subject: "u", TenantID: "t"}, mkParams())
	_, err := svc.ExchangeAuthCode(ctx, TokenRequest{GrantType: "authorization_code", Code: code, CodeVerifier: "wrong-verifier", RedirectURI: testRedirect, ClientID: "cid"})
	if !errors.Is(err, ErrInvalidOAuthGrant) {
		t.Errorf("wrong verifier should be invalid_grant, got %v", err)
	}
}

func TestOAuth_RedirectOrClientMismatch_Denied(t *testing.T) {
	svc := newOAuthTestService(t)
	ctx := context.Background()
	code, _ := svc.CreateAuthCode(ctx, AuthCode{Subject: "u", TenantID: "t"}, mkParams())
	_, err := svc.ExchangeAuthCode(ctx, TokenRequest{GrantType: "authorization_code", Code: code, CodeVerifier: testVerifier, RedirectURI: "http://127.0.0.1:1111/other", ClientID: "cid"})
	if !errors.Is(err, ErrInvalidOAuthGrant) {
		t.Errorf("redirect mismatch should be invalid_grant, got %v", err)
	}
}

func TestOAuth_CreateAuthCode_Validation(t *testing.T) {
	svc := newOAuthTestService(t)
	ctx := context.Background()
	// missing PKCE S256
	if _, err := svc.CreateAuthCode(ctx, AuthCode{Subject: "u"}, AuthorizeParams{ClientID: "c", RedirectURI: testRedirect}); !errors.Is(err, ErrInvalidOAuthRequest) {
		t.Errorf("missing PKCE should be invalid_request, got %v", err)
	}
	// non-loopback redirect
	if _, err := svc.CreateAuthCode(ctx, AuthCode{Subject: "u"}, AuthorizeParams{ClientID: "c", RedirectURI: "https://evil.com/cb", CodeChallenge: pkceS256(testVerifier), CodeChallengeMethod: "S256"}); !errors.Is(err, ErrInvalidOAuthRequest) {
		t.Errorf("non-loopback redirect should be invalid_request, got %v", err)
	}
}
