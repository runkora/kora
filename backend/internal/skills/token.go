// Package skills implements the read-only Agent Marketplace: browsing and
// serving skills hosted in GitHub repositories declared as marketplace
// sources. See docs/adr/2026-09-09-agent-marketplace-startup-preload-per-repo-redis-keys.md
// (a byte-for-byte copy is also kept sibling to the repo, per that ADR's own
// history, as 2026-08-28-marketplace_adr_backend.md).
package skills

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

// installationTokenPermissions is the minimal GitHub App permission set
// TokenProvider requests when minting an installation token: read-only
// repository contents, which is all the skills crawler's Trees API and raw
// file fetches need. An empty InstallationTokenRequest would instead request
// the App installation's full configured access (see
// ghclient.InstallationTokenRequest's doc comment), which could include
// write access or other permission categories the installation happens to
// hold for unrelated reasons — requesting only what this read-only feature
// needs follows the least-privilege principle (AGENTS.md, "Security
// Guidelines").
var installationTokenPermissions = map[string]string{"contents": "read"}

// installationTokenMinter is the subset of the platform GitHub client that
// TokenProvider depends on: exchanging a signed App JWT for a short-lived
// installation access token. Declared narrowly here (rather than taking
// *ghclient.Client) so tests can supply a fake without an HTTP server.
type installationTokenMinter interface {
	CreateInstallationToken(ctx context.Context, appJWT string, installationID int64, body ghclient.InstallationTokenRequest) (ghclient.InstallationToken, error)
}

// mintResult is the outcome of one mint attempt: either a token and its
// expiry, or an error. Shared verbatim with every caller waiting on that
// attempt, so a failure during an outage does not turn N concurrent callers
// into N sequential upstream calls.
type mintResult struct {
	token     string
	expiresAt time.Time
	err       error
}

// TokenProvider mints and caches a GitHub App installation access token for
// one App + installation pair, refreshing it before expiry. Safe for
// concurrent use: Token shares one in-flight mint's result (success or
// error) with every caller waiting on it, so at most one mint happens at a
// time regardless of how many callers arrive concurrently — see
// TestToken_ConcurrentCallsMintOnce and
// TestToken_ConcurrentCallsDuringFailureMintOnce. TokenProvider holds no
// Kubernetes or secret-store dependency and never keeps the App's decoded
// private key resident in memory — the caller passes the path to the
// mounted private key file at construction, and signAppJWT reads and parses
// it fresh on every mint. This mirrors
// internal/credentials/resolvers/k8sfile's "every Resolve call reads the
// file fresh" pattern for the same reason: it keeps the key material's
// in-memory lifetime limited to the moment it is actually used, and picks up
// a rotated key on the next mint without a process restart.
type TokenProvider struct {
	client         installationTokenMinter
	appID          string
	installationID int64
	privateKeyPath string
	now            func() time.Time

	mu        sync.Mutex
	token     string
	expiresAt time.Time
	inflight  chan struct{} // non-nil while a mint is in progress; closed when it completes
	result    mintResult    // the most recently completed mint's outcome
}

// NewTokenProvider constructs a TokenProvider for the given GitHub App
// (appID, installationID). privateKeyPath is the filesystem path to the
// App's RSA private key (PEM encoded, e.g. a mounted Kubernetes Secret file)
// — it is read fresh on every mint, never at construction, so a missing or
// invalid key surfaces as an error from Token, not from NewTokenProvider.
func NewTokenProvider(client installationTokenMinter, appID string, installationID int64, privateKeyPath string) *TokenProvider {
	return &TokenProvider{
		client:         client,
		appID:          appID,
		installationID: installationID,
		privateKeyPath: privateKeyPath,
		now:            time.Now,
	}
}

