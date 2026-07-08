package operator

import (
	"context"
	"fmt"
	"maps"
	"regexp"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/clear-route/orbit/internal/platform/logging"
)

const (
	// devEnvTTLHardCap is the maximum lifetime of an environment from
	// creation, regardless of how many times spec.ttl is extended (decision 8).
	devEnvTTLHardCap = 7 * 24 * time.Hour
	// defaultDevEnvTTL applies when spec.ttl is unset (defensive fallback —
	// the CRD schema's own default should normally supply this).
	defaultDevEnvTTL = 24 * time.Hour
	// defaultIdleTimeout applies when spec.idleTimeout is unset.
	defaultIdleTimeout = 2 * time.Hour

	// vclusterReleaseName is the fixed Helm release name for the vcluster
	// installed into each environment's namespace — one per namespace, so a
	// fixed name (rather than one templated per environment) is sufficient.
	vclusterReleaseName = "vcluster"
	vclusterChartName   = "vcluster"
	vclusterChartRepo   = "https://charts.loft.sh"

	// preSleepReplicasAnnotation records a workload's replica count before
	// the reconciler scales it to zero for sleep, so wake restores the exact
	// prior count instead of guessing a fixed value.
	preSleepReplicasAnnotation = "orbit.io/pre-sleep-replicas"

	// Namespace-level ResourceQuota — the capacity backstop decision 1
	// requires on single-pool clusters. A full Orbit install nested inside a
	// vcluster nested in the host is heavy; these are deliberately generous
	// rather than tuned, since the intent is "stop one environment from
	// starving the node," not "predict the stack's true footprint."
	devEnvQuotaCPURequests    = "6"
	devEnvQuotaMemoryRequests = "12Gi"
	devEnvQuotaCPULimits      = "12"
	devEnvQuotaMemoryLimits   = "24Gi"
	devEnvQuotaMaxPods        = "60"

	// LimitRange defaults applied to containers that don't specify their own
	// requests/limits, so a single unbounded pod can't consume the entire
	// namespace quota above. The memory default limit in particular must
	// exceed the largest default REQUEST any third-party chart we install
	// sets without its own limit — istiod's own chart requests 2Gi by
	// default with no limit, which a smaller default here would reject
	// outright (Kubernetes requires limit >= request on every container).
	devEnvDefaultContainerCPURequest    = "100m"
	devEnvDefaultContainerMemoryRequest = "128Mi"
	devEnvDefaultContainerCPULimit      = "2"
	devEnvDefaultContainerMemoryLimit   = "3Gi"
)

