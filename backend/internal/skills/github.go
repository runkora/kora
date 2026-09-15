// github.go provides a typed HTTP client for reading GitHub repository
// content — the file tree (via the Git Trees API) and raw file bytes (via
// raw.githubusercontent.com) — for the Agent Marketplace crawler.
//
// It lives in this feature package rather than internal/platform/clients,
// deliberately deviating from that general convention: the Agent Marketplace
// ADR's Service Layout and its Notion ticket both specify this exact path
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

// GetTree fetches the full recursive file tree of repo at branch (a branch
// name, tag, or commit SHA — the source's configured branch in the
// Marketplace ConfigMap).
//
// Check Tree.Truncated before assuming full coverage: GitHub caps the
// recursive listing size and sets this flag rather than paginating.
func (c *Client) GetTree(ctx context.Context, org, repo, branch string) (Tree, error) {
	token, err := c.tokens.Token(ctx, org, repo)
	if err != nil {
		return Tree{}, fmt.Errorf("%w: %w", ErrTokenSource, err)
	}

	endpoint := fmt.Sprintf("%s/repos/%s/%s/git/trees/%s?recursive=1", c.apiBaseURL, org, repo, branch)
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
	token, err := c.tokens.Token(ctx, org, repo)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrTokenSource, err)
	}

	endpoint := fmt.Sprintf("%s/%s/%s/%s/%s", c.rawBaseURL, org, repo, branch, path)
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		_ = c.RecordError(span, ErrReadBody)
		return nil, fmt.Errorf("%w: %w", ErrReadBody, err)
	}
	return body, nil
}
