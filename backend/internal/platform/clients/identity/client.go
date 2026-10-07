// Package identity provides a typed HTTP client for the kora identity service.
//
// This is the platform-layer client that satisfies the docs/adr/2026-03-19-multi-service-backend-deployment-structure.md mandate for
// service-to-service HTTP calls. Any kora service or sidecar that needs to
// call the identity service must use this client rather than constructing a raw
// http.Client inline.
//
// Wire type ownership:
// The types in this package represent the consumer's view of the identity
// service HTTP wire format. They are NOT imported from internal/identity —
// that would couple this client to the identity service's internal domain types.
// If the identity service refactors its internal structs but keeps the JSON
// output identical, this client is unaffected. See docs/adr/2026-03-19-multi-service-backend-deployment-structure.md §Wire type ownership.
package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/clear-route/orbit/internal/platform/clients/httpclient"
	"github.com/clear-route/orbit/internal/platform/logging"
)

const (
	tracerName    = "orbit.identity-client"
	clientTimeout = 10 * time.Second
)

// Sentinel errors for the identity client. Callers may test for these with
// errors.Is to distinguish failure categories without string matching.
var (
	// ErrUnexpectedStatus is returned when the identity service responds with
	// any HTTP status other than 200 OK.
	ErrUnexpectedStatus = errors.New("unexpected status from identity exchange")

	// ErrMissingToken is returned when the identity exchange response decodes
	// successfully but contains an empty access_token field.
	ErrMissingToken = errors.New("identity exchange response missing access token")

	// ErrDelegationNotFound is returned by GetDelegationForRun when the identity
	// service reports no active delegation issuance for the run (HTTP 404 with
	// code "delegation_not_found") — the run is autonomous, or the issuance was
	// revoked. Callers treat this as "no delegation", never as a failure.
	ErrDelegationNotFound = errors.New("no active delegation issuance for run")

	// ErrDelegationForbidden is returned when the identity service refuses a
	// delegation operation (HTTP 403) — e.g. the caller's principal type may
	// not mint, or the retrieval caller's attested identity does not match the
	// delegation's agent principal.
	ErrDelegationForbidden = errors.New("delegation operation forbidden")
)

// ErrDecodeResponse is an alias for httpclient.ErrDecodeBody scoped to this
// package. Callers can test for decode failures with either sentinel:
//
//	errors.Is(err, identity.ErrDecodeResponse)
//	errors.Is(err, httpclient.ErrDecodeBody)
var ErrDecodeResponse = httpclient.ErrDecodeBody

// exchangeResponse is the client's own representation of the identity service's
// /api/v1/identity/exchange wire format. Only the fields the client cares about
// are decoded; additional fields in the response are silently ignored.
type exchangeResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"` // seconds
	IssuedAt    int64  `json:"issued_at"`  // Unix seconds
}

// Client is a typed HTTP client for the kora identity service.
//
// In the Istio mesh, Envoy intercepts outbound HTTP from the caller's pod and
// upgrades it to mTLS using the pod's SPIRE-issued SVID automatically (via the
// "spire" injection template). The identity service's Envoy terminates mTLS and
// injects the x-forwarded-client-cert (XFCC) header from the validated peer
// cert, which the identity service reads to establish the caller's principal
// (docs/adr/2026-03-05-spiffe-spire-workload-attestation.md, docs/adr/2026-03-05-universal-gateway-policy-enforcement.md, docs/adr/2026-03-05-service-mesh-and-network-enforcement-infrastructure.md, docs/adr/2026-03-21-envoy-ext-proc-sidecar-token-injection.md).
//
// We do NOT perform manual mTLS here — doing so would produce a double-TLS
// situation where Envoy sees an opaque TCP stream and never injects XFCC.
//
// The zero value is not usable — construct with NewClient.
type Client struct {
	*httpclient.Base
	// baseURL is the scheme+host base URL of the identity service,
	// e.g. "http://orbit-identity.orbit-system.svc.cluster.local".
	// Do NOT include a path suffix or trailing slash.
	// Note: plain HTTP — Istio handles the mTLS upgrade transparently.
	// Supplied via ORBIT_IDENTITY_SERVICE_URL environment variable.
	baseURL string
}

