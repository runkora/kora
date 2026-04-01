# Umbrella Chart Migration Guide

## Overview

This guide documents the migration from an operator-driven deployment model to an umbrella chart architecture for Orbit platform and applications.

## Architecture

### Before: Operator-Driven Model

```
orbit-operator (Helm chart)
├── CRDs: OrbitPlatform, OrbitTenant
├── Operator Deployment
└── Reconciliation Loop:
    ├── Watches OrbitPlatform CR
    ├── Installs: spire-crds → spire → istio-base → istiod → gateway
    ├── Applies orbit-platform chart
    ├── Watches OrbitTenant CRs
    └── Updates orbit-tenants ConfigMap
```

**Challenges:**
- Indirect control over upstream charts (SPIRE, Istio)
- Operator complexity for chart installation
- Reconciliation loops for simple config changes
- Values injection obscures actual configuration

### After: Umbrella Chart Model

```
orbit-platform-stack (umbrella)
├── Remote dependencies:
│   ├── spire-crds (0.5.0)
│   ├── spire (0.28.3)
│   ├── istio-base (1.29.1)
│   ├── istiod (1.29.1)
│   └── istio-gateway (1.29.1)
└── Local dependency:
    └── orbit-platform (0.1.0)

orbit-apps (umbrella)
├── Templates:
│   ├── namespaces.yaml
│   ├── secrets.yaml
│   ├── configmaps.yaml
│   └── orbit-values.yaml
└── Local dependency:
    └── orbit (0.1.0)
```

**Benefits:**
- Direct control over all components
- Declarative configuration in values files
- Standard Helm workflow (no operator runtime)
- Transparent dependency management
- Easy version upgrades

## Migration Steps

### 1. Install Platform Stack

```bash
# Add remote Helm repositories
helm repo add spiffe https://spiffe.github.io/helm-charts-hardened/
helm repo add istio https://istio-release.storage.googleapis.com/charts
helm repo update

# Update dependencies (downloads remote charts)
helm dependency update ./charts/orbit-platform-stack

# Install platform infrastructure
helm upgrade --install orbit-platform \
  ./charts/orbit-platform-stack \
  --namespace orbit-system \
  --create-namespace \
  -f ./charts/orbit-platform-stack/values.yaml \
  --wait --timeout 10m

# Verify SPIRE
kubectl wait --for=condition=ready pod \
  -l app=spire-server -n spire-system --timeout=300s

# Verify Istio
kubectl wait --for=condition=ready pod \
  -l app=istiod -n istio-system --timeout=300s
```

### 2. Migrate Tenant Configuration

Convert OrbitTenant CRs to values.yaml entries:

**Before** (`deploy/local/tenants/example.yaml`):
```yaml
apiVersion: orbit.sh/v1alpha1
kind: OrbitTenant
metadata:
  name: example
spec:
  displayName: "Example Tenant"
  active: true
  oidc:
    issuerURL: "http://dex.orbit.local:5556/dex"
    clientID: "orbit-local"
    scopes:
      - openid
      - profile
```

**After** (`charts/orbit-apps/values.yaml`):
```yaml
tenants:
  - name: example
    displayName: "Example Tenant"
    active: true
    oidc:
      issuerURL: "http://dex.orbit.local:5556/dex"
      clientID: "orbit-local"
      scopes:
        - openid
        - profile
```

### 3. Install Applications

```bash
# Install orbit-identity and tenant configuration
helm upgrade --install orbit-apps \
  ./charts/orbit-apps \
  --namespace orbit-system \
  -f ./charts/orbit-apps/values.yaml \
  --wait --timeout 5m

# Verify identity service
kubectl wait --for=condition=ready pod \
  -l app=orbit-identity -n orbit-system --timeout=300s
```

### 4. Verify Migration

```bash
# Check SPIRE trust domain
kubectl get configmap -n spire-system spire-server -o yaml | grep trustDomain

# Check Istio trust domain
kubectl get configmap -n istio-system istio -o yaml | grep trustDomain

# Check tenant namespaces
kubectl get namespaces -l orbit.sh/tenant

# Check tenant registry
kubectl get configmap -n orbit-system orbit-tenants -o yaml

# Test identity service
kubectl port-forward -n orbit-system svc/orbit-identity 8080:8080
curl http://localhost:8080/health
```

### 5. Remove Operator (Optional)

```bash
# Delete OrbitPlatform and OrbitTenant CRs
kubectl delete orbitplatform --all
kubectl delete orbittenant --all

# Uninstall operator
helm uninstall orbit-operator --namespace orbit-system

# Remove CRDs (if no longer needed)
kubectl delete crd orbitplatforms.orbit.sh
kubectl delete crd orbittenants.orbit.sh
```

## Configuration Management

### Trust Domain Consistency

**CRITICAL**: The trust domain must match across all charts:

```yaml
# charts/orbit-platform-stack/values.yaml
global:
  trustDomain: "orbit.local"

spire:
  global:
    spire:
      trustDomain: "orbit.local"

istiod:
  global:
    trustDomain: "orbit.local"
  meshConfig:
    trustDomain: "orbit.local"

orbit-platform:
  global:
    trustDomain: "orbit.local"
```

```yaml
# charts/orbit-apps/values.yaml
global:
  trustDomain: "orbit.local"

orbit:
  global:
    trustDomain: "orbit.local"
```

### Secrets Management

