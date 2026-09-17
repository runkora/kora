package operator

import (
	"encoding/json"
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func testMutator() *PodMutator {
	return &PodMutator{Config: InjectionConfig{
		SystemNamespace:         "orbit-system",
		ImagePullSecrets:        []string{"regcred"},
		OTLPEndpoint:            "http://otel-collector.observability.svc:4317",
		IdentityProxyImage:      "ghcr.io/clear-route/orbit-identity-proxy@sha256:abc",
		IdentityProxyPullPolicy: corev1.PullIfNotPresent,
		OPAImage:                "docker.io/openpolicyagent/opa:1.15.2-envoy-static",
		OPAPullPolicy:           corev1.PullIfNotPresent,
		OPALogLevel:             "info",
	}}
}

func sandboxLikePod() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "sandbox-1", Namespace: "orbit-tenant-example",
			Labels: map[string]string{"orbit.io/role": "sandbox"},
			Annotations: map[string]string{
				"proxy.istio.io/config":                        `{"holdApplicationUntilProxyStarts": true}`,
				"traffic.sidecar.istio.io/excludeInboundPorts": "50051",
			},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "sandbox-exec", Image: "x"}}},
	}
}

// A sandbox's egress is brokered and never reaches the collector, so its
// identity-proxy is given no endpoint rather than a target it cannot route to.
// An agent pod, whose egress is not restricted that way, keeps it.
func TestMutate_OTLPEndpointSkippedForSandboxes(t *testing.T) {
	endpointOf := func(pod *corev1.Pod) string {
		for _, c := range pod.Spec.Containers {
			if c.Name != containerIdentityProxy {
				continue
			}
			for _, e := range c.Env {
				if e.Name == "ORBIT_OTLP_ENDPOINT" {
					return e.Value
				}
			}
		}
		return "<absent>"
	}

	sandbox := sandboxLikePod()
	if err := testMutator().mutate(sandbox, "example", ""); err != nil {
		t.Fatal(err)
	}
	if got := endpointOf(sandbox); got != "" {
		t.Errorf("sandbox identity-proxy endpoint = %q, want empty", got)
	}

	agent := sandboxLikePod()
	agent.Labels = map[string]string{}
	if err := testMutator().mutate(agent, "example", ""); err != nil {
		t.Fatal(err)
	}
	if got, want := endpointOf(agent), "http://otel-collector.observability.svc:4317"; got != want {
		t.Errorf("agent identity-proxy endpoint = %q, want %q", got, want)
	}
}

func TestMutate_AttachesEnforcement(t *testing.T) {
	m := testMutator()
	pod := sandboxLikePod()
	if err := m.mutate(pod, "example", ""); err != nil {
		t.Fatal(err)
	}

	for k, want := range map[string]string{podLabelOPAPolicy: "enabled", podLabelIdentityProxy: "enabled"} {
		if pod.Labels[k] != want {
			t.Errorf("label %s = %q, want %q", k, pod.Labels[k], want)
		}
	}
	if pod.Annotations[podAnnotationTenantID] != "example" {
		t.Errorf("tenant-id annotation = %q", pod.Annotations[podAnnotationTenantID])
	}
	if pod.Annotations["traffic.sidecar.istio.io/excludeInboundPorts"] != "50051" {
		t.Error("existing annotation was lost")
	}

	var proxyCfg map[string]any
	if err := json.Unmarshal([]byte(pod.Annotations[podAnnotationProxyConfig]), &proxyCfg); err != nil {
		t.Fatalf("proxy config not JSON: %v", err)
	}
	if proxyCfg["holdApplicationUntilProxyStarts"] != true {
		t.Error("holdApplicationUntilProxyStarts was not preserved")
	}
	meta := proxyCfg["proxyMetadata"].(map[string]any)
	if meta["PILOT_CERT_PROVIDER"] != "spire" || meta["SPIFFE_ENDPOINT_SOCKET"] != spiffeSocketPath {
		t.Errorf("proxyMetadata = %v", meta)
	}

	var mounts map[string]map[string]any
	if err := json.Unmarshal([]byte(pod.Annotations[podAnnotationUserVolumeMount]), &mounts); err != nil {
		t.Fatalf("userVolumeMount not JSON: %v", err)
	}
	if _, shadowed := mounts[volumeSpireSocket]; shadowed || mounts[volumeIdentityProxy]["mountPath"] != identityProxyMnt {
		t.Errorf("userVolumeMount = %v", mounts)
	}

	if len(pod.Spec.ImagePullSecrets) != 1 || pod.Spec.ImagePullSecrets[0].Name != "regcred" {
		t.Errorf("imagePullSecrets = %v", pod.Spec.ImagePullSecrets)
	}
	for _, v := range []string{volumeSpireSocket, volumeIdentityProxy, volumeOPAConfig} {
		if !hasVolume(pod, v) {
			t.Errorf("volume %s missing", v)
		}
	}
	if !hasContainer(pod, containerIdentityProxy) || !hasContainer(pod, containerOPA) {
		t.Fatalf("containers = %v", containerNames(pod))
	}
	opa := containerByName(pod, containerOPA)
	if !containsArg(opa.Args, "--addr=127.0.0.1:8181") {
		t.Errorf("OPA REST API must bind loopback, args = %v", opa.Args)
	}
	if !containsArg(opa.Args, "--authorization=basic") {
		t.Errorf("OPA REST API must require authorization, args = %v", opa.Args)
	}
	for _, c := range []corev1.Container{*containerByName(pod, containerIdentityProxy), *opa} {
		sc := c.SecurityContext
		if sc == nil || sc.AllowPrivilegeEscalation == nil || *sc.AllowPrivilegeEscalation ||
			sc.Capabilities == nil || len(sc.Capabilities.Drop) != 1 || sc.Capabilities.Drop[0] != "ALL" ||
			sc.SeccompProfile == nil || sc.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
			t.Errorf("container %s is not restricted-compliant: %+v", c.Name, sc)
		}
	}
}

