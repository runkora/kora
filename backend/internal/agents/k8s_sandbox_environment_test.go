package agents

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/runkora/kora/backend/internal/agents/sandbox/runfs"
)

// testSandboxSpec is the built-in default environment on the base image.
func testSandboxSpec() SandboxSpec {
	return SandboxSpec{
		Environment: DefaultEnvironmentName,
		Image:       "ghcr.io/runkora/kora-sandbox-base@sha256:base",
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
		return m.Name == localDiskVolume && m.MountPath == runfs.LocalDir("/workspaces/run-1") && m.SubPath == "home"
	}) {
		t.Errorf("mounts = %+v, want the local disk's home directory at the run's local dir", c.VolumeMounts)
	}
	// /tmp is the sandbox's TMPDIR, on the local disk rather than the shared workspace volume.
	if !slices.ContainsFunc(c.VolumeMounts, func(m corev1.VolumeMount) bool {
		return m.Name == localDiskVolume && m.MountPath == "/tmp" && m.SubPath == ".tmp"
	}) {
		t.Errorf("mounts = %+v, want /tmp on the local disk", c.VolumeMounts)
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

// The sandbox reserves its size: requests equal limits, and ephemeral storage adds the scratch allowance to the disk.
func TestSandboxResources_RequestsEqualLimits(t *testing.T) {
	for _, tc := range []struct {
		size                       EnvironmentSize
		cpu, memory, ephemeralWant string
	}{
		{testSizes.Default, "2", "4Gi", "11Gi"},
		{EnvironmentSize{CPU: "50m", Memory: "128Mi", Disk: "1Gi"}, "50m", "128Mi", "2Gi"},
		{EnvironmentSize{CPU: "2500m", Memory: "12Gi", Disk: "15Gi"}, "2500m", "12Gi", "16Gi"},
	} {
		rr, err := sandboxResources(tc.size)
		if err != nil {
			t.Fatalf("size %+v: %v", tc.size, err)
		}
		want := map[corev1.ResourceName]string{
			corev1.ResourceCPU:              tc.cpu,
			corev1.ResourceMemory:           tc.memory,
			corev1.ResourceEphemeralStorage: tc.ephemeralWant,
		}
		for name, w := range want {
			wq := resource.MustParse(w)
			req, lim := rr.Requests[name], rr.Limits[name]
			if lim.Cmp(wq) != 0 {
				t.Errorf("size %+v: %s limit = %s, want %s", tc.size, name, lim.String(), w)
			}
			if req.Cmp(lim) != 0 {
				t.Errorf("size %+v: %s request = %s, want it equal to the limit %s", tc.size, name, req.String(), lim.String())
			}
		}
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

// A live sandbox is reused for its own environment and refused for another.
// Only replace_environment switches it.
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

// A wait that ends on a pod the scheduler cannot place reports the scheduler's reason.
func TestWaitForPodReady_UnschedulableReportsReason(t *testing.T) {
	pending := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "sandbox-run-1", Namespace: "ns"},
		Status: corev1.PodStatus{Phase: corev1.PodPending, Conditions: []corev1.PodCondition{{
			Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: corev1.PodReasonUnschedulable,
			Message: "0/3 nodes are available: 3 Insufficient cpu.",
		}}},
	}
	p := podSecurityProvisioner()
	p.client = fake.NewSimpleClientset(pending)
	p.cfg.SandboxReadyWait = 10 * time.Millisecond

	_, err := p.waitForPodReady(context.Background(), "sandbox-run-1", "ns")
	if !errors.Is(err, errSandboxUnschedulable) || !strings.Contains(err.Error(), "Insufficient cpu") {
		t.Errorf("err = %v, want errSandboxUnschedulable with the scheduler's message", err)
	}

	pending.Status.Conditions[0].Status = corev1.ConditionTrue
	p.client = fake.NewSimpleClientset(pending)
	if _, err := p.waitForPodReady(context.Background(), "sandbox-run-1", "ns"); err == nil || errors.Is(err, errSandboxUnschedulable) {
		t.Errorf("err = %v, want a plain timeout for a scheduled pod", err)
	}
}
