package agents

import (
	"context"
	"encoding/json"
	"github.com/runkora/kora/backend/internal/platform/brand"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/yaml"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// podSecurityProvisioner builds a provisioner with the config a hardened
// install would carry, exercising every field the rendered specs use.
func podSecurityProvisioner() *K8sProvisioner {
	return &K8sProvisioner{
		logger: logging.NewLoggerWithOutput(io.Discard),
		cfg: K8sProvisionerConfig{
			SessionManagerImage: "ghcr.io/runkora/kora-session-manager:1.0.0",
			SandboxImage:        "ghcr.io/runkora/kora-sandbox-base:1.0.0",
			SandboxGRPCPort:     50051,
			ImagePullSecrets:    []string{"ghcr-pull", "mirror-pull"},
			TrustDomain:         "example.com",
			SystemNamespace:     "kora-prod",
			OTLPEndpoint:        "http://otel-collector.observability.svc:4317",
			SamplingRate:        "1.0",
			ProductName:         "Acme",
		},
	}
}

func renderedSandboxPod() *corev1.Pod {
	return buildTestSandboxPod(podSecurityProvisioner(), Run{ID: "run-1", PodOrdinal: 0}, "coder", "kora-tenant-acme", testSandboxSpec(), "test-exec-token")
}

func renderedStatefulSet() appsv1.StatefulSet {
	at := AgentType{Slug: "coder", TenantID: "acme"}
	return podSecurityProvisioner().buildSessionManagerStatefulSet(at, "kora-tenant-acme", podLabels(at))
}

// assertRestrictedPodSpec checks the parts of the restricted Pod Security
// Standard that kora sets itself; the remaining fields are Kubernetes defaults.
func assertRestrictedPodSpec(t *testing.T, spec corev1.PodSpec) {
	t.Helper()
	sc := spec.SecurityContext
	if sc == nil {
		t.Fatal("pod securityContext missing")
	}
	if sc.RunAsNonRoot == nil || !*sc.RunAsNonRoot {
		t.Error("pod runAsNonRoot must be true")
	}
	if sc.SeccompProfile == nil || sc.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Errorf("pod seccompProfile = %+v, want RuntimeDefault", sc.SeccompProfile)
	}
	if sc.SELinuxOptions == nil || sc.SELinuxOptions.Level != "s0" {
		t.Errorf("pod seLinuxOptions = %+v, want level s0 kept", sc.SELinuxOptions)
	}
	// Without fsGroup the workspace claim is root-owned on storage classes that
	// format volumes, and every run fails to create its workspace directory.
	if sc.FSGroup == nil || *sc.FSGroup != 100 {
		t.Errorf("pod fsGroup = %v, want 100", sc.FSGroup)
	}
	if sc.FSGroupChangePolicy == nil || *sc.FSGroupChangePolicy != corev1.FSGroupChangeOnRootMismatch {
		t.Errorf("pod fsGroupChangePolicy = %v, want OnRootMismatch", sc.FSGroupChangePolicy)
	}
	if sc.RunAsGroup == nil || *sc.RunAsGroup != 100 {
		t.Errorf("pod runAsGroup = %v, want 100", sc.RunAsGroup)
	}
	for _, c := range spec.Containers {
		csc := c.SecurityContext
		if csc == nil {
			t.Errorf("container %s: securityContext missing", c.Name)
			continue
		}
		if csc.AllowPrivilegeEscalation == nil || *csc.AllowPrivilegeEscalation {
			t.Errorf("container %s: allowPrivilegeEscalation must be false", c.Name)
		}
		if csc.Capabilities == nil || len(csc.Capabilities.Drop) != 1 || csc.Capabilities.Drop[0] != "ALL" {
			t.Errorf("container %s: capabilities.drop = %+v, want [ALL]", c.Name, csc.Capabilities)
		}
		if csc.SeccompProfile != nil {
			t.Errorf("container %s: must inherit the pod seccomp profile, not override it", c.Name)
		}
		if csc.ReadOnlyRootFilesystem == nil || !*csc.ReadOnlyRootFilesystem {
			t.Errorf("container %s: readOnlyRootFilesystem must be true", c.Name)
		}
	}
	want := []corev1.LocalObjectReference{{Name: "ghcr-pull"}, {Name: "mirror-pull"}}
	if len(spec.ImagePullSecrets) != len(want) {
		t.Fatalf("imagePullSecrets = %+v, want %+v", spec.ImagePullSecrets, want)
	}
	for i := range want {
		if spec.ImagePullSecrets[i] != want[i] {
			t.Errorf("imagePullSecrets[%d] = %+v, want %+v", i, spec.ImagePullSecrets[i], want[i])
		}
	}
}

