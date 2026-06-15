// Package githubapp implements the credential adapter for GitHub App
// installation tokens. The agent acts as the installed GitHub App (bot
// authority); the egress gateway calls Adapter.Resolve to obtain a short-lived,
// repository- and permission-scoped installation token, formatted as the git
// smart-HTTP Basic credential. The long-lived App private key never leaves this
// process.
//
// Each connection-type lives in its own subpackage so adding a new external
// service (AWS STS, OAuth2 client-credentials, API-key SaaS) is a new directory
// behind the same credentials.CredentialAdapter interface, never a change to
// the parent package. See docs/adr/2026-06-06-jit-credential-brokering-egress-gateway.md.
package githubapp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/clear-route/orbit/internal/credentials"
	ghclient "github.com/clear-route/orbit/internal/platform/clients/github"
	platformconns "github.com/clear-route/orbit/internal/platform/connections"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// appJWTTTL is the lifetime of the App JWT used to mint installation tokens.
// GitHub caps this at 10 minutes.
const appJWTTTL = 10 * time.Minute

// appJWTSkew backdates the App JWT's issued-at to tolerate clock skew between
// Orbit and GitHub.
const appJWTSkew = 60 * time.Second

// cacheSafetyMargin is how long before true expiry a cached installation token
// is treated as stale, so a caller never receives an about-to-expire credential.
const cacheSafetyMargin = time.Minute

// cacheEvictThreshold is the entry count at which store() sweeps out expired
// entries, bounding growth from keys that are minted once and never requested
// again (cached() only evicts on a same-key re-read). A var so tests can lower it.
var cacheEvictThreshold = 256

// config is the adapter-specific configuration stored on a GitHub connection's
// Config field.
type config struct {
	AppID          string            `json:"app_id"`
	InstallationID int64             `json:"installation_id"`
	Repositories   []string          `json:"repositories,omitempty"`
	Permissions    map[string]string `json:"permissions,omitempty"`
}

// API is the subset of the GitHub client this adapter depends on, declared as
// an interface so the mint logic is unit-testable without a live API.
type API interface {
	CreateInstallationToken(ctx context.Context, appJWT string, installationID int64, body ghclient.InstallationTokenRequest) (ghclient.InstallationToken, error)
}

// Adapter mints short-lived GitHub App installation tokens. Tokens are scoped to
// the connection's repositories and permissions and cached until shortly before
// expiry. Adapter implements credentials.CredentialAdapter.
type Adapter struct {
	api     API
	secrets credentials.SecretResolver
	logger  logging.Logger
	now     func() time.Time

	mu    sync.Mutex
	cache map[string]credentials.Credential
}

// New constructs an Adapter backed by the given GitHub client and secret
// resolver.
func New(api API, secrets credentials.SecretResolver, logger logging.Logger) *Adapter {
	return &Adapter{
		api:     api,
		secrets: secrets,
		logger:  logger,
		now:     time.Now,
		cache:   make(map[string]credentials.Credential),
	}
}

// Type reports the adapter type served.
func (a *Adapter) Type() credentials.AdapterType { return credentials.AdapterGitHubApp }

// Action constants returned by ClassifyAction. Sourced from the shared adapter
// metadata table so the classifier and the published vocabulary cannot drift.
// They combine with the connection name to form "connection:<name>:<action>".
const (
	ActionRepoRead  = platformconns.ActionGitHubRepoRead
	ActionRepoWrite = platformconns.ActionGitHubRepoWrite
)

// Actions enumerates every action ClassifyAction can return, delegating to the
// shared metadata table (internal/platform/connections) so the
// connection:<name>:<action> vocabulary the egress gateway publishes has exactly
// one source of truth — the same table orbit-agents reads to derive auto-egress.
func (a *Adapter) Actions() []credentials.ActionDeclaration {
	metas := platformconns.Actions(platformconns.AdapterGitHubApp)
	out := make([]credentials.ActionDeclaration, len(metas))
	for i, m := range metas {
		out[i] = credentials.ActionDeclaration{Action: m.Action, Description: m.Description}
	}
	return out
}

