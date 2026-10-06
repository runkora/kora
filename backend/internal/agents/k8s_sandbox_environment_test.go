package agents

import (
	"context"
	"errors"
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/clear-route/orbit/internal/agents/sandbox/runfs"
)

// testSandboxSpec is the built-in default environment on the base image.
func testSandboxSpec() SandboxSpec {
	return SandboxSpec{
		Environment: DefaultEnvironmentName,
		Image:       "ghcr.io/clear-route/kora-sandbox-base@sha256:base",
		Size:        testSizes.Default,
	}
}

// The sandbox runs the environment's pinned image and size with sandbox-exec as
// its command. Its local disk sits at the run's local dir.
func TestSandboxPod_FromEnvironment(t *testing.T) {
	spec := SandboxSpec{
		EnvironmentID: "env-go",
		Environment:   "go",
		Image:         "ghcr.io/acme/go@sha256:abc",
		PullSecret:    "acme-pull",
		Size:          EnvironmentSize{CPU: "4", Memory: "8Gi", Disk: "20Gi"},
	}
	pod := buildTestSandboxPod(podSecurityProvisioner(), Run{ID: "run-1"}, "coder", "orbit-tenant-acme", spec, "token")
	c := pod.Spec.Containers[0]

	if c.Image != spec.Image || c.ImagePullPolicy != corev1.PullIfNotPresent {
		t.Errorf("image %s with %s, want %s with IfNotPresent", c.Image, c.ImagePullPolicy, spec.Image)
	}
	if !slices.Equal(c.Command, []string{"/usr/local/bin/sandbox-exec"}) {
		t.Errorf("command = %v, want sandbox-exec", c.Command)
	}
	for name, want := range map[corev1.ResourceName]string{
		corev1.ResourceCPU:              "4",
		corev1.ResourceMemory:           "8Gi",
		corev1.ResourceEphemeralStorage: "21Gi",
	} {
		got := c.Resources.Limits[name]
		if got.Cmp(resource.MustParse(want)) != 0 {
			t.Errorf("limit %s = %s, want %s", name, got.String(), want)
		}
	}
	if pod.Annotations[annotationEnvironmentID] != "env-go" || pod.Annotations[annotationEnvironment] != "go" {
		t.Errorf("annotations = %v, want the environment", pod.Annotations)
	}
	if !slices.Contains(pod.Spec.ImagePullSecrets, corev1.LocalObjectReference{Name: "acme-pull"}) {
		t.Errorf("pull secrets = %v, want the image's own", pod.Spec.ImagePullSecrets)
	}

	var local *corev1.Volume
	for i := range pod.Spec.Volumes {
		if pod.Spec.Volumes[i].Name == localDiskVolume {
			local = &pod.Spec.Volumes[i]
		}
	}
	if local == nil || local.EmptyDir == nil || local.EmptyDir.SizeLimit.Cmp(resource.MustParse("20Gi")) != 0 {
		t.Fatalf("local disk volume = %+v, want a 20Gi emptyDir", local)
	}
	if !slices.ContainsFunc(c.VolumeMounts, func(m corev1.VolumeMount) bool {
		return m.Name == localDiskVolume && m.MountPath == runfs.LocalDir("/workspaces/run-1")
	}) {
		t.Errorf("mounts = %+v, want the local disk at the run's local dir", c.VolumeMounts)
	}
}

// buildTestSandboxPod builds a sandbox pod from a spec the test knows is valid.
func buildTestSandboxPod(p *K8sProvisioner, run Run, slug, namespace string, spec SandboxSpec, token string) *corev1.Pod {
	pod, err := p.buildSandboxPod(run, slug, namespace, spec, token)
	if err != nil {
		panic(err)
	}
	return pod
}

// Requests stay fixed and do not exceed a small environment's limits.
func TestSandboxResources_RequestsWithinLimits(t *testing.T) {
	rr, err := sandboxResources(EnvironmentSize{CPU: "50m", Memory: "128Mi", Disk: "1Gi"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory} {
		req, lim := rr.Requests[name], rr.Limits[name]
		if req.Cmp(lim) > 0 {
			t.Errorf("%s request %s above limit %s", name, req.String(), lim.String())
		}
	}
	rr, _ = sandboxResources(testSizes.Default)
	if cpu := rr.Requests[corev1.ResourceCPU]; cpu.String() != "100m" {
		t.Errorf("cpu request = %s, want the fixed 100m", cpu.String())
	}
}

// A size that is not a quantity is an error.
func TestSandboxPod_InvalidSizeIsAnError(t *testing.T) {
	for _, size := range []EnvironmentSize{
		{CPU: "lots", Memory: "1Gi", Disk: "1Gi"},
		{CPU: "1", Memory: "1Gi", Disk: "big"},
	} {
		spec := testSandboxSpec()
		spec.Size = size
		if _, err := podSecurityProvisioner().buildSandboxPod(Run{ID: "run-1"}, "coder", "ns", spec, "token"); !errors.Is(err, errSandboxSize) {
			t.Errorf("size %+v: err = %v, want errSandboxSize", size, err)
		}
	}
}

// A live sandbox in one environment is reused for that environment and refused
// for another, so only replace_environment switches it.
func TestProvisionSandbox_LivePodInOtherEnvironment(t *testing.T) {
	node := SandboxSpec{EnvironmentID: "env-node", Environment: "node", Image: "ghcr.io/acme/node@sha256:abc", Size: testSizes.Default}
	goSpec := SandboxSpec{EnvironmentID: "env-go", Environment: "go", Image: "ghcr.io/acme/go@sha256:abc", Size: testSizes.Default}
	run := Run{ID: "run-1"}

	live := buildTestSandboxPod(podSecurityProvisioner(), run, "coder", "ns", node, "live-token")
	live.Status = corev1.PodStatus{
		Phase:      corev1.PodRunning,
		PodIP:      "10.0.0.7",
		Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
	}
	p := podSecurityProvisioner()
	p.client = fake.NewSimpleClientset(live)

	if _, err := p.ProvisionSandbox(context.Background(), run, "coder", "ns", goSpec); !errors.Is(err, ErrEnvironmentSwitch) {
		t.Errorf("provision into go = %v, want ErrEnvironmentSwitch", err)
	}
	sb, err := p.ProvisionSandbox(context.Background(), run, "coder", "ns", node)
	if err != nil || sb.ExecToken != "live-token" || sb.Created {
		t.Errorf("provision into node = %+v, %v; want the live pod reused", sb, err)
	}
}
