// Package github provides a typed HTTP client for the GitHub REST API, limited
// to the operations the Orbit credential broker needs — currently minting
// GitHub App installation access tokens.
//
// It embeds httpclient.Base so every call gets a named OTel client span, W3C
// trace propagation, and structured logging without per-call boilerplate.
// Feature code must never construct a raw http.Client; it depends on this client
// (or the githubAPI interface in the credentials package) instead.
package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/clear-route/orbit/internal/platform/clients/httpclient"
	"github.com/clear-route/orbit/internal/platform/logging"
)

const (
	tracerName    = "orbit.github-client"
	clientTimeout = 10 * time.Second

	// DefaultBaseURL is GitHub.com's REST API root. GitHub Enterprise Server
	// installations use a different host, configured per connection.
	DefaultBaseURL = "https://api.github.com"

	acceptHeader = "application/vnd.github+json"
)

// Sentinel errors. Callers may test for these with errors.Is to branch on
// failure category without string matching.
var (
	// ErrUnexpectedStatus is returned when GitHub responds with any status other
	// than 201 Created to an installation-token request.
	ErrUnexpectedStatus = errors.New("unexpected status from github")

	// ErrMissingToken is returned when GitHub's response decodes successfully but
	// carries an empty token.
	ErrMissingToken = errors.New("github installation token response missing token")

	// ErrInstallationPermissions is returned when GitHub rejects a token-mint
	// request (HTTP 422) because the requested permission level exceeds what the
	// App installation actually holds. This is an upstream authority decision —
	// the App is the ceiling — not an Orbit-internal error, so callers map it to
	// a client-facing forbidden rather than a 500. See
	// docs/adr/2026-07-22-github-connection-category-scoped-actions.md.
	ErrInstallationPermissions = errors.New("github installation lacks the requested permission level")

	// ErrTreeTruncated is returned when GitHub's Trees API response is marked
	// truncated — the repository has more than 100,000 entries or the tree
	// exceeds 7 MB. The crawler cannot safely filter a partial tree.
	ErrTreeTruncated = errors.New("github tree response was truncated")
)

// InstallationTokenRequest optionally scopes an installation token to a subset
// of repositories and/or permissions. Empty fields request the installation's
// full configured access.
type InstallationTokenRequest struct {
	Repositories []string          `json:"repositories,omitempty"`
	Permissions  map[string]string `json:"permissions,omitempty"`
}

// InstallationToken is GitHub's response to a token-creation request. The token
// is short-lived (one hour) and scoped to the requested repositories and
// permissions.
type InstallationToken struct {
	Token               string            `json:"token"`
	ExpiresAt           time.Time         `json:"expires_at"`
	Permissions         map[string]string `json:"permissions"`
	RepositorySelection string            `json:"repository_selection"`
}

// TreeEntry is one node in a GitHub repository tree as returned by the
// Git Trees API (?recursive=1). Only Blob entries (Type == "blob") carry
// file content; Tree entries are directory nodes.
type TreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"` // "blob" or "tree"
}

// treeResponse is the wire shape of GET /repos/{org}/{repo}/git/trees/{ref}?recursive=1.
type treeResponse struct {
	Tree      []TreeEntry `json:"tree"`
	Truncated bool        `json:"truncated"`
}

// Client is a typed GitHub REST API client.
type Client struct {
	*httpclient.Base
	baseURL string
}

// NewClient constructs a GitHub client targeting baseURL. Pass DefaultBaseURL
// (or an empty string) for GitHub.com.
func NewClient(baseURL string, logger logging.Logger) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		Base:    httpclient.New(tracerName, logger, clientTimeout),
		baseURL: strings.TrimRight(baseURL, "/"),
	}
}

