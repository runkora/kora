package agents

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func splitPoolConfig() K8sProvisionerConfig {
	size := resource.MustParse("20Gi")
	return K8sProvisionerConfig{
		SessionManagerImage: "orbit-session-manager:test",
		SandboxImage:        "orbit-sandbox-exec:test",
		SandboxGRPCPort:     50051,
		SystemNamespace:     "orbit-system",
		Workloads: WorkloadsConfig{
			Common: CommonCustomization{
				PodLabels:      map[string]string{"cost-centre": "platform"},
				PodAnnotations: map[string]string{"backup.example.com/exclude": "true"},
			},
			SessionManager: WorkloadCustomization{
				NodeSelector:      map[string]string{"workload-tier": "orbit"},
				PriorityClassName: "orbit-agents",
				ServiceAccount: ServiceAccountCustomization{
					Annotations: map[string]string{"eks.amazonaws.com/role-arn": "arn:aws:iam::1:role/agent"},
				},
			},
			Sandbox: WorkloadCustomization{
				NodeSelector: map[string]string{"workload-tier": "orbit-sandbox"},
				Tolerations: []corev1.Toleration{{
					Key: "workload-tier", Operator: corev1.TolerationOpEqual, Value: "orbit-sandbox", Effect: corev1.TaintEffectNoSchedule,
				}},
				PodLabels: map[string]string{"cost-centre": "sandboxes"},
			},
		},
		Workspace: WorkspaceConfig{
			StorageClassName: "efs",
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
			Size:             &size,
		},
	}
}

// The session-manager StatefulSet carries its own placement, the merged pod
// metadata, and the configured workspace volume.
func TestBuildStatefulSet_AppliesWorkloadCustomization(t *testing.T) {
	p := &K8sProvisioner{cfg: splitPoolConfig()}
	at := AgentType{ID: "agent-1", Slug: "coder", TenantID: "example"}

	ss := p.buildSessionManagerStatefulSet(at, "tenant-example", agentTypeLabels(at))

	spec := ss.Spec.Template.Spec
	if spec.NodeSelector["workload-tier"] != "orbit" {
		t.Errorf("nodeSelector: got %v", spec.NodeSelector)
	}
	if spec.PriorityClassName != "orbit-agents" {
		t.Errorf("priorityClassName: got %q", spec.PriorityClassName)
	}
	meta := ss.Spec.Template.ObjectMeta
	if meta.Labels["cost-centre"] != "platform" {
		t.Errorf("common podLabels should reach the pod template: got %v", meta.Labels)
	}
	if meta.Annotations["backup.example.com/exclude"] != "true" {
		t.Errorf("common podAnnotations should reach the pod template: got %v", meta.Annotations)
	}

	claim := ss.Spec.VolumeClaimTemplates[0]
	if claim.Spec.StorageClassName == nil || *claim.Spec.StorageClassName != "efs" {
		t.Errorf("storageClassName: got %v", claim.Spec.StorageClassName)
	}
	if len(claim.Spec.AccessModes) != 1 || claim.Spec.AccessModes[0] != corev1.ReadWriteMany {
		t.Errorf("accessModes: got %v", claim.Spec.AccessModes)
	}
	if got := claim.Spec.Resources.Requests[corev1.ResourceStorage]; got.String() != "20Gi" {
		t.Errorf("workspace size: got %s", got.String())
	}
}

// Platform labels and mesh annotations survive customization, because they are
// what SPIRE and the sidecar injector select on.
func TestBuildStatefulSet_KeepsIdentityLabelsAndMeshAnnotations(t *testing.T) {
	cfg := splitPoolConfig()
	// Validation rejects these at startup; this proves the builder does not
	// depend on that to keep the platform's own keys.
	cfg.Workloads.SessionManager.PodLabels = map[string]string{"orbit.io/tenant": "someone-else", "app": "hijacked"}
	cfg.Workloads.SessionManager.PodAnnotations = map[string]string{"proxy.istio.io/config": "{}"}
	p := &K8sProvisioner{cfg: cfg}
	at := AgentType{ID: "agent-1", Slug: "coder", TenantID: "example"}

	ss := p.buildSessionManagerStatefulSet(at, "tenant-example", agentTypeLabels(at))

	meta := ss.Spec.Template.ObjectMeta
	if meta.Labels["orbit.io/tenant"] != "example" {
		t.Errorf("orbit.io/tenant: got %q, want the tenant the platform set", meta.Labels["orbit.io/tenant"])
	}
	if meta.Labels["app"] != "coder" {
		t.Errorf("app: got %q, want the agent slug", meta.Labels["app"])
	}
	if meta.Annotations["proxy.istio.io/config"] == "{}" {
		t.Error("mesh annotation was overwritten by podAnnotations")
	}
}

