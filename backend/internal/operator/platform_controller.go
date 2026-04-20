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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// PlatformReconciler reconciles OrbitPlatform resources. It drives the full
// Orbit Helm release sequence:
//
//	spire-crds → spire → istio-base → istiod → gateway → orbit-platform → orbit
//
// See docs/adr/0028-orbit-kubernetes-operator.md for the design.
type PlatformReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	RestConfig *rest.Config
	Logger     logging.Logger

	// ChartDir is the base directory containing local Helm charts.
	// In local dev this is the repo's charts/ directory.
	// In production this is the directory where charts are embedded.
	ChartDir string

	// Registry is shared with TenantReconciler so the platform controller can
	// pre-create the orbit-tenants ConfigMap (empty) before orbit-identity
	// starts. Without it, orbit-identity fails to mount the volume on first boot
	// when no OrbitTenant CRs have been applied yet.
	Registry *TenantRegistry

	// OPAImage is the OPA Envoy plugin sidecar image. Configurable so operators
	// can patch CVEs via Helm values without rebuilding the operator binary.
	OPAImage string

	// OPALogLevel is the log level for the OPA sidecar (info, debug, error).
	OPALogLevel string
}

// Reconcile is called by controller-runtime whenever an OrbitPlatform is
// created, updated, or when a requeue fires.
func (r *PlatformReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Logger

	platform := &OrbitPlatform{}
	if err := r.Get(ctx, req.NamespacedName, platform); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get OrbitPlatform %q: %w", req.Name, err)
	}

	log.Info(ctx, "reconciling OrbitPlatform",
		logging.Field{Key: "name", Value: platform.Name},
		logging.Field{Key: "version", Value: platform.Spec.Version},
		logging.Field{Key: "phase", Value: platform.Status.Phase},
	)

	// Transition to Installing only from an initial/failed/pending state.
	// Never regress from Ready → Installing at the start of every reconcile —
	// that would generate a status patch, trigger a new reconcile event, and
	// produce an infinite Ready↔Installing loop.
	switch platform.Status.Phase {
	case "", PhasePending, PhaseFailed:
		if err := r.setPhase(ctx, platform, PhaseInstalling); err != nil {
			return ctrl.Result{}, err
		}
	}

	if err := r.reconcileReleases(ctx, platform); err != nil {
		// Helm reports "another operation (install/upgrade/rollback) is in
		// progress" when a concurrent reconcile races on the same release.
		// Back off and retry — the in-progress operation will complete shortly.
		if isHelmBusy(err) {
			log.Info(ctx, "helm release busy, requeueing",
				logging.Field{Key: "name", Value: platform.Name},
				logging.Field{Key: "error", Value: err.Error()},
			)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		log.Error(ctx, "helm release reconciliation failed",
			logging.Field{Key: "name", Value: platform.Name},
			logging.Field{Key: "error", Value: err.Error()},
		)
		_ = r.setCondition(ctx, platform, ConditionPlatformReady, metav1.ConditionFalse,
			"HelmError", err.Error())
		_ = r.setPhase(ctx, platform, PhaseFailed)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Sync the tenant count into status.
	tenantList := &OrbitTenantList{}
	if err := r.List(ctx, tenantList); err != nil {
		return ctrl.Result{}, fmt.Errorf("list OrbitTenants: %w", err)
	}

	now := metav1.Now()
	platform.Status.TenantCount = len(tenantList.Items)
	platform.Status.Version = platform.Spec.Version
	platform.Status.LastReconciledAt = &now

	_ = r.setCondition(ctx, platform, ConditionPlatformReady, metav1.ConditionTrue,
		"Reconciled", "all platform releases deployed")
	_ = r.setPhase(ctx, platform, PhaseReady)

	log.Info(ctx, "OrbitPlatform reconciliation complete",
		logging.Field{Key: "name", Value: platform.Name},
		logging.Field{Key: "phase", Value: PhaseReady},
	)

	// Periodic re-check every 5 minutes — catches drift.
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

// SetupWithManager registers the controller with the controller-runtime Manager.
func (r *PlatformReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&OrbitPlatform{}).
		Complete(r)
}

// -----------------------------------------------------------------------------
// release sequence
// -----------------------------------------------------------------------------

// ensureSystemNamespace creates the orbit-system namespace if it does not exist.
// The namespace is created with the labels required for SPIRE and Istio injection
// before any Helm release runs. This removes the need for a separate bootstrap manifest.
func (r *PlatformReconciler) ensureSystemNamespace(ctx context.Context) error {
	ns := &corev1.Namespace{}
	err := r.Get(ctx, types.NamespacedName{Name: "orbit-system"}, ns)
	if err == nil {
		return nil // already exists
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get orbit-system namespace: %w", err)
	}
	ns = &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "orbit-system",
			Labels: map[string]string{
				"istio-injection":         "enabled",
				"spiffe.io/spire-managed": "true",
			},
		},
	}
	if createErr := r.Create(ctx, ns); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
		return fmt.Errorf("create orbit-system namespace: %w", createErr)
	}
	r.Logger.Info(ctx, "created orbit-system namespace")
	return nil
}

