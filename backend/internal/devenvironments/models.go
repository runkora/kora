// Package devenvironments implements decision 5 of
// docs/adr/2026-07-07-ephemeral-development-environments-via-vcluster.md:
// the API surface a developer's "New dev environment" button in the Orbit UI
// calls. It owns a small Postgres registry (owner, quota accounting, audit
// trail) and creates/reads/deletes OrbitDevEnvironment CRs — the operator's
// DevEnvironmentReconciler (internal/operator) does all the privileged work
// of turning a CR into a namespace, vcluster, and in-vcluster Orbit install.
//
// Hosted in cmd/identity (the only externally-reachable, platform-token-
// authority binary) rather than orbit-agents: dev environments have nothing
// to do with agent execution, so parking them on orbit-agents would couple
// two unrelated lifecycles to one service's already-broad RBAC. This
// package's own Kubernetes RBAC is deliberately narrow — CRUD on
// OrbitDevEnvironment only (see charts/orbit/templates/identity/clusterrole.yaml)
// — everything privileged (namespaces, Helm releases, the vcluster CA) stays
// in the operator.
//
// The package is self-contained: it never imports another feature package.
// The OrbitDevEnvironment Go type comes from internal/platform/orbitapi, a
// shared platform package — not internal/operator, which is itself a feature
// package under this repo's isolation convention.
package devenvironments

import (
	"errors"
	"time"
)

// Sentinel errors. Callers branch on these with errors.Is.
var (
	// ErrNotFound is returned when an environment does not exist, or exists
	// but is not visible to the caller (wrong owner, not an admin).
	ErrNotFound = errors.New("dev environment not found")
	// ErrQuotaExceeded is returned when a principal already has
	// MaxActivePerOwner active environments.
	ErrQuotaExceeded = errors.New("active dev environment quota exceeded")
	// ErrValidation indicates a malformed request.
	ErrValidation = errors.New("validation failed")
	// ErrTTLExceedsHardCap is returned when an extend request's requested TTL
	// would exceed the CRD's TTLHardCap (7 days, enforced by the operator
	// too — this is a fail-fast check so the caller gets a clear 400 instead
	// of a silently-clamped extension).
	ErrTTLExceedsHardCap = errors.New("requested ttl exceeds the hard cap")
	// ErrKubeconfigSigningTimedOut is returned when the operator does not
	// sign a freshly submitted kubeconfig CSR within the configured timeout
	// (KubeconfigConfig.SignTimeoutSeconds).
	ErrKubeconfigSigningTimedOut = errors.New("timed out waiting for the operator to sign the kubeconfig csr")
)

// ttlHardCap mirrors devEnvTTLHardCap in internal/operator/devenvironment_controller.go
// (decision 8: "extendable... up to a hard cap (7 days)"). Duplicated rather
// than imported — internal/operator is a feature package this one must not
// depend on; this is a fixed product constant, not reconciler behavior.
const ttlHardCap = 7 * 24 * time.Hour

// Environment is the API-facing view of one dev environment: the registry
// row (owner, audit) merged with the live CR's spec/status (lifecycle).
type Environment struct {
	ID        string    `json:"id"`
	Owner     string    `json:"owner"`
	Ref       string    `json:"ref"`
	CreatedAt time.Time `json:"createdAt"`
	Phase     string    `json:"phase"`
	Namespace string    `json:"namespace,omitempty"`
	// IngressHost is the kubectl route's SNI value (never a browser URL);
	// AppURL is the browser-facing entry point ({appExternalURL}/{id}) —
	// see the vcluster ADR's path-based-app-ingress amendment.
	IngressHost string     `json:"ingressHost,omitempty"`
	AppURL      string     `json:"appURL,omitempty"`
	Awake       bool       `json:"awake"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
}

// CreateRequest is the body of POST /v1/dev-environments.
type CreateRequest struct {
	// Ref selects the chart/image ref installed inside the environment.
	// Empty means "main" (the CRD's own default).
	Ref string `json:"ref,omitempty"`
}

// ExtendRequest is the body of POST /v1/dev-environments/{id}/extend.
type ExtendRequest struct {
	// TTL is the new total time-to-live from the environment's creation
	// (e.g. "48h"), not an increment. Must not exceed ttlHardCap.
	TTL string `json:"ttl"`
}

// KubeconfigResponse is the body of POST /v1/dev-environments/{id}/kubeconfig.
// Kubeconfig is a complete, ready-to-use kubeconfig YAML document — the
// private key is generated in-process for this single response and never
// persisted (decision 6).
type KubeconfigResponse struct {
	Kubeconfig string    `json:"kubeconfig"`
	ExpiresAt  time.Time `json:"expiresAt"`
}