// ClassifyAction maps a GitHub-bound HTTP request to the policy action that
// authorizes it. Two protocols share github.com:
//
//   - **git smart-HTTP** — the protocol git speaks over HTTPS. A clone/fetch
//     uses git-upload-pack (read); a push uses git-receive-pack (write). The
//     service is named in the URL: `/{owner}/{repo}.git/info/refs?service=...`
//     for the initial discovery, then `POST /{owner}/{repo}.git/git-upload-pack`
//     or `/git-receive-pack` for the pack transfer itself.
//   - **REST / GraphQL** — `api.github.com` (or `github.com/api/v3` on
//     Enterprise). Verb-based: GET → read, POST/PATCH/PUT/DELETE → write.
//
// The host is not inspected here — connection lookup already keyed on it; this
// classifier runs only once a request has been routed to this connection.
func (a *Adapter) ClassifyAction(req *http.Request) (string, error) {
	if req == nil || req.URL == nil {
		return "", credentials.ErrUnsupportedAction
	}

	if action, ok := classifyGitSmartHTTP(req); ok {
		return action, nil
	}
	return classifyREST(req.Method)
}

// classifyGitSmartHTTP recognizes the two well-known git smart-HTTP shapes:
//
//	GET  /{owner}/{repo}.git/info/refs?service=git-upload-pack    → read
//	GET  /{owner}/{repo}.git/info/refs?service=git-receive-pack   → write
//	POST /{owner}/{repo}.git/git-upload-pack                      → read
//	POST /{owner}/{repo}.git/git-receive-pack                     → write
//
// Returns (action, true) on a match, ("", false) otherwise so the caller can
// fall through to REST classification.
func classifyGitSmartHTTP(req *http.Request) (string, bool) {
	path := req.URL.Path

	if strings.HasSuffix(path, "/info/refs") {
		switch req.URL.Query().Get("service") {
		case "git-upload-pack":
			return ActionRepoRead, true
		case "git-receive-pack":
			return ActionRepoWrite, true
		}
		return "", false
	}

	switch {
	case strings.HasSuffix(path, "/git-upload-pack"):
		return ActionRepoRead, true
	case strings.HasSuffix(path, "/git-receive-pack"):
		return ActionRepoWrite, true
	}
	return "", false
}

// classifyREST applies HTTP-verb-based mapping for the GitHub REST and GraphQL
// surfaces. GraphQL is always POST, so it always classifies as a write — that
// is intentionally conservative; finer-grained GraphQL classification would
// require parsing the query and is deferred until there is a real need.
func classifyREST(method string) (string, error) {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return ActionRepoRead, nil
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return ActionRepoWrite, nil
	default:
		return "", credentials.ErrUnsupportedAction
	}
}

