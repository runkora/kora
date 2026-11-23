package agents

import (
	"context"
	"io"
	"reflect"
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

func splitPoolConfig() K8sProvisionerConfig {
	size := resource.MustParse("20Gi")
	return K8sProvisionerConfig{
		SessionManagerImage: "kora-session-manager:test",
		SandboxImage:        "kora-sandbox-base:test",
		SandboxGRPCPort:     50051,
		SystemNamespace:     "kora-system",
		Workloads: WorkloadsConfig{
			Common: CommonCustomization{
				PodLabels:      map[string]string{"cost-centre": "platform"},
				PodAnnotations: map[string]string{"backup.example.com/exclude": "true"},
			},
			SessionManager: SessionManagerCustomization{WorkloadCustomization: WorkloadCustomization{
				NodeSelector:      map[string]string{"workload-tier": "kora"},
				PriorityClassName: "kora-agents",
			}},
			Sandbox: WorkloadCustomization{
				NodeSelector: map[string]string{"workload-tier": "kora-sandbox"},
				Tolerations: []corev1.Toleration{{
					Key: "workload-tier", Operator: corev1.TolerationOpEqual, Value: "kora-sandbox", Effect: corev1.TaintEffectNoSchedule,
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
	if spec.NodeSelector["workload-tier"] != "kora" {
		t.Errorf("nodeSelector: got %v", spec.NodeSelector)
	}
	if spec.PriorityClassName != "kora-agents" {
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
	cfg.Workloads.SessionManager.PodLabels = map[string]string{"runkora.io/tenant": "someone-else", "app": "hijacked"}
	cfg.Workloads.SessionManager.PodAnnotations = map[string]string{"proxy.istio.io/config": "{}"}
	p := &K8sProvisioner{cfg: cfg}
	at := AgentType{ID: "agent-1", Slug: "coder", TenantID: "example"}

	ss := p.buildSessionManagerStatefulSet(at, "tenant-example", agentTypeLabels(at))

	meta := ss.Spec.Template.ObjectMeta
	if meta.Labels["runkora.io/tenant"] != "example" {
		t.Errorf("runkora.io/tenant: got %q, want the tenant the platform set", meta.Labels["runkora.io/tenant"])
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

	pod := buildTestSandboxPod(p, Run{ID: "run-1"}, "coder", "tenant-example", testSandboxSpec(), "test-exec-token")

	if pod.Spec.NodeSelector["workload-tier"] != "kora-sandbox" {
		t.Errorf("nodeSelector: got %v", pod.Spec.NodeSelector)
	}
	if len(pod.Spec.Tolerations) != 1 || pod.Spec.Tolerations[0].Value != "kora-sandbox" {
		t.Errorf("tolerations: got %v", pod.Spec.Tolerations)
	}
	if pod.Labels["runkora.io/role"] != "sandbox" {
		t.Errorf("sandbox identity label lost: got %v", pod.Labels)
	}
	// The sandbox's own podLabels win over the common value.
	if pod.Labels["cost-centre"] != "sandboxes" {
		t.Errorf("cost-centre: got %q, want the per-workload value", pod.Labels["cost-centre"])
	}
}

// An empty customization renders exactly what the builders produced before this
// existed, so an install that sets nothing sees no change.
func TestBuilders_EmptyCustomizationChangesNothing(t *testing.T) {
	p := &K8sProvisioner{cfg: K8sProvisionerConfig{SandboxGRPCPort: 50051}}
	at := AgentType{ID: "agent-1", Slug: "coder", TenantID: "example"}

	ss := p.buildSessionManagerStatefulSet(at, "tenant-example", agentTypeLabels(at))
	pod := buildTestSandboxPod(p, Run{ID: "run-1"}, "coder", "tenant-example", testSandboxSpec(), "test-exec-token")
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

// createdSandbox provisions run's sandbox over a live sandbox pod and returns the pod the create sent.
func createdSandbox(t *testing.T, p *K8sProvisioner, run Run) *corev1.Pod {
	t.Helper()
	live := buildTestSandboxPod(p, run, "coder", "tenant-example", testSandboxSpec(), "live-token")
	live.Status = corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.7", Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}}
	client := fake.NewSimpleClientset(live, sessionManagerPod("tenant-example"))
	p.client, p.logger = client, logging.NewLoggerWithOutput(io.Discard)
	if _, err := p.ProvisionSandbox(context.Background(), run, "coder", "tenant-example", testSandboxSpec()); err != nil {
		t.Fatal(err)
	}
	for _, a := range client.Actions() {
		if create, ok := a.(k8stesting.CreateAction); ok && a.GetResource().Resource == "pods" {
			return create.GetObject().(*corev1.Pod)
		}
	}
	t.Fatal("no sandbox pod create")
	return nil
}

// A ReadWriteOnce workspace attaches to one node, so the sandbox has to land on
// the node running the session-manager pod that owns it, and the sandbox is
// owned by that pod. Matching node selectors do not deliver that, because a
// pool has many nodes.
func TestProvisionSandbox_CoLocatesOnReadWriteOnceWorkspace(t *testing.T) {
	cfg := splitPoolConfig()
	cfg.Workspace.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
	pod := createdSandbox(t, &K8sProvisioner{cfg: cfg}, Run{ID: "run-1", PodName: "pool-a"})

	if len(pod.OwnerReferences) != 1 || pod.OwnerReferences[0].Kind != "Pod" || pod.OwnerReferences[0].UID != "pool-a-uid" {
		t.Errorf("owners = %+v, want the session-manager pod", pod.OwnerReferences)
	}
	if pod.Spec.Affinity == nil || pod.Spec.Affinity.NodeAffinity == nil || pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		t.Fatal("expected node affinity binding the sandbox to its session-manager's node")
	}
	terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if len(terms) != 1 || len(terms[0].MatchFields) != 1 {
		t.Fatalf("terms = %+v, want one term with one field", terms)
	}
	if f := terms[0].MatchFields[0]; f.Key != "metadata.name" || !slices.Equal(f.Values, []string{"node-a"}) {
		t.Errorf("field = %+v, want metadata.name in [node-a]", f)
	}
}

// ReadWriteMany crosses nodes, which is the whole point of a separate sandbox
// pool, so the sandbox must stay free to schedule away.
func TestProvisionSandbox_NoCoLocationOnReadWriteManyWorkspace(t *testing.T) {
	pod := createdSandbox(t, &K8sProvisioner{cfg: splitPoolConfig()}, Run{ID: "run-1", PodName: "pool-a"})

	if pod.Spec.Affinity != nil && pod.Spec.Affinity.NodeAffinity != nil && pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
		for _, term := range pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
			if len(term.MatchFields) > 0 {
				t.Errorf("ReadWriteMany should not pin the sandbox: got %+v", term)
			}
		}
	}
}

// Configured node affinity is kept: the node requirement joins each configured
// term, so co-location cannot be dropped by setting placement.
func TestRequireCoLocation_KeepsConfiguredAffinity(t *testing.T) {
	spec := corev1.PodSpec{Affinity: &corev1.Affinity{
		NodeAffinity: &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{{
					MatchExpressions: []corev1.NodeSelectorRequirement{{
						Key: "workload-tier", Operator: corev1.NodeSelectorOpIn, Values: []string{"kora"},
					}},
				}},
			},
		},
	}}
	requireCoLocation(&spec, "node-a")

	terms := spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if len(terms) != 1 || len(terms[0].MatchExpressions) != 1 || len(terms[0].MatchFields) != 1 || terms[0].MatchFields[0].Values[0] != "node-a" {
		t.Errorf("terms = %+v, want the configured expression and the node field in one term", terms)
	}
}

// The agent pod has no route to a cloud API, so a ServiceAccount cloud identity
// would be an unusable standing credential. See
// docs/adr/2026-09-16-first-class-workload-placement.md, Amendment 1.
func TestWorkloadCustomization_HasNoServiceAccountSetting(t *testing.T) {
	if _, ok := reflect.TypeFor[WorkloadCustomization]().FieldByName("ServiceAccount"); ok {
		t.Error("WorkloadCustomization must not carry ServiceAccount settings")
	}
}

// The configured memory is both the request and the limit of the
// session-manager container, and an unset value takes the chart default.
func TestBuildStatefulSet_SessionManagerMemoryIsRequestAndLimit(t *testing.T) {
	at := AgentType{ID: "agent-1", Slug: "coder", TenantID: "example"}
	twoGi := resource.MustParse("2Gi")
	for name, tc := range map[string]struct {
		memory *resource.Quantity
		want   string
	}{
		"configured": {&twoGi, "2Gi"},
		"unset":      {nil, "1Gi"},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := splitPoolConfig()
			cfg.Workloads.SessionManager.Memory = tc.memory
			p := &K8sProvisioner{cfg: cfg}

			ss := p.buildSessionManagerStatefulSet(at, "tenant-example", agentTypeLabels(at))

			resources := ss.Spec.Template.Spec.Containers[0].Resources
			want := resource.MustParse(tc.want)
			if got := resources.Requests[corev1.ResourceMemory]; got.Cmp(want) != 0 {
				t.Errorf("memory request = %s, want %s", got.String(), tc.want)
			}
			if got := resources.Limits[corev1.ResourceMemory]; got.Cmp(want) != 0 {
				t.Errorf("memory limit = %s, want %s", got.String(), tc.want)
			}
		})
	}
}

// The configured cpu is the session-manager request, the limit stays 2 CPU, and
// an unset value takes the chart default.
func TestBuildStatefulSet_SessionManagerCPURequest(t *testing.T) {
	at := AgentType{ID: "agent-1", Slug: "coder", TenantID: "example"}
	fifty := resource.MustParse("50m")
	for name, tc := range map[string]struct {
		cpu  *resource.Quantity
		want string
	}{
		"configured": {&fifty, "50m"},
		"unset":      {nil, "250m"},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := splitPoolConfig()
			cfg.Workloads.SessionManager.CPU = tc.cpu
			p := &K8sProvisioner{cfg: cfg}

			ss := p.buildSessionManagerStatefulSet(at, "tenant-example", agentTypeLabels(at))

			resources := ss.Spec.Template.Spec.Containers[0].Resources
			want := resource.MustParse(tc.want)
			if got := resources.Requests[corev1.ResourceCPU]; got.Cmp(want) != 0 {
				t.Errorf("cpu request = %s, want %s", got.String(), tc.want)
			}
			if got, limit := resources.Limits[corev1.ResourceCPU], resource.MustParse("2"); got.Cmp(limit) != 0 {
				t.Errorf("cpu limit = %s, want 2", got.String())
			}
		})
	}
}

