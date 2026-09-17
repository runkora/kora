package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	sigsyaml "sigs.k8s.io/yaml"
)

const (
	// Namespace labels set by TenantReconciler.ensureNamespace.
	nsLabelRole = "orbit.io/role"
	// podLabelRole carries the same key on a pod. orbit-agents sets it to
	// "sandbox" on the pods it provisions per run.
	podLabelRole   = "orbit.io/role"
	podRoleSandbox = "sandbox"
	nsLabelTenant  = "orbit.io/tenant"

	// Pod labels that activate the identity-proxy and OPA EnvoyFilters.
	podLabelOPAPolicy     = "orbit.io/opa-policy"
	podLabelIdentityProxy = "orbit.io/identity-proxy"

	// podLabelSpiffeService is matched by the orbit-tenant-service
	// ClusterSPIFFEID, which issues spiffe://<td>/tenant/<ns>/service/<value>.
	// An MCP server pod needs it: Orbit's sidecars take their certificates from
	// SPIRE, so a pod with no registration has no mesh identity.
	podLabelSpiffeService = "spiffe.io/service"

	// Pod annotations: the tenant id the OPA sidecar reads through the downward
	// API, and the two Istio annotations that make the sidecar use SPIRE and
	// mount the shared sockets.
	podAnnotationTenantID        = "orbit.io/tenant-id"
	podAnnotationProxyConfig     = "proxy.istio.io/config"
	podAnnotationUserVolumeMount = "sidecar.istio.io/userVolumeMount"

	spiffeSocketPath  = "unix:///run/secrets/workload-spiffe-uds/socket"
	spiffeSocketMount = "/run/secrets/workload-spiffe-uds"
	identityProxyMnt  = "/run/orbit"

	// Named to override Istio's own emptyDir of this name, where istio-proxy
	// looks for an external SDS socket.
	volumeSpireSocket   = "workload-socket"
	volumeIdentityProxy = "id-proxy-sock"
	volumeOPAConfig     = "orbit-opa-config"

	containerIdentityProxy = "identity-proxy"
	containerOPA           = "opa"
)

// InjectionConfig describes the containers the webhook adds to every tenant pod.
// Fixed at install time from the chart's identityProxy and opaGateway values.
type InjectionConfig struct {
	SystemNamespace  string
	ImagePullSecrets []string

	// OTLPEndpoint is passed to the injected identity-proxy so it exports to
	// the same collector as the rest of the platform. Empty disables its export.
	OTLPEndpoint string

	IdentityProxyImage      string
	IdentityProxyPullPolicy corev1.PullPolicy
	IdentityProxyResources  corev1.ResourceRequirements

	OPAImage      string
	OPAPullPolicy corev1.PullPolicy
	OPALogLevel   string
	OPAResources  corev1.ResourceRequirements
}

// PodMutator injects Orbit enforcement dependencies before Istio injection.
type PodMutator struct {
	Client  client.Client
	Decoder admission.Decoder
	Config  InjectionConfig
}

// Handle is called for every pod CREATE in a tenant namespace.
func (m *PodMutator) Handle(ctx context.Context, req admission.Request) admission.Response {
	pod := &corev1.Pod{}
	if err := m.Decoder.Decode(req, pod); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	ns := &corev1.Namespace{}
	if err := m.Client.Get(ctx, types.NamespacedName{Name: req.Namespace}, ns); err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	if ns.Labels[nsLabelRole] != "tenant" {
		return admission.Allowed("not a tenant namespace")
	}
	tenantName := ns.Labels[nsLabelTenant]
	if tenantName == "" {
		return admission.Denied("tenant namespace is missing the orbit.io/tenant label")
	}

	server, err := m.mcpServerFor(ctx, pod, req.Namespace)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	mutated := pod.DeepCopy()
	if err = m.mutate(mutated, tenantName, server); err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	raw, err := json.Marshal(mutated)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, raw)
}

