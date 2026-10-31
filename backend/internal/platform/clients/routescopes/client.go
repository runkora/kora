// Package routescopes publishes a service's route→scope mappings and scope
// vocabulary to the policy service, and keeps them published.
//
// Each service declares which scopes are required to access its HTTP routes, and
// declares the scopes it owns. The policy service upserts those scopes into the
// vocabulary and compiles the routes into OPA bundles for Envoy enforcement.
//
// The client attaches the calling service's own platform token, taken from the
// token source it is built with. Platform services pass the identity client's
// SelfTokenSource, which exchanges the pod's SVID in-process. The token's
// principal determines registration scope:
//
//   - SYSTEM principal  → global registration, compiled into every tenant bundle.
//     The policy service accepts it only for the caller's own service name.
//   - SERVICE principal → tenant-scoped registration, compiled into that tenant's bundle only
//
// # Register through KeepRegistered, not once at startup
//
// The policy service holds route registrations in memory, so they are lost when
// it restarts, and Layer 1 enforcement denies an unregistered path outright. A
// service that registers once at boot is therefore one policy-service restart
// away from having every scoped request denied, with nothing to recover it.
// That is not hypothetical: it is what [issue #170] was.
//
// So [KeepRegistered] owns registration. Give it a register function; it retries
// until the first success and then re-publishes on a fixed cadence. Call
// [Client.Register] directly only from inside that function. Use
// [RepublishOnEvent] when a domain event changes what the service publishes and
// waiting for the next tick would leave the vocabulary stale.
//
// Usage:
//
//	client := routescopes.NewClient("http://kora-policy-service.kora-system.svc", identityClient.SelfTokenSource(), logger)
//	register := func(ctx context.Context) error {
//	    return client.Register(ctx, "inference-gateway", inferenceRoutes(), inferenceScopes())
//	}
//	go routescopes.KeepRegistered(ctx, logger, "inference-gateway", register)
//
// [issue #170]: https://github.com/runkora/kora/issues/170
package routescopes

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/runkora/kora/backend/internal/platform/clients/httpclient"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// ErrRegistrationFailed is returned when the policy service rejects a registration.
var ErrRegistrationFailed = errors.New("route scope registration rejected by policy service")

// errRegisterFailed is the static span status for a registration that failed.
var errRegisterFailed = errors.New("route scope registration failed")

const registerSpan = "routescopes.Client.Register"

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
	// set (not in the stored grant), for example the GitHub repo umbrella
	// conferring each category scope.
	//
	// nil is sent as null and means unknown: the stored set is kept. A non-nil
	// empty slice is sent as [] and clears it. Only the owning service's
	// declaration changes a stored entry.
	Requires []string `json:"requires"`
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

// TokenSource returns the bearer token the registration call carries.
type TokenSource func(context.Context) (string, error)

// Client registers a service's routes and scopes with the policy service.
type Client struct {
	*httpclient.Base
	serviceURL string
	token      TokenSource

	mu   sync.Mutex
	sent map[string][sha256.Size]byte // service → digest of its last accepted registration
}

const (
	tracerName    = "kora.routescopes-client"
	clientTimeout = 10 * time.Second
)

// NewClient creates a Client targeting the given policy service base URL,
// e.g. "http://kora-policy-service.kora-system.svc". token supplies the
// caller's own platform token for each registration.
func NewClient(serviceURL string, token TokenSource, logger logging.Logger) *Client {
	return &Client{
		Base:       httpclient.New(tracerName, logger, clientTimeout),
		serviceURL: serviceURL,
		token:      token,
		sent:       make(map[string][sha256.Size]byte),
	}
}

// Register sends the service's route→scope mappings and scope declarations to
// the policy service. The scope declarations are upserted into the vocabulary so
// grants and roles can reference them. Routes are compiled into OPA bundles for
// Envoy enforcement.
//
// One attempt, no retry: the caller's loop owns recovery. [KeepRegistered] is
// that loop for every service that keeps itself registered, and it already
// retries with backoff until the policy service and the identity service that
// mints this call's token are up. A second retry layer here only stacks
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
//
// Registrations run from loops and hooks with no parent span, so Base.Do
// records none. A changed or failed registration gets a summary span instead;
// a repeat of the last accepted one is a heartbeat and gets none.
func (c *Client) register(ctx context.Context, reg registrationRequest) error {
	raw, err := json.Marshal(reg)
	if err != nil {
		return fmt.Errorf("encode registration: %w", err)
	}
	sum := sha256.Sum256(raw)
	c.mu.Lock()
	repeat := c.sent[reg.Service] == sum
	c.mu.Unlock()

	start := time.Now()
	err = c.doRegister(ctx, reg)
	if !repeat || err != nil {
		c.SpanSince(ctx, registerSpan, start, err, errRegisterFailed)
	}
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.sent[reg.Service] = sum
	c.mu.Unlock()
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
	tok, err := c.token(ctx)
	if err != nil {
		return fmt.Errorf("get platform token for registration: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)

	_, span, resp, err := c.Do(ctx, registerSpan, req)
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