func TestSandboxPod_RestrictedPodSecurity(t *testing.T) {
	pod := renderedSandboxPod()
	assertRestrictedPodSpec(t, pod.Spec)
	if pod.Spec.SecurityContext.RunAsUser == nil || *pod.Spec.SecurityContext.RunAsUser != 100 {
		t.Errorf("sandbox runAsUser = %v, want 100", pod.Spec.SecurityContext.RunAsUser)
	}
}

func TestSessionManagerStatefulSet_RestrictedPodSecurity(t *testing.T) {
	ss := renderedStatefulSet()
	assertRestrictedPodSpec(t, ss.Spec.Template.Spec)
}

// Neither runtime pod calls the Kubernetes API or reads service links.
// Both would hand an agent a token or a map of the tenant namespace.
func TestRuntimePods_NoServiceAccountTokenOrServiceLinks(t *testing.T) {
	for name, spec := range map[string]corev1.PodSpec{
		"sandbox":     renderedSandboxPod().Spec,
		"statefulset": renderedStatefulSet().Spec.Template.Spec,
	} {
		t.Run(name, func(t *testing.T) {
			if spec.AutomountServiceAccountToken == nil || *spec.AutomountServiceAccountToken {
				t.Errorf("automountServiceAccountToken = %v, want false", spec.AutomountServiceAccountToken)
			}
			if spec.EnableServiceLinks == nil || *spec.EnableServiceLinks {
				t.Errorf("enableServiceLinks = %v, want false", spec.EnableServiceLinks)
			}
		})
	}
}

// A read-only root needs writable scratch paths, and every node-disk write must be capped
// so one pod cannot fill a node.
func TestRuntimePods_BoundedScratchStorage(t *testing.T) {
	for name, tc := range map[string]struct {
		spec  corev1.PodSpec
		paths []string
	}{
		"sandbox":     {renderedSandboxPod().Spec, []string{"/var/tmp", "/home/kora"}},
		"statefulset": {renderedStatefulSet().Spec.Template.Spec, []string{"/tmp", "/home/kora"}},
	} {
		t.Run(name, func(t *testing.T) {
			c := tc.spec.Containers[0]
			for _, rl := range []corev1.ResourceList{c.Resources.Requests, c.Resources.Limits} {
				if _, ok := rl[corev1.ResourceEphemeralStorage]; !ok {
					t.Errorf("container %s: ephemeral-storage missing from %v", c.Name, rl)
				}
			}
			emptyDirs := map[string]*corev1.EmptyDirVolumeSource{}
			for _, v := range tc.spec.Volumes {
				if v.EmptyDir != nil {
					emptyDirs[v.Name] = v.EmptyDir
				}
			}
			mounted := map[string]bool{}
			for _, m := range c.VolumeMounts {
				dir, ok := emptyDirs[m.Name]
				if !ok {
					continue
				}
				mounted[m.MountPath] = true
				if dir.SizeLimit == nil || dir.SizeLimit.IsZero() {
					t.Errorf("scratch volume %s at %s has no sizeLimit", m.Name, m.MountPath)
				}
			}
			for _, p := range tc.paths {
				if !mounted[p] {
					t.Errorf("no scratch volume mounted at %s", p)
				}
			}
		})
	}
}

