package skills

import (
	"context"
	"crypto/rsa"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	ghclient "github.com/clear-route/orbit/internal/platform/clients/github"
)

// appJWTTTL is the App JWT lifetime. GitHub caps it at 10 minutes.
const appJWTTTL = 10 * time.Minute

// appJWTSkew backdates the App JWT to tolerate clock skew with GitHub.
const appJWTSkew = 60 * time.Second

// refreshBefore treats a cached token as stale this long before it expires,
// so no caller receives a token that expires mid-crawl.
const refreshBefore = 5 * time.Minute

// installationTokenMinter exchanges an App JWT for an installation token.
type installationTokenMinter interface {
	CreateInstallationToken(ctx context.Context, appJWT string, installationID int64, body ghclient.InstallationTokenRequest) (ghclient.InstallationToken, error)
}

// tokenProvider mints and caches installation tokens for one Marketplace. A
// token for an installed repository reads only that repository, with
// contents:read. Any other repository gets a metadata:read token, which reads
// public repositories only.
type tokenProvider struct {
	client         installationTokenMinter
	appID          string
	installationID int64
	repo           string
	privateKey     *rsa.PrivateKey
	now            func() time.Time

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

func newTokenProvider(client installationTokenMinter, appID string, installationID int64, repo string, privateKeyPEM []byte) (*tokenProvider, error) {
	key, err := jwt.ParseRSAPrivateKeyFromPEM(privateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse github app private key: %w", err)
	}
	return &tokenProvider{
		client:         client,
		appID:          appID,
		installationID: installationID,
		repo:           repo,
		privateKey:     key,
		now:            time.Now,
	}, nil
}

// Token returns a cached token or mints one. Callers serialize on a mint, so
// concurrent callers share one token.
func (p *tokenProvider) Token(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.token != "" && p.now().Before(p.expiresAt.Add(-refreshBefore)) {
		return p.token, nil
	}

	now := p.now()
	appJWT, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{
		Issuer:    p.appID,
		IssuedAt:  jwt.NewNumericDate(now.Add(-appJWTSkew)),
		ExpiresAt: jwt.NewNumericDate(now.Add(appJWTTTL)),
	}).SignedString(p.privateKey)
	if err != nil {
		return "", fmt.Errorf("sign github app jwt: %w", err)
	}

	tok, err := p.client.CreateInstallationToken(ctx, appJWT, p.installationID, ghclient.InstallationTokenRequest{
		Repositories: []string{p.repo},
		Permissions:  map[string]string{"contents": "read"},
	})
	// GitHub refuses the repository-scoped mint for a repository outside the
	// installation. A metadata-only token still reads a public repository.
	if errors.Is(err, ghclient.ErrInstallationPermissions) {
		tok, err = p.client.CreateInstallationToken(ctx, appJWT, p.installationID, ghclient.InstallationTokenRequest{
			Permissions: map[string]string{"metadata": "read"},
		})
	}
	// An outage may pass. Any other refusal means GitHub rejects the App,
	// installation or repository, which only a credential change fixes.
	if errors.Is(err, ghclient.ErrUnavailable) {
		return "", fmt.Errorf("mint github app installation token: %w", err)
	}
	if err != nil {
		return "", fmt.Errorf("mint github app installation token: %w: %w", ErrCredentialUnavailable, err)
	}
	p.token = tok.Token
	p.expiresAt = tok.ExpiresAt
	return p.token, nil
}
