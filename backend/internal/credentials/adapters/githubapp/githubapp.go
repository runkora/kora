// Package githubapp implements the credential adapter for GitHub App
// installation tokens. The agent acts as the installed GitHub App (bot
// authority); the egress gateway calls Mint to obtain a short-lived,
// repository- and permission-scoped installation token, formatted as the git
// smart-HTTP Basic credential. The long-lived App private key never leaves this
// process.
//
// Classify is category-scoped: it parses the request's host family and
// path into a route-family category (contents, issues, pulls, actions, checks,
// admin, metadata) combined with a read/write verb class, plus the repository
// qualifier when the path names one. GraphQL requests are classified by the
// fields they select (graphql.go). Resolve then mints the installation token
// narrowed to exactly the classified categories' permissions and the request's
// repository. See docs/adr/2026-07-22-github-connection-category-scoped-actions.md,
// docs/adr/2026-07-22-connection-scope-qualifiers.md and
// docs/adr/2026-09-29-github-graphql-field-classification.md.
//
// Each connection-type lives in its own subpackage so adding a new external
// service (AWS STS, OAuth2 client-credentials, API-key SaaS) is a new directory
// behind the same credentials.Adapter interface, never a change to
// the parent package. See docs/adr/2026-06-06-jit-credential-brokering-egress-gateway.md.
package githubapp

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
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
	AppID          string `json:"app_id"`
	InstallationID int64  `json:"installation_id"`
	// Owner is the account (org or user) the App installation is bound to.
	// When declared, Classify verifies request paths against it and
	// denies foreign-owner paths outright; when empty (legacy connections),
	// no repository qualifier is ever extracted, so only unqualified grants
	// match, which is the pre-qualifier behavior.
	Owner string `json:"owner,omitempty"`
	// Permissions is the optional permission ceiling. When set, derived
	// per-category permission sets are intersected with it, and the repo:*
	// umbrella mints it verbatim.
	Permissions map[string]string `json:"permissions,omitempty"`
}

// API is the subset of the GitHub client this adapter depends on, declared as
// an interface so the mint logic is unit-testable without a live API.
type API interface {
	CreateInstallationToken(ctx context.Context, appJWT string, installationID int64, body ghclient.InstallationTokenRequest) (ghclient.InstallationToken, error)
	ListInstallationRepositories(ctx context.Context, installationToken string) ([]string, error)
	GetInstallationPermissions(ctx context.Context, appJWT string, installationID int64) (map[string]string, error)
}

// instPermsCacheTTL is how long the installation's permission set is cached
// before re-fetching from GitHub. The set changes only when an admin modifies
// the App installation; a short TTL avoids thundering-herd on first use while
// keeping the adapter responsive to permission changes within minutes.
const instPermsCacheTTL = 5 * time.Minute

// cachedInstPerms holds a fetched installation permission set and its expiry.
type cachedInstPerms struct {
	perms     map[string]string
	expiresAt time.Time
}

// Adapter mints short-lived GitHub App installation tokens. Tokens are scoped to
// the classified category's permissions and the request's repository, and cached
// until shortly before expiry. Adapter implements credentials.Adapter.
type Adapter struct {
	api     API
	secrets credentials.SecretResolver
	logger  logging.Logger
	now     func() time.Time

	mu             sync.Mutex
	cache          map[string]credentials.Credential
	instPermsCache map[int64]cachedInstPerms
}

// New constructs an Adapter backed by the given GitHub client and secret
// resolver.
func New(api API, secrets credentials.SecretResolver, logger logging.Logger) *Adapter {
	return &Adapter{
		api:            api,
		secrets:        secrets,
		logger:         logger,
		now:            time.Now,
		cache:          make(map[string]credentials.Credential),
		instPermsCache: make(map[int64]cachedInstPerms),
	}
}

// Type reports the adapter type served.
func (a *Adapter) Type() credentials.AdapterType { return credentials.AdapterGitHubApp }