// mcpServerFor names the MCPServer whose workload selector matches the pod, or
// "" when none does. The CR is the authority: a pod carrying the class label
// without a CR selecting it is injected and validated as an agent pod
// (docs/adr/2026-09-17-mcp-workload-class.md).
func (m *PodMutator) mcpServerFor(ctx context.Context, pod *corev1.Pod, namespace string) (string, error) {
	servers := &MCPServerList{}
	if err := m.Client.List(ctx, servers, client.InNamespace(namespace)); err != nil {
		return "", fmt.Errorf("list MCPServers in %q: %w", namespace, err)
	}
	for i := range servers.Items {
		if servers.Items[i].classifies(pod.Labels) {
			return servers.Items[i].Name, nil
		}
	}
	return "", nil
}

// mutate applies the injection to pod in place. It is idempotent so a
// reinvoked webhook produces the same pod.
//
// mcpServer names the MCPServer that selected this pod, empty for an agent pod.
// An MCP server pod gets the mesh certificate wiring and stops there:
// identity-proxy would overwrite the credential it authenticates to its own
// upstream with, and OPA governs agent egress, which it does not perform
// (docs/adr/2026-09-17-mcp-workload-class.md).
func (m *PodMutator) mutate(pod *corev1.Pod, tenantName, mcpServer string) error {
	if pod.Labels == nil {
		pod.Labels = map[string]string{}
	}
	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}
	if mcpServer != "" {
		pod.Labels[podLabelMCPServer] = mcpServer
		pod.Labels[podLabelSpiffeService] = mcpServer
		// Set by the pod itself, these would attach the EnvoyFilters this class
		// exists to keep off it.
		delete(pod.Labels, podLabelIdentityProxy)
		delete(pod.Labels, podLabelOPAPolicy)
	} else {
		pod.Labels[podLabelOPAPolicy] = "enabled"
		pod.Labels[podLabelIdentityProxy] = "enabled"
		pod.Annotations[podAnnotationTenantID] = tenantName
	}

	// Orbit's own containers below are overwritten by name. Istio's are not
	// Orbit's to overwrite, and a pod that declares them itself is trying to
	// stand in for the sidecar; refuse it rather than let it through.
	if name := preDeclaredIstioContainer(&pod.Spec); name != "" {
		return fmt.Errorf("pod declares its own %q container; Istio injects that", name)
	}

	proxyCfg, err := mergeProxyConfig(pod.Annotations[podAnnotationProxyConfig])
	if err != nil {
		return fmt.Errorf("merge %s: %w", podAnnotationProxyConfig, err)
	}
	pod.Annotations[podAnnotationProxyConfig] = proxyCfg

	for _, name := range m.Config.ImagePullSecrets {
		if !hasPullSecret(pod.Spec.ImagePullSecrets, name) {
			pod.Spec.ImagePullSecrets = append(pod.Spec.ImagePullSecrets, corev1.LocalObjectReference{Name: name})
		}
	}

	// istio-proxy mounts a volume of this name, which is how the sidecar reaches
	// the SPIFFE socket for its certificate. Both classes need it.
	ensureVolume(&pod.Spec, corev1.Volume{
		Name: volumeSpireSocket,
		VolumeSource: corev1.VolumeSource{
			CSI: &corev1.CSIVolumeSource{Driver: "csi.spiffe.io", ReadOnly: new(true)},
		},
	})

	if mcpServer != "" {
		return nil
	}

	mounts, err := mergeUserVolumeMounts(pod.Annotations[podAnnotationUserVolumeMount])
	if err != nil {
		return fmt.Errorf("merge %s: %w", podAnnotationUserVolumeMount, err)
	}
	pod.Annotations[podAnnotationUserVolumeMount] = mounts

	ensureVolume(&pod.Spec, corev1.Volume{
		Name:         volumeIdentityProxy,
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	})
	ensureVolume(&pod.Spec, corev1.Volume{
		Name: volumeOPAConfig,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: "orbit-opa-config"},
			},
		},
	})

	// A sandbox's egress is brokered: its NetworkPolicy admits DNS, the mesh,
	// identity, policy-service and the egress gateway, and nothing else. Handing
	// its identity-proxy a collector it cannot route to only buys export retries.
	otlpEndpoint := m.Config.OTLPEndpoint
	if pod.Labels[podLabelRole] == podRoleSandbox {
		otlpEndpoint = ""
	}
	ensureContainer(&pod.Spec, m.identityProxyContainer(otlpEndpoint))
	ensureContainer(&pod.Spec, m.opaContainer())
	return nil
}