// DevEnvironmentReconciler reconciles OrbitDevEnvironment resources. For each
// CR it:
//  1. Creates the host namespace (orbit-devenv-{name}) with quota, limits,
//     and owner/role labels.
//  2. Installs/upgrades a vcluster Helm release into that namespace.
//  3. Derives Ready/Sleeping from spec.awake and idle time, scaling
//     workloads to zero for sleep and restoring them on wake.
//  4. Deletes the environment once its TTL (capped at devEnvTTLHardCap) elapses.
//  5. Signs any pending spec.kubeconfigCSR against the vcluster's own
//     client CA (decision 6).
//
// See docs/adr/2026-07-07-ephemeral-development-environments-via-vcluster.md.
//
// NOTE — scope of this Phase 2 slice: it stops at a Ready vcluster. It does
// NOT yet install the in-vcluster Orbit platform (orbit-operator chart +
// OrbitPlatform CR, decision 2) — the ADR itself flags that step as
// self-referential ("this feature cannot be developed inside itself until it
// exists") and it is the next increment, not a shortcut taken here.
type DevEnvironmentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Logger logging.Logger

	// HelmFactory constructs the HelmInterface used to manage each
	// environment's vcluster release. Production wiring supplies
	// NewHelmClientFactory; tests inject a factory returning a fake.
	HelmFactory HelmClientFactory

	// Clock abstracts time for TTL/idle/sleep decisions so they are
	// unit-testable without waiting on a real clock.
	Clock Clock

	// IngressDomain is the base domain used to compute status.ingressHost
	// ("{name}.{IngressDomain}"). Real ingress/DNS/TLS wiring is a staging
	// concern (ADR "Out of local scope") — locally this field is informational.
	IngressDomain string

	// VClusterVersion pins the vcluster chart version. Must stay in sync with
	// scripts/cluster/versions.env (VCLUSTER_CHART_VERSION).
	VClusterVersion string

	// ChartDir is the base directory containing local Helm charts — the same
	// convention PlatformReconciler.ChartDir uses. Needed here because
	// reconcileInVClusterPlatform installs the orbit-operator chart a second
	// time, inside each environment's vcluster (decision 2).
	ChartDir string

	// ImageRegistry is the registry prefix for the in-vcluster orbit-operator
	// image and the OrbitPlatform CR applied inside each environment. Empty
	// means "use built-in versioned image references" (production).
	ImageRegistry string

	// VClusterRestConfigFactory resolves a vcluster's rest.Config for a given
	// environment namespace. Production wiring supplies
	// NewVClusterRestConfigFactory; tests inject a factory returning a dummy
	// config so no real vcluster kubeconfig Secret is required.
	VClusterRestConfigFactory VClusterRestConfigFactory

	// VClusterClientFactory builds a controller-runtime client targeting a
	// vcluster's own API server. Production wiring supplies
	// NewVClusterClientFactory; tests inject a factory returning a fake.
	VClusterClientFactory VClusterClientFactory

	// InVClusterHelmFactory constructs a HelmInterface targeting an arbitrary
	// rest.Config (unlike HelmFactory above, which always targets the HOST
	// cluster) — used to install the orbit-operator chart inside a vcluster.
	// Production wiring wraps NewHelmClient directly; tests inject a factory
	// returning a fake.
	InVClusterHelmFactory func(namespace string, restConfig *rest.Config) HelmInterface
}

// Reconcile is called by controller-runtime whenever an OrbitDevEnvironment
// is created, updated, or when a requeue fires.
func (r *DevEnvironmentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Logger

	env := &OrbitDevEnvironment{}
	if err := r.Get(ctx, req.NamespacedName, env); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get OrbitDevEnvironment %q: %w", req.Name, err)
	}

	if !env.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, env)
	}

	if !controllerutil.ContainsFinalizer(env, devEnvironmentFinalizer) {
		patch := client.MergeFrom(env.DeepCopy())
		controllerutil.AddFinalizer(env, devEnvironmentFinalizer)
		if err := r.Patch(ctx, env, patch); err != nil {
			return ctrl.Result{}, fmt.Errorf("add finalizer to OrbitDevEnvironment %q: %w", env.Name, err)
		}
	}

	log.Info(ctx, "reconciling OrbitDevEnvironment",
		logging.Field{Key: "name", Value: env.Name},
		logging.Field{Key: "owner", Value: env.Spec.Owner},
		logging.Field{Key: "phase", Value: env.Status.Phase},
	)

	now := r.Clock.Now()
	expiresAt := computeExpiry(env)
	if !now.Before(expiresAt) {
		return r.expire(ctx, env, expiresAt)
	}

	return r.reconcileReady(ctx, env, now, expiresAt)
}

// expire deletes an OrbitDevEnvironment whose TTL has elapsed. Deletion
// itself triggers the same finalizer-driven cleanup as a direct kubectl/API
// delete (see handleDeletion) via the watch event it produces.
func (r *DevEnvironmentReconciler) expire(ctx context.Context, env *OrbitDevEnvironment, expiresAt time.Time) (ctrl.Result, error) {
	r.Logger.Info(ctx, "OrbitDevEnvironment TTL expired, deleting",
		logging.Field{Key: "name", Value: env.Name},
		logging.Field{Key: "expiresAt", Value: expiresAt.String()},
	)
	if err := r.Delete(ctx, env); err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, fmt.Errorf("delete expired OrbitDevEnvironment %q: %w", env.Name, err)
	}
	return ctrl.Result{}, nil
}