// signAppJWT signs a GitHub App authentication JWT (RS256), used to mint
// installation tokens. GitHub authenticates this JWT as the App itself, not
// the installation. It reads and parses the private key from
// p.privateKeyPath fresh on every call rather than caching the decoded key.
func (p *TokenProvider) signAppJWT() (string, error) {
	pemBytes, err := os.ReadFile(p.privateKeyPath)
	if err != nil {
		return "", fmt.Errorf("read github app private key: %w", err)
	}
	key, err := jwt.ParseRSAPrivateKeyFromPEM(pemBytes)
	if err != nil {
		return "", fmt.Errorf("parse github app private key: %w", err)
	}

	now := p.now()
	claims := jwt.RegisteredClaims{
		Issuer:    p.appID,
		IssuedAt:  jwt.NewNumericDate(now.Add(-appJWTSkew)),
		ExpiresAt: jwt.NewNumericDate(now.Add(appJWTTTL)),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(key)
	if err != nil {
		return "", fmt.Errorf("sign github app jwt: %w", err)
	}
	return signed, nil
}

// Token returns a valid GitHub App installation access token, minting one on
// first use and refreshing it 5 minutes before GitHub's 60-minute expiry.
//
// Safe for concurrent use. A caller that finds a mint already in flight waits
// on that same attempt and receives its exact result (token or error)
// instead of starting a redundant mint of its own — so a burst of concurrent
// callers during a GitHub outage produces one upstream call and one shared
// error, not one call per caller. A waiter still honors its own ctx: if ctx
// is canceled before the in-flight mint completes, Token returns ctx.Err()
// without affecting the in-flight attempt or any other waiter.
func (p *TokenProvider) Token(ctx context.Context) (string, error) {
	p.mu.Lock()
	if p.token != "" && p.now().Before(p.expiresAt.Add(-refreshBefore)) {
		tok := p.token
		p.mu.Unlock()
		return tok, nil
	}

	if ch := p.inflight; ch != nil {
		p.mu.Unlock()
		select {
		case <-ch:
			p.mu.Lock()
			res := p.result
			p.mu.Unlock()
			return res.token, res.err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	ch := make(chan struct{})
	p.inflight = ch
	p.mu.Unlock()

	res := p.mint(ctx)

	p.mu.Lock()
	p.inflight = nil
	p.result = res
	if res.err == nil {
		p.token = res.token
		p.expiresAt = res.expiresAt
	}
	p.mu.Unlock()
	close(ch)

	return res.token, res.err
}

// mint signs a fresh App JWT and exchanges it for an installation access
// token scoped to installationTokenPermissions. It never touches p's cache
// or lock — Token owns publishing the result to waiters.
func (p *TokenProvider) mint(ctx context.Context) mintResult {
	appJWT, err := p.signAppJWT()
	if err != nil {
		return mintResult{err: fmt.Errorf("sign github app jwt: %w", err)}
	}

	tok, err := p.client.CreateInstallationToken(ctx, appJWT, p.installationID, ghclient.InstallationTokenRequest{
		Permissions: installationTokenPermissions,
	})
	if err != nil {
		return mintResult{err: fmt.Errorf("mint github app installation token: %w", err)}
	}
	return mintResult{token: tok.Token, expiresAt: tok.ExpiresAt}
}

// TokenProviderPool lazily constructs and caches a TokenProvider per
// credentialsRef directory path. Safe for concurrent use. A source with an
// empty CredentialsRef falls back to the default provider supplied at
// construction, preserving behavior for sources declared before per-source
// credentials were introduced.
type TokenProviderPool struct {
	client          installationTokenMinter
	defaultProvider githubTokenProvider

	mu    sync.Mutex
	cache map[string]*TokenProvider // key: credentialsRef path
}

// NewTokenProviderPool constructs a pool. defaultProvider is used for any
// MarketplaceSource whose CredentialsRef is empty; pass nil only if every
// source is guaranteed to carry a CredentialsRef.
func NewTokenProviderPool(client installationTokenMinter, defaultProvider githubTokenProvider) *TokenProviderPool {
	return &TokenProviderPool{
		client:          client,
		defaultProvider: defaultProvider,
		cache:           map[string]*TokenProvider{},
	}
}

// For returns the token provider for credentialsRef. An empty dir returns the
// default provider. A non-empty dir must contain appID, installationID, and
// privateKey files (PEM-encoded RSA key).
func (p *TokenProviderPool) For(dir string) (githubTokenProvider, error) {
	if dir == "" {
		if p.defaultProvider == nil {
			return nil, fmt.Errorf("no credentialsRef and no default token provider configured")
		}
		return p.defaultProvider, nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if tp, ok := p.cache[dir]; ok {
		return tp, nil
	}

	appID, installationID, err := readCredentials(dir)
	if err != nil {
		return nil, fmt.Errorf("read credentials from %s: %w", dir, err)
	}

	tp := NewTokenProvider(p.client, appID, installationID, filepath.Join(dir, "privateKey"))
	p.cache[dir] = tp
	return tp, nil
}

// readCredentials reads appID and installationID from files inside dir.
// Expected layout (matches the Kubernetes Secret mount shape):
//
//	<dir>/appID           — GitHub App ID as a decimal string
//	<dir>/installationID  — installation ID as a decimal string
//	<dir>/privateKey      — PEM-encoded RSA private key (read later by TokenProvider)
func readCredentials(dir string) (appID string, installationID int64, err error) {
	raw, err := os.ReadFile(filepath.Join(dir, "appID"))
	if err != nil {
		return "", 0, fmt.Errorf("read appID: %w", err)
	}
	appID = strings.TrimSpace(string(raw))
	if appID == "" {
		return "", 0, fmt.Errorf("appID file is empty")
	}

	raw, err = os.ReadFile(filepath.Join(dir, "installationID"))
	if err != nil {
		return "", 0, fmt.Errorf("read installationID: %w", err)
	}
	installationID, err = strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil {
		return "", 0, fmt.Errorf("parse installationID: %w", err)
	}
	return appID, installationID, nil
}