// Resolve returns a cached, or freshly minted, installation token formatted as
// the git smart-HTTP Basic credential. The raw token is also exposed on the
// returned Credential so the gateway can reformat it as a Bearer for REST calls.
func (a *Adapter) Resolve(ctx context.Context, conn credentials.Connection, id credentials.CallerIdentity) (credentials.Credential, error) {
	var cfg config
	if err := json.Unmarshal(conn.Config, &cfg); err != nil {
		return credentials.Credential{}, fmt.Errorf("%w: github_app config: %w", credentials.ErrInvalidConfig, err)
	}
	if cfg.AppID == "" || cfg.InstallationID == 0 {
		return credentials.Credential{}, fmt.Errorf("%w: github_app requires app_id and installation_id", credentials.ErrInvalidConfig)
	}

	cacheKey := a.cacheKey(conn.ID, cfg)
	if cred, ok := a.cached(cacheKey); ok {
		return cred, nil
	}

	pem, err := a.secrets.Resolve(ctx, conn.SecretRef)
	if err != nil {
		return credentials.Credential{}, fmt.Errorf("resolve github app secret: %w", err)
	}
	appJWT, err := signAppJWT(cfg.AppID, pem, a.now())
	if err != nil {
		return credentials.Credential{}, fmt.Errorf("sign github app jwt: %w", err)
	}

	tok, err := a.api.CreateInstallationToken(ctx, appJWT, cfg.InstallationID, ghclient.InstallationTokenRequest{
		Repositories: cfg.Repositories,
		Permissions:  cfg.Permissions,
	})
	if err != nil {
		return credentials.Credential{}, fmt.Errorf("create installation token: %w", err)
	}

	cred := credentials.Credential{
		Token:     tok.Token,
		Header:    "Authorization",
		Value:     basicAuth("x-access-token", tok.Token),
		ExpiresAt: tok.ExpiresAt,
	}
	a.store(cacheKey, cred)

	a.logger.Info(ctx, "minted github installation token",
		logging.Field{Key: "connection", Value: conn.Name},
		logging.Field{Key: "tenant_id", Value: id.TenantID},
		logging.Field{Key: "run_id", Value: id.RunID},
		logging.Field{Key: "expires_at", Value: tok.ExpiresAt.UTC().Format(time.RFC3339)},
	)
	return cred, nil
}

func (a *Adapter) cached(key string) (credentials.Credential, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	cred, ok := a.cache[key]
	if !ok {
		return credentials.Credential{}, false
	}
	if a.now().After(cred.ExpiresAt.Add(-cacheSafetyMargin)) {
		delete(a.cache, key)
		return credentials.Credential{}, false
	}
	return cred, true
}

func (a *Adapter) store(key string, cred credentials.Credential) {
	a.mu.Lock()
	defer a.mu.Unlock()
	// Bound growth: once the cache crosses the threshold, sweep out every entry
	// already past its safety margin before inserting. Live entries are bounded by
	// real usage (connections × permission sets); this stops dead keys lingering.
	if len(a.cache) >= cacheEvictThreshold {
		now := a.now()
		for k, c := range a.cache {
			if now.After(c.ExpiresAt.Add(-cacheSafetyMargin)) {
				delete(a.cache, k)
			}
		}
	}
	a.cache[key] = cred
}

// cacheKey scopes a cached token to the connection and the exact repo/permission
// set requested, so a narrower request never reuses a broader token.
func (a *Adapter) cacheKey(connID string, cfg config) string {
	repos := append([]string(nil), cfg.Repositories...)
	sort.Strings(repos)
	perms := make([]string, 0, len(cfg.Permissions))
	for k, v := range cfg.Permissions {
		perms = append(perms, k+"="+v)
	}
	sort.Strings(perms)
	return strings.Join([]string{connID, strings.Join(repos, ","), strings.Join(perms, ",")}, "|")
}

// signAppJWT signs a GitHub App authentication JWT (RS256) with the App's
// private key, used to mint installation tokens.
func signAppJWT(appID string, privateKeyPEM []byte, now time.Time) (string, error) {
	key, err := jwt.ParseRSAPrivateKeyFromPEM(privateKeyPEM)
	if err != nil {
		return "", fmt.Errorf("parse rsa private key: %w", err)
	}
	claims := jwt.RegisteredClaims{
		Issuer:    appID,
		IssuedAt:  jwt.NewNumericDate(now.Add(-appJWTSkew)),
		ExpiresAt: jwt.NewNumericDate(now.Add(appJWTTTL)),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(key)
	if err != nil {
		return "", fmt.Errorf("sign github app jwt: %w", err)
	}
	return signed, nil
}

// basicAuth returns an HTTP Basic credential value for user:pass. git smart-HTTP
// authenticates a GitHub App installation token as user "x-access-token".
func basicAuth(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}
