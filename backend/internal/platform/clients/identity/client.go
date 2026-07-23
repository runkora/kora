// Package identity provides a typed HTTP client for the Orbit identity service.
//
// This is the platform-layer client that satisfies the docs/adr/2026-03-19-multi-service-backend-deployment-structure.md mandate for
// service-to-service HTTP calls. Any Orbit service or sidecar that needs to
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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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

// Client is a typed HTTP client for the Orbit identity service.
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

// NewClient constructs a Client for the Orbit identity service.
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

// ── Delegation issuer endpoints (docs/adr/2026-07-23-delegated-agent-identity.md) ──

// CreateDelegationRequest is the client's view of the POST
// /api/v1/identity/delegation request body.
type CreateDelegationRequest struct {
	// AgentPrincipalID is the SPIFFE URI of the agent being delegated to (act.sub).
	AgentPrincipalID string `json:"agent_principal_id"`
	// RunID binds the delegation to a single run.
	RunID string `json:"run_id"`
	// SubjectToken is the RFC 8693 subject_token: the DELEGATOR's platform
	// token, carried in the body because the Authorization header of a pod
	// with an identity-proxy sidecar is overwritten with the pod's own service
	// token on egress — a forwarded caller bearer cannot survive that hop.
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
// authenticates the TRANSPORT (in-cluster the identity-proxy overwrites it
// with the pod's own token, which is fine); the delegator's identity travels
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