// NewClient constructs a Client for the kora identity service.
//
// baseURL must be the in-cluster scheme+host only, e.g.
// "http://orbit-identity.orbit-system.svc.cluster.local" (no path, no trailing
// slash). The full exchange path (/api/v1/identity/exchange) is appended by
// Exchange().
func NewClient(baseURL string, logger logging.Logger) *Client {
	return &Client{
		Base:    httpclient.New(tracerName, logger, clientTimeout),
		baseURL: baseURL,
	}
}

// Exchange calls POST {baseURL}/api/v1/identity/exchange with no request body.
//
// Istio's Envoy sidecar presents the pod's SVID as the mTLS client certificate
// on the outbound connection. No credential material passes through this
// process — identity is asserted at the transport layer by SPIRE/Istio.
//
// Full URL example:
//
//	http://orbit-identity.orbit-system.svc.cluster.local/api/v1/identity/exchange
//
// Returns the platform token string and its absolute expiry time.
func (c *Client) Exchange(ctx context.Context) (token string, expiry time.Time, err error) {
	endpoint := c.baseURL + "/api/v1/identity/exchange"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, http.NoBody)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("build exchange request: %w", err)
	}

	ctx, span, resp, err := c.Do(ctx, "identity.Client.Exchange", req)
	defer span.End()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("exchange call: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return "", time.Time{}, c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
	}

	var parsed exchangeResponse
	if err = c.DecodeJSON(span, resp, &parsed); err != nil {
		return "", time.Time{}, err
	}

	if parsed.AccessToken == "" {
		return "", time.Time{}, c.RecordError(span, ErrMissingToken)
	}

	// Derive absolute expiry from issued_at + expires_in.
	// Fall back to now + expires_in if issued_at is absent (e.g. in tests).
	var issuedAt time.Time
	if parsed.IssuedAt > 0 {
		issuedAt = time.Unix(parsed.IssuedAt, 0)
	} else {
		issuedAt = time.Now()
	}
	tokenExpiry := issuedAt.Add(time.Duration(parsed.ExpiresIn) * time.Second)

	span.SetAttributes(
		attribute.Int("token.expires_in", parsed.ExpiresIn),
		attribute.String("token.expiry", tokenExpiry.UTC().Format(time.RFC3339)),
	)

	c.Logger().Info(ctx, "platform token exchanged",
		logging.Field{Key: "expires_in", Value: parsed.ExpiresIn},
		logging.Field{Key: "expiry", Value: tokenExpiry.UTC().Format(time.RFC3339)},
	)

	return parsed.AccessToken, tokenExpiry, nil
}

// SelfTokenSource returns a token func that exchanges this pod's SVID for a
// platform token via Exchange, caching it until shortly before expiry.
//
// The exchange is a network call on the mesh, so caching keeps it off the hot
// path; the early refresh margin avoids handing out a token that expires
// mid-request. Safe for concurrent use — callers such as the policy client
// invoke it from concurrent request handlers.
//
// Each call returns an independent cache; construct one source per binary and
// share it between the components that need a self token.
func (c *Client) SelfTokenSource() func(context.Context) (string, error) {
	const refreshMargin = 30 * time.Second

	var (
		mu     sync.Mutex
		cached string
		expiry time.Time
	)
	return func(ctx context.Context) (string, error) {
		mu.Lock()
		defer mu.Unlock()

		if cached != "" && time.Now().Before(expiry.Add(-refreshMargin)) {
			return cached, nil
		}
		tok, exp, err := c.Exchange(ctx)
		if err != nil {
			return "", fmt.Errorf("exchange SVID for platform token: %w", err)
		}
		cached, expiry = tok, exp
		return cached, nil
	}
}

// ── Delegation issuer endpoints (docs/adr/2026-07-23-delegated-agent-identity.md) ──

// CreateDelegationRequest is the client's view of the POST
// /api/v1/identity/delegation request body.
type CreateDelegationRequest struct {
	// AgentPrincipalID is the SPIFFE URI of the agent being delegated to (act.sub).
	AgentPrincipalID string `json:"agent_principal_id"`
	// RunID binds the delegation to a single run.
	RunID string `json:"run_id"`
	// SubjectToken is the RFC 8693 subject_token: the DELEGATOR's platform
	// token. The Authorization header carries the calling service's own token.
	SubjectToken string `json:"subject_token,omitempty"`
}

