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
		SandboxImage:        "kora-sandbox-base:test",
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

	pod := p.buildSandboxPod(Run{ID: "run-1"}, "coder", "tenant-example", nil, "test-exec-token")

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
	pod := p.buildSandboxPod(Run{ID: "run-1"}, "coder", "tenant-example", nil, "test-exec-token")
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

// The chart renders every key, so a default install decodes `affinity: {}` into
// a non-nil empty struct. Applying that would stamp an empty affinity onto every
// pod, which is a manifest diff for a deployment that customized nothing.
func TestApplyToPodSpec_EmptyAffinityIsNotApplied(t *testing.T) {
	w := WorkloadCustomization{Affinity: &corev1.Affinity{}}
	var spec corev1.PodSpec

	w.applyToPodSpec(&spec)

	if spec.Affinity != nil {
		t.Errorf("empty affinity should be treated as unset, got %+v", spec.Affinity)
	}
}

// A ReadWriteOnce workspace attaches to one node, so the sandbox has to land on
// the node running the session-manager replica that owns it. Matching node
// selectors do not deliver that, because a pool has many nodes.
func TestBuildSandboxPod_CoLocatesOnReadWriteOnceWorkspace(t *testing.T) {
	cfg := splitPoolConfig()
	cfg.Workspace.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
	p := &K8sProvisioner{cfg: cfg}

	pod := p.buildSandboxPod(Run{ID: "run-1", PodOrdinal: 2}, "coder", "tenant-example", nil, "test-exec-token")

	if pod.Spec.Affinity == nil || pod.Spec.Affinity.PodAffinity == nil {
		t.Fatal("expected pod affinity binding the sandbox to its session-manager")
	}
	terms := pod.Spec.Affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution
	if len(terms) != 1 {
		t.Fatalf("expected one required term, got %d", len(terms))
	}
	if terms[0].TopologyKey != "kubernetes.io/hostname" {
		t.Errorf("topologyKey: got %q", terms[0].TopologyKey)
	}
	if got := terms[0].LabelSelector.MatchLabels["statefulset.kubernetes.io/pod-name"]; got != "coder-2" {
		t.Errorf("selector names %q, want the replica holding this run's workspace", got)
	}
}

// ReadWriteMany crosses nodes, which is the whole point of a separate sandbox
// pool, so the sandbox must stay free to schedule away.
func TestBuildSandboxPod_NoCoLocationOnReadWriteManyWorkspace(t *testing.T) {
	p := &K8sProvisioner{cfg: splitPoolConfig()}

	pod := p.buildSandboxPod(Run{ID: "run-1"}, "coder", "tenant-example", nil, "test-exec-token")

	if pod.Spec.Affinity != nil && pod.Spec.Affinity.PodAffinity != nil {
		t.Errorf("ReadWriteMany should not pin the sandbox: got %+v", pod.Spec.Affinity.PodAffinity)
	}
}

// Configured affinity is kept: required terms are ANDed, so co-location cannot
// be dropped by setting placement.
func TestBuildSandboxPod_CoLocationKeepsConfiguredAffinity(t *testing.T) {
	cfg := splitPoolConfig()
	cfg.Workspace.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
	cfg.Workloads.Sandbox.Affinity = &corev1.Affinity{
		NodeAffinity: &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{{
					MatchExpressions: []corev1.NodeSelectorRequirement{{
						Key: "workload-tier", Operator: corev1.NodeSelectorOpIn, Values: []string{"orbit"},
					}},
				}},
			},
		},
	}
	p := &K8sProvisioner{cfg: cfg}

	pod := p.buildSandboxPod(Run{ID: "run-1"}, "coder", "tenant-example", nil, "test-exec-token")

	if pod.Spec.Affinity.NodeAffinity == nil {
		t.Error("configured node affinity was dropped")
	}
	if pod.Spec.Affinity.PodAffinity == nil {
		t.Error("co-location was dropped by configured affinity")
	}
}

// Cloud identity attaches when a pod is created, so a changed ServiceAccount
// annotation has to roll the StatefulSet or running agents never receive it.
func TestBuildStatefulSet_ServiceAccountAnnotationsRollThePods(t *testing.T) {
	at := AgentType{ID: "agent-1", Slug: "coder", TenantID: "example"}
	hashOf := func(annotations map[string]string) string {
		cfg := splitPoolConfig()
		cfg.Workloads.SessionManager.ServiceAccount.Annotations = annotations
		p := &K8sProvisioner{cfg: cfg}
		return p.buildSessionManagerStatefulSet(at, "tenant-example", agentTypeLabels(at)).
			Spec.Template.Annotations[serviceAccountHashAnnotation]
	}

	first := hashOf(map[string]string{"eks.amazonaws.com/role-arn": "arn:aws:iam::1:role/agent"})
	if first == "" {
		t.Fatal("expected a service account hash on the pod template")
	}
	if second := hashOf(map[string]string{"eks.amazonaws.com/role-arn": "arn:aws:iam::1:role/other"}); second == first {
		t.Error("changing the annotation must change the hash, or the pods never roll")
	}
	if none := hashOf(nil); none != "" {
		t.Errorf("no annotations should leave the pod template alone, got %q", none)
	}
}

// A nodeSelector on one side and required node affinity on the other separates
// the pods just as surely as two different selectors.
func TestSplitAcrossNodes_MixedForms(t *testing.T) {
	affinity := func(tier string) *corev1.Affinity {
		return &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{{
					MatchExpressions: []corev1.NodeSelectorRequirement{{
						Key: "workload-tier", Operator: corev1.NodeSelectorOpIn, Values: []string{tier},
					}},
				}},
			},
		}}
	}
	cases := map[string]struct {
		agent, sandbox corev1.PodSpec
		want           bool
	}{
		"selector against affinity": {
			agent:   corev1.PodSpec{NodeSelector: map[string]string{"workload-tier": "orbit"}},
			sandbox: corev1.PodSpec{Affinity: affinity("orbit-sandbox")},
			want:    true,
		},
		"affinity against selector": {
			agent:   corev1.PodSpec{Affinity: affinity("orbit")},
			sandbox: corev1.PodSpec{NodeSelector: map[string]string{"workload-tier": "orbit-sandbox"}},
			want:    true,
		},
		"identical pinning is one pool": {
			agent:   corev1.PodSpec{NodeSelector: map[string]string{"workload-tier": "orbit"}},
			sandbox: corev1.PodSpec{NodeSelector: map[string]string{"workload-tier": "orbit"}},
			want:    false,
		},
		"one side unconstrained is inconclusive": {
			agent:   corev1.PodSpec{NodeSelector: map[string]string{"workload-tier": "orbit"}},
			sandbox: corev1.PodSpec{},
			want:    false,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got, _ := splitAcrossNodes(c.agent, c.sandbox)
			if got != c.want {
				t.Errorf("splitAcrossNodes = %v, want %v", got, c.want)
			}
		})
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
		// The operator's pod validator denies these, so accepting them at
		// startup would move the failure to every provisioned pod.
		"istio injection opt-out annotation": {
			Sandbox: WorkloadCustomization{PodAnnotations: map[string]string{"sidecar.istio.io/inject": "false"}},
		},
		"istio injection opt-out label": {
			Sandbox: WorkloadCustomization{PodLabels: map[string]string{"sidecar.istio.io/inject": "false"}},
		},
		"istio interception opt-out": {
			SessionManager: WorkloadCustomization{PodAnnotations: map[string]string{"sidecar.istio.io/interceptionMode": "NONE"}},
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
