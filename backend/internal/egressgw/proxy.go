package egressgw

import (
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/clear-route/orbit/internal/credentials"
	platformapi "github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/token"
)

// ProxyHandler is the gateway's inbound HTTP handler. It expects the platform
// JWT to have already been verified by AuthMiddleware (claims live in context),
// pulls the connection name + action from request headers, runs the broker for
// authorization + minting, swaps the platform token for the minted credential,
// and forwards the request to the upstream named by the resolved connection.
//
// In Phase 2a the upstream is reached as a normal HTTPS client (the gateway is
// the originating TLS endpoint); transparent TLS termination against a
// per-tenant CA lands in Phase 2b without changing the broker logic.
type ProxyHandler struct {
	broker     *Broker
	transports *TransportCache // upstream forward transport, keyed per-connection CA trust
	logger     logging.Logger
}

// NewProxyHandler constructs a ProxyHandler. transport is the shared base
// transport used for every connection that declares no custom CA; it may be
// nil to use the process's default transport.
func NewProxyHandler(broker *Broker, transport http.RoundTripper, logger logging.Logger) *ProxyHandler {
	return &ProxyHandler{broker: broker, transports: NewTransportCache(transport), logger: logger}
}

// Transports exposes the handler's TransportCache so the owning binary can
// wire cache invalidation into the connection registry's reload hook (a
// Connection/Secret change, including a rotated ca_cert, must not require a
// gateway restart to take effect).
func (p *ProxyHandler) Transports() *TransportCache {
	return p.transports
}

// ServeHTTP implements http.Handler. It is the Phase 2a entry point.
func (p *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok {
		p.writeError(w, r, ErrUnauthenticated, http.StatusUnauthorized, "unauthenticated")
		return
	}

	decision, err := p.broker.Authorize(ctx, claims, FromHTTP(r))
	if err != nil {
		p.handleBrokerError(w, r, err)
		return
	}

	// Forward to the host the client actually requested, not the connection's
	// base_host: a single connection can serve a host family (github.com for git
	// AND api.github.com / codeload.github.com / *.githubusercontent.com for the
	// REST API, archives, and raw content). GetByHost has already constrained
	// r.Host to the connection's declared hosts, so the minted credential only
	// ever reaches the connection's own upstreams.
	upstream, err := buildUpstreamURL(r.Host)
	if err != nil {
		p.writeError(w, r, err, http.StatusBadGateway, "invalid upstream url")
		return
	}

	p.forward(w, r, upstream, decision)
}

// forward swaps the platform token for the minted credential, drops Orbit-only
// request headers, and proxies the request to the upstream URL via the
// configured transport. The response (including streaming bodies) is piped
// straight back to the caller.
func (p *ProxyHandler) forward(w http.ResponseWriter, r *http.Request, upstream *url.URL, d Decision) {
	transport, err := p.transports.For(d.Connection)
	if err != nil {
		// A CA trust failure is a distinct, logged failure mode from a policy
		// deny or an adapter resolve failure — surface it as such rather than
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
			// is not useful to the upstream — we suppress it.
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
// response. The broker's errors are already structured; we don't expose
// internal reasons to the caller (especially policy denials) beyond a coarse
// code.
func (p *ProxyHandler) handleBrokerError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrUnauthenticated):
		p.writeError(w, r, err, http.StatusUnauthorized, "unauthenticated")
	case errors.Is(err, ErrMissingHost):
		p.writeError(w, r, err, http.StatusBadRequest, "missing destination host")
	case errors.Is(err, ErrForbidden):
		p.writeError(w, r, err, http.StatusForbidden, "egress denied")
	case errors.Is(err, ErrTenantMismatch):
		p.writeError(w, r, err, http.StatusForbidden, "connection tenant mismatch")
	case errors.Is(err, credentials.ErrConnectionNotFound):
		p.writeError(w, r, err, http.StatusNotFound, "no connection registered for host")
	case errors.Is(err, credentials.ErrUnsupportedAdapter):
		p.writeError(w, r, err, http.StatusInternalServerError, "unsupported adapter")
	default:
		p.writeError(w, r, err, http.StatusInternalServerError, "broker error")
	}
}

func (p *ProxyHandler) writeError(w http.ResponseWriter, r *http.Request, cause error, status int, message string) {
	var apiErr *platformapi.APIError
	switch status {
	case http.StatusUnauthorized:
		apiErr = platformapi.UnauthorizedError(platformapi.ErrCodeUnauthorized, message, nil)
	case http.StatusForbidden:
		apiErr = platformapi.ForbiddenError(platformapi.ErrCodeForbidden, message, nil)
	case http.StatusNotFound:
		apiErr = platformapi.NotFoundError(platformapi.ErrCodeResourceNotFound, message, nil)
	case http.StatusInternalServerError:
		apiErr = platformapi.InternalError(platformapi.ErrCodeInternalError, message, nil)
	case http.StatusBadGateway, http.StatusServiceUnavailable:
		apiErr = platformapi.ServiceUnavailableError(platformapi.ErrCodeServiceUnavailable, message, nil)
	default:
		apiErr = platformapi.BadRequestError(platformapi.ErrCodeValidationFailed, message, nil)
	}
	platformapi.ErrorResponse(r.Context(), w, p.logger, apiErr, cause)
}

// buildUpstreamURL returns the upstream base URL (scheme + host) the forward
// proxy targets. The inbound request's path and query are NOT set here: the
// ReverseProxy's ProxyRequest.SetURL joins them onto this target, so setting
// them here too would double the path (e.g. "/x" → "/x/x").
func buildUpstreamURL(baseHost string) (*url.URL, error) {
	if baseHost == "" {
		return nil, errors.New("connection has empty base_host")
	}
	host := strings.TrimSpace(baseHost)
	// Allow base_host to be either a bare host ("github.com") or a host:port.
	host = strings.TrimPrefix(strings.TrimPrefix(host, "https://"), "http://")
	host = strings.TrimSuffix(host, "/")

	return &url.URL{Scheme: "https", Host: host}, nil
}