// reconcileReady drives an unexpired environment to Ready/Sleeping: namespace
// + quota/limits, the vcluster Helm release, power state, and any pending
// kubeconfig CSR — then records status and picks the next requeue.
func (r *DevEnvironmentReconciler) reconcileReady(ctx context.Context, env *OrbitDevEnvironment, now, expiresAt time.Time) (ctrl.Result, error) {
	log := r.Logger

	// Never regress Ready/Sleeping back to Provisioning at the start of every
	// reconcile — see PlatformReconciler.Reconcile for why (spurious status
	// patches would trigger a new reconcile event and loop forever).
	switch env.Status.Phase {
	case "", DevEnvPhasePending, DevEnvPhaseFailed:
		if err := r.setDevEnvPhase(ctx, env, DevEnvPhaseProvisioning); err != nil {
			return ctrl.Result{}, err
		}
	}

	if err := r.ensureNamespace(ctx, env); err != nil {
		return r.failReconcile(ctx, env, "NamespaceError", err)
	}
	_ = r.setDevEnvCondition(ctx, env, ConditionDevEnvNamespaceReady, metav1.ConditionTrue,
		"Ready", "namespace exists with quota, limits, and labels")

	if err := r.ensureVCluster(ctx, env); err != nil {
		if isHelmBusy(err) {
			log.Info(ctx, "helm release busy, requeueing",
				logging.Field{Key: "name", Value: env.Name},
				logging.Field{Key: "error", Value: err.Error()},
			)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		return r.failReconcile(ctx, env, "HelmError", err)
	}
	_ = r.setDevEnvCondition(ctx, env, ConditionDevEnvVClusterReady, metav1.ConditionTrue,
		"Deployed", "vcluster helm release deployed")

	phase, err := r.reconcilePowerState(ctx, env, now)
	if err != nil {
		return r.failReconcile(ctx, env, "PowerStateError", err)
	}

	// Only attempt the in-vcluster install while the vcluster's own control
	// plane is actually up — while Sleeping its API server is unreachable
	// (decision 8 scales it to zero), so there is nothing to apply to yet.
	if phase == DevEnvPhaseReady {
		if err := r.reconcileInVClusterPlatform(ctx, env); err != nil {
			// A transient "no matches for kind" immediately after the
			// orbit-operator chart's CRDs are first installed (API server
			// discovery cache not yet refreshed) is expected to self-heal on
			// the next reconcile — same self-healing posture as the rest of
			// this reconciler, just surfaced as a Failed blip rather than a
			// dedicated non-fatal path.
			return r.failReconcile(ctx, env, "InVClusterPlatformError", err)
		}
		_ = r.setDevEnvCondition(ctx, env, ConditionDevEnvPlatformApplied, metav1.ConditionTrue,
			"Applied", "orbit-operator installed and OrbitPlatform/OrbitTenant applied inside the vcluster")
	}

	if err := r.reconcileKubeconfigCSR(ctx, env); err != nil {
		// Non-fatal to the environment's own readiness — surfaced via the
		// KubeconfigCSRFulfilled condition instead of failing the reconcile.
		log.Error(ctx, "kubeconfig CSR fulfillment failed",
			logging.Field{Key: "name", Value: env.Name},
			logging.Field{Key: "error", Value: err.Error()},
		)
	}

	if err := r.patchStatusFields(ctx, env, func(s *OrbitDevEnvironmentStatus) {
		reconciled := metav1.NewTime(now)
		expiry := metav1.NewTime(expiresAt)
		s.Namespace = devEnvNamespace(env)
		s.IngressHost = fmt.Sprintf("%s.%s", env.Name, r.IngressDomain)
		s.ExpiresAt = &expiry
		s.LastReconciledAt = &reconciled
	}); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.setDevEnvPhase(ctx, env, phase); err != nil {
		return ctrl.Result{}, err
	}

	log.Info(ctx, "OrbitDevEnvironment reconciliation complete",
		logging.Field{Key: "name", Value: env.Name},
		logging.Field{Key: "phase", Value: phase},
	)

	return ctrl.Result{RequeueAfter: r.nextRequeueAfter(env, now, expiresAt)}, nil
}

// SetupWithManager registers the controller with the controller-runtime Manager.
func (r *DevEnvironmentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&OrbitDevEnvironment{}).
		Complete(r)
}