// ensureOrbitConfig creates or updates the shared non-secret Orbit ConfigMap in
// orbit-system when spec.config.enabled is true. The ConfigMap holds DB_* and
// REDIS_* keys derived from the enabled sections of spec.config.
func (r *PlatformReconciler) ensureOrbitConfig(ctx context.Context, p *OrbitPlatform) error {
	if !p.Spec.Config.IsEnabled() {
		r.Logger.Info(ctx, "skipping shared config ConfigMap reconciliation",
			logging.Field{Key: "configmap", Value: p.Spec.platformConfigMapName()},
		)
		return nil
	}

	name := p.Spec.platformConfigMapName()
	desired := p.Spec.Config.ManagedData()

	existing := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: systemNamespace}, existing)
	if apierrors.IsNotFound(err) {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: systemNamespace,
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "orbit-operator",
					"orbit.io/component":           "platform-config",
				},
			},
			Data: desired,
		}
		if createErr := r.Create(ctx, cm); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
			return fmt.Errorf("create %s ConfigMap: %w", name, createErr)
		}
		r.Logger.Info(ctx, "created shared Orbit config ConfigMap",
			logging.Field{Key: "configmap", Value: name},
		)
		return nil
	}
	if err != nil {
		return fmt.Errorf("get %s ConfigMap: %w", name, err)
	}

	// Update only if content has drifted.
	if existing.Data == nil {
		existing.Data = make(map[string]string)
	}
	changed := false
	for k, v := range desired {
		if existing.Data[k] != v {
			changed = true
			break
		}
	}
	if !changed {
		return nil
	}
	patch := client.MergeFrom(existing.DeepCopy())
	maps.Copy(existing.Data, desired)
	if patchErr := r.Patch(ctx, existing, patch); patchErr != nil {
		return fmt.Errorf("patch %s ConfigMap: %w", name, patchErr)
	}
	r.Logger.Info(ctx, "updated shared Orbit config ConfigMap",
		logging.Field{Key: "configmap", Value: name},
	)
	return nil
}

// reconcileReleases drives all Helm releases in dependency order.
// Each step is a named function so failures are easy to attribute.
func (r *PlatformReconciler) reconcileReleases(ctx context.Context, p *OrbitPlatform) error {
	// Ensure the system namespace exists before any Helm release runs.
	if err := r.ensureSystemNamespace(ctx); err != nil {
		return fmt.Errorf("orbit-system namespace: %w", err)
	}

	versions := r.loadVersions()

	if p.Spec.SPIRE.Enabled {
		if err := r.reconcileSPIRECRDs(ctx, p, versions); err != nil {
			return fmt.Errorf("spire-crds: %w", err)
		}
		if err := r.reconcileSPIRE(ctx, p, versions); err != nil {
			return fmt.Errorf("spire: %w", err)
		}
	}

	if p.Spec.Istio.Enabled {
		if err := r.reconcileIstioBase(ctx, p, versions); err != nil {
			return fmt.Errorf("istio-base: %w", err)
		}
		if err := r.reconcileIstiod(ctx, p, versions); err != nil {
			return fmt.Errorf("istiod: %w", err)
		}
	}

	// orbit-platform must be installed before the gateway so that the
	// ClusterSPIFFEID resources (istio-gateway, orbit-system-component, etc.)
	// exist before the gateway pod starts and requests a SPIFFE SVID from SPIRE.
	// Without this the gateway is stuck waiting for a SPIRE entry that never
	// arrives, causing the Helm install to time out and the operator to loop.
	if err := r.reconcileOrbitPlatform(ctx, p); err != nil {
		return fmt.Errorf("orbit-platform: %w", err)
	}

	if p.Spec.Istio.Enabled {
		if err := r.reconcileGateway(ctx, p, versions); err != nil {
			return fmt.Errorf("gateway: %w", err)
		}
	}

	// Ensure the shared non-secret platform config ConfigMap exists before
	// deploying orbit services when operator management is enabled. When
	// disabled, services still point at spec.config.configMapName and the
	// operator assumes that ConfigMap already exists.
	if err := r.ensureOrbitConfig(ctx, p); err != nil {
		return fmt.Errorf("%s: %w", p.Spec.platformConfigMapName(), err)
	}

	// Ensure orbit-tenants ConfigMap exists (may be empty if no OrbitTenant CRs
	// have been applied yet). orbit-identity mounts this as a required volume —
	// it will fail to start if the ConfigMap is absent on first boot.
	if r.Registry != nil {
		if err := r.Registry.Sync(ctx); err != nil {
			return fmt.Errorf("orbit-tenants: %w", err)
		}
	}

	if err := r.reconcileOrbit(ctx, p); err != nil {
		return fmt.Errorf("orbit: %w", err)
	}

	return nil
}

