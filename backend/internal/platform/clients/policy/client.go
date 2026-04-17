// Package policy provides an HTTP client for managing policy grants via the
// policy service API. Used by the identity service to create and delete PAT
// grants atomically with PAT lifecycle operations.
package policy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/clear-route/orbit/internal/platform/clients/httpclient"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// ErrGrantNotFound is returned when the grant to delete does not exist.
var ErrGrantNotFound = errors.New("grant not found")

const (
	tracerName    = "orbit.policy-client"
	clientTimeout = 10 * time.Second
)

// Grant is the subset of a policy_grant used by this client.
type Grant struct {
	ID            string   `json:"id"`
	TenantID      string   `json:"tenant_id"`
	PrincipalType string   `json:"principal_type"`
	PrincipalID   string   `json:"principal_id"`
	Scopes        []string `json:"scopes"`
}

// TokenFunc is a function that returns a Bearer token for authenticating
// outbound requests to the policy service.
type TokenFunc func(ctx context.Context) (string, error)

// Client manages PAT grants via the policy service HTTP API.
type Client struct {
	*httpclient.Base
	serviceURL string
	tokenFunc  TokenFunc
}

// NewClient creates a Client targeting the given policy service base URL,
// e.g. "http://orbit-policy-service.orbit-system.svc".
// tokenFunc is called on every outbound request to obtain a Bearer token.
func NewClient(serviceURL string, tokenFunc TokenFunc, logger logging.Logger) *Client {
	return &Client{
		Base:       httpclient.New(tracerName, logger, clientTimeout),
		serviceURL: serviceURL,
		tokenFunc:  tokenFunc,
	}
}

func (c *Client) bearerToken(ctx context.Context) (string, error) {
	return c.tokenFunc(ctx)
}

type createGrantRequest struct {
	TenantID      string   `json:"tenant_id"`
	PrincipalType string   `json:"principal_type"`
	PrincipalID   string   `json:"principal_id"`
	Scopes        []string `json:"scopes"`
}

// CreateGrant creates a grant for (principalType, principalID) → scopes within
// the given tenant. Returns the created grant ID.
func (c *Client) CreateGrant(ctx context.Context, tenantID, principalType, principalID string, scopes []string) (string, error) {
	body, err := c.EncodeJSON(createGrantRequest{
		TenantID:      tenantID,
		PrincipalType: principalType,
		PrincipalID:   principalID,
		Scopes:        scopes,
	})
	if err != nil {
		return "", fmt.Errorf("encode grant request: %w", err)
	}

	tok, err := c.bearerToken(ctx)
	if err != nil {
		return "", fmt.Errorf("get bearer token for grant request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.serviceURL+"/api/v1/policy/grants", body)
	if err != nil {
		return "", fmt.Errorf("build grant request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)

	_, span, resp, err := c.Do(ctx, "policy.Client.CreateGrant", req)
	defer span.End()
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusCreated {
		return "", c.HandleErrorResponse(span, resp, fmt.Errorf("create grant failed: status %d", resp.StatusCode))
	}

	var g Grant
	if err := c.DecodeJSON(span, resp, &g); err != nil {
		return "", fmt.Errorf("decode grant response: %w", err)
	}
	return g.ID, nil
}

// DeleteGrant removes a grant by ID within the given tenant.
func (c *Client) DeleteGrant(ctx context.Context, grantID string) error {
	tok, err := c.bearerToken(ctx)
	if err != nil {
		return fmt.Errorf("get bearer token for delete grant request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.serviceURL+"/api/v1/policy/grants/"+grantID, nil)
	if err != nil {
		return fmt.Errorf("build delete grant request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)

	_, span, resp, err := c.Do(ctx, "policy.Client.DeleteGrant", req)
	defer span.End()
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	switch resp.StatusCode {
	case http.StatusNoContent:
		return nil
	case http.StatusNotFound:
		return ErrGrantNotFound
	default:
		return c.HandleErrorResponse(span, resp, fmt.Errorf("delete grant failed: status %d", resp.StatusCode))
	}
}
