package operator

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// TenantReconciler provisions tenant resources and aggregates tenant configuration.
type TenantReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	Registry        *TenantRegistry
	Aggregator      *ClientSecretAggregator
	CAAggregator    *TenantCAAggregator
	ConnAggregator  *ConnectionAggregator
	MCPAggregator   *MCPServerAggregator
	OAuthAggregator *OAuthClientAggregator
	// MarketplaceAggregator renders tenants' SkillMarketplaceSources. An edit
	// to spec.allowedMarketplaceRepos changes the tenant's generation, so it
	// reaches Reconcile through the existing predicate.
	MarketplaceAggregator *SkillMarketplaceAggregator
	Logger                logging.Logger

	// BundlePollingMinDelay is the minimum OPA bundle polling interval in seconds.
	BundlePollingMinDelay int

	// BundlePollingMaxDelay is the maximum OPA bundle polling interval in seconds.
	BundlePollingMaxDelay int

	// IstioRevision selects the injection label written on tenant namespaces:
	// istio.io/rev=<revision> when set, istio-injection=enabled otherwise.
	IstioRevision string

	// PodSecurity holds the Pod Security Admission levels labeled onto every
	// tenant namespace. Empty entries are not written.
	PodSecurity PodSecurityLabels

	// ImagePullSecrets are the platform pull Secrets replicated from the system
	// namespace into every tenant namespace.
	ImagePullSecrets []string

	// Collector is the in-cluster OTLP collector agent pods may export to.
	Collector AgentCollector

	// SandboxBase is registered in every tenant namespace as kora-base.
	SandboxBase SandboxBase

	// SandboxBundled holds the bundled sandbox images keyed as in si.Bundled.
	// Enabled ones are registered in every tenant namespace, and the rest are removed.
	SandboxBundled map[string]BundledSandboxImage

	// Preflight reports the latest prerequisites result. While it fails, tenants
	// are reconciled but held at Degraded so no workload is admitted.
	Preflight PreflightSource
}

// PreflightSource is the narrow view of the preflight runner the reconciler needs.
type PreflightSource interface {
	Latest() (PreflightResult, bool)
}

// PreflightResult is what the reconciler gates on.
type PreflightResult struct {
	OK       bool
	Failures string
}

// PodSecurityLabels are the Pod Security Admission levels for tenant namespaces.
type PodSecurityLabels struct {
	Enforce string
	Warn    string
	Audit   string
}