func (m *PodMutator) identityProxyContainer(otlpEndpoint string) corev1.Container {
	return corev1.Container{
		Name:            containerIdentityProxy,
		Image:           m.Config.IdentityProxyImage,
		ImagePullPolicy: m.Config.IdentityProxyPullPolicy,
		Env: []corev1.EnvVar{
			{Name: "ORBIT_IDENTITY_SERVICE_URL", Value: fmt.Sprintf("http://orbit-identity.%s.svc.cluster.local", m.Config.SystemNamespace)},
			{Name: "SPIFFE_ENDPOINT_SOCKET", Value: spiffeSocketPath},
			{Name: "ORBIT_OTLP_ENDPOINT", Value: otlpEndpoint},
		},
		Ports:           []corev1.ContainerPort{{Name: "health", ContainerPort: 9090, Protocol: corev1.ProtocolTCP}},
		SecurityContext: restrictedContainerSecurityContext(1000, true),
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{Path: "/healthz", Port: intstr.FromInt32(9090)},
			},
			InitialDelaySeconds: 5,
			PeriodSeconds:       5,
			TimeoutSeconds:      3,
			FailureThreshold:    10,
		},
		Resources: m.Config.IdentityProxyResources,
		VolumeMounts: []corev1.VolumeMount{
			{Name: volumeIdentityProxy, MountPath: identityProxyMnt},
			{Name: volumeSpireSocket, MountPath: spiffeSocketMount, ReadOnly: true},
		},
	}
}

// opaContainer exposes the policy API only on loopback.
func (m *PodMutator) opaContainer() corev1.Container {
	return corev1.Container{
		Name:            containerOPA,
		Image:           m.Config.OPAImage,
		ImagePullPolicy: m.Config.OPAPullPolicy,
		Args: []string{
			"run", "--server",
			"--config-file=/config/config.yaml",
			"--addr=127.0.0.1:8181",
			// Binding to loopback only blocks other pods; the agent container
			// shares this pod's network namespace and can still reach 8181.
			// No policy defines system.authz.allow, so this denies every
			// request the loopback bind alone would let the agent make.
			"--authorization=basic",
			"--diagnostic-addr=0.0.0.0:8282",
			"--log-level=" + m.Config.OPALogLevel,
			"/config/authz.rego",
		},
		Env: []corev1.EnvVar{{
			Name: "ORBIT_TENANT_ID",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.annotations['" + podAnnotationTenantID + "']"},
			},
		}},
		Ports: []corev1.ContainerPort{
			{Name: "opa-grpc", ContainerPort: 9191, Protocol: corev1.ProtocolTCP},
			{Name: "opa-diag", ContainerPort: 8282, Protocol: corev1.ProtocolTCP},
		},
		SecurityContext: restrictedContainerSecurityContext(1337, true),
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{Path: "/health?bundles=true", Port: intstr.FromInt32(8282)},
			},
			InitialDelaySeconds: 3,
			PeriodSeconds:       5,
			FailureThreshold:    12,
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{Path: "/health", Port: intstr.FromInt32(8282)},
			},
			InitialDelaySeconds: 10,
			PeriodSeconds:       10,
		},
		Resources: m.OPAResourcesOrDefault(),
		VolumeMounts: []corev1.VolumeMount{
			{Name: volumeOPAConfig, MountPath: "/config", ReadOnly: true},
		},
	}
}

