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
	broker    *Broker
	transport http.RoundTripper // upstream forward transport; defaults to http.DefaultTransport
	logger    logging.Logger
}

// NewProxyHandler constructs a ProxyHandler. transport may be nil to use the
// process's default transport.
func NewProxyHandler(broker *Broker, transport http.RoundTripper, logger logging.Logger) *ProxyHandler {
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &ProxyHandler{broker: broker, transport: transport, logger: logger}
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

	upstream, err := buildUpstreamURL(decision.Connection.BaseHost, r.URL)
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
	rp := &httputil.ReverseProxy{
		Transport: p.transport,
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

// buildUpstreamURL combines the connection's base host with the inbound request
// path/query. In Phase 2a the gateway's HTTPS listener accepts requests at any
// path; the path is reused verbatim against the upstream.
func buildUpstreamURL(baseHost string, inbound *url.URL) (*url.URL, error) {
	if baseHost == "" {
		return nil, errors.New("connection has empty base_host")
	}
	host := strings.TrimSpace(baseHost)
	// Allow base_host to be either a bare host ("github.com") or a host:port.
	host = strings.TrimPrefix(strings.TrimPrefix(host, "https://"), "http://")
	host = strings.TrimSuffix(host, "/")

	upstream := &url.URL{
		Scheme:   "https",
		Host:     host,
		Path:     inbound.Path,
		RawPath:  inbound.RawPath,
		RawQuery: inbound.RawQuery,
	}
	return upstream, nil
}