// Action constants returned by Classify. Sourced from the shared adapter
// metadata table so the classifier and the published vocabulary cannot drift.
// They combine with the connection name (and optional repository qualifier) to
// form "connection:<name>:<category>[:<repo>]:<verb>".
const (
	ActionRepoRead      = platformconns.ActionGitHubRepoRead
	ActionRepoWrite     = platformconns.ActionGitHubRepoWrite
	ActionMetadataRead  = platformconns.ActionGitHubMetadataRead
	ActionContentsRead  = platformconns.ActionGitHubContentsRead
	ActionContentsWrite = platformconns.ActionGitHubContentsWrite
)

// Actions enumerates every action Classify can return. The shared
// metadata table is the one source of truth for the vocabulary, and it is the
// same table orbit-agents reads to derive auto-egress.
func (a *Adapter) Actions() []credentials.ActionDeclaration {
	return credentials.DeclaredActions(platformconns.AdapterGitHubApp)
}

// Open binds the adapter to one connection, decoding and validating its
// configuration once so neither Classify nor Mint revisits it.
func (a *Adapter) Open(conn credentials.Connection) (credentials.Connector, error) {
	var cfg config
	if err := credentials.DecodeConfig(conn, &cfg); err != nil {
		return nil, fmt.Errorf("github_app config: %w", err)
	}
	if cfg.AppID == "" || cfg.InstallationID == 0 {
		return nil, fmt.Errorf("%w: github_app requires app_id and installation_id", credentials.ErrInvalidConfig)
	}
	return &connector{adapter: a, conn: conn, cfg: cfg}, nil
}

// connector is the adapter bound to one connection. It holds the decoded
// config, so the request path never sees the connection's raw JSON.
type connector struct {
	adapter *Adapter
	conn    credentials.Connection
	cfg     config
}

// Classify maps a GitHub-bound request to the action and repository that
// authorize it, across the four surfaces sharing the host family: git
// smart-HTTP, the REST and GraphQL API, the content hosts, and a
// verb-classified catch-all for everything else.
//
// A repo-addressed path naming an owner other than the connection's is denied
// outright. GitHub serves foreign public repositories to any token, so
// qualifying by bare name without checking the owner would let a name
// collision widen a repository-scoped grant. Content-host paths are the
// exception: their shapes are not always owner-addressed, so one that does not
// match classifies unqualified instead.
func (c *connector) Classify(req *http.Request) (credentials.Classification, error) {
	if req == nil || req.URL == nil {
		return credentials.Classification{}, credentials.ErrUnsupportedAction
	}

	owner := normalizeName(c.cfg.Owner)

	if cls, ok, err := classifyGitSmartHTTP(req, owner); ok || err != nil {
		return cls, err
	}

	host := requestHost(req)
	path := req.URL.Path

	// The API surface: api.github.com serves paths at the root; a GitHub
	// Enterprise appliance serves the same API under /api/v3 and GraphQL at
	// /api/graphql on the shared host.
	switch {
	case host == "api.github.com":
		return classifyAPI(req, path, owner)
	case path == "/api/v3" || strings.HasPrefix(path, "/api/v3/"):
		return classifyAPI(req, strings.TrimPrefix(path, "/api/v3"), owner)
	case path == "/api/graphql":
		return classifyAPI(req, "/graphql", owner)
	}

	if host == "codeload.github.com" || strings.HasSuffix(host, ".githubusercontent.com") {
		return classifyContentHost(req.Method, path, owner)
	}

	// Catch-all: verb-classified and unqualified, the legacy behavior for
	// anything not recognized above.
	verb, err := verbClass(req.Method)
	if err != nil {
		return credentials.Classification{}, err
	}
	return credentials.Classification{Action: catRepo + ":" + verb}, nil
}

