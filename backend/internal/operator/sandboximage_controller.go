// SandboxImage reconciler: validates each registered sandbox image from its
// registry config, pins its digest and keeps a pre-pull DaemonSet for images
// marked warm. See docs/adr/2026-10-03-sandbox-environments.md.
package operator

import (
	"context"
	"errors"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/clear-route/orbit/internal/platform/logging"
	si "github.com/clear-route/orbit/internal/platform/sandboximages/v1alpha1"
)

// Warm condition reasons.
const (
	reasonWarmDisabled = "Disabled"
	reasonWarmNotValid = "NotValid"
	reasonWarmPulling  = "Pulling"
	reasonWarmPulled   = "Pulled"
)

// warmerComponent labels every pre-pull DaemonSet, so the operator caches only those.
const warmerComponent = "sandbox-image-warmer"

// labelSandboxImage names the SandboxImage a warmer pod pulls.
const labelSandboxImage = "orbit.io/sandbox-image"

// errPullSecret marks a pull secret the operator could not read.
var errPullSecret = errors.New("pull secret unreadable")

// sandboxImageClient is what the reconciler calls on the API server.
type sandboxImageClient interface {
	Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error
	Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error
	Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error
	Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error
	Status() client.SubResourceWriter
}

// WarmerPlacement is where pre-pull pods run: the sandbox workload placement.
type WarmerPlacement struct {
	NodeSelector              map[string]string                 `json:"nodeSelector,omitempty"`
	Tolerations               []corev1.Toleration               `json:"tolerations,omitempty"`
	Affinity                  *corev1.Affinity                  `json:"affinity,omitempty"`
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`
	PriorityClassName         string                            `json:"priorityClassName,omitempty"`
}

// SandboxImageReconciler validates SandboxImages and warms the valid ones that ask for it.
type SandboxImageReconciler struct {
	Client   sandboxImageClient
	Scheme   *runtime.Scheme
	Registry RegistryReader
	Logger   logging.Logger
	// PullSecrets are the install-wide pull secrets replicated into every tenant namespace.
	PullSecrets []string
	Placement   WarmerPlacement
	// Resync re-reads each image so a moved tag reaches new sandboxes.
	Resync time.Duration
}

// Reconcile validates one SandboxImage and converges its status and DaemonSet.
func (r *SandboxImageReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	img := &si.SandboxImage{}
	if err := r.Client.Get(ctx, req.NamespacedName, img); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get SandboxImage %q: %w", req.NamespacedName, err)
	}
	before := img.DeepCopy()

	cfg, reason, message, err := r.inspect(ctx, img)
	if err != nil {
		// A registry outage must not take an accepted image out of service.
		if img.Valid() {
			r.Logger.Warn(ctx, "re-reading a sandbox image failed; keeping its pinned digest",
				logging.Field{Key: "sandbox_image", Value: req.String()},
				logging.Field{Key: "error", Value: err.Error()},
			)
			return ctrl.Result{RequeueAfter: r.Resync}, nil
		}
		message = err.Error()
	}

	img.Status.ObservedGeneration = img.Generation
	if reason != reasonValidated {
		img.Status.Digest = ""
		setCondition(img, si.ConditionValid, metav1.ConditionFalse, reason, message)
		setCondition(img, si.ConditionWarm, metav1.ConditionFalse, reasonWarmNotValid, "the image is not valid")
		if err := r.deleteWarmer(ctx, img); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: r.Resync}, r.patchStatus(ctx, before, img)
	}

	img.Status.Digest = cfg.Digest
	setCondition(img, si.ConditionValid, metav1.ConditionTrue, reasonValidated,
		fmt.Sprintf("built on kora-sandbox-base %s", cfg.Labels[si.BaseLabel]))
	if err := r.reconcileWarmer(ctx, img); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: r.Resync}, r.patchStatus(ctx, before, img)
}

// inspect reads the image config and judges it. The returned error is a
// registry or pull secret failure; reason and message describe it for status.
func (r *SandboxImageReconciler) inspect(ctx context.Context, img *si.SandboxImage) (imageConfig, string, string, error) {
	auth, err := r.dockerConfigs(ctx, img)
	if err != nil {
		return imageConfig{}, reasonPullSecretError, "", err
	}
	cfg, err := r.Registry.Read(ctx, img.Spec.Image, auth)
	if err != nil {
		return imageConfig{}, reasonRegistryError, "", err
	}
	if reason, message := validateImageConfig(cfg); reason != "" {
		return cfg, reason, message, nil
	}
	return cfg, reasonValidated, "", nil
}

// dockerConfigs reads the image's own pull secret and the install's. The
// image's secret must exist; an install secret not yet replicated is skipped.
func (r *SandboxImageReconciler) dockerConfigs(ctx context.Context, img *si.SandboxImage) ([]dockerConfig, error) {
	var out []dockerConfig
	read := func(name string, required bool) error {
		sec := &corev1.Secret{}
		err := r.Client.Get(ctx, types.NamespacedName{Namespace: img.Namespace, Name: name}, sec)
		if apierrors.IsNotFound(err) && !required {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%w: %s: %w", errPullSecret, name, err)
		}
		c, err := parseDockerConfig(sec.Data[corev1.DockerConfigJsonKey])
		if err != nil {
			return fmt.Errorf("%w: %s: %w", errPullSecret, name, err)
		}
		out = append(out, c)
		return nil
	}
	if ref := img.Spec.PullSecretRef; ref != nil {
		if err := read(ref.Name, true); err != nil {
			return nil, err
		}
	}
	for _, name := range r.PullSecrets {
		if err := read(name, false); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// reconcileWarmer keeps a DaemonSet pulling the image when it is marked warm.
func (r *SandboxImageReconciler) reconcileWarmer(ctx context.Context, img *si.SandboxImage) error {
	if !img.Spec.Warm {
		setCondition(img, si.ConditionWarm, metav1.ConditionFalse, reasonWarmDisabled, "spec.warm is false")
		return r.deleteWarmer(ctx, img)
	}
	ds := r.warmerDaemonSet(img)
	if err := controllerutil.SetControllerReference(img, ds, r.Scheme); err != nil {
		return fmt.Errorf("own DaemonSet for %q: %w", img.Name, err)
	}
	err := r.Client.Create(ctx, ds)
	switch {
	case apierrors.IsAlreadyExists(err):
		if patchErr := r.Client.Patch(ctx, ds, client.Merge, client.FieldOwner("orbit-operator")); patchErr != nil {
			return fmt.Errorf("patch DaemonSet %q: %w", ds.Name, patchErr)
		}
	case err != nil:
		return fmt.Errorf("create DaemonSet %q: %w", ds.Name, err)
	}
	live := &appsv1.DaemonSet{}
	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(ds), live); err != nil {
		return fmt.Errorf("get DaemonSet %q: %w", ds.Name, err)
	}
	st := live.Status
	current := st.ObservedGeneration >= live.Generation && st.UpdatedNumberScheduled == st.DesiredNumberScheduled
	if current && st.DesiredNumberScheduled > 0 && st.NumberReady == st.DesiredNumberScheduled {
		setCondition(img, si.ConditionWarm, metav1.ConditionTrue, reasonWarmPulled,
			fmt.Sprintf("pulled on %d node(s)", st.DesiredNumberScheduled))
		return nil
	}
	setCondition(img, si.ConditionWarm, metav1.ConditionFalse, reasonWarmPulling,
		fmt.Sprintf("ready on %d of %d node(s)", st.NumberReady, st.DesiredNumberScheduled))
	return nil
}

func (r *SandboxImageReconciler) deleteWarmer(ctx context.Context, img *si.SandboxImage) error {
	ds := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: warmerName(img), Namespace: img.Namespace}}
	if err := r.Client.Delete(ctx, ds); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete DaemonSet %q: %w", ds.Name, err)
	}
	return nil
}

func warmerName(img *si.SandboxImage) string { return "sandbox-image-" + img.Name }

// warmerDaemonSet idles one pod per sandbox node with the pinned image. The
// webhook injects the tenant sidecars like any tenant pod.
func (r *SandboxImageReconciler) warmerDaemonSet(img *si.SandboxImage) *appsv1.DaemonSet {
	labels := map[string]string{
		"app.kubernetes.io/component":  warmerComponent,
		"app.kubernetes.io/managed-by": "orbit-operator",
		labelSandboxImage:              img.Name,
		// The tenant-service SPIFFE identity gives the injected istio-proxy its certificate.
		podLabelSpiffeService: warmerComponent,
	}
	pullSecrets := imagePullSecretRefs(r.PullSecrets)
	if ref := img.Spec.PullSecretRef; ref != nil && !hasPullSecret(pullSecrets, ref.Name) {
		pullSecrets = append(pullSecrets, corev1.LocalObjectReference{Name: ref.Name})
	}
	uid := int64(100)
	return &appsv1.DaemonSet{
		TypeMeta:   metav1.TypeMeta{APIVersion: "apps/v1", Kind: "DaemonSet"},
		ObjectMeta: metav1.ObjectMeta{Name: warmerName(img), Namespace: img.Namespace, Labels: labels},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{labelSandboxImage: img.Name}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					AutomountServiceAccountToken:  new(false),
					EnableServiceLinks:            new(false),
					TerminationGracePeriodSeconds: new(int64(5)),
					ImagePullSecrets:              pullSecrets,
					NodeSelector:                  r.Placement.NodeSelector,
					Tolerations:                   r.Placement.Tolerations,
					Affinity:                      r.Placement.Affinity,
					TopologySpreadConstraints:     r.Placement.TopologySpreadConstraints,
					PriorityClassName:             r.Placement.PriorityClassName,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot:   new(true),
						RunAsUser:      &uid,
						RunAsGroup:     &uid,
						SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
					},
					Containers: []corev1.Container{{
						Name:            "warm",
						Image:           img.PinnedRef(),
						ImagePullPolicy: corev1.PullIfNotPresent,
						Command:         []string{"sleep", "infinity"},
						SecurityContext: restrictedContainerSecurityContext(100, true),
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("10m"),
								corev1.ResourceMemory: resource.MustParse("16Mi"),
							},
							Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("32Mi")},
						},
					}},
				},
			},
		},
	}
}

// imagePullSecretRefs turns Secret names into pod references.
func imagePullSecretRefs(names []string) []corev1.LocalObjectReference {
	refs := make([]corev1.LocalObjectReference, 0, len(names))
	for _, n := range names {
		refs = append(refs, corev1.LocalObjectReference{Name: n})
	}
	return refs
}

func setCondition(img *si.SandboxImage, typ string, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&img.Status.Conditions, metav1.Condition{
		Type:               typ,
		Status:             status,
		ObservedGeneration: img.Generation,
		Reason:             reason,
		Message:            message,
	})
}

// patchStatus writes the status only when it changed.
func (r *SandboxImageReconciler) patchStatus(ctx context.Context, before, after *si.SandboxImage) error {
	if statusEqual(before.Status, after.Status) {
		return nil
	}
	if err := r.Client.Status().Patch(ctx, after, client.MergeFrom(before)); err != nil {
		return fmt.Errorf("patch SandboxImage %q status: %w", after.Name, err)
	}
	return nil
}

func statusEqual(a, b si.Status) bool {
	if a.Digest != b.Digest || a.ObservedGeneration != b.ObservedGeneration || len(a.Conditions) != len(b.Conditions) {
		return false
	}
	for _, c := range a.Conditions {
		o := meta.FindStatusCondition(b.Conditions, c.Type)
		if o == nil || o.Status != c.Status || o.Reason != c.Reason || o.Message != c.Message || o.ObservedGeneration != c.ObservedGeneration {
			return false
		}
	}
	return true
}

// NewWarmerDaemonSet is an empty DaemonSet, for the operator's cache configuration.
func NewWarmerDaemonSet() *appsv1.DaemonSet { return &appsv1.DaemonSet{} }

// WarmerSelector selects the DaemonSets the operator caches.
func WarmerSelector() labels.Selector {
	return labels.SelectorFromSet(labels.Set{"app.kubernetes.io/component": warmerComponent})
}

// SetupWithManager reconciles on spec changes and on changes to an owned DaemonSet.
func (r *SandboxImageReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&si.SandboxImage{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&appsv1.DaemonSet{}).
		Complete(r)
}