// DelegationIssuance is the client's view of the issuance record the identity
// service returns. It never carries a token — the signed delegation JWT is
// released only to mesh-attested identity-proxies via GetDelegationForRun.
type DelegationIssuance struct {
	JTI                    string `json:"jti"`
	RunID                  string `json:"run_id"`
	TenantID               string `json:"tenant_id"`
	DelegatorPrincipalID   string `json:"delegator_principal_id"`
	DelegatorPrincipalType string `json:"delegator_principal_type"`
	AgentPrincipalID       string `json:"agent_principal_id"`
}

// DelegationToken is the client's view of the GET
// /api/v1/identity/delegation/run/{run_id} response: a freshly re-signed
// delegation JWT and its lifetime.
type DelegationToken struct {
	DelegationToken string `json:"delegation_token"`
	ExpiresIn       int    `json:"expires_in"` // seconds
	RunID           string `json:"run_id"`
}

// errorBody is the platform API error wire shape ({"code": …, "message": …}),
// decoded from non-2xx responses to branch on distinct error codes.
type errorBody struct {
	Code string `json:"code"`
}

// CreateDelegation calls POST {baseURL}/api/v1/identity/delegation. The bearer
// is the calling service's own token and authenticates the TRANSPORT; the delegator's identity travels
// as req.SubjectToken, verified server-side, so identity never mints delegated
// authority the runner does not hold (ADR Decision 3).
func (c *Client) CreateDelegation(ctx context.Context, bearer string, req CreateDelegationRequest) (*DelegationIssuance, error) {
	body, err := c.EncodeJSON(req)
	if err != nil {
		return nil, fmt.Errorf("build delegation request: %w", err)
	}
	endpoint := c.baseURL + "/api/v1/identity/delegation"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("build delegation request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+bearer)

	ctx, span, resp, err := c.Do(ctx, "identity.Client.CreateDelegation", httpReq)
	defer span.End()
	if err != nil {
		return nil, fmt.Errorf("create delegation call: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	switch resp.StatusCode {
	case http.StatusCreated, http.StatusOK:
		// Fall through to decode.
	case http.StatusForbidden:
		return nil, c.HandleErrorResponse(span, resp, ErrDelegationForbidden)
	default:
		return nil, c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
	}

	var issuance DelegationIssuance
	if err = c.DecodeJSON(span, resp, &issuance); err != nil {
		return nil, err
	}

	c.Logger().Info(ctx, "delegation issuance minted",
		logging.Field{Key: "jti", Value: issuance.JTI},
		logging.Field{Key: "run_id", Value: issuance.RunID},
	)
	return &issuance, nil
}

// RevokeDelegation calls DELETE {baseURL}/api/v1/identity/delegation/{jti}
// with the given bearer token. orbit-agents calls this with its own exchanged
// SERVICE platform token on terminal run state — the endpoint permits the
// delegator or SERVICE/SYSTEM principals. Revoking an already-revoked or
// missing issuance is treated as success (the tombstone is the desired end
// state), so terminal-state cleanup is idempotent.
func (c *Client) RevokeDelegation(ctx context.Context, bearer, jti string) error {
	endpoint := c.baseURL + "/api/v1/identity/delegation/" + jti
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, http.NoBody)
	if err != nil {
		return fmt.Errorf("build delegation revoke request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)

	ctx, span, resp, err := c.Do(ctx, "identity.Client.RevokeDelegation", req)
	defer span.End()
	if err != nil {
		return fmt.Errorf("revoke delegation call: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	switch resp.StatusCode {
	case http.StatusNoContent, http.StatusOK, http.StatusNotFound:
		// 404 = already gone; the desired end state holds.
	case http.StatusForbidden:
		return c.HandleErrorResponse(span, resp, ErrDelegationForbidden)
	default:
		return c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
	}

	c.Logger().Info(ctx, "delegation revoked",
		logging.Field{Key: "jti", Value: jti},
	)
	return nil
}

// GetDelegationForRun calls GET {baseURL}/api/v1/identity/delegation/run/{runID}
// with no bearer token: the endpoint authenticates by the caller's mesh-attested
// SPIFFE identity from the XFCC header, exactly like Exchange. The identity
// service releases the token only when that identity matches the delegation's
// agent principal (ADR Decision 3).
//
// Returns ErrDelegationNotFound when the run has no active delegation — the
// identity-proxy caches this as "autonomous, none".
func (c *Client) GetDelegationForRun(ctx context.Context, runID string) (*DelegationToken, error) {
	endpoint := c.baseURL + "/api/v1/identity/delegation/run/" + runID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build delegation retrieval request: %w", err)
	}

	_, span, resp, err := c.Do(ctx, "identity.Client.GetDelegationForRun", req)
	defer span.End()
	if err != nil {
		return nil, fmt.Errorf("get delegation for run call: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	switch resp.StatusCode {
	case http.StatusOK:
		// Fall through to decode.
	case http.StatusNotFound:
		// Distinct code check keeps a routing-level 404 (wrong URL, service
		// misdeploy) from being mistaken for "autonomous": only the identity
		// service's explicit delegation_not_found is a negative result.
		var eb errorBody
		if decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 512)).Decode(&eb); decodeErr == nil && eb.Code == "delegation_not_found" {
			span.SetAttributes(attribute.String("delegation.result", "none"))
			return nil, ErrDelegationNotFound
		}
		return nil, c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
	case http.StatusForbidden:
		return nil, c.HandleErrorResponse(span, resp, ErrDelegationForbidden)
	default:
		return nil, c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
	}

	var out DelegationToken
	if err = c.DecodeJSON(span, resp, &out); err != nil {
		return nil, err
	}
	if out.DelegationToken == "" {
		return nil, c.RecordError(span, ErrMissingToken)
	}
	return &out, nil
}

// ── Service-token minting (docs/adr/2026-06-20-extended-trigger-types.md) ────

// ErrMintForbidden is returned when identity refuses to mint a service token
// for this caller. It means the caller's mesh-attested SPIFFE identity is not
// on identity's mint allowlist, which is a deployment fault (wrong service
// account, missing sidecar) rather than a transient error — retrying will not
// help.
var ErrMintForbidden = errors.New("identity refused to mint a service token for this caller")

type mintServiceTokenRequest struct {
	TenantID    string `json:"tenant_id"`
	PrincipalID string `json:"principal_id"`
	DisplayName string `json:"display_name"`
	TTLSeconds  int    `json:"ttl_seconds"`
}

type mintServiceTokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

// MintServiceToken obtains a short-lived platform token for the named SERVICE
// principal by calling identity, the platform's sole token authority.
//
// The caller is authenticated by its mesh identity: Istio's sidecar presents
// this pod's SVID on the outbound connection and identity reads it from the
// XFCC header. No credential material passes through this process, and nothing
// here can be leaked or replayed.
//
// This satisfies the triggers package's TokenMinter interface, which
// orbit-triggers uses in place of the in-process identity service.
func (c *Client) MintServiceToken(ctx context.Context, tenantID, principalID, displayName string, ttl time.Duration) (string, error) {
	endpoint := c.baseURL + "/api/v1/identity/internal/service-token"

	body, err := json.Marshal(mintServiceTokenRequest{
		TenantID:    tenantID,
		PrincipalID: principalID,
		DisplayName: displayName,
		TTLSeconds:  int(ttl.Seconds()),
	})
	if err != nil {
		return "", fmt.Errorf("marshal mint request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build mint request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	_, span, resp, err := c.Do(ctx, "identity.Client.MintServiceToken", req)
	defer span.End()
	if err != nil {
		return "", fmt.Errorf("mint call: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusForbidden, http.StatusUnauthorized:
		return "", c.HandleErrorResponse(span, resp, ErrMintForbidden)
	default:
		return "", c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
	}

	var parsed mintServiceTokenResponse
	if err = c.DecodeJSON(span, resp, &parsed); err != nil {
		return "", err
	}
	if parsed.AccessToken == "" {
		return "", c.RecordError(span, ErrMissingToken)
	}

	span.SetAttributes(attribute.Int("token.expires_in", parsed.ExpiresIn))

	return parsed.AccessToken, nil
}