func (r *PlatformReconciler) reconcileSPIRECRDs(ctx context.Context, p *OrbitPlatform, versions *chartVersions) error {
	values := buildSPIRECRDsValues(p.Spec.SPIRECRDsValues)

	helm := NewHelmClient("spire-system", r.RestConfig, r.Logger)
	_, err := helm.InstallOrUpgrade(ctx,
		"spire-crds",
		"spire-crds",
		"https://spiffe.github.io/helm-charts-hardened",
		versions.SPIRECRDs,
		values,
	)
	if err != nil {
		return fmt.Errorf("install/upgrade spire-crds: %w", err)
	}
	_ = r.setCondition(ctx, p, ConditionSPIREReady, metav1.ConditionFalse,
		"CRDsInstalled", "spire-crds chart deployed; waiting for SPIRE server")
	return nil
}

func (r *PlatformReconciler) reconcileSPIRE(ctx context.Context, p *OrbitPlatform, versions *chartVersions) error {
	values := buildSPIREValues(p)

	helm := NewHelmClient("spire-system", r.RestConfig, r.Logger)
	_, err := helm.InstallOrUpgrade(ctx,
		"spire",
		"spire",
		"https://spiffe.github.io/helm-charts-hardened",
		versions.SPIRE,
		values,
	)
	if err != nil {
		return fmt.Errorf("install/upgrade spire: %w", err)
	}
	_ = r.setCondition(ctx, p, ConditionSPIREReady, metav1.ConditionTrue,
		"Deployed", "spire chart deployed")
	return nil
}

func (r *PlatformReconciler) reconcileIstioBase(ctx context.Context, p *OrbitPlatform, versions *chartVersions) error {
	values := buildIstioBaseValues(p)

	helm := NewHelmClient("istio-system", r.RestConfig, r.Logger)
	_, err := helm.InstallOrUpgrade(ctx,
		"istio-base",
		"base",
		"https://istio-release.storage.googleapis.com/charts",
		versions.Istio,
		values,
	)
	if err != nil {
		return fmt.Errorf("install/upgrade istio-base: %w", err)
	}
	return nil
}

func (r *PlatformReconciler) reconcileIstiod(ctx context.Context, p *OrbitPlatform, versions *chartVersions) error {
	values := buildIstiodValues(p)

	helm := NewHelmClient("istio-system", r.RestConfig, r.Logger)
	_, err := helm.InstallOrUpgrade(ctx,
		"istiod",
		"istiod",
		"https://istio-release.storage.googleapis.com/charts",
		versions.Istio,
		values,
	)
	if err != nil {
		return fmt.Errorf("install/upgrade istiod: %w", err)
	}
	_ = r.setCondition(ctx, p, ConditionIstioReady, metav1.ConditionTrue,
		"Deployed", "istiod deployed")
	return nil
}

func (r *PlatformReconciler) reconcileGateway(ctx context.Context, p *OrbitPlatform, versions *chartVersions) error {
	values := buildGatewayValues(p)

	helm := NewHelmClient("istio-system", r.RestConfig, r.Logger)
	_, err := helm.InstallOrUpgrade(ctx,
		"gateway",
		"gateway",
		"https://istio-release.storage.googleapis.com/charts",
		versions.Istio,
		values,
	)
	if err != nil {
		return fmt.Errorf("install/upgrade gateway: %w", err)
	}
	return nil
}

func (r *PlatformReconciler) reconcileOrbitPlatform(ctx context.Context, p *OrbitPlatform) error {
	values := buildOrbitPlatformValues(
		r.loadValuesFile("platform-values.yaml"),
		p.Spec.TrustDomain,
		r.tenantNamespaces(ctx),
		p.Spec.PlatformValues,
	)

	helm := NewHelmClient("orbit-system", r.RestConfig, r.Logger)
	_, err := helm.InstallOrUpgrade(ctx,
		"orbit-platform",
		r.chartPath("orbit-platform"),
		"", // local chart path — no repoURL
		"",
		values,
	)
	if err != nil {
		return fmt.Errorf("install/upgrade orbit-platform: %w", err)
	}
	return nil
}

