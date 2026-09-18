package agents

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/yaml"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// podSecurityProvisioner builds a provisioner with the config a hardened
// install would carry, exercising every field the rendered specs use.
func podSecurityProvisioner() *K8sProvisioner {
	return &K8sProvisioner{
		logger: logging.NewLoggerWithOutput(io.Discard),
		cfg: K8sProvisionerConfig{
			SessionManagerImage: "ghcr.io/clear-route/orbit-session-manager:1.0.0",
			SandboxImage:        "ghcr.io/clear-route/orbit-sandbox-exec:1.0.0",
			SandboxGRPCPort:     50051,
			ImagePullSecrets:    []string{"ghcr-pull", "mirror-pull"},
			TrustDomain:         "orbit.example.com",
			SystemNamespace:     "orbit-prod",
			OTLPEndpoint:        "http://otel-collector.observability.svc:4317",
		},
	}
}

func renderedSandboxPod() *corev1.Pod {
	return podSecurityProvisioner().buildSandboxPod(Run{ID: "run-1", PodOrdinal: 0}, "coder", "orbit-tenant-acme", nil, "test-exec-token")
}

func renderedStatefulSet() appsv1.StatefulSet {
	at := AgentType{Slug: "coder", TenantID: "acme"}
	return podSecurityProvisioner().buildSessionManagerStatefulSet(at, "orbit-tenant-acme", podLabels(at))
}

// assertRestrictedPodSpec checks the parts of the restricted Pod Security
// Standard that Orbit sets itself; the remaining fields are Kubernetes defaults.
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
	c := ss.Spec.Template.Spec.Containers[0]
	if c.SecurityContext.ReadOnlyRootFilesystem != nil {
		t.Error("session-manager must not set readOnlyRootFilesystem; the agent writes to its home directory")
	}
}

func TestSessionManagerStatefulSet_UsesConfiguredSystemNamespace(t *testing.T) {
	ss := renderedStatefulSet()
	env := map[string]string{}
	for _, e := range ss.Spec.Template.Spec.Containers[0].Env {
		env[e.Name] = e.Value
	}
	if got, want := env["ANTHROPIC_BASE_URL"], "http://orbit-inference-gateway.orbit-prod.svc.cluster.local"; got != want {
		t.Errorf("ANTHROPIC_BASE_URL = %q, want %q", got, want)
	}
	if got, want := env["ORBIT_AGENTS_URL"], "http://orbit-agents.orbit-prod.svc.cluster.local"; got != want {
		t.Errorf("ORBIT_AGENTS_URL = %q, want %q", got, want)
	}
	// Without this a provisioned agent traces nothing, and the only symptom is
	// an absence of spans.
	if got, want := env["ORBIT_OTLP_ENDPOINT"], "http://otel-collector.observability.svc:4317"; got != want {
		t.Errorf("ORBIT_OTLP_ENDPOINT = %q, want %q", got, want)
	}
}

func TestRuntimePods_NoPullSecretsWhenUnconfigured(t *testing.T) {
	p := podSecurityProvisioner()
	p.cfg.ImagePullSecrets = nil
	if pod := p.buildSandboxPod(Run{ID: "run-1"}, "coder", "ns", nil, "test-exec-token"); pod.Spec.ImagePullSecrets != nil {
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
	const ns = "orbit-tenant-acme"
	stale := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: at.Slug, Namespace: ns},
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
					"inject.istio.io/templates": "sidecar,spire,identity-proxy",
				}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  sessionManagerContainerName,
						Image: "old",
						Env:   []corev1.EnvVar{{Name: "ANTHROPIC_BASE_URL", Value: "http://orbit-inference-gateway.orbit-system.svc.cluster.local"}},
					}},
				},
			},
		},
	}
	p := podSecurityProvisioner()
	p.client = fake.NewSimpleClientset(stale)
	if err := p.ensureStatefulSet(context.Background(), at, ns, podLabels(at), metav1.OwnerReference{}); err != nil {
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
	if base != "http://orbit-inference-gateway.orbit-prod.svc.cluster.local" {
		t.Errorf("ANTHROPIC_BASE_URL not synced to the configured system namespace: %q", base)
	}
}

// TestEnsureStatefulSet_ExistingPicksUpHardening covers the re-provision path:
// an agent StatefulSet created before this hardening must receive the pod and
// container contexts and pull secrets without a manual delete.
func TestEnsureStatefulSet_ExistingPicksUpHardening(t *testing.T) {
	at := AgentType{Slug: "coder", TenantID: "acme"}
	const ns = "orbit-tenant-acme"
	stale := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: at.Slug, Namespace: ns},
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

	if err := p.ensureStatefulSet(context.Background(), at, ns, podLabels(at), metav1.OwnerReference{}); err != nil {
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

// TestRuntimePods_RenderYAML marshals both runtime-created pod specs. With
// ORBIT_RENDER_POD_SPECS_DIR set it writes them out so CI can evaluate the
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
	dir := os.Getenv("ORBIT_RENDER_POD_SPECS_DIR")
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
