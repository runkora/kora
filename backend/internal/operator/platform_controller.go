package operator

import (
	"context"
	"fmt"
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
// See docs/adr/0025-orbit-kubernetes-operator.md for the design.
type PlatformReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	RestConfig *rest.Config
	Logger     logging.Logger

	// ChartDir is the base directory containing local Helm charts.
	// In local dev this is the repo's charts/ directory.
	// In production this is the directory where charts are embedded.
	ChartDir string
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

// reconcileReleases drives all Helm releases in dependency order.
// Each step is a named function so failures are easy to attribute.
func (r *PlatformReconciler) reconcileReleases(ctx context.Context, p *OrbitPlatform) error {
	if p.Spec.SPIRE.Enabled {
		if err := r.reconcileSPIRECRDs(ctx, p); err != nil {
			return fmt.Errorf("spire-crds: %w", err)
		}
		if err := r.reconcileSPIRE(ctx, p); err != nil {
			return fmt.Errorf("spire: %w", err)
		}
	}

	if p.Spec.Istio.Enabled {
		if err := r.reconcileIstioBase(ctx); err != nil {
			return fmt.Errorf("istio-base: %w", err)
		}
		if err := r.reconcileIstiod(ctx, p); err != nil {
			return fmt.Errorf("istiod: %w", err)
		}
		if err := r.reconcileGateway(ctx); err != nil {
			return fmt.Errorf("gateway: %w", err)
		}
	}

	if err := r.reconcileOrbitPlatform(ctx, p); err != nil {
		return fmt.Errorf("orbit-platform: %w", err)
	}

	if err := r.reconcileOrbit(ctx, p); err != nil {
		return fmt.Errorf("orbit: %w", err)
	}

	return nil
}

func (r *PlatformReconciler) reconcileSPIRECRDs(ctx context.Context, p *OrbitPlatform) error {
	versions := r.loadVersions()
	helm := NewHelmClient("spire-system", r.RestConfig, r.Logger)
	_, err := helm.InstallOrUpgrade(ctx,
		"spire-crds",
		"spire-crds",
		"https://spiffe.github.io/helm-charts-hardened",
		versions.SPIRECRDs,
		map[string]any{},
	)
	if err != nil {
		return fmt.Errorf("install/upgrade spire-crds: %w", err)
	}
	_ = r.setCondition(ctx, p, ConditionSPIREReady, metav1.ConditionFalse,
		"CRDsInstalled", "spire-crds chart deployed; waiting for SPIRE server")
	return nil
}

func (r *PlatformReconciler) reconcileSPIRE(ctx context.Context, p *OrbitPlatform) error {
	versions := r.loadVersions()
	spireValues := r.loadValuesFile("spire-values.yaml")
	helm := NewHelmClient("spire-system", r.RestConfig, r.Logger)
	_, err := helm.InstallOrUpgrade(ctx,
		"spire",
		"spire",
		"https://spiffe.github.io/helm-charts-hardened",
		versions.SPIRE,
		spireValues,
	)
	if err != nil {
		return fmt.Errorf("install/upgrade spire: %w", err)
	}
	_ = r.setCondition(ctx, p, ConditionSPIREReady, metav1.ConditionTrue,
		"Deployed", "spire chart deployed")
	return nil
}

func (r *PlatformReconciler) reconcileIstioBase(ctx context.Context) error {
	versions := r.loadVersions()
	helm := NewHelmClient("istio-system", r.RestConfig, r.Logger)
	_, err := helm.InstallOrUpgrade(ctx,
		"istio-base",
		"base",
		"https://istio-release.storage.googleapis.com/charts",
		versions.Istio,
		map[string]any{},
	)
	if err != nil {
		return fmt.Errorf("install/upgrade istio-base: %w", err)
	}
	return nil
}

func (r *PlatformReconciler) reconcileIstiod(ctx context.Context, p *OrbitPlatform) error {
	versions := r.loadVersions()
	istioValues := r.loadValuesFile("istio-values.yaml")
	// Inject the trust domain into istiod values.
	istioValues["meshConfig"] = mergeMaps(
		toMap(istioValues["meshConfig"]),
		map[string]any{
			"trustDomain": p.Spec.TrustDomain,
		},
	)
	helm := NewHelmClient("istio-system", r.RestConfig, r.Logger)
	_, err := helm.InstallOrUpgrade(ctx,
		"istiod",
		"istiod",
		"https://istio-release.storage.googleapis.com/charts",
		versions.Istio,
		istioValues,
	)
	if err != nil {
		return fmt.Errorf("install/upgrade istiod: %w", err)
	}
	_ = r.setCondition(ctx, p, ConditionIstioReady, metav1.ConditionTrue,
		"Deployed", "istiod deployed")
	return nil
}

func (r *PlatformReconciler) reconcileGateway(ctx context.Context) error {
	versions := r.loadVersions()
	// Load optional gateway values file (e.g. deploy/local/gateway-values.yaml).
	// This allows overriding service.type=NodePort on k3d where no cloud
	// LoadBalancer controller is present — without it Helm's --wait blocks
	// indefinitely waiting for the EXTERNAL-IP to be assigned.
	gatewayValues := r.loadValuesFile("gateway-values.yaml")
	helm := NewHelmClient("istio-system", r.RestConfig, r.Logger)
	_, err := helm.InstallOrUpgrade(ctx,
		"gateway",
		"gateway",
		"https://istio-release.storage.googleapis.com/charts",
		versions.Istio,
		gatewayValues,
	)
	if err != nil {
		return fmt.Errorf("install/upgrade gateway: %w", err)
	}
	return nil
}

func (r *PlatformReconciler) reconcileOrbitPlatform(ctx context.Context, p *OrbitPlatform) error {
	// Start with values from the local values file (e.g. deploy/local/platform-values.yaml
	// in dev, /etc/orbit/values/platform-values.yaml in-cluster).
	platformValues := r.loadValuesFile("platform-values.yaml")

	// Overlay computed values — these always win over file-based values.
	platformValues = mergeMaps(platformValues, map[string]any{
		"global": map[string]any{
			"trustDomain": p.Spec.TrustDomain,
		},
		"namespaces": map[string]any{
			"system":  "orbit-system",
			"tenants": r.tenantNamespaces(ctx),
		},
	})
	// Merge any raw values from the CR spec (highest priority).
	platformValues = mergeMaps(platformValues, p.Spec.PlatformValues)

	helm := NewHelmClient("orbit-system", r.RestConfig, r.Logger)
	_, err := helm.InstallOrUpgrade(ctx,
		"orbit-platform",
		r.chartPath("orbit-platform"),
		"", // local chart path — no repoURL
		"",
		platformValues,
	)
	if err != nil {
		return fmt.Errorf("install/upgrade orbit-platform: %w", err)
	}
	return nil
}

func (r *PlatformReconciler) reconcileOrbit(ctx context.Context, p *OrbitPlatform) error {
	secretName, err := r.platformSecretName(p)
	if err != nil {
		return err
	}

	// Start with values from the local values file (e.g. deploy/local/orbit-values.yaml
	// in dev, /etc/orbit/values/orbit-values.yaml in-cluster). This carries image
	// repository/tag, appEnv, imagePullPolicy, and other deployment-specific settings.
	orbitValues := r.loadValuesFile("orbit-values.yaml")

	// Overlay computed values — these always win over file-based values.
	orbitValues = mergeMaps(orbitValues, map[string]any{
		"global": map[string]any{
			"trustDomain": p.Spec.TrustDomain,
		},
		"identity": map[string]any{
			"existingSecret":           secretName,
			"existingConfigMap":        "orbit-config",
			"existingTenantsConfigMap": "orbit-tenants",
		},
		"platform": map[string]any{
			"namespaces": map[string]any{
				"system": "orbit-system",
			},
		},
	})

	// Apply db/redis config if set.
	if p.Spec.Config.DB.Host != "" {
		orbitValues = mergeMaps(orbitValues, map[string]any{
			"config": map[string]any{
				"db": map[string]any{
					"host": p.Spec.Config.DB.Host,
					"name": p.Spec.Config.DB.Name,
					"port": p.Spec.Config.DB.Port,
					"user": p.Spec.Config.DB.User,
				},
			},
		})
	}
	if p.Spec.Config.Redis.Host != "" {
		orbitValues = mergeMaps(orbitValues, map[string]any{
			"config": map[string]any{
				"redis": map[string]any{
					"host": p.Spec.Config.Redis.Host,
					"port": p.Spec.Config.Redis.Port,
				},
			},
		})
	}

	// Merge any raw values from the CR spec.
	orbitValues = mergeMaps(orbitValues, p.Spec.OrbitValues)

	helm := NewHelmClient("orbit-system", r.RestConfig, r.Logger)
	_, err = helm.InstallOrUpgrade(ctx,
		"orbit",
		r.chartPath("orbit"),
		"", // local chart path — no repoURL
		"",
		orbitValues,
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
func (r *PlatformReconciler) platformSecretName(p *OrbitPlatform) (string, error) {
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
	if err := r.Get(context.Background(), types.NamespacedName{Name: name, Namespace: ns}, secret); err != nil {
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
	for _, t := range list.Items {
		if t.Spec.Active && t.Spec.Namespace != "" {
			ns = append(ns, t.Spec.Namespace)
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