func TestSessionManagerStatefulSet_UsesConfiguredSystemNamespace(t *testing.T) {
	ss := renderedStatefulSet()
	env := map[string]string{}
	for _, e := range ss.Spec.Template.Spec.Containers[0].Env {
		env[e.Name] = e.Value
	}
	if got, want := env["ANTHROPIC_BASE_URL"], "http://kora-inference-gateway.kora-prod.svc.cluster.local"; got != want {
		t.Errorf("ANTHROPIC_BASE_URL = %q, want %q", got, want)
	}
	if got, want := env["KORA_AGENTS_URL"], "http://kora-agents.kora-prod.svc.cluster.local"; got != want {
		t.Errorf("KORA_AGENTS_URL = %q, want %q", got, want)
	}
	if got, want := env["KORA_INFERENCE_GATEWAY_URL"], "http://kora-inference-gateway.kora-prod.svc.cluster.local"; got != want {
		t.Errorf("KORA_INFERENCE_GATEWAY_URL = %q, want %q", got, want)
	}
	if got, want := env["KORA_TOOL_GATEWAY_URL"], "http://kora-tool-gateway.kora-prod.svc.cluster.local"; got != want {
		t.Errorf("KORA_TOOL_GATEWAY_URL = %q, want %q", got, want)
	}
	// Without this a provisioned agent traces nothing, and the only symptom is
	// an absence of spans.
	if got, want := env["KORA_OTLP_ENDPOINT"], "http://otel-collector.observability.svc:4317"; got != want {
		t.Errorf("KORA_OTLP_ENDPOINT = %q, want %q", got, want)
	}
	// Without this a provisioned agent samples at its image default, so its
	// traces are dropped at a different rate than the platform's.
	if got, want := env["KORA_SAMPLING_RATE"], "1.0"; got != want {
		t.Errorf("KORA_SAMPLING_RATE = %q, want %q", got, want)
	}
	// The env var is the only way the product name reaches a session pod.
	if got, want := env[brand.EnvProductName], "Acme"; got != want {
		t.Errorf("%s = %q, want %q", brand.EnvProductName, got, want)
	}
}

// The session-manager caps the Node heap of its runs from this value, so an
// install's setting must reach every agent pod.
func TestSessionManagerStatefulSet_PassesNodeHeapCap(t *testing.T) {
	p := podSecurityProvisioner()
	p.cfg.NodeMaxOldSpaceMB = 768
	at := AgentType{Slug: "coder", TenantID: "acme"}
	ss := p.buildSessionManagerStatefulSet(at, "ns", podLabels(at))
	for _, e := range ss.Spec.Template.Spec.Containers[0].Env {
		if e.Name == "KORA_NODE_MAX_OLD_SPACE_MB" {
			if e.Value != "768" {
				t.Errorf("KORA_NODE_MAX_OLD_SPACE_MB = %q, want 768", e.Value)
			}
			return
		}
	}
	t.Error("session-manager env is missing KORA_NODE_MAX_OLD_SPACE_MB")
}

// The install's Landlock mode must reach every agent pod.
func TestSessionManagerStatefulSet_PassesLandlockMode(t *testing.T) {
	p := podSecurityProvisioner()
	p.cfg.Landlock = "bestEffort"
	at := AgentType{Slug: "coder", TenantID: "acme"}
	ss := p.buildSessionManagerStatefulSet(at, "ns", podLabels(at))
	for _, e := range ss.Spec.Template.Spec.Containers[0].Env {
		if e.Name == "KORA_LANDLOCK" {
			if e.Value != "bestEffort" {
				t.Errorf("KORA_LANDLOCK = %q, want bestEffort", e.Value)
			}
			return
		}
	}
	t.Error("session-manager env is missing KORA_LANDLOCK")
}

