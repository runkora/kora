// Package policy provides an HTTP client for managing policy grants via the
// policy service API. Used by the identity service to create and delete PAT
// grants as part of PAT lifecycle workflows via separate HTTP operations.
package policy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/runkora/kora/backend/internal/platform/clients/httpclient"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// ErrGrantNotFound is returned when the grant to delete does not exist.
var ErrGrantNotFound = errors.New("grant not found")

// ErrNoCallerBearer is returned when a caller-scoped request has no caller token.
var ErrNoCallerBearer = errors.New("no caller bearer token")

// ErrScopeNotInVocabulary is returned when the policy service refuses a grant
// because one of its scopes is not in the tenant's scope vocabulary.
var ErrScopeNotInVocabulary = errors.New("scope is not in the scope vocabulary")

// ScopeNotInVocabularyError names the scope the policy service refused. It
// matches ErrScopeNotInVocabulary under errors.Is.
type ScopeNotInVocabularyError struct {
	Scope string
}

func (e *ScopeNotInVocabularyError) Error() string {
	return ErrScopeNotInVocabulary.Error() + ": " + e.Scope
}

func (e *ScopeNotInVocabularyError) Is(target error) bool { return target == ErrScopeNotInVocabulary }

// errGrantRejected is any other 400 the policy service answers a grant with.
var errGrantRejected = errors.New("create grant rejected")

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

	if resp.StatusCode == http.StatusBadRequest {
		upstream := c.ReadErrorBody(span, resp)
		if scope, ok := vocabularyScope(upstream); ok {
			_ = c.RecordError(span, ErrScopeNotInVocabulary)
			return "", &ScopeNotInVocabularyError{Scope: scope}
		}
		return "", fmt.Errorf("%w: status %d: %s", c.RecordError(span, errGrantRejected), resp.StatusCode, upstream)
	}
	if resp.StatusCode != http.StatusCreated {
		return "", c.HandleErrorResponse(span, resp, fmt.Errorf("create grant failed: status %d", resp.StatusCode))
	}

	var g Grant
	if err := c.DecodeJSON(span, resp, &g); err != nil {
		return "", fmt.Errorf("decode grant response: %w", err)
	}
	return g.ID, nil
}

// CallerEffectiveScopes returns the effective scopes of the principal that
// bearer authenticates. It sends the caller's own token, not the service's,
// so the policy service resolves the principal from verified claims.
func (c *Client) CallerEffectiveScopes(ctx context.Context, bearer string) ([]string, error) {
	if bearer == "" {
		return nil, ErrNoCallerBearer
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.serviceURL+"/api/v1/policy/effective-scopes", nil)
	if err != nil {
		return nil, fmt.Errorf("build effective scopes request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)

	_, span, resp, err := c.Do(ctx, "policy.Client.CallerEffectiveScopes", req)
	defer span.End()
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.HandleErrorResponse(span, resp, fmt.Errorf("effective scopes failed: status %d", resp.StatusCode))
	}

	var result struct {
		Scopes []string `json:"scopes"`
	}
	if err := c.DecodeJSON(span, resp, &result); err != nil {
		return nil, fmt.Errorf("decode effective scopes response: %w", err)
	}
	return result.Scopes, nil
}

// ListGrants returns all grants for a principal within a tenant.
// Used by the provisioner to find grant IDs for deletion on deprovision.
func (c *Client) ListGrants(ctx context.Context, tenantID, principalType, principalID string) ([]Grant, error) {
	tok, err := c.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("get bearer token for list grants request: %w", err)
	}

	u, err := url.Parse(c.serviceURL + "/api/v1/policy/grants")
	if err != nil {
		return nil, fmt.Errorf("parse list grants URL: %w", err)
	}
	q := u.Query()
	q.Set("tenant_id", tenantID)
	q.Set("principal_type", principalType)
	q.Set("principal_id", principalID)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build list grants request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)

	_, span, resp, err := c.Do(ctx, "policy.Client.ListGrants", req)
	defer span.End()
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, c.HandleErrorResponse(span, resp, fmt.Errorf("list grants failed: status %d", resp.StatusCode))
	}

	var result struct {
		Grants []Grant `json:"grants"`
	}
	if err := c.DecodeJSON(span, resp, &result); err != nil {
		return nil, fmt.Errorf("decode list grants response: %w", err)
	}
	return result.Grants, nil
}

// GetGrant returns a grant by ID. Returns ErrGrantNotFound if it does not exist.
func (c *Client) GetGrant(ctx context.Context, grantID string) (*Grant, error) {
	tok, err := c.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("get bearer token for get grant request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.serviceURL+"/api/v1/policy/grants/"+url.PathEscape(grantID), nil)
	if err != nil {
		return nil, fmt.Errorf("build get grant request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)

	_, span, resp, err := c.Do(ctx, "policy.Client.GetGrant", req)
	defer span.End()
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	switch resp.StatusCode {
	case http.StatusOK:
		var g Grant
		if err := c.DecodeJSON(span, resp, &g); err != nil {
			return nil, fmt.Errorf("decode get grant response: %w", err)
		}
		return &g, nil
	case http.StatusNotFound:
		return nil, ErrGrantNotFound
	default:
		return nil, c.HandleErrorResponse(span, resp, fmt.Errorf("get grant failed: status %d", resp.StatusCode))
	}
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

// vocabularyScope returns the scope a policy service 400 names when it refuses
// a scope outside the vocabulary. The service sets details.scope only then.
func vocabularyScope(body string) (string, bool) {
	var e struct {
		Code    string `json:"code"`
		Details struct {
			Scope string `json:"scope"`
		} `json:"details"`
	}
	if json.Unmarshal([]byte(body), &e) != nil || e.Code != "invalid_field" || e.Details.Scope == "" {
		return "", false
	}
	return e.Details.Scope, true
}