// TestMutate_RejectsPreDeclaredIstioContainers: a pod that ships its own
// istio-proxy is standing in for the sidecar; with failurePolicy Fail the
// error becomes a denial.
func TestMutate_RejectsPreDeclaredIstioContainers(t *testing.T) {
	for _, name := range []string{"istio-proxy", "istio-validation", "istio-init"} {
		pod := sandboxLikePod()
		pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{Name: name, Image: "busybox"})
		if err := testMutator().mutate(pod, "example", ""); err == nil {
			t.Errorf("pod declaring %q was mutated instead of rejected", name)
		}
	}
}

func TestMutate_Idempotent(t *testing.T) {
	m := testMutator()
	pod := sandboxLikePod()
	if err := m.mutate(pod, "example", ""); err != nil {
		t.Fatal(err)
	}
	once := pod.DeepCopy()
	if err := m.mutate(pod, "example", ""); err != nil {
		t.Fatal(err)
	}
	if len(pod.Spec.Containers) != len(once.Spec.Containers) || len(pod.Spec.Volumes) != len(once.Spec.Volumes) || len(pod.Spec.ImagePullSecrets) != 1 {
		t.Errorf("second mutation changed the pod: containers %d->%d volumes %d->%d", len(once.Spec.Containers), len(pod.Spec.Containers), len(once.Spec.Volumes), len(pod.Spec.Volumes))
	}
	if pod.Annotations[podAnnotationUserVolumeMount] != once.Annotations[podAnnotationUserVolumeMount] {
		t.Error("userVolumeMount changed on reinvocation")
	}
}

func TestMergeUserVolumeMounts_PreservesExisting(t *testing.T) {
	out, err := mergeUserVolumeMounts(`{"custom":{"mountPath":"/x"}}`)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(out), &m)
	if _, ok := m["custom"]; !ok {
		t.Error("existing mount dropped")
	}
	if _, ok := m[volumeIdentityProxy]; !ok {
		t.Error("identity-proxy mount missing")
	}
}

func hasVolume(pod *corev1.Pod, name string) bool {
	for _, v := range pod.Spec.Volumes {
		if v.Name == name {
			return true
		}
	}
	return false
}

func containerByName(pod *corev1.Pod, name string) *corev1.Container {
	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name == name {
			return &pod.Spec.Containers[i]
		}
	}
	return nil
}

func containerNames(pod *corev1.Pod) []string {
	var out []string
	for _, c := range pod.Spec.Containers {
		out = append(out, c.Name)
	}
	return out
}

func containsArg(args []string, want string) bool {
	return slices.Contains(args, want)
}