// OPAResourcesOrDefault returns the configured OPA resources, or a small
// default that still satisfies a namespace LimitRange.
func (m *PodMutator) OPAResourcesOrDefault() corev1.ResourceRequirements {
	if len(m.Config.OPAResources.Requests) > 0 || len(m.Config.OPAResources.Limits) > 0 {
		return m.Config.OPAResources
	}
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{"cpu": resource.MustParse("50m"), "memory": resource.MustParse("64Mi")},
		Limits:   corev1.ResourceList{"cpu": resource.MustParse("250m"), "memory": resource.MustParse("128Mi")},
	}
}

// mergeProxyConfig sets the SPIRE certificate provider on top of whatever proxy
// configuration the pod already carries.
func mergeProxyConfig(existing string) (string, error) {
	cfg := map[string]any{}
	if existing != "" {
		if err := sigsyaml.Unmarshal([]byte(existing), &cfg); err != nil {
			return "", err
		}
	}
	if _, ok := cfg["holdApplicationUntilProxyStarts"]; !ok {
		cfg["holdApplicationUntilProxyStarts"] = true
	}
	meta, _ := cfg["proxyMetadata"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
	}
	meta["PILOT_CERT_PROVIDER"] = "spire"
	meta["SPIFFE_ENDPOINT_SOCKET"] = spiffeSocketPath
	cfg["proxyMetadata"] = meta
	out, err := json.Marshal(cfg)
	return string(out), err
}

// mergeUserVolumeMounts adds the identity-proxy socket mount
// for istio-proxy to any mounts the pod already requests.
func mergeUserVolumeMounts(existing string) (string, error) {
	mounts := map[string]any{}
	if existing != "" {
		if err := json.Unmarshal([]byte(existing), &mounts); err != nil {
			return "", err
		}
	}
	mounts[volumeIdentityProxy] = map[string]any{"mountPath": identityProxyMnt}
	out, err := json.Marshal(mounts)
	return string(out), err
}

// ensureVolume sets an Orbit-owned volume by name, overwriting any pod-supplied
// definition of the same name. Every caller here is one of Orbit's own fixed
// volume names (workload-socket, id-proxy-sock, orbit-opa-config): a tenant pod
// that pre-declares one of these itself must not get its definition kept, or the
// injected sidecar mounts an attacker-controlled volume instead of the real one.
func ensureVolume(spec *corev1.PodSpec, v corev1.Volume) {
	for i := range spec.Volumes {
		if spec.Volumes[i].Name == v.Name {
			spec.Volumes[i] = v
			return
		}
	}
	spec.Volumes = append(spec.Volumes, v)
}

// preDeclaredIstioContainer returns the first container or init container
// named like one Istio injects, or "" when there is none.
func preDeclaredIstioContainer(spec *corev1.PodSpec) string {
	for _, list := range [][]corev1.Container{spec.InitContainers, spec.Containers} {
		for _, c := range list {
			switch c.Name {
			case "istio-proxy", "istio-validation", "istio-init":
				return c.Name
			}
		}
	}
	return ""
}

func ensureContainer(spec *corev1.PodSpec, c corev1.Container) {
	for i := range spec.Containers {
		if spec.Containers[i].Name == c.Name {
			spec.Containers[i] = c
			return
		}
	}
	spec.Containers = append(spec.Containers, c)
}

func hasPullSecret(refs []corev1.LocalObjectReference, name string) bool {
	for _, r := range refs {
		if r.Name == name {
			return true
		}
	}
	return false
}

// restrictedContainerSecurityContext satisfies the restricted Pod Security
// Standard; the pod-level seccomp profile comes from the workload's own spec.
func restrictedContainerSecurityContext(uid int64, readOnlyRoot bool) *corev1.SecurityContext {
	return &corev1.SecurityContext{
		RunAsNonRoot:             new(true),
		RunAsUser:                new(uid),
		AllowPrivilegeEscalation: new(false),
		ReadOnlyRootFilesystem:   new(readOnlyRoot),
		Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
	}
}