func TestBuildSandboxPod_AppliesSandboxCustomization(t *testing.T) {
	p := &K8sProvisioner{cfg: splitPoolConfig()}

	pod := p.buildSandboxPod(Run{ID: "run-1"}, "coder", "tenant-example", nil)

	if pod.Spec.NodeSelector["workload-tier"] != "orbit-sandbox" {
		t.Errorf("nodeSelector: got %v", pod.Spec.NodeSelector)
	}
	if len(pod.Spec.Tolerations) != 1 || pod.Spec.Tolerations[0].Value != "orbit-sandbox" {
		t.Errorf("tolerations: got %v", pod.Spec.Tolerations)
	}
	if pod.Labels["orbit.io/role"] != "sandbox" {
		t.Errorf("sandbox identity label lost: got %v", pod.Labels)
	}
	// The sandbox's own podLabels win over the common value.
	if pod.Labels["cost-centre"] != "sandboxes" {
		t.Errorf("cost-centre: got %q, want the per-workload value", pod.Labels["cost-centre"])
	}
}

// Cloud identity annotations land on the agent's ServiceAccount, which only the
// session-manager uses.
func TestBuildServiceAccount_AppliesAnnotations(t *testing.T) {
	p := &K8sProvisioner{cfg: splitPoolConfig()}

	sa := p.buildServiceAccount("coder", "tenant-example", map[string]string{"app": "coder"})

	if sa.Annotations["eks.amazonaws.com/role-arn"] != "arn:aws:iam::1:role/agent" {
		t.Errorf("serviceAccount annotations: got %v", sa.Annotations)
	}
}

// An empty customization renders exactly what the builders produced before this
// existed, so an install that sets nothing sees no change.
func TestBuilders_EmptyCustomizationChangesNothing(t *testing.T) {
	p := &K8sProvisioner{cfg: K8sProvisionerConfig{SandboxGRPCPort: 50051}}
	at := AgentType{ID: "agent-1", Slug: "coder", TenantID: "example"}

	ss := p.buildSessionManagerStatefulSet(at, "tenant-example", agentTypeLabels(at))
	pod := p.buildSandboxPod(Run{ID: "run-1"}, "coder", "tenant-example", nil)
	sa := p.buildServiceAccount("coder", "tenant-example", map[string]string{"app": "coder"})

	if ss.Spec.Template.Spec.NodeSelector != nil || pod.Spec.NodeSelector != nil {
		t.Error("node selectors should stay unset")
	}
	if sa.Annotations != nil {
		t.Errorf("serviceAccount annotations should stay unset: got %v", sa.Annotations)
	}
	claim := ss.Spec.VolumeClaimTemplates[0]
	if claim.Spec.StorageClassName != nil {
		t.Errorf("storageClassName should stay unset: got %v", *claim.Spec.StorageClassName)
	}
	if len(claim.Spec.AccessModes) != 1 || claim.Spec.AccessModes[0] != corev1.ReadWriteOnce {
		t.Errorf("accessModes: got %v, want the ReadWriteOnce default", claim.Spec.AccessModes)
	}
	if got := claim.Spec.Resources.Requests[corev1.ResourceStorage]; got.String() != "10Gi" {
		t.Errorf("workspace size: got %s, want the 10Gi default", got.String())
	}
}

func TestWorkloadsConfig_ValidateRejectsPlatformKeys(t *testing.T) {
	cases := map[string]WorkloadsConfig{
		"identity label on the sandbox": {
			Sandbox: WorkloadCustomization{PodLabels: map[string]string{"orbit.io/role": "not-a-sandbox"}},
		},
		"selector label on the session-manager": {
			SessionManager: WorkloadCustomization{PodLabels: map[string]string{"app": "hijacked"}},
		},
		"identity label in common": {
			Common: CommonCustomization{PodLabels: map[string]string{"spiffe.io/agent": "other"}},
		},
		"mesh annotation": {
			Sandbox: WorkloadCustomization{PodAnnotations: map[string]string{"proxy.istio.io/config": "{}"}},
		},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			if err := cfg.Validate(); err == nil {
				t.Fatal("expected a validation error")
			}
		})
	}
}

func TestWorkloadsConfig_ValidateAcceptsOrdinaryMetadata(t *testing.T) {
	cfg := WorkloadsConfig{
		Common:  CommonCustomization{PodLabels: map[string]string{"cost-centre": "platform"}},
		Sandbox: WorkloadCustomization{PodAnnotations: map[string]string{"backup.example.com/exclude": "true"}},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
