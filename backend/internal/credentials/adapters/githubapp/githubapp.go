// Package githubapp implements the credential adapter for GitHub App
// installation tokens. The agent acts as the installed GitHub App (bot
// authority); the egress gateway calls Adapter.Resolve to obtain a short-lived,
// repository- and permission-scoped installation token, formatted as the git
// smart-HTTP Basic credential. The long-lived App private key never leaves this
// process.
//
// ClassifyAction is category-scoped: it parses the request's host family and
// path into a route-family category (contents, issues, pulls, actions, checks,
// admin, metadata) combined with a read/write verb class, plus the repository
// qualifier when the path names one. Resolve then mints the installation token
// narrowed to exactly the classified category's permissions and the request's
// repository. See docs/adr/2026-07-22-github-connection-category-scoped-actions.md
// and docs/adr/2026-07-22-connection-scope-qualifiers.md.
//
// Each connection-type lives in its own subpackage so adding a new external
// service (AWS STS, OAuth2 client-credentials, API-key SaaS) is a new directory
// behind the same credentials.CredentialAdapter interface, never a change to
// the parent package. See docs/adr/2026-06-06-jit-credential-brokering-egress-gateway.md.
package githubapp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"regexp"
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
	AppID          string `json:"app_id"`
	InstallationID int64  `json:"installation_id"`
	// Owner is the account (org or user) the App installation is bound to.
	// When declared, ClassifyAction verifies request paths against it and
	// denies foreign-owner paths outright; when empty (legacy connections),
	// no repository qualifier is ever extracted, so only unqualified grants
	// match — the pre-qualifier semantics.
	Owner string `json:"owner,omitempty"`
	// Repositories is the optional mint-time repository ceiling; the broker
	// additionally enforces it as the qualifier ceiling at the gate.
	Repositories []string `json:"repositories,omitempty"`
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
}

// Adapter mints short-lived GitHub App installation tokens. Tokens are scoped to
// the classified category's permissions and the request's repository, and cached
// until shortly before expiry. Adapter implements credentials.CredentialAdapter.
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
// They combine with the connection name (and optional repository qualifier) to
// form "connection:<name>:<category>[:<repo>]:<verb>".
const (
	ActionRepoRead      = platformconns.ActionGitHubRepoRead
	ActionRepoWrite     = platformconns.ActionGitHubRepoWrite
	ActionMetadataRead  = platformconns.ActionGitHubMetadataRead
	ActionContentsRead  = platformconns.ActionGitHubContentsRead
	ActionContentsWrite = platformconns.ActionGitHubContentsWrite
)

// Actions enumerates every action ClassifyAction can return, delegating to the
// shared metadata table (internal/platform/connections) so the
// connection:<name>:<action> vocabulary the egress gateway publishes has exactly
// one source of truth — the same table orbit-agents reads to derive auto-egress.
func (a *Adapter) Actions() []credentials.ActionDeclaration {
	metas := platformconns.Actions(platformconns.AdapterGitHubApp)
	out := make([]credentials.ActionDeclaration, len(metas))
	for i, m := range metas {
		out[i] = credentials.ActionDeclaration{Action: m.Action, Description: m.Description, Requires: m.Requires}
	}
	return out
}

