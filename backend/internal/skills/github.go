// github.go provides a typed HTTP client for reading GitHub repository
// content — the file tree (via the Git Trees API) and raw file bytes (via
// raw.githubusercontent.com) — for the Agent Marketplace crawler.
//
// It lives in this feature package rather than internal/platform/clients,
// deliberately deviating from that general convention: see
// docs/adr/2026-09-09-agent-marketplace-startup-preload-per-repo-redis-keys.md
// ("Service Layout"), which specifies this exact path
// (backend/internal/skills/github.go) rather than a platform-clients
// subpackage.
//
// It is deliberately narrower than internal/platform/clients/github, which is
// scoped to minting GitHub App installation tokens for the credential broker.
// This client only reads repository content; it resolves the token to use
// per call via the injected tokenSource, never mints or refreshes one itself.
//
// It embeds httpclient.Base so every call gets a named OTel client span, W3C
// trace propagation, and structured logging without per-call boilerplate.
// Feature code must never construct a raw http.Client; it depends on this
// client instead.
package skills

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/clear-route/orbit/internal/platform/clients/httpclient"
	"github.com/clear-route/orbit/internal/platform/logging"
)

const (
	tracerName    = "orbit.skills.github-client"
	clientTimeout = 15 * time.Second

	// DefaultAPIBaseURL is GitHub.com's REST API root, used for the Trees API.
	DefaultAPIBaseURL = "https://api.github.com"

	// DefaultRawBaseURL is GitHub.com's raw-content host, used to fetch file
	// bytes directly without a base64-encoded blob response.
	DefaultRawBaseURL = "https://raw.githubusercontent.com"

	acceptHeader = "application/vnd.github+json"

	// MaxFileSize caps the bytes read from a single GetFile response. Skill
	// content (Markdown plus small bundled resources) is expected to be well
	// under this; anything larger is rejected rather than buffered fully
	// into memory, since GetFile reads from third-party repositories the
	// crawler does not control.
	MaxFileSize = 10 * 1024 * 1024 // 10 MiB
)

// Sentinel errors. Callers may test for these with errors.Is to branch on
// failure category without string matching.
var (
	// ErrTokenSource is returned when the injected tokenSource fails to
	// resolve a token for the requested repository.
	ErrTokenSource = errors.New("failed to resolve github token")

	// ErrRepositoryNotFound is returned when GitHub responds 404 to a tree
	// request — the org/repo/branch combination does not exist or the token
	// cannot see it. Distinct from ErrFileNotFound: this means the whole
	// source is unreachable, not that one file within it is missing.
	ErrRepositoryNotFound = errors.New("github repository or branch not found")

	// ErrFileNotFound is returned when GitHub responds 404 to a raw-content
	// request — the repository/branch exists but path does not. Distinct
	// from ErrRepositoryNotFound so a caller can skip one missing file
	// without treating the whole source as failed.
	ErrFileNotFound = errors.New("github file not found at path")

	// ErrUnexpectedStatus is returned when GitHub responds with any other
	// non-2xx status.
	ErrUnexpectedStatus = errors.New("unexpected status from github")

	// ErrReadBody is returned when a successful (2xx) response body cannot be
	// fully read.
	ErrReadBody = errors.New("failed to read raw file body")

	// ErrFileTooLarge is returned when a GetFile response body exceeds
	// MaxFileSize.
	ErrFileTooLarge = errors.New("github file exceeds maximum allowed size")

	// ErrInvalidPath is returned when an org, repo, branch, or path argument
	// to GetTree or GetFile is empty, starts with "/", or contains a
	// "/"-delimited ".." or empty segment — rejected before it's
	// interpolated into the request URL, rather than sent as-is. Checking by
	// segment (not substring) avoids rejecting a legitimate value that
	// merely contains ".." inside a longer segment, e.g. "v1..2".
	ErrInvalidPath = errors.New("invalid path: empty, absolute, or contains a traversal segment")
)

// tokenSource resolves the GitHub bearer token to use for a given repository.
// This is the narrowest dependency Client needs — not the full GitHub App
// TokenProvider (mint, cache, refresh), just the ability to ask for a live
// token per (org, repo). The concrete implementation (GitHub App installation
// token minting) is out of scope for this plan; see "Explicitly out of scope"
// at the end of the plan this file was built from.
type tokenSource interface {
	Token(ctx context.Context, org, repo string) (string, error)
}

// TreeEntry is one entry in a Git tree — either a file ("blob") or a
// subdirectory ("tree").
type TreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
	Size int    `json:"size"`
}

// Tree is the GitHub Trees API response for a recursive tree listing.
// Truncated is true when the repository has more entries than GitHub will
// return in one response (its non-paginated recursive limit); callers should
// treat a truncated tree as incomplete rather than assuming full coverage.
type Tree struct {
	SHA       string      `json:"sha"`
	Entries   []TreeEntry `json:"tree"`
	Truncated bool        `json:"truncated"`
}

