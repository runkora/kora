// Package routescopes provides the client for registering a service's route→scope
// mappings and scope vocabulary with the policy service at startup.
//
// Each service declares which scopes are required to access its HTTP routes, and
// declares the scopes it owns. The policy service upserts those scopes into the
// vocabulary and compiles the routes into OPA bundles for Envoy enforcement.
//
// The calling service's platform token (injected automatically by the
// identity-proxy sidecar) determines registration scope:
//
//   - SYSTEM principal  → global registration, compiled into every tenant bundle
//   - SERVICE principal → tenant-scoped registration, compiled into that tenant's bundle only
//
// Usage:
//
//	client := routescopes.NewClient("http://orbit-policy-service.orbit-system.svc", logger)
//	if err := client.Register(ctx, "inference-gateway",
//	    []routescopes.RouteScope{
//	        {PathPrefix: "/v1/inference/", Method: "POST", RequiredScope: "inference:invoke"},
//	        {PathPrefix: "/v1/inference/", Method: "GET",  RequiredScope: "inference:list"},
//	    },
//	    []routescopes.ScopeDeclaration{
//	        {Scope: "inference:invoke", ResourceType: "inference", PathSegments: []string{}, Action: "invoke", Description: "Invoke any inference model"},
//	        {Scope: "inference:list",   ResourceType: "inference", PathSegments: []string{}, Action: "list",   Description: "List inference models"},
//	    },
//	); err != nil {
//	    logger.Error(ctx, "route registration failed", logging.Field{Key: "error", Value: err.Error()})
//	}
package routescopes

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/clear-route/orbit/internal/platform/clients/httpclient"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// ErrRegistrationFailed is returned when the policy service rejects a registration.
var ErrRegistrationFailed = errors.New("route scope registration rejected by policy service")

// RouteScope maps an HTTP path prefix and method to the scope required to access it.
type RouteScope struct {
	PathPrefix    string `json:"path_prefix"`
	Method        string `json:"method"`
	RequiredScope string `json:"required_scope"`
}

// ScopeDeclaration declares a scope that this service owns. The policy service
// upserts these into the vocabulary so grants and roles can reference them.
type ScopeDeclaration struct {
	Scope        string   `json:"scope"`
	ResourceType string   `json:"resource_type"`
	PathSegments []string `json:"path"`
	Action       string   `json:"action"`
	Description  string   `json:"description,omitempty"`
}

// registrationRequest is the wire format for POST /api/v1/policy/routes/register.
type registrationRequest struct {
	Service string             `json:"service"`
	Routes  []RouteScope       `json:"routes"`
	Scopes  []ScopeDeclaration `json:"scopes,omitempty"`
}

// Client registers a service's routes and scopes with the policy service.
type Client struct {
	*httpclient.Base
	serviceURL   string
	initialDelay time.Duration
}

const (
	tracerName          = "orbit.routescopes-client"
	clientTimeout       = 10 * time.Second
	defaultInitialDelay = 15 * time.Second
)

// Option configures the Client.
type Option func(*Client)

// WithInitialDelay overrides the default 15-second startup delay that allows
// the identity-proxy sidecar to acquire a platform token before the first
// registration attempt.
func WithInitialDelay(d time.Duration) Option {
	return func(c *Client) { c.initialDelay = d }
}

// NewClient creates a Client targeting the given policy service base URL,
// e.g. "http://orbit-policy-service.orbit-system.svc".
func NewClient(serviceURL string, logger logging.Logger, opts ...Option) *Client {
	c := &Client{
		Base:         httpclient.New(tracerName, logger, clientTimeout),
		serviceURL:   serviceURL,
		initialDelay: defaultInitialDelay,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Register sends the service's route→scope mappings and scope declarations to
// the policy service. The scope declarations are upserted into the vocabulary so
// grants and roles can reference them. Routes are compiled into OPA bundles for
// Envoy enforcement.
//
// Retries up to maxAttempts times with exponential backoff (capped at 30s). An
// initial delay allows the identity-proxy sidecar to exchange its SPIFFE SVID
// for a platform token before the first attempt.
func (c *Client) Register(ctx context.Context, service string, routes []RouteScope, scopes []ScopeDeclaration) error {
	const (
		maxAttempts = 15
		maxBackoff  = 30 * time.Second
	)

	if c.initialDelay > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(c.initialDelay):
		}
	}

	reg := registrationRequest{Service: service, Routes: routes, Scopes: scopes}
	backoff := 2 * time.Second

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := c.doRegister(ctx, reg); err != nil {
			lastErr = err
			c.Logger().Warn(ctx, "route scope registration attempt failed, retrying",
				logging.Field{Key: "service", Value: service},
				logging.Field{Key: "attempt", Value: attempt},
				logging.Field{Key: "error", Value: err.Error()},
			)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
				if backoff < maxBackoff {
					backoff *= 2
					if backoff > maxBackoff {
						backoff = maxBackoff
					}
				}
			}
			continue
		}
		c.Logger().Info(ctx, "route scopes registered",
			logging.Field{Key: "service", Value: service},
			logging.Field{Key: "route_count", Value: len(routes)},
			logging.Field{Key: "scope_count", Value: len(scopes)},
		)
		return nil
	}
	return fmt.Errorf("route scope registration failed after %d attempts: %w", maxAttempts, lastErr)
}

func (c *Client) doRegister(ctx context.Context, reg registrationRequest) error {
	body, err := c.EncodeJSON(reg)
	if err != nil {
		return fmt.Errorf("encode registration: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.serviceURL+"/api/v1/policy/routes/register", body)
	if err != nil {
		return fmt.Errorf("build registration request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	_, span, resp, err := c.Do(ctx, "routescopes.Client.Register", req)
	defer span.End()
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusNoContent {
		return c.HandleErrorResponse(span, resp, ErrRegistrationFailed)
	}
	return nil
}