// ClassifyAction maps a GitHub-bound HTTP request to the policy action and
// repository qualifier that authorize it. Four surfaces share the host family:
//
//  1. git smart-HTTP — clone/fetch (git-upload-pack) is contents:read, push
//     (git-receive-pack) is contents:write; the repository comes from the
//     /{owner}/{repo}(.git)/ path.
//  2. The REST/GraphQL API — api.github.com, or /api/v3 (+/api/graphql) on a
//     GitHub Enterprise appliance host. Route families under
//     /repos/{owner}/{repo}/… map to categories; the verb supplies read/write.
//  3. The content-host family — codeload.github.com, *.githubusercontent.com:
//     GET/HEAD only, contents:read.
//  4. Anything else — verb-classified into the repo:read/repo:write catch-all,
//     unqualified (the pre-category behavior).
//
// Owner verification: when the connection declares its account, any
// repo-addressed path naming a different owner is denied outright — GitHub
// serves foreign public repos to any token, so qualifying by bare name without
// verifying the owner would let name collisions widen a repo-scoped grant.
// Content-host paths are the exception: their shapes are not always
// owner-addressed (opaque object URLs), so a non-matching shape classifies
// unqualified instead — satisfiable only by an unqualified contents:read grant.
func (a *Adapter) ClassifyAction(req *http.Request, conn credentials.Connection) (credentials.Classification, error) {
	if req == nil || req.URL == nil {
		return credentials.Classification{}, credentials.ErrUnsupportedAction
	}

	var cfg config
	if len(conn.Config) > 0 {
		// A malformed config cannot declare an owner; Resolve rejects it later.
		_ = json.Unmarshal(conn.Config, &cfg)
	}
	owner := normalizeName(cfg.Owner)

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

	// Catch-all: verb-classified, unqualified — unchanged legacy behavior for
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
// ok=false means "not a git smart-HTTP shape — fall through".
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

// graphqlBodySizeLimit caps how much of a GraphQL request body is read for
// classification. Queries larger than this are treated as unparseable and fall
// back to the conservative repo:write default. 512 KiB covers any realistic
// query; requests larger than this are almost certainly bulk data, not queries.
const graphqlBodySizeLimit = 512 * 1024

// graphqlRequest is the JSON envelope GitHub accepts for GraphQL.
type graphqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

// classifyGraphQL parses the request body of a GraphQL call to distinguish
// queries (reads) from mutations (writes), and attempts best-effort repository
// qualifier extraction when the query or variables name an owner+repository.
//
// The body is always restored so the egress gateway can forward the original
// request byte-for-byte. If the body is absent, unreadable, too large, or
// unparseable the function fails closed: unqualified repo:write is returned,
// identical to the pre-fix behavior.
func classifyGraphQL(req *http.Request, owner string) (credentials.Classification, error) {
	fallback := credentials.Classification{Action: ActionRepoWrite}

	if req.Body == nil || req.Body == http.NoBody {
		return fallback, nil
	}

	original := req.Body
	limited := io.LimitReader(original, graphqlBodySizeLimit+1)
	buf, err := io.ReadAll(limited)

	if err != nil || int64(len(buf)) > graphqlBodySizeLimit {
		// Unreadable or oversized body: fail closed on classification, but still
		// forward the body WITHOUT truncation — the bytes already read (buf)
		// followed by whatever remains unread in the original stream. The wrapper
		// closes the original once forwarding is done, so nothing leaks. Leave
		// req.GetBody untouched: we must not install one that replays only buf.
		req.Body = &chainedBody{r: io.MultiReader(bytes.NewReader(buf), original), c: original}
		return fallback, nil
	}

	// Whole body is buffered (<= limit): the original stream is fully drained,
	// so close it (no leak) and serve byte-identical copies for forwarding and
	// any resend.
	_ = original.Close()
	req.Body = io.NopCloser(bytes.NewReader(buf))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(buf)), nil
	}

	var gql graphqlRequest
	if jsonErr := json.Unmarshal(buf, &gql); jsonErr != nil || gql.Query == "" {
		return fallback, nil
	}

	verb := graphqlVerb(gql.Query)
	action := catRepo + ":" + verb

	qualifier := ""
	if owner != "" {
		q, deny := graphqlRepoQualifier(gql.Query, gql.Variables, owner)
		if deny {
			return credentials.Classification{}, credentials.ErrUnsupportedAction
		}
		qualifier = q
	}

	return credentials.Classification{Action: action, Qualifier: qualifier}, nil
}

// graphqlVerb inspects the leading operation keyword of a GraphQL document and
// returns verbRead for queries (including the anonymous shorthand { … } and
// introspection) or verbWrite for mutations. All other documents (subscriptions,
// anything unrecognized) default to verbWrite so classification never downgrades
// a real write to a read.
func graphqlVerb(query string) string {
	t := strings.TrimSpace(query)
	// Anonymous shorthand: document starts with { — it's a query.
	if strings.HasPrefix(t, "{") {
		return verbRead
	}
	// Named operation: first token is the operation type keyword.
	word := firstWord(t)
	switch strings.ToLower(word) {
	case "query":
		return verbRead
	case "fragment":
		// Leading fragment definition(s): the operation type appears later in
		// the document. Only downgrade to read when there is no mutation
		// anywhere — never downgrade a real write to a read.
		if graphqlMutationRe.MatchString(t) {
			return verbWrite
		}
		return verbRead
	default:
		// "mutation", "subscription", or anything else → conservative write.
		return verbWrite
	}
}

