package egressgw

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/runkora/kora/backend/internal/credentials"
	platformapi "github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// ProxyHandler is the gateway's inbound HTTP handler. AuthMiddleware has
// already verified the platform JWT and put the claims in the request context.
// The handler runs the broker to resolve the connection from the request Host,
// authorize the call and mint a credential, then swaps the platform token for
// that credential and forwards to the upstream.
//
// It serves both entry points: a direct HTTPS request, and the inner request
// the CONNECT handler reads off a terminated TLS tunnel.
type ProxyHandler struct {
	broker     *Broker
	transports *TransportCache // upstream forward transport, keyed per-connection CA trust
	brand      productNamer    // names the product in the denial message the agent reads
	logger     logging.Logger
}

// productNamer is the one brand method the proxy calls.
type productNamer interface {
	ProductName() string
}

// NewProxyHandler constructs a ProxyHandler. transport is the base transport for
// the forward leg, cloned per connection that declares its own CA. Every
// transport handed to the proxy is traced, so spans cover each forwarded call.
// brand supplies the product name denial messages read.
func NewProxyHandler(broker *Broker, transport *http.Transport, brand productNamer, logger logging.Logger) *ProxyHandler {
	return &ProxyHandler{broker: broker, transports: NewTransportCache(transport), brand: brand, logger: logger}
}

// Transports exposes the handler's TransportCache so the owning binary can
// wire cache invalidation into the connection registry's reload hook (a
// Connection/Secret change, including a rotated ca_cert, must not require a
// gateway restart to take effect).
func (p *ProxyHandler) Transports() *TransportCache {
	return p.transports
}

// ServeHTTP implements http.Handler.
func (p *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok {
		p.handleBrokerError(w, r, ErrUnauthenticated)
		return
	}

	egress := FromHTTP(r)
	decision, err := p.broker.Authorize(ctx, claims, egress)
	if err != nil {
		p.handleBrokerError(w, r, err)
		return
	}

	// Forward to the host the client asked for, not the connection's base_host:
	// one connection can serve a host family (github.com for git, plus
	// api.github.com, codeload.github.com and *.githubusercontent.com for the
	// REST API, archives and raw content). ListByHost has already constrained
	// that host to the connection's own, so the minted credential never leaves
	// them.
	//
	// It is the host the broker authorized, not r.Host: the two differ when the
	// request carries its authority in the URL rather than the Host header, and
	// the credential is scoped to whichever one policy was evaluated against.
	upstream, err := buildUpstreamURL(egress.Host)
	if err != nil {
		p.handleBrokerError(w, r, fmt.Errorf("%w: %w", ErrMissingHost, err))
		return
	}

	p.forward(w, r, upstream, decision)
}

// forward swaps the platform token for the minted credential, drops kora-only
// request headers, and proxies the request to the upstream URL via the
// configured transport. The response (including streaming bodies) is piped
// straight back to the caller.
func (p *ProxyHandler) forward(w http.ResponseWriter, r *http.Request, upstream *url.URL, d Decision) {
	transport, err := p.transports.For(d.Connection)
	if err != nil {
		// A CA trust failure is a distinct, logged failure mode from a policy
		// deny or an adapter resolve failure. Surface it as such rather than
		// letting it masquerade as a generic forward error.
		p.logger.Error(r.Context(), "egress upstream transport unavailable",
			logging.Field{Key: "connection", Value: d.Connection.Name},
			logging.Field{Key: "error", Value: err.Error()},
		)
		platformapi.ErrorResponse(r.Context(), w, p.logger,
			platformapi.ServiceUnavailableError(platformapi.ErrCodeServiceUnavailable, "upstream connection unavailable", nil),
			err)
		return
	}

	rp := &httputil.ReverseProxy{
		Transport: transport,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(upstream)
			// Tools expect their original Host (e.g. github.com), not the
			// gateway's. SetXForwarded would publish the agent pod's IP, which
			// is not useful to the upstream, so it is suppressed.
			pr.Out.Host = upstream.Host

			// Strip the platform token; the minted credential replaces it.
			pr.Out.Header.Del("Authorization")
			pr.Out.Header.Del("X-Forwarded-For")
			pr.Out.Header.Del("X-Forwarded-Host")
			pr.Out.Header.Del("X-Forwarded-Proto")

			// Inject the minted upstream credential.
			if d.Credential.Header != "" {
				pr.Out.Header.Set(d.Credential.Header, d.Credential.Value)
			}
		},
		ErrorHandler: func(rw http.ResponseWriter, req *http.Request, err error) {
			p.logger.Error(req.Context(), "egress forward failed",
				logging.Field{Key: "connection", Value: d.Connection.Name},
				logging.Field{Key: "upstream", Value: upstream.String()},
				logging.Field{Key: "error", Value: err.Error()},
			)
			platformapi.ErrorResponse(req.Context(), rw, p.logger,
				platformapi.ServiceUnavailableError(platformapi.ErrCodeServiceUnavailable, "upstream call failed", nil),
				err)
		},
	}
	rp.ServeHTTP(w, r)
}