// classifyGitSmartHTTP recognizes the two well-known git smart-HTTP shapes:
//
//	GET  /{owner}/{repo}(.git)/info/refs?service=git-upload-pack    → contents:read
//	GET  /{owner}/{repo}(.git)/info/refs?service=git-receive-pack   → contents:write
//	POST /{owner}/{repo}(.git)/git-upload-pack                      → contents:read
//	POST /{owner}/{repo}(.git)/git-receive-pack                     → contents:write
//
// Returns (cls, true, nil) on a match, denying with ErrUnsupportedAction when
// the connection's declared owner does not match the path's owner segment.
// ok=false means the path is not a git smart-HTTP shape, so fall through.
func classifyGitSmartHTTP(req *http.Request, owner string) (credentials.Classification, bool, error) {
	path := req.URL.Path

	var action string
	switch {
	case strings.HasSuffix(path, "/info/refs"):
		switch req.URL.Query().Get("service") {
		case "git-upload-pack":
			action = ActionContentsRead
		case "git-receive-pack":
			action = ActionContentsWrite
		default:
			return credentials.Classification{}, false, nil
		}
	case strings.HasSuffix(path, "/git-upload-pack"):
		action = ActionContentsRead
	case strings.HasSuffix(path, "/git-receive-pack"):
		action = ActionContentsWrite
	default:
		return credentials.Classification{}, false, nil
	}

	qualifier := ""
	if segs := pathSegments(path); len(segs) >= 3 && owner != "" {
		if normalizeName(segs[0]) != owner {
			return credentials.Classification{}, true, credentials.ErrUnsupportedAction
		}
		qualifier = normalizeName(strings.TrimSuffix(segs[1], ".git"))
	}
	return credentials.Classification{Action: action, Qualifier: qualifier}, true, nil
}

// Verb classes and category names, shared by the classifier and the minting
// permission table.
const (
	verbRead  = "read"
	verbWrite = "write"

	catMetadata = "metadata"
	catContents = "contents"
	catIssues   = "issues"
	catPulls    = "pulls"
	catActions  = "actions"
	catChecks   = "checks"
	catAdmin    = "admin"
	catRepo     = "repo" // catch-all + legacy umbrella
)

// classifyAPI maps a REST/GraphQL API path (host prefix already stripped) to a
// category action plus repository qualifier.
func classifyAPI(req *http.Request, path, owner string) (credentials.Classification, error) {
	method := req.Method
	verb, err := verbClass(method)
	if err != nil {
		return credentials.Classification{}, err
	}
	segs := pathSegments(path)
	if len(segs) == 0 {
		// API root discovery.
		if verb == verbRead {
			return credentials.Classification{Action: ActionMetadataRead}, nil
		}
		return credentials.Classification{Action: ActionRepoWrite}, nil
	}

	switch segs[0] {
	case "graphql":
		return classifyGraphQL(req, owner)
	case "rate_limit", "user", "installation", "app", "meta", "octocat":
		// Identity/limit/discovery endpoints most GitHub clients probe before
		// real work. The metadata baseline covers them.
		if verb == verbRead {
			return credentials.Classification{Action: ActionMetadataRead}, nil
		}
		return credentials.Classification{Action: ActionRepoWrite}, nil
	case "repos":
		if len(segs) < 3 {
			return credentials.Classification{Action: catRepo + ":" + verb}, nil
		}
		qualifier := ""
		if owner != "" {
			if normalizeName(segs[1]) != owner {
				return credentials.Classification{}, credentials.ErrUnsupportedAction
			}
			qualifier = normalizeName(segs[2])
		}
		return classifyRepoPath(verb, segs[3:], qualifier), nil
	default:
		// /search/*, /orgs/*, and every other root: the verb-classified
		// catch-all, unqualified.
		return credentials.Classification{Action: catRepo + ":" + verb}, nil
	}
}

// classifyRepoPath maps the sub-path under /repos/{owner}/{repo}/ to a category
// action. sub is the path segments after the repository; qualifier is the
// (already owner-verified) repository name, empty when the connection declares
// no owner.
func classifyRepoPath(verb string, sub []string, qualifier string) credentials.Classification {
	if len(sub) == 0 {
		// The bare repository object: reads are metadata; PATCH edits settings
		// and DELETE deletes the repository, so administration.
		if verb == verbRead {
			return credentials.Classification{Action: ActionMetadataRead, Qualifier: qualifier}
		}
		return credentials.Classification{Action: catAdmin + ":" + verbWrite, Qualifier: qualifier}
	}
	cls := credentials.Classification{Action: repoSubCategory(sub) + ":" + verb, Qualifier: qualifier}
	if sharedIssueNumberRoute(sub) {
		cls.Alternates = []string{catPulls + ":" + verb}
	}
	return cls
}

