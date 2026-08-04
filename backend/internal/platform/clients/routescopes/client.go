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
	// Requires lists other scopes this scope depends on: holding this scope in a
	// grant additionally confers the required scopes in the compiled effective
	// set (not in the stored grant). Used by first-party platform MCP tools whose
	// mcp:<server>:<tool>:use scope needs the underlying platform scope the tool
	// calls upstream (e.g. mcp:orbit:agents:list:use requires agents:types:read).
	// Empty for ordinary scopes and every external MCP.
	Requires []string `json:"requires,omitempty"`
}

// registrationRequest is the wire format for POST /api/v1/policy/routes/register.
type registrationRequest struct {
	Service string             `json:"service"`
	Routes  []RouteScope       `json:"routes"`
	Scopes  []ScopeDeclaration `json:"scopes,omitempty"`
	// SkipPrune, when true, upserts the declared scopes WITHOUT running the
	// destructive orphan-prune (DeleteOrphans + PurgeScopeFromAll). Default
	// false preserves the prune-on-publish behavior every existing caller
	// relies on. The tool-gateway sets it true while its introspection
	// keep-list is incomplete (cold/partial start), so a partial list cannot
	// revoke live mcp scopes from grants/roles.
	SkipPrune bool `json:"skip_prune,omitempty"`
}

// Client registers a service's routes and scopes with the policy service.
type Client struct {
	*httpclient.Base
	serviceURL string
}

const (
	tracerName    = "orbit.routescopes-client"
	clientTimeout = 10 * time.Second
)

// NewClient creates a Client targeting the given policy service base URL,
// e.g. "http://orbit-policy-service.orbit-system.svc".
func NewClient(serviceURL string, logger logging.Logger) *Client {
	return &Client{
		Base:       httpclient.New(tracerName, logger, clientTimeout),
		serviceURL: serviceURL,
	}
}

// Register sends the service's route→scope mappings and scope declarations to
// the policy service. The scope declarations are upserted into the vocabulary so
// grants and roles can reference them. Routes are compiled into OPA bundles for
// Envoy enforcement.
//
// One attempt, no retry: the caller's loop owns recovery. [KeepRegistered] is
// that loop for every service that keeps itself registered, and it already
// retries with backoff until the policy service (and the identity-proxy sidecar
// minting this call's token) is up. A second retry layer here only stacks
// underneath it, turning one tick of the caller's loop into minutes of blocking.
func (c *Client) Register(ctx context.Context, service string, routes []RouteScope, scopes []ScopeDeclaration) error {
	return c.register(ctx, registrationRequest{Service: service, Routes: routes, Scopes: scopes})
}

// RegisterWithPrune is Register with explicit control over the destructive
// orphan-prune. With prune=false the policy service upserts the declared scopes
// but does NOT remove this service's other scopes — used by the tool-gateway's
// prune-guard to publish newly-discovered tools while its keep-list is still
// incomplete. prune=true is identical to Register.
func (c *Client) RegisterWithPrune(ctx context.Context, service string, routes []RouteScope, scopes []ScopeDeclaration, prune bool) error {
	return c.register(ctx, registrationRequest{Service: service, Routes: routes, Scopes: scopes, SkipPrune: !prune})
}

// register makes exactly one registration call. Failure is returned to the
// caller, which owns whatever retry policy it wants.
func (c *Client) register(ctx context.Context, reg registrationRequest) error {
	if err := c.doRegister(ctx, reg); err != nil {
		return err
	}
	c.Logger().Info(ctx, "route scopes registered",
		logging.Field{Key: "service", Value: reg.Service},
		logging.Field{Key: "route_count", Value: len(reg.Routes)},
		logging.Field{Key: "scope_count", Value: len(reg.Scopes)},
	)
	return nil
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
