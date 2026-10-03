package agents

import (
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

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

// The sandbox runs the environment's pinned image with sandbox-exec as its
// command, sized from the environment, with its local disk at the run's local dir.
func TestSandboxPod_FromEnvironment(t *testing.T) {
	spec := SandboxSpec{
		EnvironmentID: "env-go",
		Environment:   "go",
		Image:         "ghcr.io/acme/go@sha256:abc",
		PullSecret:    "acme-pull",
		Size:          EnvironmentSize{CPU: "4", Memory: "8Gi", Disk: "20Gi"},
	}
	pod := podSecurityProvisioner().buildSandboxPod(Run{ID: "run-1"}, "coder", "orbit-tenant-acme", spec, "token")
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
