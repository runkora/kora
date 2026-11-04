// Package github provides a typed HTTP client for the GitHub REST API, limited
// to the operations the credential broker and skills-service need: minting
// installation tokens and reading repository trees and files.
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
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/runkora/kora/backend/internal/platform/clients/httpclient"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

const (
	tracerName    = "github.com/runkora/kora/backend/internal/platform/clients/github"
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
	// the App is the ceiling — not a kora-internal error, so callers map it to
	// a client-facing forbidden rather than a 500. See
	// docs/adr/2026-07-22-github-connection-category-scoped-actions.md.
	ErrInstallationPermissions = errors.New("github installation lacks the requested permission level")

	// ErrTreeTruncated is returned when GitHub marks a Trees API response
	// truncated, or the response is over maxTreeBytes. A partial tree cannot
	// be filtered safely.
	ErrTreeTruncated = errors.New("github tree response was truncated")

	// ErrUnavailable is returned by CreateInstallationToken, ResolveRef, GetTree
	// and GetRawContent when GitHub cannot serve the call for now: a timeout or
	// network failure, a 5xx status, or a rate limit. For a status failure, the
	// error also matches ErrUnexpectedStatus.
	ErrUnavailable = errors.New("github is unavailable or rate-limited")

	// ErrNotFound is returned by ResolveRef, GetTree and GetRawContent when the
	// repository, ref or file does not exist or the token cannot see it.
	ErrNotFound = errors.New("github repository, ref or file not found")

	// ErrTooLarge is returned by GetRawContent for a file over its byte limit.
	ErrTooLarge = errors.New("github file is over the size limit")

	// ErrBadCommit is returned when GitHub resolves a ref to something that is
	// not a commit SHA.
	ErrBadCommit = errors.New("github ref did not resolve to a commit SHA")
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

// TreeEntry is one node of a recursive Git Trees API response. Type is "blob"
// for a file or symbolic link and "tree" for a directory. Size is set for
// blobs.
type TreeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	Size int64  `json:"size"`
}

// SymlinkMode is the tree mode of a symbolic link. Its size is the length of
// the link path and the contents API returns the target.
const SymlinkMode = "120000"

