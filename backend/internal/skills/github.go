// github.go provides a typed HTTP client for reading GitHub repository
// content — the file tree (via the Git Trees API) and raw file bytes (via
// raw.githubusercontent.com) — for the Agent Marketplace crawler.
//
// It lives in this feature package rather than internal/platform/clients,
// deliberately deviating from that general convention; see the repo root's
// flags.md for why, and what to check if that deviation ever causes a
// problem.
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