func (r *PlatformReconciler) reconcileOrbit(ctx context.Context, p *OrbitPlatform) error {
	secretName, err := r.platformSecretName(ctx, p)
	if err != nil {
		return err
	}

	values := buildOrbitValues(r.loadValuesFile("orbit-values.yaml"), p, secretName)

	helm := NewHelmClient("orbit-system", r.RestConfig, r.Logger)
	_, err = helm.InstallOrUpgrade(ctx,
		"orbit",
		r.chartPath("orbit"),
		"", // local chart path — no repoURL
		"",
		values,
	)
	if err != nil {
		return fmt.Errorf("install/upgrade orbit: %w", err)
	}
	_ = r.setCondition(ctx, p, ConditionIdentityReady, metav1.ConditionTrue,
		"Deployed", "orbit chart deployed")
	return nil
}

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

// setPhase updates the OrbitPlatform status phase, patching the API server.
// It is a no-op when the phase is already set to the requested value, which
// prevents spurious status patches from triggering extra reconcile events.
func (r *PlatformReconciler) setPhase(ctx context.Context, p *OrbitPlatform, phase string) error {
	if p.Status.Phase == phase {
		return nil
	}
	patch := client.MergeFrom(p.DeepCopy())
	p.Status.Phase = phase
	if err := r.Status().Patch(ctx, p, patch); err != nil {
		return fmt.Errorf("patch OrbitPlatform status phase to %q: %w", phase, err)
	}
	return nil
}

// setCondition sets a named condition on the OrbitPlatform status.
func (r *PlatformReconciler) setCondition(
	ctx context.Context,
	p *OrbitPlatform,
	condType string,
	status metav1.ConditionStatus,
	reason, message string,
) error {
	patch := client.MergeFrom(p.DeepCopy())
	meta.SetStatusCondition(&p.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		ObservedGeneration: p.Generation,
		Reason:             reason,
		Message:            message,
	})
	if err := r.Status().Patch(ctx, p, patch); err != nil {
		return fmt.Errorf("patch OrbitPlatform condition %q: %w", condType, err)
	}
	return nil
}

// chartPath returns the absolute path to a chart in the ChartDir.
func (r *PlatformReconciler) chartPath(name string) string {
	return fmt.Sprintf("%s/%s", r.ChartDir, name)
}

// platformSecretName resolves the platform secret name from the CR spec,
// defaulting to "orbit-secrets" when not explicitly set.
func (r *PlatformReconciler) platformSecretName(ctx context.Context, p *OrbitPlatform) (string, error) {
	name := p.Spec.Secrets.PlatformSecretRef.Name
	if name == "" {
		name = "orbit-secrets"
	}
	ns := p.Spec.Secrets.PlatformSecretRef.Namespace
	if ns == "" {
		ns = "orbit-system"
	}

	// Verify the Secret exists before referencing it; emit a clear error if not.
	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return "", fmt.Errorf("platform secret %q not found in namespace %q — create it before applying OrbitPlatform", name, ns)
		}
		return "", fmt.Errorf("get platform secret %q: %w", name, err)
	}
	return name, nil
}

// tenantNamespaces returns the list of tenant namespace names from all active
// OrbitTenant CRs — used to populate orbit-platform's namespaces.tenants value.
func (r *PlatformReconciler) tenantNamespaces(ctx context.Context) []string {
	list := &OrbitTenantList{}
	if err := r.List(ctx, list); err != nil {
		r.Logger.Warn(ctx, "failed to list OrbitTenants for namespace list",
			logging.Field{Key: "error", Value: err.Error()},
		)
		return nil
	}
	ns := make([]string, 0, len(list.Items))
	for i := range list.Items {
		t := &list.Items[i]
		if t.Spec.Active {
			ns = append(ns, tenantNamespace(t))
		}
	}
	return ns
}

// loadVersions reads chart version pins. In production these are baked into
// the operator image; in local dev they come from scripts/cluster/versions.env.
// For now the versions are constants matching the values in versions.env.
func (r *PlatformReconciler) loadVersions() *chartVersions {
	return defaultChartVersions()
}

// loadValuesFile reads a Helm values YAML file from the ChartDir's parent
// (deploy/local/ in local dev, the operator image's config dir in production).
// Returns an empty map when the file is absent.
func (r *PlatformReconciler) loadValuesFile(filename string) map[string]any {
	// In local dev mode ChartDir is e.g. "/path/to/repo/charts".
	// Values files are in the sibling "deploy/local/" directory.
	// In production the operator image embeds values at /etc/orbit/values/.
	// We probe both locations.
	candidates := []string{
		fmt.Sprintf("%s/../deploy/local/%s", r.ChartDir, filename),
		fmt.Sprintf("/etc/orbit/values/%s", filename),
	}
	for _, path := range candidates {
		data, err := readYAMLFile(path)
		if err == nil {
			return data
		}
	}
	r.Logger.Warn(context.Background(), "values file not found, using empty map",
		logging.Field{Key: "filename", Value: filename},
	)
	return map[string]any{}
}
