// Package identity provides a typed HTTP client for the Orbit identity service.
//
// This is the platform-layer client that satisfies the ADR-0020 mandate for
// service-to-service HTTP calls. Any Orbit service or sidecar that needs to
// call the identity service must use this client rather than constructing a raw
// http.Client inline.
//
// Wire type ownership:
// The types in this package represent the consumer's view of the identity
// service HTTP wire format. They are NOT imported from internal/identity —
// that would couple this client to the identity service's internal domain types.
// If the identity service refactors its internal structs but keeps the JSON
// output identical, this client is unaffected. See ADR-0020 §Wire type ownership.
package identity

import (
	"context"
	"errors"
	"fmt"
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
// (ADR-0010, ADR-0011, ADR-0015, ADR-0021).
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