// -----------------------------------------------------------------------------
// deletion
// -----------------------------------------------------------------------------

// handleDeletion runs when the CR has a DeletionTimestamp — whether that was
// set by a direct kubectl/API delete or by Reconcile's own TTL-expiry delete
// above. Both paths converge here so there is exactly one cleanup sequence.
func (r *DevEnvironmentReconciler) handleDeletion(ctx context.Context, env *OrbitDevEnvironment) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(env, devEnvironmentFinalizer) {
		return ctrl.Result{}, nil
	}

	ns := devEnvNamespace(env)

	if err := r.HelmFactory(ns).Uninstall(ctx, vclusterReleaseName); err != nil {
		return ctrl.Result{}, fmt.Errorf("uninstall vcluster release for %q: %w", env.Name, err)
	}

	namespace := &corev1.Namespace{}
	err := r.Get(ctx, types.NamespacedName{Name: ns}, namespace)
	switch {
	case apierrors.IsNotFound(err):
		// already gone
	case err != nil:
		return ctrl.Result{}, fmt.Errorf("get namespace %q: %w", ns, err)
	default:
		if delErr := r.Delete(ctx, namespace); delErr != nil && !apierrors.IsNotFound(delErr) {
			return ctrl.Result{}, fmt.Errorf("delete namespace %q: %w", ns, delErr)
		}
	}

	patch := client.MergeFrom(env.DeepCopy())
	controllerutil.RemoveFinalizer(env, devEnvironmentFinalizer)
	if err := r.Patch(ctx, env, patch); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer from OrbitDevEnvironment %q: %w", env.Name, err)
	}

	r.Logger.Info(ctx, "OrbitDevEnvironment deleted, cleanup complete",
		logging.Field{Key: "name", Value: env.Name},
		logging.Field{Key: "namespace", Value: ns},
	)
	return ctrl.Result{}, nil
}

// -----------------------------------------------------------------------------
// namespace, quota, limits
// -----------------------------------------------------------------------------

func (r *DevEnvironmentReconciler) ensureNamespace(ctx context.Context, env *OrbitDevEnvironment) error {
	ns := devEnvNamespace(env)
	labels := map[string]string{
		"orbit.io/role":   "devenv",
		"orbit.io/owner":  sanitizeLabelValue(env.Spec.Owner),
		"orbit.io/devenv": env.Name,
	}

	namespace := &corev1.Namespace{}
	err := r.Get(ctx, types.NamespacedName{Name: ns}, namespace)
	switch {
	case apierrors.IsNotFound(err):
		namespace = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: ns, Labels: labels},
		}
		if createErr := r.Create(ctx, namespace); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
			return fmt.Errorf("create namespace %q: %w", ns, createErr)
		}
	case err != nil:
		return fmt.Errorf("get namespace %q: %w", ns, err)
	default:
		missing := false
		for k, v := range labels {
			if namespace.Labels[k] != v {
				missing = true
				break
			}
		}
		if missing {
			patch := client.MergeFrom(namespace.DeepCopy())
			if namespace.Labels == nil {
				namespace.Labels = map[string]string{}
			}
			maps.Copy(namespace.Labels, labels)
			if patchErr := r.Patch(ctx, namespace, patch); patchErr != nil {
				return fmt.Errorf("patch namespace %q labels: %w", ns, patchErr)
			}
		}
	}

	if err := r.ensureResourceQuota(ctx, ns); err != nil {
		return err
	}
	return r.ensureLimitRange(ctx, ns)
}

