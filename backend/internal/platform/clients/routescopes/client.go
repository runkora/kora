// Package routescopes provides the client for registering a service's route→scope
// mappings with the policy service internal endpoint at startup. Each service
// declares which scopes are required to access its HTTP routes; the policy service
// compiles these into OPA bundles so the Envoy sidecar can enforce them.
//
// Usage:
//
//	client := routescopes.NewClient("http://orbit-policy-service.orbit-system.svc:9091", logger)
//	if err := client.Register(ctx, routescopes.Registration{
//	    Service: "inference-gateway",
//	    Routes: []routescopes.RouteScope{
//	        {PathPrefix: "/v1/inference/", Method: "POST", RequiredScope: "inference:invoke"},
//	        {PathPrefix: "/v1/inference/", Method: "GET",  RequiredScope: "inference:list"},
//	    },
//	}); err != nil {
//	    logger.Error(ctx, "route registration failed", logging.Field{Key: "error", Value: err.Error()})
//	}
//
// Tenant-scoped registration (for tenant-published services) sets TenantID.
// Global registration (for platform services) omits TenantID.
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

// Registration describes a service's route→scope declarations.
// TenantID empty means global (compiled into every tenant bundle).
// TenantID set means tenant-scoped (compiled into that tenant's bundle only).
type Registration struct {
	Service  string       `json:"service"`
	TenantID string       `json:"tenant_id,omitempty"`
	Routes   []RouteScope `json:"routes"`
}

// Client registers a service's routes with the policy service internal endpoint.
type Client struct {
	*httpclient.Base
	internalURL string
}

const (
	tracerName    = "orbit.routescopes-client"
	clientTimeout = 10 * time.Second
)

// NewClient creates a Client targeting the given policy service internal base URL,
// e.g. "http://orbit-policy-service.orbit-system.svc:9091".
func NewClient(internalURL string, logger logging.Logger) *Client {
	return &Client{
		Base:        httpclient.New(tracerName, logger, clientTimeout),
		internalURL: internalURL,
	}
}

// Register sends the route registration to the policy service, retrying up to
// 5 times with exponential backoff. The retry handles the case where the policy
// service is not yet ready when the calling service starts.
func (c *Client) Register(ctx context.Context, reg Registration) error {
	const maxAttempts = 5
	backoff := 2 * time.Second

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := c.doRegister(ctx, reg); err != nil {
			lastErr = err
			c.Logger().Warn(ctx, "route scope registration attempt failed, retrying",
				logging.Field{Key: "service", Value: reg.Service},
				logging.Field{Key: "attempt", Value: attempt},
				logging.Field{Key: "error", Value: err.Error()},
			)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
				backoff *= 2
			}
			continue
		}
		c.Logger().Info(ctx, "route scopes registered",
			logging.Field{Key: "service", Value: reg.Service},
			logging.Field{Key: "tenant_id", Value: reg.TenantID},
			logging.Field{Key: "route_count", Value: len(reg.Routes)},
		)
		return nil
	}
	return fmt.Errorf("route scope registration failed after %d attempts: %w", maxAttempts, lastErr)
}

func (c *Client) doRegister(ctx context.Context, reg Registration) error {
	body, err := c.EncodeJSON(reg)
	if err != nil {
		return fmt.Errorf("encode registration: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.internalURL+"/internal/routes/register", body)
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
