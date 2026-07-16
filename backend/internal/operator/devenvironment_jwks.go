// Host JWKS handoff for OrbitDevEnvironment — the resolution of the prototype
// spike's "istiod XDS KubeJWTAuthenticator rejection" blocker (Phase 2
// sixth-slice decision: JWT_RULE + an in-pod JWKS mirror).
//
// A devenvironment's mesh sidecars are host pods (vcluster syncs them onto
// real host nodes), so the bound ServiceAccount token their istio-agent
// presents on its XDS connection is minted and signed by the HOST apiserver.
// The in-vcluster istiod cannot validate that token via its default
// KubeJWTAuthenticator (TokenReview against its OWN, virtual, apiserver —
// which never signed it), and Istio's multicluster remote-secret mechanism
// was root-caused as a structural dead end for this call path (see the ADR's
// fifth slice — stock istio-agent never sends the "clusterid" gRPC metadata
// that authenticator keys cluster selection on).
//
// The working design is istiod's JWT_RULE env var: a pure public-key JWT
// authenticator, tried BEFORE KubeJWTAuthenticator, that validates the token
// by signature against a configured issuer/JWKS — no TokenReview, no
// multicluster machinery, no RBAC grant. Two host-cluster facts shape the
// plumbing (both empirically confirmed against k3d/k3s v1.29):
//
//   - The host's OIDC discovery/JWKS endpoints are NOT anonymously readable
//     (anonymous auth is disabled), and istiod's own JWKS fetch
//     (oidc.NewRemoteKeySet) has no way to attach a credential or a custom CA.
//   - Any bound ServiceAccount token IS authorized for those endpoints via
//     the default system:service-account-issuer-discovery binding to the
//     group system:serviceaccounts — including the HOST OPERATOR's own token.
//
// So the host operator (this file) fetches the issuer and JWKS itself, using
// its own in-cluster credential, and hands both to the in-vcluster operator
// through the OrbitPlatform CR spec (MeshJWTValidation — the CR is already
// the host→in-vcluster-operator handoff vehicle, and a JWKS is public key
// material, not a secret). The in-vcluster operator's reconcileIstiod then
// serves that JWKS to istiod over pod-loopback HTTP via a credential-less
// static-file sidecar (see reconcileIstiod), so istiod's own fetch needs
// neither a credential nor CA trust. Refresh rides the reconcile loop: every
// devenvironment reconcile re-fetches and re-patches the CR spec, which
// covers the (rare, manual on k3s) signing-key rotation case at least as
// promptly as a polling sidecar would.
package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"k8s.io/client-go/rest"
)

// HostJWKSFetcher returns the host apiserver's ServiceAccount-token issuer
// and JWKS document. A reconciler field (rather than a direct call) so tests
// can inject a stub without a live apiserver.
type HostJWKSFetcher func(ctx context.Context) (issuer string, jwks string, err error)

// NewHostJWKSFetcher returns the production HostJWKSFetcher, authenticating
// as the operator's own ServiceAccount against the host apiserver. The
// operator needs no extra RBAC for this: both endpoints are readable by any
// member of the system:serviceaccounts group via the default
// system:service-account-issuer-discovery ClusterRoleBinding.
//
// Deliberately NOT built on kubernetes.Clientset's Discovery().RESTClient():
// that client's Accept header is tuned for the typed API-discovery
// negotiation format, and the apiserver's generic content-negotiation layer
// rejects it for these two non-resource, non-discovery paths outright —
// "the server was unable to respond with a content type that the client
// supports" — confirmed empirically against a live k3s host (a plain
// Bearer-token GET with no special Accept header succeeds against the exact
// same endpoint). rest.HTTPClientFor gives a plain *http.Client wired with
// the same TLS/auth as cfg but no resource-oriented content negotiation.
func NewHostJWKSFetcher(cfg *rest.Config) HostJWKSFetcher {
	return func(ctx context.Context) (string, string, error) {
		if cfg == nil {
			return "", "", fmt.Errorf("operator has no host rest.Config configured")
		}
		httpClient, err := rest.HTTPClientFor(cfg)
		if err != nil {
			return "", "", fmt.Errorf("build host HTTP client: %w", err)
		}

		// Issuer is discovered, never hardcoded: the k3s default
		// ("https://kubernetes.default.svc.cluster.local") is only a default,
		// and a real deployment may customize --service-account-issuer. The
		// JWT_RULE authenticator matches the token's iss claim against this
		// string exactly.
		discoRaw, err := getRaw(ctx, httpClient, cfg.Host+"/.well-known/openid-configuration")
		if err != nil {
			return "", "", fmt.Errorf("fetch host openid-configuration: %w", err)
		}
		var disco struct {
			Issuer string `json:"issuer"`
		}
		if unmarshalErr := json.Unmarshal(discoRaw, &disco); unmarshalErr != nil {
			return "", "", fmt.Errorf("parse host openid-configuration: %w", unmarshalErr)
		}
		if disco.Issuer == "" {
			return "", "", fmt.Errorf("host openid-configuration has no issuer")
		}

		// Fetch the JWKS from the apiserver's fixed path rather than the
		// discovery document's jwks_uri: that URI is written in terms of the
		// issuer host, which is not necessarily a name this client can reach
		// (on k3s it is a cluster-internal DNS name).
		jwksRaw, err := getRaw(ctx, httpClient, cfg.Host+"/openid/v1/jwks")
		if err != nil {
			return "", "", fmt.Errorf("fetch host JWKS: %w", err)
		}
		if !json.Valid(jwksRaw) {
			return "", "", fmt.Errorf("host JWKS endpoint returned invalid JSON")
		}
		return disco.Issuer, string(jwksRaw), nil
	}
}

// getRaw issues an authenticated GET (via httpClient's rest.Config-derived
// transport) against an absolute URL and returns the raw response body.
func getRaw(ctx context.Context, httpClient *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}