func (r *DevEnvironmentReconciler) ensureResourceQuota(ctx context.Context, ns string) error {
	const name = "orbit-devenv-quota"
	desired := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    map[string]string{"app.kubernetes.io/managed-by": "orbit-operator"},
		},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				corev1.ResourceRequestsCPU:    resource.MustParse(devEnvQuotaCPURequests),
				corev1.ResourceRequestsMemory: resource.MustParse(devEnvQuotaMemoryRequests),
				corev1.ResourceLimitsCPU:      resource.MustParse(devEnvQuotaCPULimits),
				corev1.ResourceLimitsMemory:   resource.MustParse(devEnvQuotaMemoryLimits),
				corev1.ResourcePods:           resource.MustParse(devEnvQuotaMaxPods),
			},
		},
	}

	existing := &corev1.ResourceQuota{}
	err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, existing)
	if apierrors.IsNotFound(err) {
		if createErr := r.Create(ctx, desired); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
			return fmt.Errorf("create ResourceQuota in %q: %w", ns, createErr)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("get ResourceQuota in %q: %w", ns, err)
	}
	patch := client.MergeFrom(existing.DeepCopy())
	existing.Spec = desired.Spec
	if patchErr := r.Patch(ctx, existing, patch); patchErr != nil {
		return fmt.Errorf("patch ResourceQuota in %q: %w", ns, patchErr)
	}
	return nil
}

func (r *DevEnvironmentReconciler) ensureLimitRange(ctx context.Context, ns string) error {
	const name = "orbit-devenv-limits"
	desired := &corev1.LimitRange{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    map[string]string{"app.kubernetes.io/managed-by": "orbit-operator"},
		},
		Spec: corev1.LimitRangeSpec{
			Limits: []corev1.LimitRangeItem{
				{
					Type: corev1.LimitTypeContainer,
					DefaultRequest: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse(devEnvDefaultContainerCPURequest),
						corev1.ResourceMemory: resource.MustParse(devEnvDefaultContainerMemoryRequest),
					},
					Default: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse(devEnvDefaultContainerCPULimit),
						corev1.ResourceMemory: resource.MustParse(devEnvDefaultContainerMemoryLimit),
					},
				},
			},
		},
	}

	existing := &corev1.LimitRange{}
	err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, existing)
	if apierrors.IsNotFound(err) {
		if createErr := r.Create(ctx, desired); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
			return fmt.Errorf("create LimitRange in %q: %w", ns, createErr)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("get LimitRange in %q: %w", ns, err)
	}
	patch := client.MergeFrom(existing.DeepCopy())
	existing.Spec = desired.Spec
	if patchErr := r.Patch(ctx, existing, patch); patchErr != nil {
		return fmt.Errorf("patch LimitRange in %q: %w", ns, patchErr)
	}
	return nil
}

// -----------------------------------------------------------------------------
// vcluster lifecycle
// -----------------------------------------------------------------------------

func (r *DevEnvironmentReconciler) ensureVCluster(ctx context.Context, env *OrbitDevEnvironment) error {
	ns := devEnvNamespace(env)
	version := r.VClusterVersion
	if version == "" {
		version = defaultChartVersions().VCluster
	}
	values := map[string]any{
		"controlPlane": map[string]any{
			"distro": map[string]any{
				"k8s": map[string]any{"enabled": true},
			},
		},
		"sync": map[string]any{
			"toHost": map[string]any{
				"serviceAccounts": map[string]any{"enabled": true},
				"networkPolicies": map[string]any{"enabled": true},
			},
			"fromHost": map[string]any{
				"csiDrivers": map[string]any{"enabled": true},
			},
		},
	}

	_, err := r.HelmFactory(ns).InstallOrUpgrade(ctx,
		vclusterReleaseName, vclusterChartName, vclusterChartRepo, version, values)
	if err != nil {
		return fmt.Errorf("install/upgrade vcluster release: %w", err)
	}
	return nil
}

