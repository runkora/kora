package identityproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// exchangeResponse mirrors the SessionResponse fields we care about.
// The identity service returns a full SessionResponse; we only need the
// access token and its TTL to populate the cache.
type exchangeResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"` // seconds
	IssuedAt    int64  `json:"issued_at"`  // Unix seconds
}

// Exchanger performs the POST /api/v1/identity/exchange call against the Orbit
// identity service.
//
// In the Istio mesh, Envoy intercepts outbound HTTP from the identity-proxy
// sidecar and upgrades it to mTLS using the pod's SPIRE-issued SVID
// automatically (via the "spire" injection template). The identity service's
// Envoy terminates mTLS and injects the x-forwarded-client-cert (XFCC) header
// from the validated peer cert. The identity service then reads the XFCC to
// establish the caller's principal (ADR-0010, ADR-0011, ADR-0015, ADR-0021).
//
// We do NOT perform manual mTLS here — doing so would produce a double-TLS
// situation where Envoy sees an opaque TCP stream and never injects XFCC.
type Exchanger struct {
	// identityServiceURL is the scheme+host base URL of the identity service,
	// e.g. "http://orbit-identity.orbit-system.svc.cluster.local"
	// Do NOT include a path suffix or trailing slash — Exchange() appends
	// "/api/v1/identity/exchange" itself.
	// Note: plain HTTP — Istio handles the mTLS upgrade transparently.
	// Supplied via ORBIT_IDENTITY_SERVICE_URL environment variable.
	identityServiceURL string
	client             *http.Client
	logger             logging.Logger
	tracer             trace.Tracer
}

// NewExchanger constructs an Exchanger. identityServiceURL must be the
// in-cluster scheme+host only, e.g.
// "http://orbit-identity.orbit-system.svc.cluster.local" (no path, no trailing slash).
// The full exchange path (/api/v1/identity/exchange) is appended by Exchange().
// Supplied via the ORBIT_IDENTITY_SERVICE_URL environment variable.
func NewExchanger(identityServiceURL string, logger logging.Logger) *Exchanger {
	return &Exchanger{
		identityServiceURL: identityServiceURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger,
		tracer: otel.Tracer("orbit.identity-proxy"),
	}
}

// Exchange calls POST {identityServiceURL}/api/v1/identity/exchange with no body.
// Istio's Envoy sidecar presents the pod's SVID as the mTLS client certificate
// on the outbound connection. No credential material passes through this
// process — identity is asserted at the transport layer by SPIRE/Istio.
//
// Full URL example:
//
//	http://orbit-identity.orbit-system.svc.cluster.local/api/v1/identity/exchange
//
// Returns the platform token string and its absolute expiry time.
func (e *Exchanger) Exchange(ctx context.Context) (token string, expiry time.Time, err error) {
	ctx, span := e.tracer.Start(ctx, "identityproxy.Exchange",
		trace.WithSpanKind(trace.SpanKindClient),
	)
	defer span.End()

	url := e.identityServiceURL + "/api/v1/identity/exchange"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, http.NoBody)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", time.Time{}, fmt.Errorf("build exchange request: %w", err)
	}
	// Inject W3C traceparent (and tracestate) headers so the identity service's
	// TracingMiddleware can extract them and attach its server span as a child of
	// this client span, forming a single end-to-end distributed trace.
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	resp, err := e.client.Do(req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", time.Time{}, fmt.Errorf("exchange call: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	span.SetAttributes(attribute.Int("http.status_code", resp.StatusCode))

	if resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("exchange call returned %d", resp.StatusCode)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", time.Time{}, err
	}

	var parsed exchangeResponse
	if err = json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", time.Time{}, fmt.Errorf("decode exchange response: %w", err)
	}

	if parsed.AccessToken == "" {
		err = fmt.Errorf("exchange response missing access_token")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", time.Time{}, err
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
	span.SetStatus(codes.Ok, "")

	e.logger.Info(ctx, "platform token exchanged",
		logging.Field{Key: "expires_in", Value: parsed.ExpiresIn},
		logging.Field{Key: "expiry", Value: tokenExpiry.UTC().Format(time.RFC3339)},
	)

	return parsed.AccessToken, tokenExpiry, nil
}