// An unset cpu yields 250m, and a configured one is kept.
func TestForSessionManager_CPUDefault(t *testing.T) {
	if got := (WorkloadsConfig{}).ForSessionManager().CPU; got == nil || got.String() != "250m" {
		t.Errorf("unset cpu = %v, want 250m", got)
	}
	cfg := WorkloadsConfig{SessionManager: SessionManagerCustomization{CPU: new(resource.MustParse("50m"))}}
	if got := cfg.ForSessionManager().CPU; got == nil || got.String() != "50m" {
		t.Errorf("configured cpu = %v, want 50m", got)
	}
}

// Unset warm requests yield 25m and 128Mi, and configured ones are kept.
func TestForSessionManager_WarmDefault(t *testing.T) {
	warm := (WorkloadsConfig{}).ForSessionManager().Warm
	if warm.CPU.String() != "25m" || warm.Memory.String() != "128Mi" {
		t.Errorf("unset warm = %v and %v, want 25m and 128Mi", warm.CPU, warm.Memory)
	}
	cfg := WorkloadsConfig{SessionManager: SessionManagerCustomization{Warm: WarmRequests{CPU: new(resource.MustParse("10m")), Memory: new(resource.MustParse("64Mi"))}}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("configured warm requests rejected: %v", err)
	}
	if warm := cfg.ForSessionManager().Warm; warm.CPU.String() != "10m" || warm.Memory.String() != "64Mi" {
		t.Errorf("configured warm = %v and %v, want 10m and 64Mi", warm.CPU, warm.Memory)
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
			agent:   corev1.PodSpec{NodeSelector: map[string]string{"workload-tier": "kora"}},
			sandbox: corev1.PodSpec{Affinity: affinity("kora-sandbox")},
			want:    true,
		},
		"affinity against selector": {
			agent:   corev1.PodSpec{Affinity: affinity("kora")},
			sandbox: corev1.PodSpec{NodeSelector: map[string]string{"workload-tier": "kora-sandbox"}},
			want:    true,
		},
		"identical pinning is one pool": {
			agent:   corev1.PodSpec{NodeSelector: map[string]string{"workload-tier": "kora"}},
			sandbox: corev1.PodSpec{NodeSelector: map[string]string{"workload-tier": "kora"}},
			want:    false,
		},
		"one side unconstrained is inconclusive": {
			agent:   corev1.PodSpec{NodeSelector: map[string]string{"workload-tier": "kora"}},
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
			Sandbox: WorkloadCustomization{PodLabels: map[string]string{"runkora.io/role": "not-a-sandbox"}},
		},
		"selector label on the session-manager": {
			SessionManager: SessionManagerCustomization{WorkloadCustomization: WorkloadCustomization{PodLabels: map[string]string{"app": "hijacked"}}},
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
		"zero session-manager memory": {
			SessionManager: SessionManagerCustomization{Memory: new(resource.MustParse("0"))},
		},
		"negative session-manager memory": {
			SessionManager: SessionManagerCustomization{Memory: new(resource.MustParse("-1Gi"))},
		},
		"zero session-manager cpu": {
			SessionManager: SessionManagerCustomization{CPU: new(resource.MustParse("0"))},
		},
		"session-manager cpu above the limit": {
			SessionManager: SessionManagerCustomization{CPU: new(resource.MustParse("2100m"))},
		},
		"warm memory at the full memory": {
			SessionManager: SessionManagerCustomization{Warm: WarmRequests{Memory: new(resource.MustParse("1Gi"))}},
		},
		"zero warm memory": {
			SessionManager: SessionManagerCustomization{Warm: WarmRequests{Memory: new(resource.MustParse("0"))}},
		},
		"warm cpu above the full cpu": {
			SessionManager: SessionManagerCustomization{Warm: WarmRequests{CPU: new(resource.MustParse("300m"))}},
		},
		"zero warm cpu": {
			SessionManager: SessionManagerCustomization{Warm: WarmRequests{CPU: new(resource.MustParse("0"))}},
		},
		"istio interception opt-out": {
			SessionManager: SessionManagerCustomization{WorkloadCustomization: WorkloadCustomization{PodAnnotations: map[string]string{"sidecar.istio.io/interceptionMode": "NONE"}}},
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