// CreateInstallationToken exchanges a GitHub App JWT for a short-lived
// installation access token, optionally scoped by body. appJWT must be a
// freshly-signed App JWT (RS256, at most 10 minutes TTL). GitHub returns 201
// Created on success.
func (c *Client) CreateInstallationToken(
	ctx context.Context,
	appJWT string,
	installationID int64,
	body InstallationTokenRequest,
) (InstallationToken, error) {
	endpoint := fmt.Sprintf("%s/app/installations/%d/access_tokens", c.baseURL, installationID)

	payload, err := c.EncodeJSON(body)
	if err != nil {
		return InstallationToken{}, fmt.Errorf("encode installation token request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, payload)
	if err != nil {
		return InstallationToken{}, fmt.Errorf("build installation token request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", acceptHeader)
	req.Header.Set("Content-Type", "application/json")

	_, span, resp, err := c.Do(ctx, "github.Client.CreateInstallationToken", req)
	defer span.End()
	if err != nil {
		return InstallationToken{}, fmt.Errorf("installation token call: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		// 422 on a token mint means the requested permissions exceed the
		// installation's own grant — an upstream ceiling, not a transport
		// failure. Surface it as its own sentinel so the broker can map it to a
		// forbidden instead of a 500.
		sentinel := ErrUnexpectedStatus
		if resp.StatusCode == http.StatusUnprocessableEntity {
			sentinel = ErrInstallationPermissions
		}
		return InstallationToken{}, c.HandleErrorResponse(span, resp, sentinel)
	}

	var tok InstallationToken
	if err = c.DecodeJSON(span, resp, &tok); err != nil {
		return InstallationToken{}, err
	}
	if tok.Token == "" {
		return InstallationToken{}, c.RecordError(span, ErrMissingToken)
	}
	return tok, nil
}

// listRepositoriesPageSize is GitHub's maximum per_page for the
// installation-repositories listing.
const listRepositoriesPageSize = 100

// listRepositoriesMaxPages bounds the enumeration to a picker-sized result set;
// installations larger than this are truncated (the attachment UI permits
// manual entry beyond the enumerated set).
const listRepositoriesMaxPages = 5

// installationRepositoriesResponse is the wire shape of
// GET /installation/repositories.
type installationRepositoriesResponse struct {
	TotalCount   int `json:"total_count"`
	Repositories []struct {
		Name string `json:"name"`
	} `json:"repositories"`
}

// ListInstallationRepositories enumerates the repository names the
// installation token can reach, for the connector attachment picker.
// installationToken must be a live installation access token (metadata read
// suffices). Results are bare repository names — the installation fixes the
// owner — truncated at listRepositoriesMaxPages pages.
func (c *Client) ListInstallationRepositories(ctx context.Context, installationToken string) ([]string, error) {
	names := make([]string, 0, listRepositoriesPageSize)
	for page := 1; page <= listRepositoriesMaxPages; page++ {
		endpoint := fmt.Sprintf("%s/installation/repositories?per_page=%d&page=%d", c.baseURL, listRepositoriesPageSize, page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("build installation repositories request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+installationToken)
		req.Header.Set("Accept", acceptHeader)

		_, span, resp, err := c.Do(ctx, "github.Client.ListInstallationRepositories", req)
		if err != nil {
			span.End()
			return nil, fmt.Errorf("installation repositories call: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			err = c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
			_ = resp.Body.Close()
			span.End()
			return nil, err
		}

		var body installationRepositoriesResponse
		err = c.DecodeJSON(span, resp, &body)
		_ = resp.Body.Close()
		span.End()
		if err != nil {
			return nil, err
		}
		for _, r := range body.Repositories {
			if r.Name != "" {
				names = append(names, strings.ToLower(r.Name))
			}
		}
		if len(body.Repositories) < listRepositoriesPageSize {
			break
		}
	}
	return names, nil
}

// installationResponse is the wire shape of GET /app/installations/{id}.
type installationResponse struct {
	Permissions map[string]string `json:"permissions"`
}

// GetInstallationPermissions fetches the App installation's current permission
// set from GitHub. appJWT must be a freshly-signed App JWT (RS256). The
// returned map is keyed by permission name (e.g. "contents", "pull_requests")
// with string level values ("read", "write"). It is used by the credential
// adapter to intersect the umbrella permission set with the installation's
// actual grants, avoiding GitHub 422 errors from ungranted permissions.
func (c *Client) GetInstallationPermissions(ctx context.Context, appJWT string, installationID int64) (map[string]string, error) {
	endpoint := fmt.Sprintf("%s/app/installations/%d", c.baseURL, installationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build get installation request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", acceptHeader)

	_, span, resp, err := c.Do(ctx, "github.Client.GetInstallationPermissions", req)
	defer span.End()
	if err != nil {
		return nil, fmt.Errorf("get installation call: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
	}

	var body installationResponse
	if err = c.DecodeJSON(span, resp, &body); err != nil {
		return nil, err
	}
	return body.Permissions, nil
}

// GetTree returns the flat recursive file tree for the given repository ref
// (branch, tag, or commit SHA). Only the path, type, and SHA of each entry
// are populated. Returns ErrTreeTruncated when GitHub marks the response
// truncated (repo too large to enumerate fully).
func (c *Client) GetTree(ctx context.Context, token, org, repo, ref string) ([]TreeEntry, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/%s/git/trees/%s?recursive=1", c.baseURL, org, repo, ref)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build get-tree request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", acceptHeader)

	_, span, resp, err := c.Do(ctx, "github.Client.GetTree", req)
	defer span.End()
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
	}
	var body treeResponse
	if err := c.DecodeJSON(span, resp, &body); err != nil {
		return nil, err
	}
	if body.Truncated {
		return nil, c.RecordError(span, ErrTreeTruncated)
	}
	return body.Tree, nil
}

// GetRawContent fetches the raw byte content of a single file at path in
// the repository, at the given ref (branch, tag, or commit SHA).
func (c *Client) GetRawContent(ctx context.Context, token, org, repo, ref, path string) ([]byte, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/%s/contents/%s?ref=%s", c.baseURL, org, repo, path, ref)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build get-raw-content request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github.raw+json")

	_, span, resp, err := c.Do(ctx, "github.Client.GetRawContent", req)
	defer span.End()
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	return data, nil
}