func TestRuntimePods_NoPullSecretsWhenUnconfigured(t *testing.T) {
	p := podSecurityProvisioner()
	p.cfg.ImagePullSecrets = nil
	if pod := buildTestSandboxPod(p, Run{ID: "run-1"}, "coder", "ns", testSandboxSpec(), "test-exec-token"); pod.Spec.ImagePullSecrets != nil {
		t.Errorf("sandbox imagePullSecrets = %+v, want field omitted", pod.Spec.ImagePullSecrets)
	}
	at := AgentType{Slug: "coder", TenantID: "acme"}
	if ss := p.buildSessionManagerStatefulSet(at, "ns", podLabels(at)); ss.Spec.Template.Spec.ImagePullSecrets != nil {
		t.Errorf("statefulset imagePullSecrets = %+v, want field omitted", ss.Spec.Template.Spec.ImagePullSecrets)
	}
}

// TestEnsureStatefulSet_ExistingDropsStaleTemplateAndEnv covers upgrading an
// agent created before the operator webhook took over injection: the istiod
// template annotation must go and the rendered env must replace the old one,
// while the digest-pinned image is left alone.
func TestEnsureStatefulSet_ExistingDropsStaleTemplateAndEnv(t *testing.T) {
	at := AgentType{Slug: "coder", TenantID: "acme"}
	const ns = "kora-tenant-acme"
	stale := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: at.Slug, Namespace: ns, Annotations: map[string]string{annotationImagePinned: "true"}},
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
					"inject.istio.io/templates": "sidecar,spire,identity-proxy",
				}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  sessionManagerContainerName,
						Image: "old",
						Env:   []corev1.EnvVar{{Name: "ANTHROPIC_BASE_URL", Value: "http://kora-inference-gateway.kora-system.svc.cluster.local"}},
					}},
				},
			},
		},
	}
	p := podSecurityProvisioner()
	p.client = fake.NewSimpleClientset(stale)
	if err := p.ensureStatefulSet(context.Background(), at, ns, podLabels(at), metav1.OwnerReference{}, true); err != nil {
		t.Fatalf("ensureStatefulSet: %v", err)
	}
	got, err := p.client.AppsV1().StatefulSets(ns).Get(context.Background(), at.Slug, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if _, ok := got.Spec.Template.Annotations["inject.istio.io/templates"]; ok {
		t.Error("stale inject.istio.io/templates annotation survived the update")
	}
	c := got.Spec.Template.Spec.Containers[0]
	if c.Image != "old" {
		t.Error("update path must not replace the pinned image")
	}
	var base string
	for _, e := range c.Env {
		if e.Name == "ANTHROPIC_BASE_URL" {
			base = e.Value
		}
	}
	if base != "http://kora-inference-gateway.kora-prod.svc.cluster.local" {
		t.Errorf("ANTHROPIC_BASE_URL not synced to the configured system namespace: %q", base)
	}
}

// TestEnsureStatefulSet_ExistingPicksUpHardening covers the re-provision path:
// an agent StatefulSet created before this hardening must receive the pod and
// container contexts and pull secrets without a manual delete.
func TestEnsureStatefulSet_ExistingPicksUpHardening(t *testing.T) {
	at := AgentType{Slug: "coder", TenantID: "acme"}
	const ns = "kora-tenant-acme"
	stale := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: at.Slug, Namespace: ns, Annotations: map[string]string{annotationImagePinned: "true"}},
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: sessionManagerContainerName, Image: "old"}},
				},
			},
		},
	}
	p := podSecurityProvisioner()
	p.client = fake.NewSimpleClientset(stale)

	if err := p.ensureStatefulSet(context.Background(), at, ns, podLabels(at), metav1.OwnerReference{}, true); err != nil {
		t.Fatalf("ensureStatefulSet: %v", err)
	}
	got, err := p.client.AppsV1().StatefulSets(ns).Get(context.Background(), at.Slug, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	assertRestrictedPodSpec(t, got.Spec.Template.Spec)
	if got.Spec.Template.Spec.Containers[0].Image != "old" {
		t.Error("re-provision must only sync security fields, not replace the container")
	}
}