// -----------------------------------------------------------------------------
// power state — sleep / wake
// -----------------------------------------------------------------------------

// reconcilePowerState derives Ready vs. Sleeping from spec.Awake and idle
// time, scales workloads accordingly, and returns the resulting phase.
//
// spec.Awake is the single source of truth for desired power state, so that
// wake (which decision 8 requires to be an explicit action, never automatic
// on connection) can be detected unambiguously: automatic idle-sleep flips
// spec.Awake to false itself rather than only recording Sleeping in status,
// so a later reconcile can tell "still idle, stay asleep" (Awake already
// false) apart from "an external actor re-enabled it" (Awake now true) —
// without that self-clearing flip, both cases would look identical (Awake
// still true, status still Sleeping) and idle-sleep would immediately
// wake itself back up on the very next reconcile.
func (r *DevEnvironmentReconciler) reconcilePowerState(ctx context.Context, env *OrbitDevEnvironment, now time.Time) (string, error) {
	ns := devEnvNamespace(env)
	wasAsleep := env.Status.Phase == DevEnvPhaseSleeping

	if !env.Spec.Awake {
		if err := r.scaleWorkloads(ctx, ns, true); err != nil {
			return "", fmt.Errorf("scale workloads to zero: %w", err)
		}
		return DevEnvPhaseSleeping, nil
	}

	if wasAsleep {
		// spec.Awake is true and we were asleep: an external actor (UI/API)
		// explicitly re-enabled it — decision 8's only path out of Sleeping.
		// Reset the idle clock so it doesn't immediately re-sleep.
		if err := r.patchStatusFields(ctx, env, func(s *OrbitDevEnvironmentStatus) {
			t := metav1.NewTime(now)
			s.LastActivityAt = &t
		}); err != nil {
			return "", err
		}
		if err := r.scaleWorkloads(ctx, ns, false); err != nil {
			return "", fmt.Errorf("scale workloads up: %w", err)
		}
		return DevEnvPhaseReady, nil
	}

	lastActivity := env.CreationTimestamp.Time
	if env.Status.LastActivityAt != nil {
		lastActivity = env.Status.LastActivityAt.Time
	}
	idle := env.Spec.IdleTimeout.Duration
	if idle <= 0 {
		idle = defaultIdleTimeout
	}
	if now.Sub(lastActivity) >= idle {
		if err := r.setSpecAwake(ctx, env, false); err != nil {
			return "", err
		}
		if err := r.scaleWorkloads(ctx, ns, true); err != nil {
			return "", fmt.Errorf("scale workloads to zero: %w", err)
		}
		return DevEnvPhaseSleeping, nil
	}

	if err := r.scaleWorkloads(ctx, ns, false); err != nil {
		return "", fmt.Errorf("scale workloads up: %w", err)
	}
	return DevEnvPhaseReady, nil
}

// setSpecAwake patches spec.Awake directly (not the status subresource).
func (r *DevEnvironmentReconciler) setSpecAwake(ctx context.Context, env *OrbitDevEnvironment, awake bool) error {
	if env.Spec.Awake == awake {
		return nil
	}
	patch := client.MergeFrom(env.DeepCopy())
	env.Spec.Awake = awake
	if err := r.Patch(ctx, env, patch); err != nil {
		return fmt.Errorf("patch OrbitDevEnvironment %q spec.awake to %v: %w", env.Name, awake, err)
	}
	return nil
}

// scaleWorkloads scales every Deployment and StatefulSet in the namespace to
// zero (sleep) or restores their pre-sleep replica count (wake). PVCs are
// never touched — sleep is a compute-only scale-down, per decision 8.
func (r *DevEnvironmentReconciler) scaleWorkloads(ctx context.Context, namespace string, toZero bool) error {
	deployments := &appsv1.DeploymentList{}
	if err := r.List(ctx, deployments, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("list deployments in %q: %w", namespace, err)
	}
	for i := range deployments.Items {
		if err := r.scaleDeployment(ctx, &deployments.Items[i], toZero); err != nil {
			return err
		}
	}

	statefulSets := &appsv1.StatefulSetList{}
	if err := r.List(ctx, statefulSets, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("list statefulsets in %q: %w", namespace, err)
	}
	for i := range statefulSets.Items {
		if err := r.scaleStatefulSet(ctx, &statefulSets.Items[i], toZero); err != nil {
			return err
		}
	}
	return nil
}

