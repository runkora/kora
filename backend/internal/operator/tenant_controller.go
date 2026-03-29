package operator

import (
	"context"
	"fmt"
	"maps"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// TenantReconciler reconciles OrbitTenant resources.
//
// For each OrbitTenant it:
//  1. Creates the tenant namespace with the required Istio/SPIRE labels.
//  2. Syncs the orbit-tenants ConfigMap (via TenantRegistry) so the
//     identity service picks up the new OIDC provider.
//  3. Reconciles the OrbitPlatform release to add the new namespace to the
//     orbit-platform chart's namespaces.tenants list (NetworkPolicies,
//     ClusterSPIFFEIDs, etc.).
//
// See docs/adr/0025-orbit-kubernetes-operator.md for the design.
type TenantReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Registry *TenantRegistry
	Logger   logging.Logger
}

// Reconcile is called by controller-runtime whenever an OrbitTenant is
// created, updated, or when a requeue fires.
func (r *TenantReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Logger

	tenant := &OrbitTenant{}
	if err := r.Get(ctx, req.NamespacedName, tenant); err != nil {
		if apierrors.IsNotFound(err) {
			// CR deleted — trigger a registry sync so the tenant is removed.
			return ctrl.Result{}, r.Registry.Sync(ctx)
		}
		return ctrl.Result{}, fmt.Errorf("get OrbitTenant %q: %w", req.Name, err)
	}

	ns := tenantNamespace(tenant)

	log.Info(ctx, "reconciling OrbitTenant",
		logging.Field{Key: "tenant", Value: tenant.Name},
		logging.Field{Key: "namespace", Value: ns},
		logging.Field{Key: "phase", Value: tenant.Status.Phase},
	)

	// Handle suspended tenants — update registry and status, no namespace work.
	if !tenant.Spec.Active {
		return r.reconcileSuspended(ctx, tenant)
	}

	// Only transition to Provisioning from an initial/unset state. Once the
	// tenant has reached Ready, we do not regress it to Provisioning at the
	// start of every reconcile — that would generate a status patch, trigger a
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

	// Step 2: rebuild the tenant registry ConfigMap.
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

	// Step 3: mark ready and record timestamp.
	now := metav1.Now()
	tenant.Status.Namespace = ns
	tenant.Status.LastReconciledAt = &now

	_ = r.setTenantPhase(ctx, tenant, TenantPhaseReady)

	log.Info(ctx, "OrbitTenant reconciliation complete",
		logging.Field{Key: "tenant", Value: tenant.Name},
		logging.Field{Key: "namespace", Value: ns},
	)

	// Periodic re-check every 5 minutes — catches namespace label drift.
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

// SetupWithManager registers the controller with the controller-runtime Manager.
func (r *TenantReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&OrbitTenant{}).
		Complete(r)
}

// -----------------------------------------------------------------------------
// private helpers
// -----------------------------------------------------------------------------

// ensureNamespace creates the tenant namespace if it does not exist, or patches
// it if required labels are missing. Namespace creation is done via client-go
// CreateOrUpdate so Helm never owns the namespace (avoiding the ownership
// conflict described in ADR-0025).
func (r *TenantReconciler) ensureNamespace(ctx context.Context, tenant *OrbitTenant) error {
	ns := tenantNamespace(tenant)

	existing := &corev1.Namespace{}
	err := r.Get(ctx, client.ObjectKey{Name: ns}, existing)

	switch {
	case apierrors.IsNotFound(err):
		obj := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   ns,
				Labels: tenantNamespaceLabels(tenant),
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
		// Namespace exists — ensure required labels are present.
		if !hasRequiredLabels(existing.Labels, tenantNamespaceLabels(tenant)) {
			patch := client.MergeFrom(existing.DeepCopy())
			if existing.Labels == nil {
				existing.Labels = make(map[string]string)
			}
			maps.Copy(existing.Labels, tenantNamespaceLabels(tenant))
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
	r.Logger.Info(ctx, "tenant is suspended, syncing registry only",
		logging.Field{Key: "tenant", Value: tenant.Name},
	)
	if err := r.Registry.Sync(ctx); err != nil {
		_ = r.setTenantPhase(ctx, tenant, TenantPhaseDegraded)
		return ctrl.Result{RequeueAfter: 15 * time.Second}, fmt.Errorf("sync registry for suspended tenant: %w", err)
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

// tenantNamespaceLabels returns the required set of labels for a tenant
// namespace. These are checked on every reconcile and added if missing.
func tenantNamespaceLabels(tenant *OrbitTenant) map[string]string {
	return map[string]string{
		"istio-injection":              "enabled",
		"spiffe.io/spire-managed":      "true",
		"orbit.io/role":                "tenant",
		"orbit.io/tenant":              tenant.Name,
		"app.kubernetes.io/managed-by": "orbit-operator",
	}
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
