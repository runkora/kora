package agents

import (
	"context"
	"errors"
	"io"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// TestBuildSessionManagerStatefulSet_WorkspaceClaimIsDeletedWithTheSet asserts
// the StatefulSet carries a Delete/Retain persistentVolumeClaimRetentionPolicy
// and that the claim template is labeled with the agent type's own labels.
func TestBuildSessionManagerStatefulSet_WorkspaceClaimIsDeletedWithTheSet(t *testing.T) {
	p := &K8sProvisioner{}
	at := AgentType{ID: "agent-1", Slug: "coder", TenantID: "acme"}

	ss := p.buildSessionManagerStatefulSet(at, "orbit-tenant-acme", agentTypeLabels(at))

	policy := ss.Spec.PersistentVolumeClaimRetentionPolicy
	if policy == nil {
		t.Fatal("persistentVolumeClaimRetentionPolicy is nil, want Delete on whenDeleted")
	}
	if policy.WhenDeleted != appsv1.DeletePersistentVolumeClaimRetentionPolicyType {
		t.Errorf("whenDeleted = %q, want Delete", policy.WhenDeleted)
	}
	if policy.WhenScaled != appsv1.RetainPersistentVolumeClaimRetentionPolicyType {
		t.Errorf("whenScaled = %q, want Retain (HPA scale-down is not agent deletion)", policy.WhenScaled)
	}

	claimLabels := ss.Spec.VolumeClaimTemplates[0].Labels
	if claimLabels["orbit.io/agent-type"] != "agent-1" {
		t.Errorf("claim labels = %v, want orbit.io/agent-type=agent-1 so DeprovisionAgentType can select it", claimLabels)
	}
}

// TestDeprovisionAgentType_DeletesWorkspacePVCs asserts DeprovisionAgentType
// sweeps every ordinal's claim by label, and leaves other agent types' claims
// alone.
func TestDeprovisionAgentType_DeletesWorkspacePVCs(t *testing.T) {
	const ns = "orbit-tenant-acme"
	at := AgentType{ID: "agent-1", Slug: "coder", TenantID: "acme"}

	matching := agentTypeLabels(at)
	pvc0 := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "workspaces-coder-0", Namespace: ns, Labels: matching},
	}
	pvc1 := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "workspaces-coder-1", Namespace: ns, Labels: matching},
	}
	// A claim for a different agent type must survive.
	other := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "workspaces-other-0",
			Namespace: ns,
			Labels:    agentTypeLabels(AgentType{ID: "agent-2", Slug: "other", TenantID: "acme"}),
		},
	}
	ss := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: at.Slug, Namespace: ns}}

	typed := fake.NewSimpleClientset(ss, pvc0, pvc1, other)
	scheme := runtime.NewScheme()
	dc := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{
		authorizationPolicyGVR: "AuthorizationPolicyList",
		destinationRuleGVR:     "DestinationRuleList",
	})

	p := &K8sProvisioner{
		client:        typed,
		dynamicClient: dc,
		logger:        logging.NewLoggerWithOutput(io.Discard),
	}

	if err := p.DeprovisionAgentType(context.Background(), at, ns); err != nil {
		t.Fatalf("DeprovisionAgentType: %v", err)
	}

	list, err := typed.CoreV1().PersistentVolumeClaims(ns).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list pvcs: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].Name != "workspaces-other-0" {
		t.Fatalf("pvcs after deprovision = %v, want only workspaces-other-0 to survive", list.Items)
	}
}

// TestDeprovisionAgentType_NoPVCsIsNotAnError asserts deprovisioning an agent
// type with no workspace claims is not an error.
func TestDeprovisionAgentType_NoPVCsIsNotAnError(t *testing.T) {
	const ns = "orbit-tenant-acme"
	at := AgentType{ID: "agent-1", Slug: "coder", TenantID: "acme"}

	typed := fake.NewSimpleClientset()
	scheme := runtime.NewScheme()
	dc := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{
		authorizationPolicyGVR: "AuthorizationPolicyList",
		destinationRuleGVR:     "DestinationRuleList",
	})

	p := &K8sProvisioner{
		client:        typed,
		dynamicClient: dc,
		logger:        logging.NewLoggerWithOutput(io.Discard),
	}

	if err := p.DeprovisionAgentType(context.Background(), at, ns); err != nil {
		t.Fatalf("DeprovisionAgentType: %v", err)
	}
}

// TestDeleteWorkspacePVCs_ContinuesPastAFailedDelete asserts a failed delete
// doesn't stop the rest from being attempted.
func TestDeleteWorkspacePVCs_ContinuesPastAFailedDelete(t *testing.T) {
	const ns = "orbit-tenant-acme"
	at := AgentType{ID: "agent-1", Slug: "coder", TenantID: "acme"}

	matching := agentTypeLabels(at)
	pvc0 := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "workspaces-coder-0", Namespace: ns, Labels: matching},
	}
	pvc1 := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "workspaces-coder-1", Namespace: ns, Labels: matching},
	}

	typed := fake.NewSimpleClientset(pvc0, pvc1)
	wantErr := errors.New("permission denied")
	typed.PrependReactor("delete", "persistentvolumeclaims", func(action k8stesting.Action) (bool, runtime.Object, error) {
		da := action.(k8stesting.DeleteAction)
		if da.GetName() == "workspaces-coder-0" {
			return true, nil, wantErr
		}
		return false, nil, nil
	})

	p := &K8sProvisioner{client: typed, logger: logging.NewLoggerWithOutput(io.Discard)}

	err := p.deleteWorkspacePVCs(context.Background(), at, ns)
	if !errors.Is(err, wantErr) {
		t.Fatalf("deleteWorkspacePVCs error = %v, want %v", err, wantErr)
	}

	if _, err := typed.CoreV1().PersistentVolumeClaims(ns).Get(context.Background(), "workspaces-coder-0", metav1.GetOptions{}); err != nil {
		t.Fatalf("workspaces-coder-0 should still exist since its delete failed: %v", err)
	}
	if _, err := typed.CoreV1().PersistentVolumeClaims(ns).Get(context.Background(), "workspaces-coder-1", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("workspaces-coder-1 should have been deleted despite workspaces-coder-0 failing, get err = %v", err)
	}
}