// sharedIssueNumberRoute reports whether sub addresses a single issue or pull
// request by number, or one comment or event by ID, along with anything beneath
// them. GitHub numbers issues and pull requests in one sequence and serves both
// under /issues, but checks the pull_requests permission when the target is a
// pull request, so the path alone cannot say which permission the call needs.
// The repository-wide /issues, /issues/comments and /issues/events collections
// are excluded: they address no single target.
func sharedIssueNumberRoute(sub []string) bool {
	if len(sub) < 2 || sub[0] != "issues" {
		return false
	}
	if sub[1] == "comments" || sub[1] == "events" {
		return len(sub) >= 3 && isNumeric(sub[2])
	}
	return isNumeric(sub[1])
}

// isNumeric reports whether s is a base-10 unsigned integer.
func isNumeric(s string) bool {
	_, err := strconv.ParseUint(s, 10, 64)
	return err == nil
}

// repoSubCategories maps unconditional first segments under
// /repos/{owner}/{repo}/ to their category. Segments whose category depends on
// deeper path shape (commits, branches, pulls, actions, dependabot) are handled
// in repoSubCategory.
var repoSubCategories = map[string]string{
	"contents": catContents, "git": catContents, "tags": catContents,
	"compare": catContents, "releases": catContents,

	"issues": catIssues, "labels": catIssues, "milestones": catIssues, "assignees": catIssues,

	"check-runs": catChecks, "check-suites": catChecks, "statuses": catChecks,

	"hooks": catAdmin, "keys": catAdmin, "collaborators": catAdmin,
}

// repoSubCategory resolves the category for a non-empty sub-path under
// /repos/{owner}/{repo}/. Unlisted route families land in the repo catch-all.
func repoSubCategory(sub []string) string {
	if cat, ok := repoSubCategories[sub[0]]; ok {
		return cat
	}
	if resolve, ok := repoSubResolvers[sub[0]]; ok {
		return resolve(sub)
	}
	return catRepo
}

// repoSubResolvers handles the route families whose category depends on deeper
// path shape.
var repoSubResolvers = map[string]func(sub []string) string{
	// /commits/{ref}/{check-runs|check-suites|status|statuses} is the CI
	// surface, not the git-data surface.
	"commits": func(sub []string) string {
		if len(sub) >= 3 {
			switch sub[2] {
			case "check-runs", "check-suites", "status", "statuses":
				return catChecks
			}
		}
		return catContents
	},
	"branches": func(sub []string) string {
		if len(sub) >= 3 && sub[2] == "protection" {
			return catAdmin
		}
		return catContents
	},
	// Merging mutates contents; GitHub itself demands contents:write for it.
	"pulls": func(sub []string) string {
		if len(sub) >= 3 && sub[2] == "merge" {
			return catContents
		}
		return catPulls
	},
	// Actions secrets and variables are credential-adjacent administration, so
	// actions:* can be granted without them.
	"actions": func(sub []string) string {
		if len(sub) >= 2 && (sub[1] == "secrets" || sub[1] == "variables") {
			return catAdmin
		}
		return catActions
	},
	"dependabot": func(sub []string) string {
		if len(sub) >= 2 && sub[1] == "secrets" {
			return catAdmin
		}
		return catRepo
	},
}

// classifyContentHost handles the read-only download family
// (codeload.github.com, *.githubusercontent.com). Only reads are legitimate
// there; the repository qualifier is extracted when the path is owner-addressed
// (/{owner}/{repo}/…) and the owner matches. Opaque shapes (LFS object URLs)
// classify unqualified rather than denying, satisfiable only by an unqualified
// contents:read grant, per the qualifier pattern's fail-closed rule.
func classifyContentHost(method, path, owner string) (credentials.Classification, error) {
	verb, err := verbClass(method)
	if err != nil || verb != verbRead {
		return credentials.Classification{}, credentials.ErrUnsupportedAction
	}
	qualifier := ""
	if segs := pathSegments(path); owner != "" && len(segs) >= 2 && normalizeName(segs[0]) == owner {
		qualifier = normalizeName(segs[1])
	}
	return credentials.Classification{Action: ActionContentsRead, Qualifier: qualifier}, nil
}

