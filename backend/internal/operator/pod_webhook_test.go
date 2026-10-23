package operator

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/probes"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func testMutator() *PodMutator {
	return &PodMutator{Config: InjectionConfig{
		SystemNamespace:         "orbit-system",
		ImagePullSecrets:        []string{"regcred"},
		OTLPEndpoint:            "http://otel-collector.observability.svc:4317",
		SamplingRate:            "1.0",
		IdentityProxyImage:      "ghcr.io/runkora/kora-identity-proxy@sha256:abc",
		IdentityProxyPullPolicy: corev1.PullIfNotPresent,
		OPAImage:                "docker.io/openpolicyagent/opa:1.21.0-envoy-static",
		OPAPullPolicy:           corev1.PullIfNotPresent,
		OPALogLevel:             "info",
	}}
}

func sandboxLikePod() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "sandbox-1", Namespace: "orbit-tenant-example",
			Labels: map[string]string{"runkora.io/role": "sandbox"},
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
		for _, c := range pod.Spec.InitContainers {
			if c.Name != containerIdentityProxy {
				continue
			}
			for _, e := range c.Env {
				if e.Name == "KORA_OTLP_ENDPOINT" {
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

// The enforcement sidecars are native sidecars after the pod's own init containers,
// and a regular container of the same name is replaced.
func TestMutate_InjectsNativeSidecars(t *testing.T) {
	pod := sandboxLikePod()
	pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{Name: containerOPA, Image: "stale"})
	pod.Spec.InitContainers = []corev1.Container{{Name: "setup", Image: "x"}}
	if err := testMutator().mutate(pod, "example", ""); err != nil {
		t.Fatal(err)
	}
	for _, c := range pod.Spec.Containers {
		if c.Name == containerIdentityProxy || c.Name == containerOPA {
			t.Errorf("%s is a regular container", c.Name)
		}
	}
	var names []string
	for _, c := range pod.Spec.InitContainers {
		names = append(names, c.Name)
		if c.Name != "setup" && (c.RestartPolicy == nil || *c.RestartPolicy != corev1.ContainerRestartPolicyAlways) {
			t.Errorf("%s is not a native sidecar", c.Name)
		}
	}
	if !slices.Equal(names, []string{"setup", containerIdentityProxy, containerOPA}) {
		t.Errorf("init containers = %v, want setup then the sidecars", names)
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
	if len(pod.Spec.Containers) != len(once.Spec.Containers) || len(pod.Spec.InitContainers) != len(once.Spec.InitContainers) || len(pod.Spec.Volumes) != len(once.Spec.Volumes) || len(pod.Spec.ImagePullSecrets) != 1 {
		t.Errorf("second mutation changed the pod: containers %d->%d init containers %d->%d volumes %d->%d", len(once.Spec.Containers), len(pod.Spec.Containers), len(once.Spec.InitContainers), len(pod.Spec.InitContainers), len(once.Spec.Volumes), len(pod.Spec.Volumes))
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

// Peer-metadata headers stay in the mesh whatever the pod asked for, and other
// proxyHeaders settings survive the merge.
func TestMergeProxyConfig_PeerMetadataInMeshOnly(t *testing.T) {
	cases := []struct {
		name     string
		existing string
		keep     string
	}{
		{name: "no annotation"},
		{name: "existing proxy headers kept", existing: `{"proxyHeaders":{"server":{"disabled":true}}}`, keep: "server"},
		{name: "pod opting into headers is overridden", existing: `{"proxyHeaders":{"metadataExchangeHeaders":{"mode":"ALWAYS"}}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := mergeProxyConfig(tc.existing)
			if err != nil {
				t.Fatal(err)
			}
			var cfg struct {
				ProxyHeaders map[string]json.RawMessage `json:"proxyHeaders"`
			}
			if err := json.Unmarshal([]byte(out), &cfg); err != nil {
				t.Fatal(err)
			}
			if got := string(cfg.ProxyHeaders["metadataExchangeHeaders"]); got != `{"mode":"IN_MESH"}` {
				t.Errorf("metadataExchangeHeaders = %s", got)
			}
			if _, ok := cfg.ProxyHeaders[tc.keep]; tc.keep != "" && !ok {
				t.Errorf("proxyHeaders.%s dropped: %s", tc.keep, out)
			}
		})
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

// containerByName finds an injected sidecar, which is a native sidecar init container.
func containerByName(pod *corev1.Pod, name string) *corev1.Container {
	for i := range pod.Spec.InitContainers {
		if pod.Spec.InitContainers[i].Name == name {
			return &pod.Spec.InitContainers[i]
		}
	}
	return nil
}

func containerNames(pod *corev1.Pod) []string {
	var out []string
	for _, c := range append(pod.Spec.InitContainers, pod.Spec.Containers...) {
		out = append(out, c.Name)
	}
	return out
}

func containsArg(args []string, want string) bool {
	return slices.Contains(args, want)
}

// A sandbox pod's sidecars reserve what they can use. An agent pod keeps the usual resources.
func TestMutate_PinsSandboxSidecars(t *testing.T) {
	m := testMutator()
	m.Config.IdentityProxyResources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("50m")},
		Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("200m")},
	}
	m.Config.SandboxSidecars = SandboxSidecars{
		IstioProxy:    corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("200m"), corev1.ResourceMemory: resource.MustParse("128Mi")},
		IdentityProxy: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("200m"), corev1.ResourceMemory: resource.MustParse("64Mi")},
		OPA:           corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m"), corev1.ResourceMemory: resource.MustParse("128Mi")},
	}
	containerOf := func(pod *corev1.Pod, name string) corev1.Container {
		for _, c := range append(pod.Spec.InitContainers, pod.Spec.Containers...) {
			if c.Name == name {
				return c
			}
		}
		t.Fatalf("no %s container", name)
		return corev1.Container{}
	}

	sandbox := sandboxLikePod()
	if err := m.mutate(sandbox, "example", ""); err != nil {
		t.Fatal(err)
	}
	for name, cpu := range map[string]string{containerIdentityProxy: "200m", containerOPA: "500m"} {
		r := containerOf(sandbox, name).Resources
		if r.Requests.Cpu().String() != cpu || r.Limits.Cpu().String() != cpu || r.Requests.Memory().Cmp(*r.Limits.Memory()) != 0 {
			t.Errorf("%s resources = %+v, want requests equal to limits at %s CPU", name, r, cpu)
		}
	}
	for key, want := range map[string]string{
		podAnnotationProxyCPU: "200m", podAnnotationProxyCPULimit: "200m",
		podAnnotationProxyMemory: "128Mi", podAnnotationProxyMemoryLimit: "128Mi",
	} {
		if got := sandbox.Annotations[key]; got != want {
			t.Errorf("annotation %s = %q, want %q", key, got, want)
		}
	}

	agent := sandboxLikePod()
	agent.Labels = map[string]string{}
	if err := m.mutate(agent, "example", ""); err != nil {
		t.Fatal(err)
	}
	if r := containerOf(agent, containerIdentityProxy).Resources; r.Requests.Cpu().String() != "50m" {
		t.Errorf("agent identity-proxy requests = %+v, want the usual 50m", r.Requests)
	}
	if _, ok := agent.Annotations[podAnnotationProxyCPU]; ok {
		t.Errorf("agent pod carries %s, want no istio-proxy pin", podAnnotationProxyCPU)
	}
}

// Each sidecar gets a one-second startup probe so the pod becomes Ready soon
// after the sidecar is up. The startup probe allows at least as long as the
// old readiness probe did, so a slow start does not restart the container.
func TestMutate_SidecarStartupProbes(t *testing.T) {
	pod := sandboxLikePod()
	if err := testMutator().mutate(pod, "example", ""); err != nil {
		t.Fatal(err)
	}
	// The default must stay above the worst case of the previous readiness
	// probes: delay plus period times threshold.
	if probes.DefaultStartupSeconds < 5+5*10 || probes.DefaultStartupSeconds < 3+5*12 {
		t.Fatalf("default startup budget %ds is below the old readiness budgets", probes.DefaultStartupSeconds)
	}
	seen := 0
	for _, c := range pod.Spec.InitContainers {
		if c.Name != containerIdentityProxy && c.Name != containerOPA {
			continue
		}
		seen++
		sp, rp := c.StartupProbe, c.ReadinessProbe
		if sp == nil || rp == nil || sp.HTTPGet == nil || rp.HTTPGet == nil {
			t.Fatalf("%s: startup %+v readiness %+v, want HTTP probes", c.Name, sp, rp)
		}
		if sp.HTTPGet.Path != rp.HTTPGet.Path || sp.HTTPGet.Port != rp.HTTPGet.Port {
			t.Errorf("%s: startup checks %+v, readiness checks %+v, want the same endpoint", c.Name, sp.HTTPGet, rp.HTTPGet)
		}
		if sp.PeriodSeconds != 1 || sp.InitialDelaySeconds != 0 || rp.InitialDelaySeconds != 0 {
			t.Errorf("%s: startup period %d, startup delay %d, readiness delay %d, want 1/0/0", c.Name, sp.PeriodSeconds, sp.InitialDelaySeconds, rp.InitialDelaySeconds)
		}
		if got := sp.FailureThreshold * sp.PeriodSeconds; got != probes.DefaultStartupSeconds {
			t.Errorf("%s: startup allows %ds, want the default %ds", c.Name, got, probes.DefaultStartupSeconds)
		}
	}
	if seen != 2 {
		t.Fatalf("checked %d sidecars, want 2", seen)
	}

	m := testMutator()
	m.Config.StartupProbeSeconds = 300
	pod = sandboxLikePod()
	if err := m.mutate(pod, "example", ""); err != nil {
		t.Fatal(err)
	}
	for _, c := range pod.Spec.InitContainers {
		if c.Name == containerIdentityProxy || c.Name == containerOPA {
			if got := c.StartupProbe.FailureThreshold * c.StartupProbe.PeriodSeconds; got != 300 {
				t.Errorf("%s: startup allows %ds with an override of 300s", c.Name, got)
			}
		}
	}
}