**Development** (Helm-managed):
```yaml
# charts/orbit-apps/values.yaml
secrets:
  create: true
  database:
    password: "postgres"
  redis:
    password: "redis"
  tokenSigningKey: |
    LS0tLS1CRUdJTi...
```

**Production** (External Secrets Operator):
```yaml
# charts/orbit-apps/values.yaml
secrets:
  create: false

# Create ExternalSecret CRD separately
---
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: orbit-secrets
  namespace: orbit-system
spec:
  secretStoreRef:
    name: aws-secrets-manager
    kind: SecretStore
  target:
    name: orbit-secrets
  data:
    - secretKey: DB_PASSWORD
      remoteRef:
        key: orbit/database/password
    - secretKey: REDIS_PASSWORD
      remoteRef:
        key: orbit/redis/password
    - secretKey: ORBIT_TOKEN_SIGNING_KEY
      remoteRef:
        key: orbit/jwt/signing-key
```

### Tenant Management

Add a new tenant:

```yaml
# charts/orbit-apps/values.yaml
tenants:
  - name: new-tenant
    displayName: "New Tenant"
    active: true
    oidc:
      issuerURL: "https://auth.example.com"
      clientID: "new-tenant-client"
      clientSecret: "secret-value"  # Optional
      scopes:
        - openid
        - profile
        - email
```

Apply changes:
```bash
helm upgrade orbit-apps ./charts/orbit-apps \
  --namespace orbit-system \
  -f ./charts/orbit-apps/values.yaml
```

This will:
1. Create `orbit-tenant-new-tenant` namespace
2. Add entry to `orbit-tenants` ConfigMap
3. Add secret to `orbit-tenant-oidc-secrets` (if clientSecret provided)
4. Identity service hot-reloads tenant registry (no restart needed)

## Workflow Comparison

### Operator Model

```bash
# Edit CR
kubectl edit orbitplatform orbit-local

# Operator watches and reconciles
# (wait for reconciliation loop)

# Operator updates Helm releases
# (multiple release updates)

# Check operator logs
kubectl logs -n orbit-system deploy/orbit-operator
```

### Umbrella Chart Model

```bash
# Edit values file
vim charts/orbit-platform-stack/values.yaml

# Apply changes
helm upgrade orbit-platform ./charts/orbit-platform-stack \
  --namespace orbit-system \
  -f ./charts/orbit-platform-stack/values.yaml

# Standard Helm status
helm status orbit-platform -n orbit-system
```

## Rollback Strategy

```bash
# List releases
helm history orbit-platform -n orbit-system
helm history orbit-apps -n orbit-system

# Rollback to previous version
helm rollback orbit-platform 1 -n orbit-system
helm rollback orbit-apps 1 -n orbit-system
```

## Troubleshooting

### Dependency Update Failures

```bash
# Clear dependency cache
rm -rf charts/orbit-platform-stack/charts/*.tgz
rm -rf charts/orbit-platform-stack/Chart.lock

# Update dependencies
helm dependency update ./charts/orbit-platform-stack
```

### Trust Domain Mismatch

```bash
# Audit trust domain across all components
grep -r "trustDomain" charts/orbit-platform-stack/values.yaml
grep -r "trustDomain" charts/orbit-apps/values.yaml

# Verify running configuration
kubectl get configmap -n spire-system spire-server -o yaml | grep trustDomain
kubectl get configmap -n istio-system istio -o yaml | grep trustDomain
kubectl get configmap -n orbit-system orbit-config -o yaml | grep TRUST_DOMAIN
```

### Tenant Registration Issues

```bash
# Check tenant ConfigMap
kubectl get configmap -n orbit-system orbit-tenants -o yaml

# Check tenant secrets
kubectl get secret -n orbit-system orbit-tenant-oidc-secrets -o yaml

# Check identity service logs
kubectl logs -n orbit-system deploy/orbit-identity -c identity

# Verify hot-reload
kubectl get events -n orbit-system --field-selector involvedObject.name=orbit-tenants
```

## Performance Considerations

### Operator Model
- Reconciliation loops consume CPU/memory
- Watch events trigger reconciliation
- Multiple Helm releases per reconciliation
- Eventual consistency model

### Umbrella Chart Model
- No runtime overhead (no operator)
- Explicit upgrades via `helm upgrade`
- Single Helm release per umbrella
- Immediate consistency

## Security Implications

### Operator Model
- Operator requires cluster-admin for CRD management
- Operator has broad RBAC permissions
- Secret injection via operator code
- Runtime mutation of resources

### Umbrella Chart Model
- Standard Helm RBAC (no cluster-admin required)
- Secrets managed via values/ESO
- No runtime mutation
- Transparent resource ownership

## Future Enhancements

- **GitOps Integration**: ArgoCD/Flux for automated sync
- **Multi-cluster**: Umbrella charts per cluster with shared config
- **Blue-Green Deployments**: Separate umbrella releases with traffic shifting
- **Canary Releases**: Gradual rollout via Flagger integration

## References

- [Helm Umbrella Charts](https://helm.sh/docs/howto/charts_tips_and_tricks/#complex-charts-with-many-dependencies)
- [SPIRE Helm Charts](https://github.com/spiffe/helm-charts-hardened)
- [Istio Helm Charts](https://istio.io/latest/docs/setup/install/helm/)
- [External Secrets Operator](https://external-secrets.io/)