// Run start creates a missing StatefulSet and leaves an existing one alone, so starting a run never rolls pods.
func TestEnsureStatefulSet_CreateOnly(t *testing.T) {
	at := AgentType{Slug: "coder", TenantID: "acme"}
	const ns = "kora-tenant-acme"
	stale := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: at.Slug, Namespace: ns},
		Spec: appsv1.StatefulSetSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: sessionManagerContainerName, Image: "old"}},
		}}},
	}
	p := podSecurityProvisioner()
	p.client = fake.NewSimpleClientset(stale)

	if err := p.ensureStatefulSet(context.Background(), at, ns, podLabels(at), metav1.OwnerReference{}, false); err != nil {
		t.Fatalf("ensureStatefulSet: %v", err)
	}
	got, err := p.client.AppsV1().StatefulSets(ns).Get(context.Background(), at.Slug, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Spec.Template.Spec.SecurityContext != nil || got.Spec.Template.Spec.Containers[0].Image != "old" {
		t.Error("a create-only provision changed an existing StatefulSet")
	}

	other := AgentType{Slug: "fresh", TenantID: "acme"}
	if err := p.ensureStatefulSet(context.Background(), other, ns, podLabels(other), metav1.OwnerReference{}, false); err != nil {
		t.Fatalf("ensureStatefulSet: %v", err)
	}
	if _, err := p.client.AppsV1().StatefulSets(ns).Get(context.Background(), other.Slug, metav1.GetOptions{}); err != nil {
		t.Errorf("a create-only provision did not create a missing StatefulSet: %v", err)
	}
}

// TestRuntimePods_RenderYAML marshals both runtime-created pod specs. With
// KORA_RENDER_POD_SPECS_DIR set it writes them out so CI can evaluate the
// files against the pod-security restricted policy set.
func TestRuntimePods_RenderYAML(t *testing.T) {
	pod := renderedSandboxPod()
	pod.TypeMeta = metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"}
	ss := renderedStatefulSet()
	ss.TypeMeta = metav1.TypeMeta{APIVersion: "apps/v1", Kind: "StatefulSet"}

	docs := map[string]any{
		"sandbox-pod.yaml":                 pod,
		"session-manager-statefulset.yaml": ss,
	}
	dir := os.Getenv("KORA_RENDER_POD_SPECS_DIR")
	for name, obj := range docs {
		out, err := yaml.Marshal(obj)
		if err != nil {
			t.Fatalf("marshal %s: %v", name, err)
		}
		if dir == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), out, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

// TestRuntimePods_NoIstioTemplatesAnnotation guards against naming istiod
// injection templates; Istio's injector fails the pod when one is missing. The operator webhook wires SPIRE and identity-proxy.
func TestRuntimePods_NoIstioTemplatesAnnotation(t *testing.T) {
	const key = "inject.istio.io/templates"
	if _, ok := renderedSandboxPod().Annotations[key]; ok {
		t.Errorf("sandbox pod must not set %s", key)
	}
	ss := renderedStatefulSet()
	if _, ok := ss.Spec.Template.Annotations[key]; ok {
		t.Errorf("session-manager pod template must not set %s", key)
	}
	if ss.Spec.Template.Annotations["proxy.istio.io/config"] == "" || ss.Spec.Template.Annotations["traffic.sidecar.istio.io/excludeOutboundPorts"] == "" {
		t.Error("proxy.istio.io/config and excludeOutboundPorts must stay")
	}
}

// The rendered templates carry IN_MESH so a reconcile rolls pods created before
// the operator webhook enforced it.
func TestRuntimePods_PeerMetadataInMeshOnly(t *testing.T) {
	for name, ann := range map[string]map[string]string{
		"sandbox pod":     renderedSandboxPod().Annotations,
		"session-manager": renderedStatefulSet().Spec.Template.Annotations,
	} {
		var cfg struct {
			Hold         bool `json:"holdApplicationUntilProxyStarts"`
			ProxyHeaders struct {
				MetadataExchangeHeaders struct {
					Mode string `json:"mode"`
				} `json:"metadataExchangeHeaders"`
			} `json:"proxyHeaders"`
		}
		if err := json.Unmarshal([]byte(ann["proxy.istio.io/config"]), &cfg); err != nil {
			t.Fatalf("%s proxy config: %v", name, err)
		}
		if !cfg.Hold || cfg.ProxyHeaders.MetadataExchangeHeaders.Mode != "IN_MESH" {
			t.Errorf("%s proxy config = %+v", name, cfg)
		}
	}
}

// An unpinned StatefulSet moves to the rendered session-manager image on
// reconcile, so a chart upgrade reaches existing agents.
func TestEnsureStatefulSet_UnpinnedImageFollowsRelease(t *testing.T) {
	at := AgentType{Slug: "coder", TenantID: "acme"}
	const ns = "kora-tenant-acme"
	stale := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: at.Slug, Namespace: ns},
		Spec: appsv1.StatefulSetSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: sessionManagerContainerName, Image: "old"}},
		}}},
	}
	p := podSecurityProvisioner()
	p.client = fake.NewSimpleClientset(stale)
	if err := p.ensureStatefulSet(context.Background(), at, ns, podLabels(at), metav1.OwnerReference{}, true); err != nil {
		t.Fatalf("ensureStatefulSet: %v", err)
	}
	got, err := p.client.AppsV1().StatefulSets(ns).Get(context.Background(), at.Slug, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if img := got.Spec.Template.Spec.Containers[0].Image; img == "old" {
		t.Errorf("unpinned image stayed %q; want the rendered image", img)
	}
}

