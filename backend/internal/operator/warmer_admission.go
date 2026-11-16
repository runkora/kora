package operator

import (
	"context"
	"fmt"

	si "github.com/runkora/kora/backend/internal/platform/sandboximages/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// daemonSetControllerUser is the user the kube-controller-manager creates DaemonSet pods as.
// It runs each controller under its own service account (--use-service-account-credentials).
const daemonSetControllerUser = "system:serviceaccount:kube-system:daemon-set-controller"

// isSandboxWarmer reports whether a pod being created is a sandbox image warmer. A warmer
// runs with no sidecars (docs/adr/2026-10-03-sandbox-environments.md, Amendment 4).
//
// The decision rests on who created the pod and what controls it, never on what the pod
// claims about itself. kora-agents can create pods in a tenant namespace with any labels
// and owner references, but it cannot create DaemonSets there. So a pod is a warmer only
// when all of these hold:
//   - the DaemonSet controller is the user creating it;
//   - its controlling owner is a DaemonSet in the same namespace with the same UID;
//   - that DaemonSet carries the warmer labels and is controlled by a SandboxImage.
func isSandboxWarmer(ctx context.Context, c client.Client, req admission.Request, pod *corev1.Pod) (bool, error) {
	if req.UserInfo.Username != daemonSetControllerUser {
		return false, nil
	}
	owner := metav1.GetControllerOf(pod)
	if owner == nil || owner.Kind != "DaemonSet" || owner.APIVersion != appsv1.SchemeGroupVersion.String() {
		return false, nil
	}

	ds := &appsv1.DaemonSet{}
	err := c.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: owner.Name}, ds)
	if apierrors.IsNotFound(err) {
		// The operator caches only warmer DaemonSets, so any other DaemonSet reads as missing.
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get DaemonSet %q: %w", owner.Name, err)
	}
	if ds.UID != owner.UID {
		return false, nil
	}
	if ds.Labels["app.kubernetes.io/component"] != warmerComponent || ds.Labels["app.kubernetes.io/managed-by"] != "kora-operator" {
		return false, nil
	}
	dsOwner := metav1.GetControllerOf(ds)
	if dsOwner == nil || dsOwner.Kind != "SandboxImage" || dsOwner.APIVersion != si.GroupVersion.String() {
		return false, nil
	}
	return true, nil
}
