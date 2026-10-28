package skills

import (
	"context"
	"crypto/rsa"
	"fmt"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	ghclient "github.com/clear-route/orbit/internal/platform/clients/github"
)

// appJWTTTL is the lifetime of the GitHub App authentication JWT used to mint
// installation tokens. GitHub caps this at 10 minutes.
const appJWTTTL = 10 * time.Minute

// appJWTSkew backdates the App JWT's issued-at to tolerate clock skew between
// Orbit and GitHub.
const appJWTSkew = 60 * time.Second

// refreshBefore is how long before GitHub's stated expiry a cached
// installation token is treated as stale, so Token never hands out a
// token that is about to expire mid-use.
const refreshBefore = 5 * time.Minute

// installationTokenMinter is the subset of the platform GitHub client that
// TokenProvider depends on: exchanging a signed App JWT for a short-lived
// installation access token. Declared narrowly here (rather than taking
// *ghclient.Client) so tests can supply a fake without an HTTP server.
type installationTokenMinter interface {
	CreateInstallationToken(ctx context.Context, appJWT string, installationID int64, body ghclient.InstallationTokenRequest) (ghclient.InstallationToken, error)
}

// TokenProvider mints and caches a GitHub App installation access token for
// one App + installation pair, refreshing it before expiry. Safe for
// concurrent use: Token serializes callers during a refresh so at most one
// mint happens at a time. TokenProvider holds no Kubernetes or secret-store
// dependency — the caller resolves the App private key (e.g. from a mounted
// Secret) and passes the PEM bytes in at construction.
type TokenProvider struct {
	client         installationTokenMinter
	appID          string
	installationID int64
	privateKey     *rsa.PrivateKey
	now            func() time.Time

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

// NewTokenProvider constructs a TokenProvider for the given GitHub App
// (appID, installationID) using privateKeyPEM (the App's RSA private key, PEM
// encoded) to sign App JWTs. Returns an error if privateKeyPEM does not parse
// as an RSA private key.
func NewTokenProvider(client installationTokenMinter, appID string, installationID int64, privateKeyPEM []byte) (*TokenProvider, error) {
	key, err := jwt.ParseRSAPrivateKeyFromPEM(privateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse github app private key: %w", err)
	}
	return &TokenProvider{
		client:         client,
		appID:          appID,
		installationID: installationID,
		privateKey:     key,
		now:            time.Now,
	}, nil
}

// signAppJWT signs a GitHub App authentication JWT (RS256), used to mint
// installation tokens. GitHub authenticates this JWT as the App itself, not
// the installation.
func (p *TokenProvider) signAppJWT() (string, error) {
	now := p.now()
	claims := jwt.RegisteredClaims{
		Issuer:    p.appID,
		IssuedAt:  jwt.NewNumericDate(now.Add(-appJWTSkew)),
		ExpiresAt: jwt.NewNumericDate(now.Add(appJWTTTL)),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(p.privateKey)
	if err != nil {
		return "", fmt.Errorf("sign github app jwt: %w", err)
	}
	return signed, nil
}

// Token returns a valid GitHub App installation access token, minting one on
// first use and refreshing it 5 minutes before GitHub's 60-minute expiry.
// Safe for concurrent use: this method holds p.mu for the full mint call, so
// concurrent callers serialize on a refresh instead of each minting their own
// token — see TestToken_ConcurrentCallsMintOnce.
func (p *TokenProvider) Token(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.token != "" && p.now().Before(p.expiresAt.Add(-refreshBefore)) {
		return p.token, nil
	}

	appJWT, err := p.signAppJWT()
	if err != nil {
		return "", fmt.Errorf("sign github app jwt: %w", err)
	}

	tok, err := p.client.CreateInstallationToken(ctx, appJWT, p.installationID, ghclient.InstallationTokenRequest{
		Permissions: map[string]string{"contents": "read"},
	})
	if err != nil {
		return "", fmt.Errorf("mint github app installation token: %w", err)
	}

	p.token = tok.Token
	p.expiresAt = tok.ExpiresAt
	return p.token, nil
}
