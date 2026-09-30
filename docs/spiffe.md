# SPIFFE / SVID Identity Inventory

> Source of truth: live cluster (`orbit-local`), queried 2026-04-29.

## Trust Domain

All identities use trust domain: **`orbit.test`**

Confirmed in Istio mesh config (`kubectl get configmap -n istio-system istio`):
```
trustDomain: orbit.test
```

---

## Identity Schemes

There are four SPIFFE ID path formats in use:

| Scheme | Format | Issued via |
|---|---|---|
| kora system component | `spiffe://orbit.test/system/<component>` | `orbit-system-component` ClusterSPIFFEID |
| kora tenant agent | `spiffe://orbit.test/tenant/<namespace>/agent/<name>` | `orbit-tenant-agent` ClusterSPIFFEID |
| kora tenant service | `spiffe://orbit.test/tenant/<namespace>/service/<name>` | `orbit-tenant-service` ClusterSPIFFEID |
| Istio standard (`ns/sa`) | `spiffe://orbit.test/ns/<namespace>/sa/<service-account>` | `istio-gateway` / `istio-sidecar` / SPIRE infra ClusterSPIFFEIDs |

---

## Active SPIRE Entries (from `spire-server entry show`)

8 entries registered, bound by pod UID.

### kora System Components (`/system/` path)

| SPIFFE ID | Namespace | Workload |
|---|---|---|
| `spiffe://orbit.test/system/frontend` | orbit-system | orbit-frontend |
| `spiffe://orbit.test/system/identity` | orbit-system | orbit-identity |
| `spiffe://orbit.test/system/inference-gateway` | orbit-system | orbit-inference-gateway |
| `spiffe://orbit.test/system/opa-gateway` | orbit-system | orbit-opa-gateway |
| `spiffe://orbit.test/system/policy-gateway` | orbit-system | orbit-policy-gateway |
| `spiffe://orbit.test/system/policy-service` | orbit-system | orbit-policy-service |

### kora Tenant Workloads (`/tenant/` path)

| SPIFFE ID | Namespace | Workload |
|---|---|---|
| `spiffe://orbit.test/tenant/orbit-tenant-example/agent/example` | orbit-tenant-example | orbit-example (agent container) |

### Infrastructure (`/ns/sa/` path)

| SPIFFE ID | Namespace | Workload |
|---|---|---|
| `spiffe://orbit.test/ns/istio-system/sa/gateway` | istio-system | Istio gateway |
| `spiffe://orbit.test/ns/spire-system/sa/spire-spiffe-oidc-discovery-provider` | spire-system | SPIFFE OIDC discovery provider |

---

## ClusterSPIFFEID Auto-Registration Rules

The SPIRE controller manager manages registration via these `ClusterSPIFFEID` resources:

### kora-managed (from `orbit-platform` Helm release)

| Name | Namespace selector | Pod selector | SPIFFE ID template |
|---|---|---|---|
| `orbit-system-component` | `orbit-system` | pod has label `spiffe.io/component` | `spiffe://orbit.test/system/{{ .Labels["spiffe.io/component"] }}` |
| `orbit-tenant-agent` | namespaces with `orbit.io/role: tenant` | pod has label `spiffe.io/agent` | `spiffe://orbit.test/tenant/{{ .Namespace }}/agent/{{ .Labels["spiffe.io/agent"] }}` |
| `orbit-tenant-service` | namespaces with `orbit.io/role: tenant` | pod has label `spiffe.io/service` | `spiffe://orbit.test/tenant/{{ .Namespace }}/service/{{ .Labels["spiffe.io/service"] }}` |
| `istio-gateway` | `istio-system` | `app: gateway` | `spiffe://orbit.test/ns/{{ .Namespace }}/sa/{{ .ServiceAccount }}` |
| `istio-sidecar` | not `orbit-system`, not `orbit.io/role: tenant` | `spiffe.io/spire-managed-identity: "true"` | `spiffe://orbit.test/ns/{{ .Namespace }}/sa/{{ .ServiceAccount }}` |

### SPIRE-internal (from `spire` Helm release)

| Name | Pod selector | SPIFFE ID template |
|---|---|---|
| `spire-oidc-discovery-provider` | `component: oidc-discovery-provider` | `spiffe://orbit.test/ns/{{ .Namespace }}/sa/{{ .ServiceAccount }}` |
| `spire-test-keys` | `component: test-keys` | `spiffe://orbit.test/ns/{{ .Namespace }}/sa/{{ .ServiceAccount }}` |
| `spire-spike-keeper` | `component: spike-keeper` | `spiffe://orbit.test/spike/keeper/{{ .PodName }}` |
| `spire-spike-nexus` | `component: spike-nexus` | `spiffe://orbit.test/spike/nexus/{{ .PodName }}` |
| `spire-spike-pilot` | `component: spike-pilot` | `spiffe://orbit.test/spike/pilot/role/superuser` |
| `spire-spike-bootstrap` | `component: spike-bootstrap` | `spiffe://orbit.test/spike/bootstrap/{{ .PodName }}` |

---

## How Istio/Envoy Uses These Identities

kora uses **`ISTIO_MUTUAL`** mode with SPIRE as the certificate authority. Envoy sidecars obtain their X.509-SVID from SPIRE (via the SPIFFE CSI driver) and Istio uses the SAN from that cert for peer verification.

`DestinationRule` resources in `orbit-system` pin the expected server SAN, so Envoy (as client) will reject any connection where the server cert's SPIFFE ID doesn't match:

| DestinationRule | Host | Expected SPIFFE ID (subjectAltName) |
|---|---|---|
| `orbit-policy-service-client-policy` | `orbit-policy-service.orbit-system.svc.cluster.local` | `spiffe://orbit.test/system/policy-service` |
| `orbit-frontend-client-policy` | `orbit-frontend.orbit-system.svc.cluster.local` | `spiffe://orbit.test/system/frontend` |
| `orbit-inference-gateway-client-policy` | `orbit-inference-gateway.orbit-system.svc.cluster.local` | `spiffe://orbit.test/system/inference-gateway` |
| `orbit-identity-client-policy` | `orbit-identity.orbit-system.svc.cluster.local` | `spiffe://orbit.test/system/identity` |
| `orbit-policy-gateway-client-policy` | `orbit-policy-gateway.orbit-system.svc.cluster.local` | `spiffe://orbit.test/system/policy-gateway` |

---

## AuthorizationPolicy

| Name | Namespace | Action | Effect |
|---|---|---|---|
| `orbit-policy-service-gateway-bundle` | orbit-system | DENY | Blocks access to `/api/v1/policy/bundles/gateway` from any namespace other than `orbit-system` |

