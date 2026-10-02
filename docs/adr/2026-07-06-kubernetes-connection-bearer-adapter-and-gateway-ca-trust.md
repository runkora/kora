# Kubernetes Connection — `kubernetes` Adapter and Per-Connection Gateway CA Trust

<!-- Filename: 2026-07-06-kubernetes-connection-bearer-adapter-and-gateway-ca-trust.md -->

**Status:** Proposed
**Date:** 2026-07-06
**Deciders:** Platform Engineering
**Technical Story:** [Kubernetes Connection PRD](../prd/kubernetes-connection.md) — implements the `kubernetes` adapter the credential-broker ADR already reserved a name for but never built.

> **Amended 2026-07-06** — the two-tier `cluster:read`/`cluster:write` verb-class taxonomy (Decision B) is replaced by a **resource-category taxonomy**: `workloads`, `config`, `secrets`, `networking`, `storage`, `rbac`, each with its own `:read`/`:write` pair, plus `cluster:read`/`cluster:write` repurposed as the catch-all for other well-known cluster-scoped/admin resources and API discovery. `ClassifyAction` now parses the Kubernetes API request path, not just the HTTP verb. See [Amendment 1 — 2026-07-06](#amendment-1--2026-07-06-resource-category-verb-taxonomy), which resolves Open Question 2 and supersedes Decision B below.
>
> **Further amended 2026-07-06** — live-cluster validation against a real GKE cluster surfaced a structural constraint this ADR's original design didn't anticipate: a `kubernetes` connection's `baseHost` must be a DNS hostname, not an IP literal, or the sandbox's own transparent-egress interception fails closed on the resulting empty TLS SNI. This is now enforced at the schema/operator/adapter-registry layers — see [Amendment 2 — 2026-07-06](#amendment-2--2026-07-06-basehost-must-be-a-dns-hostname-not-an-ip-literal). A same-day local re-verification against a hostname-based connection then confirmed the full adapter/broker/CA-trust path end-to-end — see [Amendment 3 — 2026-07-06](#amendment-3--2026-07-06-local-re-verification-of-the-full-adapterbroker-path).

---

## Context

### Background

The [JIT Credential Brokering ADR](2026-06-06-jit-credential-brokering-egress-gateway.md) designed the egress-gateway credential broker and built it end-to-end for exactly one connection type: GitHub (`github_app`). That ADR's own "Per-Tool Flows" section already sketches a Kubernetes flow and lists it in the adapter/pattern table as **Pattern A — minted**, implying a `TokenRequest`-style per-call mint mirroring GitHub's App-JWT → installation-token exchange. `AdapterKubernetesBearer = "kubernetes"` (`internal/credentials/models.go:29`) has sat as a declared constant with no adapter behind it since Amendment 3 (2026-06-12) reshaped connection registration onto GitOps `Connection` CRs.

The [Kubernetes Connection PRD](../prd/kubernetes-connection.md) scopes the actual v1 build and, in doing so, **narrows** the original ADR's Kubernetes flow: v1 resolves a **static, admin-supplied ServiceAccount bearer token** (Pattern B — header-swap with a static secret), not a per-call minted token (Pattern A). `TokenRequest`-based minting is explicitly deferred (PRD Non-Goals, Alternative 1) because it would require kora to hold a credential capable of minting tokens for arbitrary ServiceAccounts on every customer's cluster — a materially larger trust ask with no existing safe design.

**This ADR corrects the 2026-06-06 ADR's "Kubernetes — Pattern A" classification for v1** to Pattern B, and designs the two pieces of code that classification actually requires: the `k8sbearer` adapter, and the one genuinely new gateway capability the PRD calls out — per-connection custom CA trust on the re-originated TLS leg (GitHub's upstream is publicly CA-trusted; a Kubernetes API server commonly is not).

### Problem Statement

Build `kubernetes` as the second `CredentialAdapter` on the existing broker architecture, with no new core mechanism, such that:
1. An agent's `kubectl`/`client-go` calls reach a granted cluster's API server with the real ServiceAccount bearer token injected only on the gateway→upstream leg.
2. The gateway can validate the real upstream API server's certificate against a private or self-signed CA declared per-connection, since Kubernetes API servers are not typically publicly CA-trusted the way `github.com` is.
3. The coarse `cluster:read` / `cluster:write` verb-class policy gate is enforced the same way `repo:read` / `repo:write` already is for GitHub.

### Goals

- Reuse `credentials.CredentialAdapter`, `platform/connections` registry metadata, `egressgw.AdapterRegistry`, the GitOps `Connection` CR, and the operator's `ConnectionAggregator` completely unchanged.
- Add exactly one new gateway capability: per-connection upstream CA trust, designed generically enough that any future adapter needing a private CA (not just Kubernetes) can opt in without further gateway changes.
- Ship with zero CRD schema change and zero new frontend components.

### Non-Goals

- `TokenRequest`-based minting (tracked as the ADR's own future amendment once a safe cross-cluster minting design exists).
- Client-certificate authentication (a possible sibling adapter, not this one).
- Fine-grained (namespace/resource/verb) authorization inside kora — delegated to the target cluster's own RBAC on the injected identity.
- `kubectl exec` / `port-forward` (SPDY/WebSocket-upgraded, not plain REST) — see Decision → Verb Classification.

---

## Decision Drivers

- **No new core mechanism.** The credential-broker architecture is proven exactly once (GitHub); the goal of this feature is partly to prove it generalizes, so every design choice should reuse existing seams (`CredentialAdapter`, the registry metadata table, `AdapterRegistry`) rather than add new ones.
- **The bearer token must never enter the sandbox**, structurally, not by convention — the same guarantee GitHub already provides.
- **The re-origination leg must validate the real upstream cert.** Skipping validation (or a global `InsecureSkipVerify`) would silently defeat the "never trust, always verify" principle for exactly the connection type most likely to run destructive `kubectl apply`/`delete` calls.
- **CA trust must not become Kubernetes-specific plumbing.** A future adapter (e.g. an internal API-key SaaS behind a private CA) should be able to opt into the same mechanism without gateway code changes, consistent with "adding a service that fits an existing adapter is a registration, not code."
- **Minimal frontend/product surface**, per the PRD's explicit success metric of "no new UI component."

---

## Considered Options

### Decision A — Shape of the `k8sbearer` adapter

#### Option 1: A `CredentialAdapter` implementation mirroring `githubapp`'s shape, minus minting (Chosen)

**Description:** New package `internal/credentials/adapters/k8sbearer/` implementing `Type`, `Actions`, `ClassifyAction`, `Resolve`. `Resolve` reads the static bearer token from the mounted secret via the already-injected `credentials.SecretResolver` (the same `k8sfile.SecretResolver` `githubapp` uses) and returns it verbatim as a `Bearer` credential — no JWT signing, no token-minting API call, no cache (nothing to amortize: it's not a network round trip, so caching an already-in-memory string buys nothing `k8sfile`'s own mount-read doesn't already give for free).

**Pros:**
- Same package shape as `githubapp` — same interface, same subpackage-per-adapter convention, same `platformconns.Actions(...)` delegation for the vocabulary table. A reviewer who understands `githubapp` understands this immediately.
- Genuinely simpler than `githubapp`: no JWT, no cache, no mint API client — closer to `passthrough` in weight, but with a real credential to inject.

**Cons:**
- None material. The only asymmetry with `githubapp` (no minting) is a direct, correct consequence of Pattern B vs Pattern A, not a shortcut.

#### Option 2: Reuse the `passthrough` adapter with the bearer token injected by the connection registry itself

**Description:** Skip a Kubernetes-specific adapter; have the aggregator or registry loader stuff a pre-formatted `Authorization` header value onto the `Connection`, and let `passthrough`'s no-op `Resolve` be swapped for a generic "static-header" adapter.

**Why not chosen:** This is really `AdapterAPIKey`'s job (header/query static-secret injection), already reserved as a distinct constant, and Kubernetes still needs its own `ClassifyAction` (verb-class mapping from K8s API request shape) — a concern `api_key`/`passthrough` correctly have no notion of. Building a one-off "static-header-plus-custom-classifier" adapter would be more code than a dedicated `k8sbearer` package, not less, and would blur the "one adapter type maps to one clear credential mechanism + classification shape" convention the registry table depends on.

### Decision B — Verb-class taxonomy (PRD Open Question 2)

#### Option 1: Two-tier `cluster:read` / `cluster:write` (Chosen for v1)

**Description:** `ClassifyAction` maps `GET`/`HEAD`/`OPTIONS` → `cluster:read`; `POST`/`PUT`/`PATCH`/`DELETE` → `cluster:write`. This mirrors `githubapp`'s REST verb-mapping exactly (`classifyREST`).

**Pros:** Matches the PRD's shipped scope; matches the existing GitHub precedent bit-for-bit; the target cluster's own RBAC is the documented backstop for anything finer (delete vs. patch vs. create), so kora doesn't need to re-derive it from the HTTP verb.

**Cons:** A `cluster:write` grant covers `DELETE` too — an admin cannot grant "can create/patch but not delete" through kora's own policy layer alone.

#### Option 2: Three-tier, adding `cluster:delete`

**Description:** Split `DELETE` (and `deletecollection`) out from `cluster:write` into its own action.

**Why not chosen (for v1):** The PRD leaves this as an explicitly open question owned by Platform Engineering, not a launch requirement, and the risk it addresses (an over-broad `write` grant) is already named as a known, accepted risk mitigated by cluster-side RBAC (defense in depth). Shipping two-tier now and revisiting based on real usage avoids guessing a taxonomy no customer has asked for yet. `platformconns.Actions` is a simple table to extend later without touching the adapter's `Resolve` path.

### Decision C — Per-connection upstream CA trust on the re-origination leg

The gateway's outbound leg (`ProxyHandler.forward`, `internal/egressgw/proxy.go`) currently forwards every connection through one process-wide `http.RoundTripper` (`http.DefaultTransport` in production), which validates the upstream certificate against the OS trust store. That is correct for `github.com` and wrong for a private/self-signed cluster CA.

#### Option 1: A per-connection `*http.Transport` cache, selected by `ProxyHandler.forward`, keyed off a generic `config.ca_cert` field (Chosen)

**Description:** Add a small helper (`internal/egressgw/upstreamtransport.go`) that:
1. Given a `*credentials.Connection`, peeks its `Config` for an optional `ca_cert` PEM string — via a minimal, adapter-agnostic struct, not any adapter's own config type.
2. If absent, returns the shared default transport unchanged (every existing connection, e.g. GitHub, is unaffected).
3. If present, builds (once, then caches by `Connection.ID`) a cloned `*http.Transport` whose `TLSClientConfig.RootCAs` is the system pool plus the connection's declared CA, and returns that.
4. `ProxyHandler.forward` asks this helper for the transport to use per-request instead of always using its single configured transport.

**Pros:**
- Trust is scoped to exactly the connection that declared the CA — a leaked or misconfigured CA for one connection cannot affect certificate validation for any other connection (GitHub's or another tenant's cluster's), even within the same per-tenant gateway process.
- Adapter-agnostic: any future adapter (not just `kubernetes`) gets private-CA support for free by putting `ca_cert` in its `Connection.Config`, with no gateway code change — satisfies the "registration, not code" principle for this capability too.
- Reuses the existing `Connection.Config` freeform-JSON path (`x-kubernetes-preserve-unknown-fields: true`) — no CRD schema change, matching the PRD's explicit claim.
- Small, testable, isolated: one new file, one new call site in `forward`, no change to the `Broker`, `AdapterRegistry`, or any adapter's `Resolve`/`ClassifyAction`.

**Cons:**
- Introduces one more piece of per-connection state the gateway holds in memory (a transport, which itself owns a connection pool) — bounded by the number of registered connections declaring a custom CA, the same bound `githubapp`'s token cache already accepts.
- Needs explicit cache invalidation on registry reload (a `ca_cert` rotation via CR update), addressed by hooking the existing `ConnectionRegistry.OnReload` callback.

#### Option 2: One shared transport with a single unioned `RootCAs` pool built from every connection's CA

**Description:** At registry load/reload, build one `*x509.CertPool` = system pool + every registered connection's `ca_cert`, and use one shared `*http.Transport` for all forwarding, same as today.

**Why not chosen:** Unions every tenant's and every connection's trust into one pool used for every outbound call, including ones (like GitHub) that never asked for it. A certificate that validates against *any* connection's CA would be accepted when forwarding to *any* host, not just that CA's own connection's host — a real, if narrow, cross-connection trust leak with no corresponding benefit over per-connection scoping. Marginally less code, but weakens an isolation property (per-connection trust boundary) for no gain.

#### Option 3: Push CA trust to the mesh/Envoy layer instead of the gateway's own Go re-origination code

**Description:** Configure Istio/Envoy upstream TLS origination per-cluster-connection via `DestinationRule`/`EnvoyFilter`, rather than in `proxy.go`.

**Why not chosen:** The gateway already re-originates TLS itself in Go (`ReverseProxy` + a configured `Transport` — see 2026-06-06 ADR, "Phase 2a"/"Phase 2b"); a mesh-level mechanism here would be a *second*, differently-configured place upstream trust is decided, for connections whose CA is only known from a GitOps CR the gateway already parses. It would also require generating per-connection Envoy config from CRs — new operator surface — to do something the Go `http.Transport` already does natively with one field.

#### Option 4: `Connection.CACertRef` — a first-class CRD field, reusing the secret pipeline

**Description:** Add `SecretRef`/`SecretKey`-style indirection for the CA too, rather than an inline `ca_cert` string in `Config`.

**Why not chosen:** The PRD's Open Question 3 already resolved this: CA certificates are public material, not secrets, so routing them through the secret pipeline adds indirection with no confidentiality benefit, and — critically — a new CRD field is a schema change the PRD explicitly avoids. An inline PEM string in the existing freeform `Config` costs nothing structurally.

---

## Decision

**Chosen:** Option 1 for the adapter shape (Decision A), two-tier verb classification for v1 (Decision B), and a per-connection transport cache keyed off a generic `config.ca_cert` field for gateway CA trust (Decision C).

### Rationale

Every choice reuses an existing seam instead of adding a new one: the adapter reuses `CredentialAdapter` + `SecretResolver` + the `platform/connections` metadata table exactly as `githubapp` does; the verb taxonomy reuses `githubapp`'s verb-mapping shape; and CA trust reuses the existing `Connection.Config` freeform-JSON path rather than a schema or CRD change. The one place this ADR adds genuinely new gateway code — the transport cache — is deliberately scoped as connection-generic infrastructure, not a Kubernetes special case, so the next adapter that needs a private CA (there will be one) needs zero gateway changes.

### Implementation

#### 1. `internal/platform/connections/adapters.go` — registry metadata

```go
const (
	AdapterGitHubApp        = "github_app"
	AdapterPassthrough      = "passthrough"
	AdapterKubernetesBearer = "kubernetes"
)

const (
	ActionGitHubRepoRead    = "repo:read"
	ActionGitHubRepoWrite   = "repo:write"
	ActionPassthroughUse    = "use"
	ActionClusterRead       = "cluster:read"
	ActionClusterWrite      = "cluster:write"
)

var registry = map[string]adapterMeta{
	// ... existing entries unchanged ...
	AdapterKubernetesBearer: {
		actions: []ActionMeta{
			{Action: ActionClusterRead, Description: "Read access to the connection's Kubernetes cluster (get/list/watch)"},
			{Action: ActionClusterWrite, Description: "Write access to the connection's Kubernetes cluster (create/update/patch/delete)"},
		},
		egressHosts: singleHostEgress, // a cluster connection serves exactly its own API server host:port — no host family, unlike GitHub
	},
}
```

`singleHostEgress` is the same shape as the existing `passthroughEgressHosts` — a Kubernetes connection has no multi-host family the way GitHub does (`api.github.com`, `codeload.github.com`, …); it is exactly the declared `base_host`.

#### 2. `internal/credentials/adapters/k8sbearer/k8sbearer.go` — the adapter

```go
package k8sbearer

type config struct {
	ClusterName string `json:"cluster_name"`
	CACert      string `json:"ca_cert,omitempty"` // read by the gateway's transport cache, not by this adapter
}

type Adapter struct {
	secrets credentials.SecretResolver
	logger  logging.Logger
}

func New(secrets credentials.SecretResolver, logger logging.Logger) *Adapter { ... }

func (a *Adapter) Type() credentials.AdapterType { return credentials.AdapterKubernetesBearer }

func (a *Adapter) Actions() []credentials.ActionDeclaration {
	// delegates to platformconns.Actions(platformconns.AdapterKubernetesBearer), same as githubapp/passthrough
}

// ClassifyAction denies SPDY/WebSocket-upgraded requests outright (exec/port-forward,
// explicitly out of scope — see PRD Open Question 5) rather than misclassifying them,
// then maps verbs exactly like githubapp's classifyREST.
func (a *Adapter) ClassifyAction(req *http.Request) (string, error) {
	if req == nil {
		return "", credentials.ErrUnsupportedAction
	}
	if isUpgradeRequest(req) { // Connection: Upgrade / Upgrade: SPDY|websocket
		return "", credentials.ErrUnsupportedAction
	}
	switch strings.ToUpper(req.Method) {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return platformconns.ActionClusterRead, nil
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return platformconns.ActionClusterWrite, nil
	default:
		return "", credentials.ErrUnsupportedAction
	}
}

// Resolve reads the static bearer token from the mounted secret and returns it
// as-is. No minting, no cache — there is no round trip to amortize.
func (a *Adapter) Resolve(ctx context.Context, conn credentials.Connection, id credentials.CallerIdentity) (credentials.Credential, error) {
	tok, err := a.secrets.Resolve(ctx, conn.SecretRef)
	if err != nil {
		return credentials.Credential{}, fmt.Errorf("resolve k8s bearer secret: %w", err)
	}
	return credentials.Credential{
		Token:  string(tok),
		Header: "Authorization",
		Value:  "Bearer " + string(tok),
		// ExpiresAt left zero: the token is static; rotation is a Secret update
		// picked up on the gateway's next resolve, per the existing fsnotify reload.
	}, nil
}
```

Explicitly denying upgrade requests (rather than letting them fall through to a REST verb guess) makes `kubectl exec`/`port-forward` fail cleanly and audibly, matching the PRD's stated non-goal instead of silently mis-brokering a bidirectional stream.

#### 3. `cmd/egress-gateway/main.go` — registration

```go
registry.Register(githubapp.New(...))
registry.Register(passthrough.New())
registry.Register(k8sbearer.New(secretResolver, logger)) // reuses the same k8sfile.SecretResolver instance
```

#### 4. `internal/egressgw/upstreamtransport.go` — the new gateway capability

```go
// connectionTLSConfig is deliberately NOT any adapter's config type — ca_cert is
// a connection-level, adapter-agnostic convention any adapter's Config may carry.
type connectionTLSConfig struct {
	CACert string `json:"ca_cert,omitempty"`
}

// TransportCache resolves the http.RoundTripper the proxy should use to
// re-originate a call to a given connection's upstream. Connections with no
// declared ca_cert get the shared default transport (github.com's behavior is
// unchanged); a connection with a ca_cert gets its own cached *http.Transport
// trusting the system roots plus that CA.
type TransportCache struct {
	base http.RoundTripper

	mu    sync.RWMutex
	byConn map[string]http.RoundTripper
}

func NewTransportCache(base http.RoundTripper) *TransportCache { ... }

func (c *TransportCache) For(conn *credentials.Connection) (http.RoundTripper, error) {
	// unmarshal conn.Config into connectionTLSConfig; if CACert == "", return c.base.
	// else: read-lock check byConn[conn.ID]; on miss, build pool (x509.SystemCertPool
	// + AppendCertsFromPEM), clone base.(*http.Transport) with TLSClientConfig.RootCAs
	// set, write-lock and cache by conn.ID, return.
}

// Invalidate drops every cached transport — wired into ConnectionRegistry.OnReload
// so a CR/Secret change (including a rotated ca_cert) is picked up without a
// gateway restart, without needing to know which specific connection changed.
func (c *TransportCache) Invalidate() { ... }
```

`ProxyHandler.forward` changes its one line:

```go
transport, err := p.transports.For(d.Connection)
if err != nil {
	// CA parse failure is a distinct, logged failure mode from a policy deny —
	// satisfies the PRD's observability requirement that adapter/CA failures
	// are distinguishable from policy denials.
	...
}
rp := &httputil.ReverseProxy{Transport: transport, ...}
```

`main.go` wires `connRegistry.OnReload(func(ctx) { transportCache.Invalidate(); scopePub.Publish(...) })` alongside the existing scope-republish hook.

#### 5. `deploy/local/connections/kubernetes.yaml` — example CR

Mirrors `deploy/local/connections/github.yaml`'s shape and comment style:

```yaml
apiVersion: orbit.io/v1alpha1
kind: Connection
metadata:
  name: orbit-local   # the cluster it points at, not a generic "kubernetes" —
                       # a tenant may register more than one kubernetes
                       # connection, and this name is what the Connections UI
                       # and agent-type connectors reference
  namespace: orbit-tenant-example
spec:
  adapter: kubernetes
  baseHost: k3d-orbit-local-server-0:6443   # the k3d server container's own name — already a SAN on its
                                              # kube-apiserver TLS cert. See Amendment 2: a bare IP is
                                              # rejected outright. resolve-docker-ips.sh patches CoreDNS
                                              # NodeHosts so this name resolves in-cluster.
  secretRef: orbit-local-token
  secretKey: token
  config:
    cluster_name: orbit-local
    ca_cert: |
      -----BEGIN CERTIFICATE-----
      ... k3s server CA, e.g. from the server node's /var/lib/rancher/k3s/server/tls/server-ca.crt ...
      -----END CERTIFICATE-----
```

The referenced Secret (`orbit-local-token`) holds a ServiceAccount token provisioned against the local k3d cluster with least-privilege RBAC (e.g. read-only for `cluster:read` verification, or namespace-scoped for a write test) — provisioned via the deployer's own `kubectl create token`/RBAC flow, consistent with "the admin brings their own token."

#### 6. `frontend/src/app/(dashboard)/connections/_types/index.ts` — label

```ts
const ADAPTER_LABELS: Record<string, string> = {
  github_app: 'GitHub App',
  passthrough: 'Passthrough',
  kubernetes: 'Kubernetes',
};
```

No other frontend change — the table, action badges, and "used by" computation are already adapter-agnostic (`buildConnectionRows`).

---

## Consequences

### Positive

- Proves the credential-broker's adapter pattern generalizes past its first (GitHub) instance with a genuinely different injection shape (static vs. minted) and a genuinely new gateway capability (per-connection CA trust), while touching zero core broker/proxy authorization logic.
- The CA-trust mechanism is reusable infrastructure: any future private-CA-backed connection (a self-hosted GitLab, an internal API-key SaaS) gets it for free via `config.ca_cert`, with no gateway code change.
- Zero CRD schema change, zero new frontend component — matches the PRD's stated success metrics exactly.
- Audit/observability requirements are satisfied by existing generic mechanisms: `Broker.Authorize`'s structured logging (allow/deny, connection, action, tenant) already covers every brokered call; the only new distinguishable failure mode (CA trust failure) surfaces through the existing `ReverseProxy.ErrorHandler` path already used for every forward failure.

### Negative

- A long-lived, admin-provisioned bearer token is a standing privileged credential if the referenced Secret leaks — an accepted, documented risk (PRD Risks table) mitigated by admin-side least-privilege RBAC and bounded-TTL token provisioning guidance, not by anything this ADR builds. `TokenRequest` minting remains the fast-follow that closes this gap structurally.
- The two-tier verb-class (`cluster:write` including `DELETE`) cannot express "can create but not delete" inside kora's own policy layer; that granularity is explicitly delegated to the target cluster's RBAC on the injected identity (defense in depth), consistent with the PRD's stated non-goal.
- The gateway now holds one more piece of per-connection cached state (a `*http.Transport` per CA-declaring connection) — bounded by connection count, the same class of bound `githubapp`'s existing token cache already accepts, and invalidated wholesale on any registry reload rather than per-connection (simpler, acceptable given connection config changes rarely per the existing ADR's own Amendment 3 argument).

### Neutral

- `kubectl exec`/`port-forward` fail with an explicit, audited denial (`ErrUnsupportedAction`) rather than being silently mishandled as a REST call — a deliberate non-goal boundary enforced in code, not just documentation.
- The original 2026-06-06 ADR's per-tool table entry for Kubernetes ("Pattern A — minted") is now stale for v1; this ADR's correction should be read alongside it until that ADR is amended to reflect Pattern B for the shipped version.

---

## Amendment 1 — 2026-07-06: Resource-category verb taxonomy

**Status:** Accepted — corrects **Decision B** and resolves the [Kubernetes Connection PRD](../prd/kubernetes-connection.md)'s Open Question 2.

### Why

Decision B shipped a two-tier `cluster:read`/`cluster:write` taxonomy, explicitly flagged as a known gap: a `cluster:write` grant lets an agent create, patch, *or delete* anything in the cluster, with no way for an admin to grant "can touch pods and deployments" without also granting "can touch Secrets and RBAC objects." Option 2 in the original Decision B (a third `cluster:delete` tier) was considered and deferred as not addressing this; it only split one verb out of one bucket. Real usage patterns for ops agents (deploy/restart workloads vs. read config vs. manage RBAC) call for splitting by **resource category**, not just by verb.

### The correction: category-scoped actions, not one coarse pair

`k8sbearer.ClassifyAction` now parses the Kubernetes API request **path** (not just the HTTP verb) to determine which resource category a call touches, then combines that with a read/write verb class exactly as before. This replaces the single `cluster:read`/`cluster:write` pair with:

| Category | Resources | Read verbs | Write verbs |
|---|---|---|---|
| `workloads` | pods, deployments, replicasets, statefulsets, daemonsets, jobs, cronjobs, replicationcontrollers | GET/HEAD/OPTIONS | POST/PUT/PATCH/DELETE |
| `config` | configmaps | ″ | ″ |
| `secrets` | secrets | ″ | ″ |
| `networking` | services, endpoints, endpointslices, ingresses, ingressclasses, networkpolicies | ″ | ″ |
| `storage` | persistentvolumeclaims, persistentvolumes, storageclasses, volumeattachments | ″ | ″ |
| `rbac` | roles, rolebindings, clusterroles, clusterrolebindings, serviceaccounts | ″ | ″ |
| `cluster` (catch-all) | namespaces, nodes, events, resourcequotas, limitranges, **plus all non-resource API discovery paths** (`/api`, `/apis`, `/apis/{group}`, `/version`, `/openapi/*`, `/healthz`, root) | ″ | ″ |

**Secrets are deliberately split from ConfigMaps** into their own category rather than bundled under `config` — Secrets can hold credentials, materially more sensitive than ConfigMaps, so an admin can grant ConfigMap access without also granting Secret access (least privilege).

**A resource type not in this table (a CRD, or anything not yet enumerated) is denied outright** (`ErrUnsupportedAction`), the same fail-closed treatment already given to `kubectl exec`/`port-forward`. It is *not* folded into the `cluster` catch-all — that bucket is reserved for resources this ADR has explicitly reviewed and categorized. Extending coverage to a new resource (or a CRD) is a one-line addition to the adapter's category map, consistent with the "registration, not code" spirit elsewhere in this ADR, though it is a code change here (not a `Connection` CR field) because the mapping is Kubernetes-API-shape knowledge, not per-connection configuration.

### Discovery endpoints and the kubectl UX consequence

kubectl and client-go always call the non-resource discovery endpoints (`/api`, `/apis/...`) before any real request, to build their REST mapping. This taxonomy buckets those into `cluster` rather than denying them — mirroring upstream Kubernetes' own RBAC model, where `system:discovery` is bound to `system:authenticated`, i.e. every authenticated identity can hit these regardless of role.

**Practical consequence, called out explicitly so it isn't a support surprise:** any agent using kubectl or client-go needs `cluster:read` granted *in addition to* whichever specific categories it needs for its actual work. Granting only `workloads:read`, for example, is not enough — kubectl's own discovery call is denied first, before it ever gets to list a Pod. This is a deliberate trade-off: the alternative (exempting discovery from the policy check entirely) would require the broker to support an adapter-declared "no scope needed" classification, a bigger change to `Broker.Authorize`'s contract than this ADR scopes. Admin setup documentation for a Kubernetes connection should state this plainly: grant `cluster:read` alongside the specific category actions the agent needs.

### Statements superseded by this amendment

- **Decision B** (two-tier `cluster:read`/`cluster:write`, and Option 2's "three-tier, adding `cluster:delete`") → replaced by the category table above. `cluster:write` is now the catch-all's write action, not "every write in the cluster."
- **Consequences → Negative**, "The two-tier verb-class (`cluster:write` including `DELETE`) cannot express 'can create but not delete'…" → still true *within* a category (delegated to cluster RBAC, unchanged), but the blast radius of any single category is now bounded to that category's resources, not the whole cluster.
- **Notes**, "Open Question 2 (should a third `cluster:delete` tier exist) remain[s] open" → resolved by this amendment in favor of resource-category splitting instead.

The adapter's shape (`CredentialAdapter`, `Resolve`, secret-based bearer token, no minting), the per-connection CA trust mechanism (Decision C), and the SPDY/WebSocket-upgrade denial for `exec`/`port-forward` are unchanged.

---

## Amendment 2 — 2026-07-06: `baseHost` must be a DNS hostname, not an IP literal

**Status:** Accepted — adds a schema-level constraint discovered during live-cluster validation against a real, cloud-hosted cluster (GKE), not anticipated by the original Decision A/C design.

### Why

`charts/orbit/templates/egress-gateway/transparent-egress.yaml` authorizes a sandbox's outbound HTTPS by reading the TLS ClientHello's SNI extension and matching it against the connection's granted `egress:<host>:connect` scope. Per the TLS spec, a client never sends SNI when the destination is an IP literal. Confirmed directly against a real GKE cluster: the sandbox's OPA decision log showed the generated CONNECT with an empty `:authority` host and a `false` result — the transparent-egress interceptor's own header comment already documents this as intentional fail-closed behavior for empty SNI.

**Consequence:** any `kubernetes` connection whose `baseHost` is a bare IP can never be driven through a live agent sandbox — including the ADR's own original local k3d example (`<k3d server address>:6443`), which used a raw Docker IP. This is not fixable by connection configuration alone; it is a structural incompatibility between IP-literal hosts and the transparent-egress design generally, not specific to this adapter.

Since a real Kubernetes cluster (cloud-managed or self-hosted) is always reachable by *some* DNS hostname, this is enforced rather than merely documented:

- `internal/platform/connections/adapters.go` — `AdapterKubernetesBearer`'s metadata sets `requiresHostname: true`; the exported `ValidateBaseHost(adapter, baseHost) error` rejects an IPv4/IPv6 literal (with or without a port, brackets, or an `http(s)://` prefix) with sentinel `ErrBaseHostMustBeHostname`. Other adapters are unaffected.
- `internal/operator/connection_aggregator.go` — `Sync` calls `ValidateBaseHost` per connection; a failing connection is skipped (logged `Warn`) rather than aggregated into the gateway's mounted registry, the same pattern already used for a missing Secret.
- `charts/orbit-operator/templates/crd-connection.yaml` — an `x-kubernetes-validations` CEL rule rejects a `kubernetes` connection with a bare IPv4 `baseHost` at `kubectl apply` time, so this fails immediately and loudly instead of the connection silently vanishing from the gateway's registry later. Verified via `kubectl apply --dry-run=server`.

The local k3d example (`deploy/local/connections/kubernetes.yaml`) was updated to use the k3d server container's own name (`k3d-orbit-local-server-0`) as `baseHost` instead of its Docker IP — that name is already a SAN on the container's own kube-apiserver TLS cert (k3d puts it there itself), so it satisfies this constraint with no `--tls-san` flag or custom cert needed. `scripts/cluster/resolve-docker-ips.sh` patches CoreDNS's `NodeHosts` so the name resolves in-cluster to the container's Docker IP, mirroring the existing `orbit-postgres`/`orbit-redis` mechanism.

### Related finding, not solved by this amendment: GKE's DNS endpoint is IAM-gated

The obvious next step — give the connection a real hostname by enabling GKE's DNS-based control-plane endpoint (`gcloud container clusters update --enable-dns-access`) — was tried and does not work. That endpoint presents a real, publicly-trusted certificate for a real hostname, but requests to it return an HTTP 401 from Google's own edge **before** the request ever reaches kube-apiserver's bearer-token RBAC check — confirmed with a raw `curl` carrying a valid ServiceAccount token, bypassing kora entirely. GKE's built-in endpoints (IP or DNS) are therefore a dead end for this adapter's static-bearer-token design specifically; closing this gap for GKE would need either a self-managed hostname+cert fronting the cluster, or a gateway change decoupling the dial host/SNI from the TLS `ServerName` used for upstream certificate verification (`internal/egressgw/upstreamtransport.go`) — a real architectural change, tracked as a follow-up, not attempted here.

### Separate confirmed bug, independent of the above (unfixed)

`internal/agents/effective_scopes.go`'s Connector auto-egress derivation compiles `"egress:" + baseHost + ":connect"` verbatim. If `baseHost` includes an explicit port — which every realistic `kubernetes` `baseHost` does, including the local k3d example's own `:6443` — the resulting scope has two colons and fails `isStructurallyValidEgressScope` in `internal/policyservice/api/service.go` (which forbids `:` in the host segment). **Any Kubernetes connection whose API server isn't on the default HTTPS port 443 can never get a working Connector-derived grant today.** Worth a follow-up fix (strip the port before compiling the egress scope, or allow one trailing `:<port>` segment in `isStructurallyValidEgressScope`) — out of scope for this ADR, called out here so it isn't lost.

---

## Amendment 3 — 2026-07-06: Local re-verification of the full adapter/broker path

**Status:** Accepted — closes out the validation gap Amendment 2 left open (GKE was a dead end for both the IP-literal and DNS-gated endpoint) by re-running the full test matrix against the local k3d cluster's own hostname-based connection instead.

### What was verified

Using this ADR's own local example connection (`deploy/local/connections/kubernetes.yaml`, `baseHost: k3d-orbit-local-server-0:6443`), re-ran the complete 8-item test matrix from the GKE validation session against the rebuilt `k3d-orbit-local` cluster, via the same direct-HTTP mechanism (driving the gateway's CONNECT port with a minted agent JWT, bypassing the sandbox mesh):

1. Read succeeds — `GET /api/v1/pods` → `200` with real cluster data.
2. No real token reaches the caller — the injected bearer token has zero occurrences in gateway logs (only `resolved k8s bearer credential` metadata) or in the response body.
3. An ungranted category (`secrets:read`) is denied by policy (`deny:no_matching_grant`) before `Resolve` is ever called.
4. A `kubectl exec`-shaped request (`Upgrade: SPDY/3.1` against an `exec` subresource) is denied via the distinct `ErrUnsupportedAction` path — confirming Amendment 1's category classification and the SPDY/WebSocket-upgrade denial both work as designed.
5. An invalid `ca_cert` fails closed (`503`, `connection ca_cert contains no usable certificate`); a corrected `ca_cert` recovers to `200` once the kubelet remounts the ConfigMap — no gateway restart.
6. A `workloads:write` grant, plus namespace-scoped cluster-side RBAC (the test ServiceAccount is otherwise bound only to the read-only `view` ClusterRole), successfully creates and deletes a `Job` through the gateway.
7. Rotating the referenced Secret's token is picked up by the gateway's existing `fsnotify` reload with zero pod restarts.
8. Gateway logs show four distinguishable classes across the run — policy deny, adapter-classification deny, CA/transport failure, and clean authorized success — exceeding this ADR's own three-class bar (see Validation → Monitoring, updated below).

Full request/response detail lives in the working test log (`kube_test.md` at the repo root, dated 2026-07-06); this amendment folds its conclusions in here per the same practice Amendment 2 already established.

### What remains unverified

Both this local pass and the earlier GKE pass used the **direct-HTTP mechanism** — driving the gateway's CONNECT port directly with a minted agent JWT — not a live agent sandbox with a real kubeconfig pointed at the gateway. That mechanism exercises `Broker.Authorize` and every adapter/CA-trust code path this ADR designed, but it bypasses the sandbox's own transparent-egress interception (the Envoy/OPA SNI check) entirely. Unlike the GKE case — structurally blocked by Amendment 2's IP-literal SNI problem — a hostname-based connection like this one *should* pass a live sandbox's SNI check; that path is simply not yet exercised. Closing this remaining gap means running a real agent sandbox and driving `kubectl` through its actual kubeconfig, not curl through a port-forwarded CONNECT port. Tracked as a follow-up, not attempted here.

Separately, because of the unfixed Connector auto-egress bug (previous section), the policy grant used in this verification was created directly via the policy-service API with an explicit `egress:<host>:connect` scope, not derived automatically from the agent type's Connector — a live-sandbox test would need the same workaround until that bug is fixed.

---

## Validation

### Success Metrics

- The broker/adapter/CA-trust path equivalent to `kubectl get pods`/`kubectl apply -f ...` succeeds against a granted `kubernetes` connection with no real token present in the response or gateway logs. **Confirmed** (Amendment 3), via the direct-HTTP mechanism against both a real GKE cluster (Amendment 2, before its schema fix landed) and the local k3d cluster (Amendment 3, after). A literal live-sandbox `kubectl` run with a real kubeconfig — as opposed to curl through a port-forwarded CONNECT port — is not yet exercised (see Amendment 3 → "What remains unverified").
- A call against a connection with a self-signed/private CA in `config.ca_cert` succeeds (cert validated against that CA); a call with the CA omitted or wrong fails cert verification, not a silent passthrough. **Confirmed** (Amendment 3, item 5).
- An agent with no grant on the connection is denied before any credential is resolved, and a `kubectl exec` attempt is denied with `ErrUnsupportedAction` rather than reaching the cluster. **Confirmed** (Amendment 3, items 3–4).
- Rotating the referenced Secret's token requires no gateway restart (existing `fsnotify` reload path). **Confirmed** (Amendment 3, item 7).

### Monitoring

- Gateway structured logs distinguish (at least) three failure/success classes for a Kubernetes connection call: policy deny (`egress denied by policy`), adapter resolve/classification failure (`egress denied: adapter could not classify action` for `ErrUnsupportedAction`, distinct from a missing/unreadable secret), and CA/transport failure (`egress upstream transport unavailable`) — satisfying the PRD's observability requirement without new logging plumbing. Amendment 3's live re-verification observed **four** distinguishable classes in practice (policy deny, adapter-classification deny, CA/transport failure, and clean `egress authorized` success), exceeding this bar.

---

## Related Decisions

- [jit-credential-brokering-egress-gateway](2026-06-06-jit-credential-brokering-egress-gateway.md) — the architecture this ADR extends; corrects its Kubernetes flow from Pattern A to Pattern B for v1, and reuses Amendment 3's GitOps `Connection` CR model unchanged.
- [orbit-kubernetes-operator](2026-03-31-orbit-kubernetes-operator.md) — the `ConnectionAggregator`/CR reconciliation this feature reuses unchanged.

## References

- [Kubernetes Connection PRD](../prd/kubernetes-connection.md)
- [Credential Broker & Outbound Connections PRD](../prd/credential-broker-and-outbound-connections.md)
- `kube_test.md` (repo root) — working test log for the live GKE and local-k3d validation passes folded into Amendments 2 and 3
- `internal/credentials/models.go:29` — the reserved `AdapterKubernetesBearer` constant
- `internal/credentials/adapters/githubapp/githubapp.go` — the adapter shape mirrored
- `internal/egressgw/proxy.go` — the forward path the transport cache attaches to

---

## Notes

- Open Question 1 (should kora ever mint via `TokenRequest`) remains open, owned by Platform Engineering, and is unaffected by this ADR's decisions — an additive change to the adapter this ADR introduces, not blocked by it. Open Question 2 (verb-class taxonomy) is **resolved** by [Amendment 1](#amendment-1--2026-07-06-resource-category-verb-taxonomy): resource-category actions, not a third verb tier.
- [Amendment 2](#amendment-2--2026-07-06-basehost-must-be-a-dns-hostname-not-an-ip-literal) was discovered via live-cluster validation against a real GKE cluster (not the local k3d cluster this ADR's examples target) — that validation session's fixture (`deploy/local/connections/kubernetes-cloud.yaml`) has served its purpose and was removed once its findings were folded in here; it is not needed as ongoing infrastructure.
- [Amendment 3](#amendment-3--2026-07-06-local-re-verification-of-the-full-adapterbroker-path) re-ran the same live validation against the local k3d cluster's own hostname-based connection, confirming the adapter/broker/CA-trust path end-to-end now that GKE (Amendment 2) is a structural dead end for this adapter. The live-agent-sandbox `kubectl` leg specifically — as opposed to the direct-HTTP mechanism both live validation passes actually used — remains open.
- A resource-category taxonomy is necessarily an incomplete enumeration — CRDs and any resource not yet in the adapter's category map are denied outright (Amendment 1) rather than guessed at. Extending coverage as real usage surfaces gaps is expected, ordinary maintenance of that map, not a new ADR decision each time.
- If a `kubernetes`-sibling client-certificate adapter is built later (PRD Future Considerations), it can reuse the same `TransportCache`/`config.ca_cert` convention for its own upstream trust, and would need its own `tls.Certificate` injection path distinct from the header-swap `Credential` shape — tracked as a follow-up design question for that adapter, not this one.

**Last Updated:** 2026-07-06