// graphqlMutationRe matches a `mutation` operation keyword as a whole word.
// GraphQL keywords are case-sensitive (lowercase), so it does not fold case. A
// stray match (e.g. a field literally named "mutation") only over-classifies a
// read as a write, which fails closed and never downgrades a write.
var graphqlMutationRe = regexp.MustCompile(`\bmutation\b`)

// chainedBody adapts a chained io.Reader to an io.ReadCloser that closes the
// underlying request body on Close. It lets an oversized GraphQL request be
// forwarded intact (already-read bytes followed by the unread remainder)
// without leaking the original body reader.
type chainedBody struct {
	r io.Reader
	c io.Closer
}

func (b *chainedBody) Read(p []byte) (int, error) { return b.r.Read(p) }
func (b *chainedBody) Close() error               { return b.c.Close() }

// firstWord returns the first whitespace-delimited token of s.
func firstWord(s string) string {
	end := strings.IndexAny(s, " \t\n\r(")
	if end < 0 {
		return s
	}
	return s[:end]
}

// graphqlRepoQualifier attempts to extract owner+repository from a GraphQL
// query string and variables. It returns (qualifier, false) on success,
// ("", false) when no repository can be determined (safe unqualified read),
// and ("", true) when a repository is found but names a foreign owner (deny).
//
// Extraction is best-effort: it searches for the literal patterns that the
// GitHub CLI and most GitHub GraphQL clients use. It does not parse the query
// as a proper AST; a query that successfully hides its repository from this
// scanner is simply left unqualified — it must be satisfied by an unqualified
// grant, which is still correct and safe.
func graphqlRepoQualifier(query string, variables map[string]any, connOwner string) (qualifier string, deny bool) {
	repoOwner, repoName := "", ""

	// Try variables first: GitHub CLI passes { owner, name } (or { owner, repo }).
	if variables != nil {
		repoOwner = stringVar(variables, "owner")
		if n := stringVar(variables, "name"); n != "" {
			repoName = n
		} else {
			repoName = stringVar(variables, "repo")
		}
	}

	// Fall back to inline literal in the query text:
	// repository(owner: "x", name: "y") or repository(owner:"x",name:"y").
	if repoOwner == "" || repoName == "" {
		o, n := extractInlineOwnerName(query)
		if repoOwner == "" {
			repoOwner = o
		}
		if repoName == "" {
			repoName = n
		}
	}

	if repoOwner == "" || repoName == "" {
		// Repository cannot be determined — unqualified, not denied.
		return "", false
	}

	if normalizeName(repoOwner) != connOwner {
		return "", true
	}
	return normalizeName(repoName), false
}