// maxTreeBytes bounds a tree response. A larger tree returns ErrTreeTruncated.
const maxTreeBytes = 16 << 20

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
// Returns an error that matches ErrUnavailable when GitHub is down or
// rate-limits the call.
func (c *Client) CreateInstallationToken(
	ctx context.Context,
	appJWT string,
	installationID int64,
	body InstallationTokenRequest,
) (InstallationToken, error) {
	payload, err := c.EncodeJSON(body)
	if err != nil {
		return InstallationToken{}, fmt.Errorf("encode installation token request: %w", err)
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+appJWT)
	header.Set("Accept", acceptHeader)
	header.Set("Content-Type", "application/json")

	_, span, resp, err := c.Do(ctx, httpclient.Request{
		Method: http.MethodPost, BaseURL: c.baseURL, Path: "/app/installations/{installationID}/access_tokens",
		Params: []string{strconv.FormatInt(installationID, 10)}, Header: header, Body: payload,
	})
	defer span.End()
	if err != nil {
		return InstallationToken{}, transportError(ctx, fmt.Errorf("installation token call: %w", err))
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
		return InstallationToken{}, statusError(resp, c.HandleErrorResponse(span, resp, sentinel))
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
		header := http.Header{}
		header.Set("Authorization", "Bearer "+installationToken)
		header.Set("Accept", acceptHeader)

		_, span, resp, err := c.Do(ctx, httpclient.Request{
			Method: http.MethodGet, BaseURL: c.baseURL, Path: "/installation/repositories", Header: header,
			Query: url.Values{"per_page": {strconv.Itoa(listRepositoriesPageSize)}, "page": {strconv.Itoa(page)}},
		})
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
	header := http.Header{}
	header.Set("Authorization", "Bearer "+appJWT)
	header.Set("Accept", acceptHeader)

	_, span, resp, err := c.Do(ctx, httpclient.Request{
		Method: http.MethodGet, BaseURL: c.baseURL, Path: "/app/installations/{installationID}",
		Params: []string{strconv.FormatInt(installationID, 10)}, Header: header,
	})
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

// ResolveRef returns the commit SHA that ref (a branch, tag or commit SHA)
// points at. Pass "HEAD" for the default branch.
func (c *Client) ResolveRef(ctx context.Context, token, owner, repo, ref string) (string, error) {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)
	header.Set("Accept", "application/vnd.github.sha")

	_, span, resp, err := c.Do(ctx, httpclient.Request{
		Method: http.MethodGet, BaseURL: c.baseURL, Path: "/repos/{owner}/{repo}/commits/{ref}",
		Params: []string{owner, repo, ref}, Header: header,
	})
	defer span.End()
	if err != nil {
		return "", transportError(ctx, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// GitHub answers an unknown ref with 422 and an unknown repository with 404.
	if resp.StatusCode != http.StatusOK {
		return "", statusError(resp, c.HandleErrorResponse(span, resp, ErrUnexpectedStatus), http.StatusNotFound, http.StatusUnprocessableEntity)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return "", transportError(ctx, fmt.Errorf("read response body: %w", err))
	}
	sha := strings.TrimSpace(string(body))
	if !isCommitSHA(sha) {
		return "", c.RecordError(span, ErrBadCommit)
	}
	return sha, nil
}

// isCommitSHA reports whether s is a 40-character lowercase hex SHA.
func isCommitSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// GetTree returns the recursive file tree at sha. It returns ErrTreeTruncated
// when GitHub marks the response truncated.
func (c *Client) GetTree(ctx context.Context, token, owner, repo, sha string) ([]TreeEntry, error) {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)
	header.Set("Accept", acceptHeader)

	_, span, resp, err := c.Do(ctx, httpclient.Request{
		Method: http.MethodGet, BaseURL: c.baseURL, Path: "/repos/{owner}/{repo}/git/trees/{sha}",
		Params: []string{owner, repo, sha}, Query: url.Values{"recursive": {"1"}}, Header: header,
	})
	defer span.End()
	if err != nil {
		return nil, transportError(ctx, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, statusError(resp, c.HandleErrorResponse(span, resp, ErrUnexpectedStatus), http.StatusNotFound)
	}
	limited := &io.LimitedReader{R: resp.Body, N: maxTreeBytes}
	resp.Body = struct {
		io.Reader
		io.Closer
	}{limited, resp.Body}
	var body treeResponse
	if err := c.DecodeJSON(span, resp, &body); err != nil {
		if limited.N == 0 {
			return nil, c.RecordError(span, ErrTreeTruncated)
		}
		return nil, err
	}
	if body.Truncated {
		return nil, c.RecordError(span, ErrTreeTruncated)
	}
	return body.Tree, nil
}

// GetRawContent returns the content of the file at path and ref, reading at
// most maxBytes. A longer file returns ErrTooLarge.
func (c *Client) GetRawContent(ctx context.Context, token, owner, repo, ref, path string, maxBytes int64) ([]byte, error) {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)
	header.Set("Accept", "application/vnd.github.raw+json")

	_, span, resp, err := c.Do(ctx, httpclient.Request{
		Method: http.MethodGet, BaseURL: c.baseURL, Path: "/repos/{owner}/{repo}/contents/{path}",
		Params: []string{owner, repo, path}, Query: url.Values{"ref": {ref}}, Header: header,
	})
	defer span.End()
	if err != nil {
		return nil, transportError(ctx, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, statusError(resp, c.HandleErrorResponse(span, resp, ErrUnexpectedStatus), http.StatusNotFound)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, transportError(ctx, fmt.Errorf("read response body: %w", err))
	}
	if int64(len(data)) > maxBytes {
		return nil, c.RecordError(span, ErrTooLarge)
	}
	return data, nil
}

// transportError wraps err in ErrUnavailable: the call got no complete
// response, so a later retry can succeed. A call that the caller canceled,
// or whose deadline passed, is not a GitHub outage and stays unwrapped.
func transportError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		// The caller stopped the call.
		return err
	}
	// A timeout or a network failure.
	return fmt.Errorf("%w: %w", ErrUnavailable, err)
}

// statusError wraps err, the error of a non-2xx response, in ErrUnavailable
// when the status shows an outage or a rate limit, and in ErrNotFound when the
// status is one of notFound. Any other status stays unwrapped.
func statusError(resp *http.Response, err error, notFound ...int) error {
	switch {
	case isUnavailable(resp, err):
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	case slices.Contains(notFound, resp.StatusCode):
		return fmt.Errorf("%w: %w", ErrNotFound, err)
	}
	return err
}

// isUnavailable reports whether a non-2xx response shows a GitHub outage or
// a rate limit. err is the error of that response, its text holds the start
// of GitHub's error body. A rate limit is a 429, or a 403 with GitHub's
// signal: x-ratelimit-remaining 0, a retry-after header, or a "secondary
// rate limit" message. Any other 403 is, for example, lost access or a file
// that is too large. See
// https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api#exceeding-the-rate-limit
func isUnavailable(resp *http.Response, err error) bool {
	switch {
	case resp.StatusCode >= http.StatusInternalServerError:
		// A GitHub outage.
		return true
	case resp.StatusCode == http.StatusTooManyRequests:
		// A primary or secondary rate limit.
		return true
	case resp.StatusCode == http.StatusForbidden:
		// A 403 is a rate limit only with GitHub's signal.
		return resp.Header.Get("X-RateLimit-Remaining") == "0" ||
			resp.Header.Get("Retry-After") != "" ||
			strings.Contains(strings.ToLower(err.Error()), "secondary rate limit")
	default:
		// Any other status, for example 404.
		return false
	}
}