func (r *DevEnvironmentReconciler) scaleDeployment(ctx context.Context, d *appsv1.Deployment, toZero bool) error {
	if toZero {
		current := int32(1)
		if d.Spec.Replicas != nil {
			current = *d.Spec.Replicas
		}
		if current == 0 {
			return nil
		}
		patch := client.MergeFrom(d.DeepCopy())
		setPreSleepReplicas(d, current)
		zero := int32(0)
		d.Spec.Replicas = &zero
		return r.Patch(ctx, d, patch)
	}

	restore, ok := preSleepReplicas(d)
	if !ok {
		return nil
	}
	patch := client.MergeFrom(d.DeepCopy())
	delete(d.Annotations, preSleepReplicasAnnotation)
	d.Spec.Replicas = &restore
	return r.Patch(ctx, d, patch)
}

func (r *DevEnvironmentReconciler) scaleStatefulSet(ctx context.Context, s *appsv1.StatefulSet, toZero bool) error {
	if toZero {
		current := int32(1)
		if s.Spec.Replicas != nil {
			current = *s.Spec.Replicas
		}
		if current == 0 {
			return nil
		}
		patch := client.MergeFrom(s.DeepCopy())
		setPreSleepReplicas(s, current)
		zero := int32(0)
		s.Spec.Replicas = &zero
		return r.Patch(ctx, s, patch)
	}

	restore, ok := preSleepReplicas(s)
	if !ok {
		return nil
	}
	patch := client.MergeFrom(s.DeepCopy())
	delete(s.Annotations, preSleepReplicasAnnotation)
	s.Spec.Replicas = &restore
	return r.Patch(ctx, s, patch)
}

func setPreSleepReplicas(obj metav1.Object, replicas int32) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[preSleepReplicasAnnotation] = strconv.Itoa(int(replicas))
	obj.SetAnnotations(annotations)
}

func preSleepReplicas(obj metav1.Object) (int32, bool) {
	raw, ok := obj.GetAnnotations()[preSleepReplicasAnnotation]
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return 0, false
	}
	return int32(n), true
}

// -----------------------------------------------------------------------------
// TTL
// -----------------------------------------------------------------------------

// computeExpiry returns creationTimestamp + spec.ttl, capped at
// creationTimestamp + devEnvTTLHardCap regardless of how large spec.ttl is
// set (decision 8 — TTL is extendable but never unbounded).
func computeExpiry(env *OrbitDevEnvironment) time.Time {
	ttl := env.Spec.TTL.Duration
	if ttl <= 0 {
		ttl = defaultDevEnvTTL
	}
	if ttl > devEnvTTLHardCap {
		ttl = devEnvTTLHardCap
	}
	return env.CreationTimestamp.Add(ttl)
}

// nextRequeueAfter picks the soonest of: TTL expiry, the idle-sleep deadline
// (only relevant while Awake and Ready), and a 5-minute drift-detection
// ceiling — floored at 10s so a nearly-expired or nearly-idle environment
// can't produce a tight reconcile loop.
func (r *DevEnvironmentReconciler) nextRequeueAfter(env *OrbitDevEnvironment, now, expiresAt time.Time) time.Duration {
	requeue := 5 * time.Minute
	if until := expiresAt.Sub(now); until > 0 && until < requeue {
		requeue = until
	}

	if env.Spec.Awake && env.Status.Phase == DevEnvPhaseReady {
		idle := env.Spec.IdleTimeout.Duration
		if idle <= 0 {
			idle = defaultIdleTimeout
		}
		lastActivity := env.CreationTimestamp.Time
		if env.Status.LastActivityAt != nil {
			lastActivity = env.Status.LastActivityAt.Time
		}
		if untilIdle := lastActivity.Add(idle).Sub(now); untilIdle > 0 && untilIdle < requeue {
			requeue = untilIdle
		}
	}

	if requeue < 10*time.Second {
		requeue = 10 * time.Second
	}
	return requeue
}