// stringVar retrieves a string value from the variables map, returning "" when
// the key is absent or the value is not a string.
func stringVar(vars map[string]any, key string) string {
	v, ok := vars[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// extractInlineOwnerName searches for the literal GitHub GraphQL pattern
// repository(owner:"x", name:"y") (with optional whitespace around punctuation)
// within the query document and returns the owner and name values found, or
// empty strings when the pattern is absent.
func extractInlineOwnerName(query string) (owner, name string) {
	// Locate the "repository(" call site.
	const marker = "repository("
	idx := strings.Index(strings.ToLower(query), marker)
	if idx < 0 {
		return "", ""
	}
	// Work only within the argument list that follows.
	fragment := query[idx+len(marker):]
	end := strings.Index(fragment, ")")
	if end >= 0 {
		fragment = fragment[:end]
	}

	owner = extractArgValue(fragment, "owner")
	name = extractArgValue(fragment, "name")
	return owner, name
}

// extractArgValue returns the quoted string value of a named GraphQL argument
// within a fragment such as `owner: "clear-route", name: "orbit"`.
func extractArgValue(fragment, arg string) string {
	// Find "arg" followed by optional whitespace, colon, optional whitespace,
	// and a double-quoted value.
	needle := arg
	pos := strings.Index(strings.ToLower(fragment), needle)
	if pos < 0 {
		return ""
	}
	rest := strings.TrimSpace(fragment[pos+len(needle):])
	if !strings.HasPrefix(rest, ":") {
		return ""
	}
	rest = strings.TrimSpace(rest[1:])
	if !strings.HasPrefix(rest, `"`) {
		return ""
	}
	rest = rest[1:] // skip opening quote
	before, _, ok := strings.Cut(rest, `"`)
	if !ok {
		return ""
	}
	return before
}

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
		// real work — the metadata baseline covers them.
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
		// and DELETE deletes the repository — administration.
		if verb == verbRead {
			return credentials.Classification{Action: ActionMetadataRead, Qualifier: qualifier}
		}
		return credentials.Classification{Action: catAdmin + ":" + verbWrite, Qualifier: qualifier}
	}
	return credentials.Classification{Action: repoSubCategory(sub) + ":" + verb, Qualifier: qualifier}
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
	// Actions secrets/variables are credential-adjacent — administration, so
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
// classify unqualified rather than denying — satisfiable only by an unqualified
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
// read). The repo catch-all/umbrella derives the union — with workflows
// deliberately excluded: an agent that can push should not silently also be able
// to rewrite CI workflow files (the classic secrets-exfiltration escalation); a
// connection whose agents legitimately edit workflows declares workflows:write
// in its permission ceiling, which the umbrella mints verbatim.
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
func permissionsFor(action string, ceiling map[string]string) map[string]string {
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
		// the configured ceiling verbatim when declared, else the union of every
		// category at the action's verb class.
		if len(ceiling) > 0 {
			return maps.Clone(ceiling)
		}
		for cat, keys := range categoryPermissionKeys {
			level := verb
			if cat == catMetadata {
				level = verbRead
			}
			for _, key := range keys {
				derived[key] = level
			}
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

// splitAction splits an action fragment at its last colon into category and
// verb. A fragment with no colon yields (fragment, "read") defensively.
func splitAction(action string) (category, verb string) {
	if i := strings.LastIndex(action, ":"); i > 0 {
		return action[:i], action[i+1:]
	}
	return action, verbRead
}

// reposFor derives the mint-time repository narrowing: the request's qualifier
// when one was classified (the broker has already ceiling-checked it), else the
// connection's configured list.
func reposFor(qualifier string, ceiling []string) []string {
	if qualifier != "" {
		return []string{qualifier}
	}
	return append([]string(nil), ceiling...)
}

// Resolve returns a cached, or freshly minted, installation token formatted as
// the git smart-HTTP Basic credential, narrowed to the classified category's
// permissions and the request's repository. The raw token is also exposed on
// the returned Credential so the gateway can reformat it as a Bearer for REST.
func (a *Adapter) Resolve(ctx context.Context, conn credentials.Connection, id credentials.CallerIdentity) (credentials.Credential, error) {
	var cfg config
	if err := json.Unmarshal(conn.Config, &cfg); err != nil {
		return credentials.Credential{}, fmt.Errorf("%w: github_app config: %w", credentials.ErrInvalidConfig, err)
	}
	if cfg.AppID == "" || cfg.InstallationID == 0 {
		return credentials.Credential{}, fmt.Errorf("%w: github_app requires app_id and installation_id", credentials.ErrInvalidConfig)
	}

	perms := permissionsFor(id.Action, cfg.Permissions)
	repos := reposFor(id.Qualifier, cfg.Repositories)

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
		// A 422 permissions rejection is the App installation's own ceiling —
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
		logging.Field{Key: "repository", Value: id.Qualifier},
		logging.Field{Key: "expires_at", Value: tok.ExpiresAt.UTC().Format(time.RFC3339)},
	)
	return cred, nil
}

// ListQualifierValues enumerates the installation's repository names for the
// connector attachment picker (the egress gateway's optional
// QualifierEnumerator capability). It resolves a metadata-scoped installation
// token — reusing the cache — and lists the installation's repositories.
func (a *Adapter) ListQualifierValues(ctx context.Context, conn credentials.Connection) ([]string, error) {
	cred, err := a.Resolve(ctx, conn, credentials.CallerIdentity{
		TenantID: conn.TenantID,
		Action:   ActionMetadataRead,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve metadata token for repository listing: %w", err)
	}
	repos, err := a.api.ListInstallationRepositories(ctx, cred.Token)
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
	rs := append([]string(nil), repos...)
	sort.Strings(rs)
	ps := make([]string, 0, len(perms))
	for k, v := range perms {
		ps = append(ps, k+"="+v)
	}
	sort.Strings(ps)
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
