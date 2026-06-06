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
		return InstallationToken{}, c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
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
