package egressgw

import (
	"net/http"
	"net/url"

	"github.com/clear-route/orbit/internal/credentials"
)

// EgressRequest is the broker's view of one inbound outbound-call attempt. It
// is constructed from the inbound *http.Request. The broker never sees raw
// HTTP itself, just this typed input plus the verified claims in context.
type EgressRequest struct {
	// Host is the upstream the caller is trying to reach (e.g. "github.com").
	// Connection lookup is keyed on this. In Phase 2a it comes from the inbound
	// http.Request's Host; in Phase 2b it comes from the SNI/Host the
	// transparent TLS terminator observed.
	Host string

	// Method is the HTTP verb of the call.
	Method string

	// URL is the upstream URL the call is destined for. Only its path/query are
	// inspected by adapter classifiers; the scheme and host are reconstructed
	// for the forward leg from the resolved connection's BaseHost.
	URL *url.URL

	// HTTPRequest is the original inbound request. The broker passes it to the
	// adapter's ClassifyAction so adapter-specific shapes (git smart-HTTP,
	// REST verbs, GraphQL) can be inspected without leaking that knowledge into
	// the broker.
	HTTPRequest *http.Request
}

// FromHTTP builds an EgressRequest from the inbound *http.Request.
func FromHTTP(r *http.Request) EgressRequest {
	host := r.Host
	if host == "" && r.URL != nil {
		host = r.URL.Host
	}
	return EgressRequest{
		Host:        host,
		Method:      r.Method,
		URL:         r.URL,
		HTTPRequest: r,
	}
}

// Decision is the broker's response to an EgressRequest. On allow it carries
// the credential to inject and the resolved connection so the proxy can update
// the outbound Host. On deny the proxy returns the appropriate error response.
type Decision struct {
	// Connection is the resolved registration whose BaseHost matched the
	// request's Host within the caller's tenant.
	Connection *credentials.Connection
	// Action is the policy action the adapter classified the request as,
	// e.g. "contents:write". Included in audit and observability.
	Action string
	// Qualifier is the resource the request was attributed to (e.g. the
	// repository name), empty when none. Included in audit and observability.
	Qualifier string
	// Credential is what the proxy injects on the outbound leg. The platform
	// token must be removed from the outbound request before this is applied.
	Credential credentials.Credential
}