// Reconcile is called by controller-runtime whenever an OrbitTenant is
// created, updated, or when a requeue fires.
func (r *TenantReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Logger

	tenant := &OrbitTenant{}
	if err := r.Get(ctx, req.NamespacedName, tenant); err != nil {
		if apierrors.IsNotFound(err) {
			// CR deleted: re-run every aggregator so the removed tenant is no
			// longer served (registry, OIDC secrets, CAs, connections, MCP
			// servers, OAuth clients, skill marketplace sources).
			for _, sync := range []func(context.Context) error{
				r.Registry.Sync,
				r.Aggregator.Sync,
				r.CAAggregator.Sync,
				r.ConnAggregator.Sync,
				r.MCPAggregator.Sync,
				r.OAuthAggregator.Sync,
				r.MarketplaceAggregator.Sync,
			} {
				if syncErr := sync(ctx); syncErr != nil {
					return ctrl.Result{}, syncErr
				}
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get OrbitTenant %q: %w", req.Name, err)
	}

	ns := tenantNamespace(tenant)

	log.Info(ctx, "reconciling OrbitTenant",
		logging.Field{Key: "tenant", Value: tenant.Name},
		logging.Field{Key: "namespace", Value: ns},
		logging.Field{Key: "phase", Value: tenant.Status.Phase},
	)

	// Handle suspended tenants: update registry and status, no namespace work.
	if !tenant.Spec.Active {
		return r.reconcileSuspended(ctx, tenant)
	}

	// Only transition to Provisioning from an initial/unset state. Once the
	// tenant has reached Ready, we do not regress it to Provisioning at the
	// start of every reconcile, since that would generate a status patch, trigger a
	// new reconcile event, and produce an infinite Ready↔Provisioning loop.
	if tenant.Status.Phase == "" || tenant.Status.Phase == TenantPhasePending {
		if err := r.setTenantPhase(ctx, tenant, TenantPhaseProvisioning); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Step 1: ensure the tenant namespace exists with correct labels.
	if err := r.ensureNamespace(ctx, tenant); err != nil {
		log.Error(ctx, "failed to ensure tenant namespace",
			logging.Field{Key: "tenant", Value: tenant.Name},
			logging.Field{Key: "namespace", Value: ns},
			logging.Field{Key: "error", Value: err.Error()},
		)
		_ = r.setTenantCondition(ctx, tenant, ConditionNamespaceReady, metav1.ConditionFalse,
			"CreateFailed", err.Error())
		_ = r.setTenantPhase(ctx, tenant, TenantPhaseDegraded)
		return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
	}
	_ = r.setTenantCondition(ctx, tenant, ConditionNamespaceReady, metav1.ConditionTrue,
		"Ready", fmt.Sprintf("namespace %q exists with correct labels", ns))

	// Step 2: rebuild the tenant registry ConfigMap. Tracked separately
	// from the post-namespace ensure-steps below because it produces a
	// dedicated Condition (RegistryUpdated).
	if err := r.Registry.Sync(ctx); err != nil {
		log.Error(ctx, "failed to sync tenant registry",
			logging.Field{Key: "tenant", Value: tenant.Name},
			logging.Field{Key: "error", Value: err.Error()},
		)
		_ = r.setTenantCondition(ctx, tenant, ConditionRegistryUpdated, metav1.ConditionFalse,
			"SyncFailed", err.Error())
		_ = r.setTenantPhase(ctx, tenant, TenantPhaseDegraded)
		return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
	}
	_ = r.setTenantCondition(ctx, tenant, ConditionRegistryUpdated, metav1.ConditionTrue,
		"Synced", "orbit-tenants ConfigMap updated")

	// Steps 3-7: per-tenant resource ensure-steps. Each follows the same
	// failure shape: log, degrade, requeue. They're collapsed into a
	// table rather than duplicating the boilerplate per step.
	steps := []reconcileStep{
		{
			// Pull Secrets first: runtime pods and injected containers in this
			// namespace cannot pull without them.
			name: "image pull secrets",
			run:  func(ctx context.Context) error { return r.ensureImagePullSecrets(ctx, tenant) },
		},
		{
			// The base image every environment without its own image runs.
			name: "base sandbox image",
			run:  func(ctx context.Context) error { return r.ensureBaseSandboxImage(ctx, tenant) },
		},
		{
			name: "bundled sandbox images",
			run:  func(ctx context.Context) error { return r.ensureBundledSandboxImages(ctx, tenant) },
		},
		{
			name: "OPA config",
			run:  func(ctx context.Context) error { return r.ensureOPAConfig(ctx, tenant) },
		},
		{
			// Tenant CA Secret: see
			// docs/adr/2026-06-06-jit-credential-brokering-egress-gateway.md.
			// The gateway projects the aggregated form below; the sandbox
			// projects ca.crt only into its trust store.
			name: "tenant CA",
			run:  func(ctx context.Context) error { return r.ensureTenantCA(ctx, tenant) },
		},
		{
			// Lock sandbox egress to the per-tenant gateway. Without this
			// the structural credential-isolation guarantee weakens to a
			// convention: agents could dial external hosts directly.
			name: "sandbox egress NetworkPolicy",
			run:  func(ctx context.Context) error { return r.ensureSandboxEgressPolicy(ctx, tenant) },
		},
		{
			// Admit only this tenant's agent pods to a sandbox's exec port. The
			// port is outside the inbound mesh, so nothing else narrows the set
			// of callers that can reach it.
			name: "sandbox ingress NetworkPolicy",
			run:  func(ctx context.Context) error { return r.ensureSandboxIngressPolicy(ctx, tenant) },
		},
		{
			// Agent pods reach only the platform services they need
			// (docs/adr/2026-09-30-agent-pod-egress-lockdown.md).
			name: "agent egress NetworkPolicy",
			run:  func(ctx context.Context) error { return r.ensureAgentEgressPolicy(ctx, tenant) },
		},
		{
			// Has the agent pod's Envoy refuse any other host at once.
			name: "agent egress Sidecar",
			run:  func(ctx context.Context) error { return r.ensureAgentEgressSidecar(ctx, tenant) },
		},
		{
			// Aggregated OIDC client secrets: confidential tenants need
			// their client_secret available to the identity service
			// without a pod restart.
			name: "aggregated client secrets",
			run:  func(ctx context.Context) error { return r.Aggregator.Sync(ctx) },
		},
		{
			// Aggregated tenant CAs: pods can only mount Secrets from
			// their own namespace, so the per-tenant orbit-tenant-ca
			// Secrets are aggregated into one orbit-system Secret the
			// egress gateway projects.
			name: "aggregated tenant CAs",
			run:  func(ctx context.Context) error { return r.CAAggregator.Sync(ctx) },
		},
		{
			// Aggregated connections: each tenant's Connection CRs (and
			// their referenced Secrets) are aggregated into the
			// orbit-tenant-connections ConfigMap + orbit-tenant-connection-secrets
			// Secret in orbit-system the egress gateway mounts (Amendment 3).
			name: "aggregated connections",
			run:  func(ctx context.Context) error { return r.ConnAggregator.Sync(ctx) },
		},
		{
			// Aggregated MCP servers: every MCPServer CR across all namespaces
			// is rendered into the orbit-mcp-servers ConfigMap in orbit-system
			// the tool-gateway mounts. Re-aggregated here so a tenant suspend /
			// reactivate includes/excludes that tenant's servers.
			name: "aggregated MCP servers",
			run:  func(ctx context.Context) error { return r.MCPAggregator.Sync(ctx) },
		},
		{
			// Aggregated OAuth clients: each tenant's OAuthClient CRs are
			// rendered into the orbit-tenant-oauth-clients ConfigMap in
			// orbit-system the identity service mounts, so a pre-registered
			// hosted-app client appears without a pod restart. Re-aggregated
			// here so a tenant suspend / reactivate includes/excludes its clients.
			name: "aggregated OAuth clients",
			run:  func(ctx context.Context) error { return r.OAuthAggregator.Sync(ctx) },
		},
		{
			// Aggregated skill marketplace sources: each tenant's
			// SkillMarketplaceSource CRs are rendered into the
			// orbit-skill-marketplace-sources ConfigMap skills-service mounts.
			// Re-aggregated here because an allowlist edit, a suspend and a
			// reactivation all change which sources are served.
			name: "aggregated skill marketplace sources",
			run:  func(ctx context.Context) error { return r.MarketplaceAggregator.Sync(ctx) },
		},
	}
	for _, step := range steps {
		if err := step.run(ctx); err != nil {
			log.Error(ctx, "failed to ensure "+step.name,
				logging.Field{Key: "tenant", Value: tenant.Name},
				logging.Field{Key: "error", Value: err.Error()},
			)
			_ = r.setTenantPhase(ctx, tenant, TenantPhaseDegraded)
			return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
		}
	}

	if result, blocked := r.applyPreflightGate(ctx, tenant); blocked {
		return result, nil
	}

	if err := r.markReady(ctx, tenant, ns); err != nil {
		log.Error(ctx, "failed to patch OrbitTenant status",
			logging.Field{Key: "tenant", Value: tenant.Name},
			logging.Field{Key: "error", Value: err.Error()},
		)
	}

	log.Info(ctx, "OrbitTenant reconciliation complete",
		logging.Field{Key: "tenant", Value: tenant.Name},
		logging.Field{Key: "namespace", Value: ns},
	)

	// Periodic re-check every 5 minutes: catches namespace label drift.
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

// markReady patches Namespace, LastReconciledAt and Phase in one call.
// setTenantPhase alone would skip this: it no-ops when the phase is already
// Ready, which is every reconcile after the first, so Namespace and
// LastReconciledAt would never reach the API server on steady-state
// reconciles.
func (r *TenantReconciler) markReady(ctx context.Context, tenant *OrbitTenant, ns string) error {
	patch := client.MergeFrom(tenant.DeepCopy())
	now := metav1.Now()
	tenant.Status.Namespace = ns
	tenant.Status.LastReconciledAt = &now
	tenant.Status.Phase = TenantPhaseReady
	if err := r.Status().Patch(ctx, tenant, patch); err != nil {
		return fmt.Errorf("patch OrbitTenant %q status: %w", tenant.Name, err)
	}
	return nil
}

// SetupWithManager registers the controller with the controller-runtime Manager.
// Do not watch Secrets: a cluster-wide informer would cache every Secret.
// Periodic reconciliation picks up rotations.
func (r *TenantReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// markReady patches status on every pass; without this predicate that
		// patch re-enqueues the tenant and the reconciler spins.
		For(&OrbitTenant{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		// A Connection CR change must re-aggregate the orbit-system connection
		// registry/secrets the egress gateway mounts (Amendment 3). The
		// Connection lives in a tenant namespace, so map it to the owning
		// OrbitTenant's reconcile, which runs ConnAggregator.Sync.
		Watches(
			&Connection{},
			handler.EnqueueRequestsFromMapFunc(r.connectionToTenants),
		).
		// An OAuthClient CR change must re-aggregate the orbit-system
		// oauth-clients ConfigMap the identity service mounts. The OAuthClient
		// lives in a tenant namespace, so map it to the owning OrbitTenant's
		// reconcile, which runs OAuthAggregator.Sync.
		Watches(
			&OAuthClient{},
			handler.EnqueueRequestsFromMapFunc(r.oauthClientToTenants),
		).
		Complete(r)
}

// connectionToTenants maps a Connection event to the OrbitTenant whose
// namespace the Connection lives in, so applying/deleting a Connection triggers
// re-aggregation of the mounted gateway config.
func (r *TenantReconciler) connectionToTenants(ctx context.Context, obj client.Object) []reconcile.Request {
	conn, ok := obj.(*Connection)
	if !ok {
		return nil
	}

	list := &OrbitTenantList{}
	if err := r.List(ctx, list); err != nil {
		r.Logger.Error(ctx, "failed to list OrbitTenants for connection watch",
			logging.Field{Key: "error", Value: err.Error()},
		)
		return nil
	}

	var reqs []reconcile.Request
	for i := range list.Items {
		t := &list.Items[i]
		if tenantNamespace(t) == conn.Namespace {
			reqs = append(reqs, reconcile.Request{
				NamespacedName: client.ObjectKey{Name: t.Name},
			})
		}
	}
	return reqs
}

// oauthClientToTenants maps an OAuthClient event to the OrbitTenant whose
// namespace the OAuthClient lives in, so applying/deleting an OAuthClient
// triggers re-aggregation of the mounted identity oauth-clients ConfigMap.
func (r *TenantReconciler) oauthClientToTenants(ctx context.Context, obj client.Object) []reconcile.Request {
	oc, ok := obj.(*OAuthClient)
	if !ok {
		return nil
	}

	list := &OrbitTenantList{}
	if err := r.List(ctx, list); err != nil {
		r.Logger.Error(ctx, "failed to list OrbitTenants for oauth client watch",
			logging.Field{Key: "error", Value: err.Error()},
		)
		return nil
	}

	var reqs []reconcile.Request
	for i := range list.Items {
		t := &list.Items[i]
		if tenantNamespace(t) == oc.Namespace {
			reqs = append(reqs, reconcile.Request{
				NamespacedName: client.ObjectKey{Name: t.Name},
			})
		}
	}
	return reqs
}

// -----------------------------------------------------------------------------
// private helpers
// -----------------------------------------------------------------------------

// ensureNamespace creates the tenant namespace if it does not exist, or patches
// it if required labels are missing. Namespace creation is done via client-go
// CreateOrUpdate so Helm never owns the namespace (avoiding the ownership
// conflict described in docs/adr/2026-03-31-orbit-kubernetes-operator.md).
func (r *TenantReconciler) ensureNamespace(ctx context.Context, tenant *OrbitTenant) error {
	ns := tenantNamespace(tenant)

	existing := &corev1.Namespace{}
	err := r.Get(ctx, client.ObjectKey{Name: ns}, existing)

	switch {
	case apierrors.IsNotFound(err):
		obj := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   ns,
				Labels: r.tenantNamespaceLabels(tenant),
				Annotations: map[string]string{
					"orbit.io/managed-by": "orbit-operator",
					"orbit.io/tenant":     tenant.Name,
				},
			},
		}
		if err = r.Create(ctx, obj); err != nil {
			return fmt.Errorf("create namespace %q: %w", ns, err)
		}
		r.Logger.Info(ctx, "created tenant namespace",
			logging.Field{Key: "namespace", Value: ns},
			logging.Field{Key: "tenant", Value: tenant.Name},
		)
	case err != nil:
		return fmt.Errorf("get namespace %q: %w", ns, err)
	default:
		// Namespace exists: ensure required labels are present and the
		// injection label for the other Istio mode is gone. Istio's revision
		// webhook only matches namespaces without istio-injection.
		stale := staleInjectionLabel(r.IstioRevision)
		_, hasStale := existing.Labels[stale]
		if !hasRequiredLabels(existing.Labels, r.tenantNamespaceLabels(tenant)) || hasStale {
			patch := client.MergeFrom(existing.DeepCopy())
			if existing.Labels == nil {
				existing.Labels = make(map[string]string)
			}
			maps.Copy(existing.Labels, r.tenantNamespaceLabels(tenant))
			delete(existing.Labels, stale)
			if err := r.Patch(ctx, existing, patch); err != nil {
				return fmt.Errorf("patch namespace %q labels: %w", ns, err)
			}
			r.Logger.Info(ctx, "patched tenant namespace labels",
				logging.Field{Key: "namespace", Value: ns},
				logging.Field{Key: "tenant", Value: tenant.Name},
			)
		}
	}
	return nil
}

// reconcileSuspended handles the case where spec.active is false. The
// namespace is left intact; only the registry is synced to reflect suspension.
func (r *TenantReconciler) reconcileSuspended(ctx context.Context, tenant *OrbitTenant) (ctrl.Result, error) {
	r.Logger.Info(ctx, "tenant is suspended, syncing registry and aggregated secrets",
		logging.Field{Key: "tenant", Value: tenant.Name},
	)
	if err := r.Registry.Sync(ctx); err != nil {
		_ = r.setTenantPhase(ctx, tenant, TenantPhaseDegraded)
		return ctrl.Result{RequeueAfter: 15 * time.Second}, fmt.Errorf("sync registry for suspended tenant: %w", err)
	}
	if err := r.Aggregator.Sync(ctx); err != nil {
		_ = r.setTenantPhase(ctx, tenant, TenantPhaseDegraded)
		return ctrl.Result{RequeueAfter: 15 * time.Second}, fmt.Errorf("sync aggregated secrets for suspended tenant: %w", err)
	}
	if err := r.CAAggregator.Sync(ctx); err != nil {
		_ = r.setTenantPhase(ctx, tenant, TenantPhaseDegraded)
		return ctrl.Result{RequeueAfter: 15 * time.Second}, fmt.Errorf("sync aggregated tenant CAs for suspended tenant: %w", err)
	}
	if err := r.ConnAggregator.Sync(ctx); err != nil {
		_ = r.setTenantPhase(ctx, tenant, TenantPhaseDegraded)
		return ctrl.Result{RequeueAfter: 15 * time.Second}, fmt.Errorf("sync aggregated connections for suspended tenant: %w", err)
	}
	if err := r.MCPAggregator.Sync(ctx); err != nil {
		_ = r.setTenantPhase(ctx, tenant, TenantPhaseDegraded)
		return ctrl.Result{RequeueAfter: 15 * time.Second}, fmt.Errorf("sync aggregated MCP servers for suspended tenant: %w", err)
	}
	if err := r.OAuthAggregator.Sync(ctx); err != nil {
		_ = r.setTenantPhase(ctx, tenant, TenantPhaseDegraded)
		return ctrl.Result{RequeueAfter: 15 * time.Second}, fmt.Errorf("sync aggregated OAuth clients for suspended tenant: %w", err)
	}
	if err := r.MarketplaceAggregator.Sync(ctx); err != nil {
		_ = r.setTenantPhase(ctx, tenant, TenantPhaseDegraded)
		return ctrl.Result{RequeueAfter: 15 * time.Second}, fmt.Errorf("sync skill marketplace sources for suspended tenant: %w", err)
	}
	_ = r.setTenantPhase(ctx, tenant, TenantPhaseSuspended)
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

// setTenantPhase patches the OrbitTenant status phase.
// It is a no-op when the phase is already set to the requested value, which
// prevents spurious status patches from triggering extra reconcile events.
func (r *TenantReconciler) setTenantPhase(ctx context.Context, t *OrbitTenant, phase string) error {
	if t.Status.Phase == phase {
		return nil
	}
	patch := client.MergeFrom(t.DeepCopy())
	t.Status.Phase = phase
	if err := r.Status().Patch(ctx, t, patch); err != nil {
		return fmt.Errorf("patch OrbitTenant %q status phase to %q: %w", t.Name, phase, err)
	}
	return nil
}

// setTenantCondition sets a named condition on the OrbitTenant status.
func (r *TenantReconciler) setTenantCondition(
	ctx context.Context,
	t *OrbitTenant,
	condType string,
	status metav1.ConditionStatus,
	reason, message string,
) error {
	patch := client.MergeFrom(t.DeepCopy())
	meta.SetStatusCondition(&t.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		ObservedGeneration: t.Generation,
		Reason:             reason,
		Message:            message,
	})
	if err := r.Status().Patch(ctx, t, patch); err != nil {
		return fmt.Errorf("patch OrbitTenant %q condition %q: %w", t.Name, condType, err)
	}
	return nil
}

// ensureOPAConfig creates or updates the orbit-opa-config ConfigMap in the
// tenant namespace. The webhook mounts it into the OPA sidecar it injects: the
// policy-service bundle endpoint and the tenant bundle path, resolved at runtime
// from the pod's orbit.io/tenant-id annotation.
func (r *TenantReconciler) ensureOPAConfig(ctx context.Context, tenant *OrbitTenant) error {
	ns := tenantNamespace(tenant)

	// OPA bundle polling interval is configurable via operator config. After a
	// grant write, enforcement is stale for up to max_delay_seconds until the
	// next bundle poll picks up the change. Emergency revocation uses the JTI
	// blocklist (docs/adr/2026-03-05-unified-identity-model.md), not bundle propagation.
	minDelay := r.BundlePollingMinDelay
	if minDelay == 0 {
		minDelay = 30
	}
	maxDelay := r.BundlePollingMaxDelay
	if maxDelay == 0 {
		maxDelay = 60
	}
	opaConfigYAML := fmt.Sprintf(`services:
  policy-service:
    url: http://orbit-policy-service.%s.svc/api/v1/policy

bundles:
  authz:
    service: policy-service
    resource: "bundles/${ORBIT_TENANT_ID}"
    polling:
      min_delay_seconds: %d
      max_delay_seconds: %d

plugins:
  envoy_ext_authz_grpc:
    addr: localhost:9191
    path: orbit/authz/allow
    allow_if_undefined: false

decision_logs:
  console: true
`, systemNamespace, minDelay, maxDelay)

	// --authorization=basic (pod_webhook.go) applies to every listener the
	// process serves, diagnostics included: without this rule the liveness
	// and readiness probes on the diagnostic port get denied too.
	const authzRego = `package system.authz

default allow := false

allow if input.path == ["health"]
`

	desired := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "orbit-opa-config",
			Namespace: ns,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "orbit-operator",
				"orbit.io/tenant":              tenant.Name,
			},
		},
		Data: map[string]string{
			"config.yaml": opaConfigYAML,
			"authz.rego":  authzRego,
		},
	}

	existing := &corev1.ConfigMap{}
	err := r.Get(ctx, client.ObjectKey{Name: "orbit-opa-config", Namespace: ns}, existing)
	if apierrors.IsNotFound(err) {
		if createErr := r.Create(ctx, desired); createErr != nil {
			return fmt.Errorf("create orbit-opa-config in %q: %w", ns, createErr)
		}
		r.Logger.Info(ctx, "created orbit-opa-config",
			logging.Field{Key: "namespace", Value: ns},
			logging.Field{Key: "tenant", Value: tenant.Name},
		)
		return nil
	}
	if err != nil {
		return fmt.Errorf("get orbit-opa-config in %q: %w", ns, err)
	}

	// Update if data has drifted. Compares the whole map, not one key: an
	// existing ConfigMap could be missing a key added since it was written
	// (config.yaml unchanged, authz.rego new) and a single-key comparison
	// would never see that as drift.
	if !reflect.DeepEqual(existing.Data, desired.Data) {
		patch := client.MergeFrom(existing.DeepCopy())
		existing.Data = desired.Data
		if err := r.Patch(ctx, existing, patch); err != nil {
			return fmt.Errorf("patch orbit-opa-config in %q: %w", ns, err)
		}
		r.Logger.Info(ctx, "patched orbit-opa-config",
			logging.Field{Key: "namespace", Value: ns},
		)
	}
	return nil
}

// applyPreflightGate marks the tenant Degraded until prerequisites pass.
func (r *TenantReconciler) applyPreflightGate(ctx context.Context, tenant *OrbitTenant) (ctrl.Result, bool) {
	if r.Preflight == nil {
		return ctrl.Result{}, false
	}
	res, ok := r.Preflight.Latest()
	switch {
	case !ok:
		_ = r.setTenantCondition(ctx, tenant, ConditionPrerequisitesSatisfied, metav1.ConditionUnknown,
			"Pending", "preflight has not completed yet")
		_ = r.setTenantPhase(ctx, tenant, TenantPhaseProvisioning)
		return ctrl.Result{RequeueAfter: 15 * time.Second}, true
	case !res.OK:
		_ = r.setTenantCondition(ctx, tenant, ConditionPrerequisitesSatisfied, metav1.ConditionFalse,
			"PreflightFailed", res.Failures)
		_ = r.setTenantPhase(ctx, tenant, TenantPhaseDegraded)
		return ctrl.Result{RequeueAfter: time.Minute}, true
	}
	_ = r.setTenantCondition(ctx, tenant, ConditionPrerequisitesSatisfied, metav1.ConditionTrue,
		"Satisfied", "Istio and SPIRE prerequisites verified")
	return ctrl.Result{}, false
}

// tenantNamespaceLabels returns the labels every tenant namespace must carry:
// kora's markers, the Istio injection label for the configured revision, and
// the Pod Security Admission levels. Re-applied on every reconcile.
// staleInjectionLabel is the Istio injection label the configured mode does
// not use: istio-injection under a named revision, istio.io/rev otherwise.
func staleInjectionLabel(revision string) string {
	if revision != "" {
		return "istio-injection"
	}
	return "istio.io/rev"
}

func (r *TenantReconciler) tenantNamespaceLabels(tenant *OrbitTenant) map[string]string {
	labels := map[string]string{
		"spiffe.io/spire-managed":      "true",
		"orbit.io/role":                "tenant",
		"orbit.io/tenant":              tenant.Name,
		"app.kubernetes.io/managed-by": "orbit-operator",
	}
	if r.IstioRevision != "" {
		labels["istio.io/rev"] = r.IstioRevision
	} else {
		labels["istio-injection"] = "enabled"
	}
	if r.PodSecurity.Enforce != "" {
		labels["pod-security.kubernetes.io/enforce"] = r.PodSecurity.Enforce
	}
	if r.PodSecurity.Warn != "" {
		labels["pod-security.kubernetes.io/warn"] = r.PodSecurity.Warn
	}
	if r.PodSecurity.Audit != "" {
		labels["pod-security.kubernetes.io/audit"] = r.PodSecurity.Audit
	}
	return labels
}

// hasRequiredLabels returns true when all entries in required are present in
// actual with matching values.
func hasRequiredLabels(actual, required map[string]string) bool {
	for k, v := range required {
		if actual[k] != v {
			return false
		}
	}
	return true
}

// reconcileStep is one entry in the table of post-namespace
// ensure-/sync-steps run from Reconcile. Each step's failure is handled
// uniformly: log, mark the tenant Degraded, requeue.
type reconcileStep struct {
	name string
	run  func(ctx context.Context) error
}
