// Package policy provides an HTTP client for managing policy grants via the
// policy service API. Used by the identity service to create and delete PAT
// grants as part of PAT lifecycle workflows via separate HTTP operations.
package policy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
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

// BudgetAssignment is the inline budget configuration carried by a grant.
// LimitUSD is the per-window (weekly/monthly) ceiling; PerRunLimitUSD is the
// windowless per-run ceiling for agent runs.
type BudgetAssignment struct {
	LimitUSD       float64 `json:"limit_usd,omitempty"`
	ResetCadence   string  `json:"reset_cadence,omitempty"`
	PerRunLimitUSD float64 `json:"per_run_limit_usd,omitempty"`
}

// Grant is the subset of a policy_grant used by this client.
type Grant struct {
	ID            string            `json:"id"`
	TenantID      string            `json:"tenant_id"`
	PrincipalType string            `json:"principal_type"`
	PrincipalID   string            `json:"principal_id"`
	Scopes        []string          `json:"scopes"`
	Budget        *BudgetAssignment `json:"budget,omitempty"`
	// DelegationRequired is the delegated-agent fail-closed marker
	// (delegated-agent-identity ADR, Decision 2): the grant is a ceiling, not
	// an entitlement, and the principal may act only under valid delegation.
	DelegationRequired bool `json:"delegation_required"`
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
	TenantID           string            `json:"tenant_id"`
	PrincipalType      string            `json:"principal_type"`
	PrincipalID        string            `json:"principal_id"`
	Scopes             []string          `json:"scopes"`
	Budget             *BudgetAssignment `json:"budget,omitempty"`
	DelegationRequired bool              `json:"delegation_required,omitempty"`
}

// CreateGrant creates a grant for (principalType, principalID) → scopes within
// the given tenant, optionally carrying a budget assignment (nil = none).
// delegationRequired stamps the delegated-agent fail-closed marker on the grant
// (delegated-agent-identity ADR, Decision 2); pass false for ordinary grants.
// Returns the created grant ID.
func (c *Client) CreateGrant(ctx context.Context, tenantID, principalType, principalID string, scopes []string, budget *BudgetAssignment, delegationRequired bool) (string, error) {
	body, err := c.EncodeJSON(createGrantRequest{
		TenantID:           tenantID,
		PrincipalType:      principalType,
		PrincipalID:        principalID,
		Scopes:             scopes,
		Budget:             budget,
		DelegationRequired: delegationRequired,
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

// ListGrants returns the grants for a principal within a tenant, up to the
// service's default page. Used by the provisioner to find grant IDs for
// deletion on deprovision.
func (c *Client) ListGrants(ctx context.Context, tenantID, principalType, principalID string) ([]Grant, error) {
	grants, _, err := c.listGrantsPage(ctx, tenantID, principalType, principalID, 0, 0)
	return grants, err
}

// grantPageSize is the page size ListGrantsByType asks for. The service lowers
// it to its own maximum if that is smaller, which the loop below tolerates by
// reading the total the service reports rather than assuming a full page.
const grantPageSize = 1000

// ListGrantsByType returns every grant of one principal type within a tenant,
// reading the policy service a page at a time until it has read the total the
// service reports, or a page comes back empty.
func (c *Client) ListGrantsByType(ctx context.Context, tenantID, principalType string) ([]Grant, error) {
	var all []Grant
	for {
		page, total, err := c.listGrantsPage(ctx, tenantID, principalType, "", grantPageSize, len(all))
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if len(page) == 0 || len(all) >= total {
			return all, nil
		}
	}
}

// listGrantsPage reads one page of grants. A zero limit takes the service default.
func (c *Client) listGrantsPage(ctx context.Context, tenantID, principalType, principalID string, limit, offset int) ([]Grant, int, error) {
	tok, err := c.bearerToken(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("get bearer token for list grants request: %w", err)
	}

	u, err := url.Parse(c.serviceURL + "/api/v1/policy/grants")
	if err != nil {
		return nil, 0, fmt.Errorf("parse list grants URL: %w", err)
	}
	q := u.Query()
	q.Set("tenant_id", tenantID)
	q.Set("principal_type", principalType)
	q.Set("principal_id", principalID)
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
		q.Set("offset", strconv.Itoa(offset))
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, 0, fmt.Errorf("build list grants request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)

	_, span, resp, err := c.Do(ctx, "policy.Client.ListGrants", req)
	defer span.End()
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, 0, c.HandleErrorResponse(span, resp, fmt.Errorf("list grants failed: status %d", resp.StatusCode))
	}

	var result struct {
		Grants []Grant `json:"grants"`
		Total  int     `json:"total"`
	}
	if err := c.DecodeJSON(span, resp, &result); err != nil {
		return nil, 0, fmt.Errorf("decode list grants response: %w", err)
	}
	return result.Grants, result.Total, nil
}

// UpdateGrantBudget replaces the budget on an existing grant in place
// (PATCH /policy/grants/{id}), preserving the grant ID and its scopes. A nil
// budget removes the assignment. Returns ErrGrantNotFound if it does not exist.
func (c *Client) UpdateGrantBudget(ctx context.Context, grantID string, budget *BudgetAssignment) error {
	body, err := c.EncodeJSON(struct {
		Budget *BudgetAssignment `json:"budget"`
	}{Budget: budget})
	if err != nil {
		return fmt.Errorf("encode update grant budget request: %w", err)
	}

	tok, err := c.bearerToken(ctx)
	if err != nil {
		return fmt.Errorf("get bearer token for update grant budget request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.serviceURL+"/api/v1/policy/grants/"+grantID, body)
	if err != nil {
		return fmt.Errorf("build update grant budget request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)

	_, span, resp, err := c.Do(ctx, "policy.Client.UpdateGrantBudget", req)
	defer span.End()
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusNotFound:
		return ErrGrantNotFound
	default:
		return c.HandleErrorResponse(span, resp, fmt.Errorf("update grant budget failed: status %d", resp.StatusCode))
	}
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