// Client is a typed client for reading GitHub repository content.
type Client struct {
	*httpclient.Base
	tokens     tokenSource
	apiBaseURL string
	rawBaseURL string
}

// NewClient constructs a Client targeting GitHub.com. tokens resolves the
// bearer token to use for each call, per (org, repo).
func NewClient(logger logging.Logger, tokens tokenSource) *Client {
	return &Client{
		Base:       httpclient.New(tracerName, logger, clientTimeout),
		tokens:     tokens,
		apiBaseURL: DefaultAPIBaseURL,
		rawBaseURL: DefaultRawBaseURL,
	}
}

// validateRepoPath rejects an org, repo, branch, or file-path argument that
// is empty, starts with "/", or contains a "/"-delimited ".." or empty
// segment. It checks by segment rather than by substring so a legitimate
// value that merely contains ".." inside a longer segment (e.g. "v1..2") is
// not rejected.
func validateRepoPath(value string) error {
	if value == "" || strings.HasPrefix(value, "/") {
		return ErrInvalidPath
	}
	for seg := range strings.SplitSeq(value, "/") {
		if seg == "" || seg == ".." {
			return ErrInvalidPath
		}
	}
	return nil
}

// escapePathSegments percent-encodes each "/"-delimited segment of value
// independently and rejoins them with "/", so a segment containing a URL
// metacharacter (e.g. "#", "?", a space) cannot alter the shape of the
// request. fmt.Sprintf, used to build the endpoint below, does no escaping
// of its own — this mirrors the url.PathEscape precedent already used in
// internal/platform/clients/agents/client.go.
func escapePathSegments(value string) string {
	segs := strings.Split(value, "/")
	for i, seg := range segs {
		segs[i] = url.PathEscape(seg)
	}
	return strings.Join(segs, "/")
}

// GetTree fetches the full recursive file tree of repo at branch (a branch
// name, tag, or commit SHA — the source's configured branch in the
// Marketplace ConfigMap).
//
// Check Tree.Truncated before assuming full coverage: GitHub caps the
// recursive listing size and sets this flag rather than paginating.
func (c *Client) GetTree(ctx context.Context, org, repo, branch string) (Tree, error) {
	if err := validateRepoPath(org); err != nil {
		return Tree{}, err
	}
	if err := validateRepoPath(repo); err != nil {
		return Tree{}, err
	}
	if err := validateRepoPath(branch); err != nil {
		return Tree{}, err
	}

	token, err := c.tokens.Token(ctx, org, repo)
	if err != nil {
		return Tree{}, fmt.Errorf("%w: %w", ErrTokenSource, err)
	}

	endpoint := fmt.Sprintf("%s/repos/%s/%s/git/trees/%s?recursive=1",
		c.apiBaseURL, escapePathSegments(org), escapePathSegments(repo), escapePathSegments(branch))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return Tree{}, fmt.Errorf("build get tree request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", acceptHeader)

	_, span, resp, err := c.Do(ctx, "skills.Client.GetTree", req)
	defer span.End()
	if err != nil {
		return Tree{}, fmt.Errorf("get tree call: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return Tree{}, c.HandleErrorResponse(span, resp, ErrRepositoryNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return Tree{}, c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
	}

	var tree Tree
	if err := c.DecodeJSON(span, resp, &tree); err != nil {
		return Tree{}, err
	}
	return tree, nil
}

// GetFile fetches the raw bytes of path in repo at branch, directly from
// raw.githubusercontent.com (no base64 decoding required, unlike the
// Contents API).
func (c *Client) GetFile(ctx context.Context, org, repo, branch, path string) ([]byte, error) {
	if err := validateRepoPath(org); err != nil {
		return nil, err
	}
	if err := validateRepoPath(repo); err != nil {
		return nil, err
	}
	if err := validateRepoPath(branch); err != nil {
		return nil, err
	}
	if err := validateRepoPath(path); err != nil {
		return nil, err
	}

	token, err := c.tokens.Token(ctx, org, repo)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrTokenSource, err)
	}

	endpoint := fmt.Sprintf("%s/%s/%s/%s/%s",
		c.rawBaseURL, escapePathSegments(org), escapePathSegments(repo), escapePathSegments(branch), escapePathSegments(path))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build get file request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	_, span, resp, err := c.Do(ctx, "skills.Client.GetFile", req)
	defer span.End()
	if err != nil {
		return nil, fmt.Errorf("get file call: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, c.HandleErrorResponse(span, resp, ErrFileNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxFileSize+1))
	if err != nil {
		_ = c.RecordError(span, ErrReadBody)
		return nil, fmt.Errorf("%w: %w", ErrReadBody, err)
	}
	if len(body) > MaxFileSize {
		return nil, c.RecordError(span, ErrFileTooLarge)
	}
	return body, nil
}