// handleBrokerError maps a Broker.Authorize sentinel to the right HTTP error
// response. A policy denial names the host, action and what would allow it;
// nothing else about the tenant's grants or connections is exposed.
func (p *ProxyHandler) handleBrokerError(w http.ResponseWriter, r *http.Request, err error) {
	var apiErr *platformapi.APIError
	var denial *Denial
	switch {
	case errors.Is(err, ErrUnauthenticated):
		apiErr = platformapi.UnauthorizedError(platformapi.ErrCodeUnauthorized, "unauthenticated", nil)
	case errors.Is(err, ErrMissingHost):
		apiErr = platformapi.BadRequestError(platformapi.ErrCodeValidationFailed, "missing destination host", nil)
	// The operator's own faults are matched before the caller's. A connection
	// kora cannot read denies the call, but it is not a policy decision, and
	// wrapping one in ErrForbidden anywhere upstream would otherwise report it
	// as a denial and send an operator hunting a grant.
	case errors.Is(err, credentials.ErrInvalidConfig):
		apiErr = platformapi.InternalError(platformapi.ErrCodeInternalError, "connection configuration is invalid", nil)
	case errors.Is(err, credentials.ErrUnsupportedAdapter):
		apiErr = platformapi.InternalError(platformapi.ErrCodeInternalError, "unsupported adapter", nil)
	case errors.As(err, &denial):
		apiErr = platformapi.ForbiddenError(platformapi.ErrCodeForbidden, denial.Message(p.brand.ProductName()), denial.Details())
	case errors.Is(err, ErrForbidden):
		apiErr = platformapi.ForbiddenError(platformapi.ErrCodeForbidden, "egress denied", nil)
	case errors.Is(err, ErrTenantMismatch):
		apiErr = platformapi.ForbiddenError(platformapi.ErrCodeForbidden, "connection tenant mismatch", nil)
	case errors.Is(err, credentials.ErrConnectionNotFound):
		apiErr = platformapi.NotFoundError(platformapi.ErrCodeResourceNotFound, "no connection registered for host", nil)
	default:
		apiErr = platformapi.InternalError(platformapi.ErrCodeInternalError, "broker error", nil)
	}
	platformapi.ErrorResponse(r.Context(), w, p.logger, apiErr, err)
}

// buildUpstreamURL returns the upstream base URL (scheme + host) the forward
// proxy targets. The inbound request's path and query are NOT set here: the
// ReverseProxy's ProxyRequest.SetURL joins them onto this target, so setting
// them here too would double the path (e.g. "/x" → "/x/x").
func buildUpstreamURL(requestHost string) (*url.URL, error) {
	if requestHost == "" {
		return nil, errors.New("request has no host")
	}
	host := strings.TrimSpace(requestHost)
	// Accept either a bare host ("github.com") or a host:port.
	host = strings.TrimPrefix(strings.TrimPrefix(host, "https://"), "http://")
	host = strings.TrimSuffix(host, "/")

	return &url.URL{Scheme: "https", Host: host}, nil
}