func TestReadyOrdinals(t *testing.T) {
	const ns, slug = "kora-tenant-acme", "coder"
	sel := map[string]string{"app": slug}
	ss := func(gen, observed int64, rev string) *appsv1.StatefulSet {
		return &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: slug, Namespace: ns, Generation: gen},
			Spec:       appsv1.StatefulSetSpec{Selector: &metav1.LabelSelector{MatchLabels: sel}},
			Status:     appsv1.StatefulSetStatus{ObservedGeneration: observed, UpdateRevision: rev},
		}
	}
	pod := func(name, rev string, ready, deleting bool) *corev1.Pod {
		st := corev1.ConditionFalse
		if ready {
			st = corev1.ConditionTrue
		}
		p := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: map[string]string{
				"app": slug, appsv1.ControllerRevisionHashLabelKey: rev,
			}},
			Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: st}}},
		}
		if deleting {
			now := metav1.Now()
			p.DeletionTimestamp = &now
		}
		return p
	}
	for _, tc := range []struct {
		name string
		objs []runtime.Object
		want []int
	}{
		{"no statefulset", nil, nil},
		{"roll not yet observed", []runtime.Object{ss(2, 1, "r2"), pod("coder-0", "r1", true, false)}, []int{0}},
		{"not ready", []runtime.Object{ss(1, 1, "r1"), pod("coder-0", "r1", false, false)}, nil},
		{"old revision during a roll", []runtime.Object{ss(2, 2, "r2"), pod("coder-0", "r1", true, false), pod("coder-1", "r2", false, false)}, []int{0}},
		{"terminating", []runtime.Object{ss(1, 1, "r1"), pod("coder-0", "r1", true, true)}, nil},
		{"ready current", []runtime.Object{ss(1, 1, "r1"), pod("coder-1", "r1", true, false), pod("coder-0", "r1", true, false), pod("coder-2", "r1", false, false)}, []int{0, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := podSecurityProvisioner()
			p.client = fake.NewSimpleClientset(tc.objs...)
			got, err := p.ReadyOrdinals(context.Background(), slug, ns)
			if err != nil {
				t.Fatalf("ReadyOrdinals: %v", err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