// verbClass maps an HTTP method to the read/write verb class.
func verbClass(method string) (string, error) {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return verbRead, nil
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return verbWrite, nil
	default:
		return "", credentials.ErrUnsupportedAction
	}
}

// requestHost returns the request's destination host, without any port.
func requestHost(req *http.Request) string {
	h := req.Host
	if h == "" {
		h = req.URL.Host
	}
	h = strings.ToLower(strings.TrimSpace(h))
	if i := strings.LastIndex(h, ":"); i > 0 && !strings.Contains(h[i+1:], "]") {
		h = h[:i]
	}
	return h
}

// pathSegments splits a URL path into non-empty segments.
func pathSegments(path string) []string {
	var out []string
	for s := range strings.SplitSeq(path, "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// normalizeName lowercases and trims a GitHub owner or repository name (GitHub
// treats both case-insensitively).
func normalizeName(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// permissionLevels ranks GitHub App permission levels for ceiling intersection.
var permissionLevels = map[string]int{verbRead: 1, verbWrite: 2, "admin": 3}

// categoryPermissionKeys maps each category to the installation-token permission
// keys it needs. The level minted is the action's verb class (metadata is always
// read). The umbrella (repo:*) uses this map only for explicit per-category
// actions; the umbrella itself intersects with the installation's actual
// granted permissions rather than deriving a static union.
var categoryPermissionKeys = map[string][]string{
	"metadata": {"metadata"},
	"contents": {"contents"},
	"issues":   {"issues"},
	"pulls":    {"pull_requests"},
	"actions":  {"actions"},
	"checks":   {"checks", "statuses"},
	"admin":    {"administration", "repository_hooks", "secrets", "dependabot_secrets"},
}

// permissionsFor derives the installation-token permission set for the
// classified action, intersected with the connection's declared ceiling.
// installationPerms is the App installation's actual granted permissions (from
// GetInstallationPermissions); it is used as the intersection floor for the
// repo:* umbrella when no explicit ceiling is declared, so the umbrella can
// never request a permission the installation does not hold. For non-umbrella
// categories (and umbrella with a declared ceiling) installationPerms is unused.
func permissionsFor(action string, ceiling map[string]string, installationPerms map[string]string) map[string]string {
	category, verb := splitAction(action)

	derived := map[string]string{}
	switch category {
	case catMetadata:
		derived[catMetadata] = verbRead
	case catContents, catIssues, catPulls, catActions, catChecks, catAdmin:
		for _, key := range categoryPermissionKeys[category] {
			derived[key] = verb
		}
	default:
		// The repo catch-all/umbrella (and, defensively, anything unrecognized):
		// when a ceiling is declared, mint it verbatim. Otherwise, mint the
		// intersection of the action's verb class with the installation's actual
		// granted permissions so the umbrella never requests a level the
		// installation does not hold (which would 422).
		//
		// When installationPerms is nil or empty (no permissions fetched, only
		// possible in unit tests that construct permissionsFor directly, or if
		// the API returned an empty map), we return only metadata:read: the
		// minimum implicit grant on every installation token. This is safe and
		// explicit rather than relying on the post-loop metadata floor.
		if len(ceiling) > 0 {
			return maps.Clone(ceiling)
		}
		if len(installationPerms) == 0 {
			return map[string]string{catMetadata: verbRead}
		}
		for key, grantedLevel := range installationPerms {
			// Skip permissions with unknown level strings: permissionLevels maps
			// only "read", "write", "admin". An unknown value maps to 0, which
			// would silently pass the cap check and let an unrecognized level
			// through. Omitting it is the safe default.
			if permissionLevels[grantedLevel] == 0 {
				continue
			}
			level := verb
			if key == catMetadata {
				level = verbRead
			}
			// Cap at the installation's actual grant level.
			if permissionLevels[grantedLevel] < permissionLevels[level] {
				level = grantedLevel
			}
			derived[key] = level
		}
	}

	// Pull-request operations also need read access to repository contents:
	// creating or editing a PR reads the head/base branch refs, and GitHub
	// rejects the call with 422 "not all refs are readable" when the token lacks
	// contents:read. This is a read floor scoped to the same repository
	// qualifier, so it never widens write access; it is still bounded by the
	// connection ceiling below.
	if category == catPulls && permissionLevels[derived["contents"]] < permissionLevels[verbRead] {
		derived["contents"] = verbRead
	}

	if _, ok := derived[catMetadata]; !ok {
		derived[catMetadata] = verbRead
	}

	applyCeiling(derived, ceiling)
	return derived
}

// applyCeiling intersects the derived permission set with the connection's
// declared ceiling in place: permissions absent from the ceiling are dropped
// (except the implicitly-granted metadata read), and any level above the
// ceiling is lowered to it. A nil/empty ceiling means "no ceiling declared" and
// leaves the set unchanged.
func applyCeiling(derived, ceiling map[string]string) {
	if len(ceiling) == 0 {
		return
	}
	for key, level := range derived {
		ceilLevel, ok := ceiling[key]
		switch {
		case !ok && key == catMetadata:
			// GitHub grants metadata read on every installation token
			// implicitly; keep it even when the ceiling omits it.
		case !ok:
			delete(derived, key)
		case permissionLevels[ceilLevel] < permissionLevels[level]:
			derived[key] = ceilLevel
		}
	}
}

// needsInstallationPerms reports whether any action is the repo:* umbrella,
// whose permission set is derived from the installation's actual grants.
func needsInstallationPerms(actions []string) bool {
	for _, a := range actions {
		if category, _ := splitAction(a); category == catRepo {
			return true
		}
	}
	return false
}

// splitAction splits an action fragment at its last colon into category and
// verb. A fragment with no colon yields (fragment, "read") defensively.
func splitAction(action string) (category, verb string) {
	if i := strings.LastIndex(action, ":"); i > 0 {
		return action[:i], action[i+1:]
	}
	return action, verbRead
}

// reposFor derives the mint-time repository narrowing from the request's
// classified repository. An unqualified call narrows to nothing, so the token
// covers the installation: which repositories an agent may reach is expressed
// by its grant, and the installation is the outer ceiling on all of them.
func reposFor(qualifier string) []string {
	if qualifier == "" {
		return nil
	}
	return []string{qualifier}
}

// Resolve returns a cached, or freshly minted, installation token formatted as
// the git smart-HTTP Basic credential, narrowed to the classified category's
// permissions and the request's repository. The raw token is also exposed on
// the returned Credential so the gateway can reformat it as a Bearer for REST.
// installationPermissions returns the App installation's permission set,
// fetching from GitHub if the cached entry has expired. Results are cached for
// instPermsCacheTTL to bound API calls across concurrent umbrella mints.
// Double-check locking: the cache is read under the lock; on miss, the API call
// runs unlocked, then re-checks under the lock before writing, so concurrent
// callers that both see a stale entry each make the API call but the last writer
// wins with a consistent result.
func (a *Adapter) installationPermissions(ctx context.Context, appJWT string, installationID int64) (map[string]string, error) {
	a.mu.Lock()
	if cached, ok := a.instPermsCache[installationID]; ok && a.now().Before(cached.expiresAt) {
		perms := maps.Clone(cached.perms)
		a.mu.Unlock()
		return perms, nil
	}
	a.mu.Unlock()

	perms, err := a.api.GetInstallationPermissions(ctx, appJWT, installationID)
	if err != nil {
		return nil, err
	}

	a.mu.Lock()
	// Double-check: another goroutine may have populated the cache while we
	// were fetching. Prefer its entry if it arrived first and is still valid.
	if cached, ok := a.instPermsCache[installationID]; ok && a.now().Before(cached.expiresAt) {
		perms = maps.Clone(cached.perms)
		a.mu.Unlock()
		return perms, nil
	}
	a.instPermsCache[installationID] = cachedInstPerms{
		perms:     maps.Clone(perms),
		expiresAt: a.now().Add(instPermsCacheTTL),
	}
	a.mu.Unlock()

	return perms, nil
}

func (c *connector) Mint(ctx context.Context, id credentials.CallerIdentity) (credentials.Credential, error) {
	a, conn, cfg := c.adapter, c.conn, c.cfg

	repos := reposFor(id.Qualifier)

	// For the repo:* umbrella with no declared ceiling, derive the permission set
	// by intersecting with the App installation's actual grants. This prevents
	// the umbrella from requesting a permission the installation does not hold,
	// which would cause GitHub to reject the entire token mint with 422.
	// The installation's permissions are cached (instPermsCacheTTL) so the API
	// call is amortized across mints; the App JWT is only signed when the cache
	// is cold.
	actions := append([]string{id.Action}, id.AlsoAuthorized...)
	var installationPerms map[string]string
	if needsInstallationPerms(actions) && len(cfg.Permissions) == 0 {
		pem, err := a.secrets.Resolve(ctx, conn.SecretRef)
		if err != nil {
			return credentials.Credential{}, fmt.Errorf("resolve github app secret: %w", err)
		}
		appJWT, err := signAppJWT(cfg.AppID, pem, a.now())
		if err != nil {
			return credentials.Credential{}, fmt.Errorf("sign github app jwt: %w", err)
		}
		installationPerms, err = a.installationPermissions(ctx, appJWT, cfg.InstallationID)
		if err != nil {
			return credentials.Credential{}, fmt.Errorf("fetch installation permissions: %w", err)
		}
	}

	perms := permissionsFor(id.Action, cfg.Permissions, installationPerms)
	// Alternates the grant also holds widen the token, each at no more than its
	// own derived level, so the credential never exceeds what was authorized.
	for _, alt := range id.AlsoAuthorized {
		for key, level := range permissionsFor(alt, cfg.Permissions, installationPerms) {
			if permissionLevels[level] > permissionLevels[perms[key]] {
				perms[key] = level
			}
		}
	}

	cacheKey := a.cacheKey(conn.ID, repos, perms)
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
		Repositories: repos,
		Permissions:  perms,
	})
	if err != nil {
		// A 422 permissions rejection is the App installation's own ceiling.
		// Orbit authorized the action, but the App is not granted this
		// permission level. Translate to the adapter-agnostic sentinel so the
		// broker returns a forbidden, not a 500.
		if errors.Is(err, ghclient.ErrInstallationPermissions) {
			return credentials.Credential{}, fmt.Errorf("%w: %s on %q: %w",
				credentials.ErrCredentialScopeRejected, id.Action, id.Qualifier, err)
		}
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
		logging.Field{Key: "action", Value: id.Action},
		logging.Field{Key: "also_authorized", Value: strings.Join(id.AlsoAuthorized, ",")},
		logging.Field{Key: "repository", Value: id.Qualifier},
		logging.Field{Key: "expires_at", Value: tok.ExpiresAt.UTC().Format(time.RFC3339)},
	)
	return cred, nil
}

// ListQualifierValues enumerates the installation's repository names for the
// connector attachment picker (the egress gateway's optional
// QualifierEnumerator capability). It resolves a metadata-scoped installation
// token, reusing the cache, and lists the installation's repositories.
func (c *connector) ListQualifierValues(ctx context.Context) ([]string, error) {
	cred, err := c.Mint(ctx, credentials.CallerIdentity{
		TenantID: c.conn.TenantID,
		Action:   ActionMetadataRead,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve metadata token for repository listing: %w", err)
	}
	repos, err := c.adapter.api.ListInstallationRepositories(ctx, cred.Token)
	if err != nil {
		return nil, fmt.Errorf("list installation repositories: %w", err)
	}
	return repos, nil
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
	// real usage (connections × repositories × categories); this stops dead keys
	// lingering.
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

// cacheKey scopes a cached token to the connection and the exact derived
// repository/permission set minted, so a narrower request never reuses a
// broader token.
func (a *Adapter) cacheKey(connID string, repos []string, perms map[string]string) string {
	rs := slices.Sorted(slices.Values(repos))
	ps := make([]string, 0, len(perms))
	for k, v := range perms {
		ps = append(ps, k+"="+v)
	}
	slices.Sort(ps)
	return strings.Join([]string{connID, strings.Join(rs, ","), strings.Join(ps, ",")}, "|")
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