// -----------------------------------------------------------------------------
// status helpers
// -----------------------------------------------------------------------------

// patchStatusFields snapshots env BEFORE mutate runs, so the resulting merge
// patch's diff is exactly the fields mutate changed — snapshotting after
// mutation (or reusing a snapshot taken before an earlier direct field
// assignment elsewhere) would silently drop those fields from the patch.
func (r *DevEnvironmentReconciler) patchStatusFields(ctx context.Context, env *OrbitDevEnvironment, mutate func(*OrbitDevEnvironmentStatus)) error {
	patch := client.MergeFrom(env.DeepCopy())
	mutate(&env.Status)
	if err := r.Status().Patch(ctx, env, patch); err != nil {
		return fmt.Errorf("patch OrbitDevEnvironment %q status: %w", env.Name, err)
	}
	return nil
}

func (r *DevEnvironmentReconciler) setDevEnvPhase(ctx context.Context, env *OrbitDevEnvironment, phase string) error {
	if env.Status.Phase == phase {
		return nil
	}
	patch := client.MergeFrom(env.DeepCopy())
	env.Status.Phase = phase
	if err := r.Status().Patch(ctx, env, patch); err != nil {
		return fmt.Errorf("patch OrbitDevEnvironment %q status phase to %q: %w", env.Name, phase, err)
	}
	return nil
}

func (r *DevEnvironmentReconciler) setDevEnvCondition(
	ctx context.Context,
	env *OrbitDevEnvironment,
	condType string,
	status metav1.ConditionStatus,
	reason, message string,
) error {
	patch := client.MergeFrom(env.DeepCopy())
	meta.SetStatusCondition(&env.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		ObservedGeneration: env.Generation,
		Reason:             reason,
		Message:            message,
	})
	if err := r.Status().Patch(ctx, env, patch); err != nil {
		return fmt.Errorf("patch OrbitDevEnvironment %q condition %q: %w", env.Name, condType, err)
	}
	return nil
}

func (r *DevEnvironmentReconciler) failReconcile(ctx context.Context, env *OrbitDevEnvironment, reason string, cause error) (ctrl.Result, error) {
	r.Logger.Error(ctx, "OrbitDevEnvironment reconciliation failed",
		logging.Field{Key: "name", Value: env.Name},
		logging.Field{Key: "reason", Value: reason},
		logging.Field{Key: "error", Value: cause.Error()},
	)
	_ = r.setDevEnvPhase(ctx, env, DevEnvPhaseFailed)
	// Returning a nil error deliberately: an error return would ALSO trigger
	// controller-runtime's default exponential backoff on top of this
	// explicit RequeueAfter, double-applying backoff (see PlatformReconciler
	// for the same pattern).
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

// -----------------------------------------------------------------------------
// misc
// -----------------------------------------------------------------------------

// labelUnsafeChars matches anything not valid in a Kubernetes label value.
var labelUnsafeChars = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// sanitizeLabelValue folds an arbitrary principal identifier (e.g. an email
// address) into a valid Kubernetes label value: unsafe characters become
// "-", leading/trailing separators are trimmed, and the result is capped at
// the 63-character label-value limit.
func sanitizeLabelValue(v string) string {
	v = labelUnsafeChars.ReplaceAllString(v, "-")
	v = strings.Trim(v, "-_.")
	if len(v) > 63 {
		v = strings.TrimRight(v[:63], "-_.")
	}
	if v == "" {
		return "unknown"
	}
	return v
}
