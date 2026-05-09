package agents

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	policyclient "github.com/clear-route/orbit/internal/platform/clients/policy"
	"github.com/clear-route/orbit/internal/platform/logging"
)

var destinationRuleGVR = schema.GroupVersionResource{
	Group:    "networking.istio.io",
	Version:  "v1beta1",
	Resource: "destinationrules",
}


// K8sProvisionerConfig holds the parameters the provisioner needs that cannot
// be derived from the agent type or run model.
type K8sProvisionerConfig struct {
	SessionManagerImage       string
	SandboxImage              string
	SandboxGRPCPort           int32
	SessionManagerPostgresSSL string
	// TrustDomain is the SPIFFE trust domain (e.g. "orbit.test").
	// Used to build the agent's SPIFFE principal ID for grant creation.
	TrustDomain string
	// SystemNamespace is the namespace where orbit-agents runs.
	// DestinationRules are created here so Istio applies them to outbound
	// traffic from orbit-agents pods targeting agent session-managers.
	SystemNamespace string
}

// K8sProvisioner manages Kubernetes resources on behalf of orbit-agents.
// It is the only component with RBAC to create/delete StatefulSets, Services,
// HPAs, PDBs, sandbox Pods, and Istio DestinationRules in tenant namespaces.
type K8sProvisioner struct {
	client        kubernetes.Interface
	dynamicClient dynamic.Interface
	logger        logging.Logger
	cfg           K8sProvisionerConfig
	policyClient  *policyclient.Client
}

// NewK8sProvisioner builds a provisioner using in-cluster config when inCluster
// is true, otherwise falling back to KUBECONFIG (local development).
// policyClient may be nil — grant creation is skipped when unset (local dev).
func NewK8sProvisioner(inCluster bool, logger logging.Logger, cfg K8sProvisionerConfig, policyClient *policyclient.Client) (*K8sProvisioner, error) {
	restCfg, err := buildRestConfig(inCluster)
	if err != nil {
		return nil, fmt.Errorf("build k8s rest config: %w", err)
	}

	cs, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("create k8s clientset: %w", err)
	}

	dc, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("create dynamic k8s client: %w", err)
	}

	return &K8sProvisioner{client: cs, dynamicClient: dc, logger: logger, cfg: cfg, policyClient: policyClient}, nil
}

func buildRestConfig(inCluster bool) (*rest.Config, error) {
	if inCluster {
		return rest.InClusterConfig()
	}
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		rules, &clientcmd.ConfigOverrides{},
	).ClientConfig()
}

// ── Agent type provisioning ───────────────────────────────────────────────────

// ProvisionAgentType creates all k8s resources for a managed agent type in the
// given namespace. Idempotent — existing resources are left unchanged.
func (p *K8sProvisioner) ProvisionAgentType(ctx context.Context, at AgentType, namespace string) error {
	slug := at.Slug
	labels := agentTypeLabels(at)

	if err := p.ensureServiceAccount(ctx, slug, namespace, labels); err != nil {
		return err
	}
	if err := p.ensureStatefulSet(ctx, at, namespace, labels); err != nil {
		return err
	}
	if err := p.ensureHeadlessService(ctx, slug, namespace, labels); err != nil {
		return err
	}
	if err := p.ensureHPA(ctx, slug, namespace, labels); err != nil {
		return err
	}
	if err := p.ensurePDB(ctx, slug, namespace, labels); err != nil {
		return err
	}
	if err := p.ensureAgentGrant(ctx, at, namespace); err != nil {
		return err
	}
	if err := p.ensureDestinationRule(ctx, at, namespace); err != nil {
		return err
	}
	return nil
}

// DeprovisionAgentType deletes all k8s resources created by ProvisionAgentType.
// Missing resources are silently ignored.
func (p *K8sProvisioner) DeprovisionAgentType(ctx context.Context, at AgentType, namespace string) error {
	slug := at.Slug
	del := metav1.DeletePropagationForeground
	opts := metav1.DeleteOptions{PropagationPolicy: &del}

	resources := []func() error{
		func() error {
			return ignoreNotFound(p.client.AppsV1().StatefulSets(namespace).Delete(ctx, slug, opts))
		},
		func() error {
			return ignoreNotFound(p.client.CoreV1().Services(namespace).Delete(ctx, slug, opts))
		},
		func() error {
			return ignoreNotFound(p.client.AutoscalingV2().HorizontalPodAutoscalers(namespace).Delete(ctx, slug, opts))
		},
		func() error {
			return ignoreNotFound(p.client.PolicyV1().PodDisruptionBudgets(namespace).Delete(ctx, slug, opts))
		},
		func() error {
			return ignoreNotFound(p.client.CoreV1().ServiceAccounts(namespace).Delete(ctx, slug, metav1.DeleteOptions{}))
		},
		func() error {
			return p.deleteAgentGrant(ctx, at, namespace)
		},
		func() error {
			return p.deleteDestinationRule(ctx, at.Slug)
		},
	}

	var firstErr error
	for _, fn := range resources {
		if err := fn(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ReplicaCount returns the current number of desired replicas for an agent type's
// StatefulSet. Returns 1 if the StatefulSet is not found (safe default for pod selection).
func (p *K8sProvisioner) ReplicaCount(ctx context.Context, slug, namespace string) (int, error) {
	ss, err := p.client.AppsV1().StatefulSets(namespace).Get(ctx, slug, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return 1, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get statefulset %s/%s: %w", namespace, slug, err)
	}
	if ss.Spec.Replicas == nil {
		return 1, nil
	}
	return int(*ss.Spec.Replicas), nil
}

// ── Sandbox provisioning ──────────────────────────────────────────────────────

// ProvisionSandbox creates a sandbox pod for the given run and waits for it to
// be Ready. Returns the gRPC endpoint string ("podIP:port") on success.
// The caller (session-manager) is responsible for writing the endpoint file.
//
// The sandbox pod mounts the same workspace PVC as the agent StatefulSet pod
// (named "workspaces-{slug}-{ordinal}") so files written by the agent container
// (Edit, Write tools) are immediately visible in the sandbox without copying.
func (p *K8sProvisioner) ProvisionSandbox(ctx context.Context, run Run, agentTypeSlug, namespace string) (string, error) {
	podName := fmt.Sprintf("sandbox-%s", run.ID)
	uid := int64(100)
	nonRoot := true
	// PVC name follows StatefulSet volumeClaimTemplate convention:
	// {volumeName}-{statefulSetName}-{ordinal}
	workspacesPVC := fmt.Sprintf("workspaces-%s-%d", agentTypeSlug, run.PodOrdinal)

	labels := map[string]string{
		"orbit.io/role":           "sandbox",
		"orbit.io/run-id":         run.ID,
		"orbit.io/skip-injection": "true",
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
			Labels:    labels,
			Annotations: map[string]string{
				// No sidecars — sandbox-exec is a pure gRPC server with no outbound
				// calls. The session-manager's Envoy excludes port 50051 so orbit-shell
				// connects directly via plain TCP without mTLS.
				"sidecar.istio.io/inject": "false",
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			SecurityContext: &corev1.PodSecurityContext{
				RunAsUser:    &uid,
				RunAsNonRoot: &nonRoot,
			},
			Volumes: []corev1.Volume{
				{
					Name: "workspaces",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: workspacesPVC,
						},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name:            "sandbox-exec",
					Image:           p.cfg.SandboxImage,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Ports: []corev1.ContainerPort{
						{Name: "grpc", ContainerPort: p.cfg.SandboxGRPCPort, Protocol: corev1.ProtocolTCP},
					},
					Env: []corev1.EnvVar{
						{Name: "SANDBOX_EXEC_PORT", Value: fmt.Sprintf("%d", p.cfg.SandboxGRPCPort)},
					},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "workspaces", MountPath: "/workspaces"},
					},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							TCPSocket: &corev1.TCPSocketAction{
								Port: intstr.FromInt32(p.cfg.SandboxGRPCPort),
							},
						},
						InitialDelaySeconds: 2,
						PeriodSeconds:       3,
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("256Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2"),
							corev1.ResourceMemory: resource.MustParse("4Gi"),
						},
					},
				},
			},
		},
	}

	_, err := p.client.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return "", fmt.Errorf("create sandbox pod: %w", err)
	}
	if apierrors.IsAlreadyExists(err) {
		p.logger.Info(ctx, "Sandbox pod already exists, waiting for ready",
			logging.Field{Key: "run_id", Value: run.ID},
			logging.Field{Key: "pod", Value: podName},
		)
	} else {
		p.logger.Info(ctx, "Sandbox pod created, waiting for ready",
			logging.Field{Key: "run_id", Value: run.ID},
			logging.Field{Key: "pod", Value: podName},
			logging.Field{Key: "namespace", Value: namespace},
		)
	}

	podIP, err := p.waitForPodReady(ctx, podName, namespace)
	if err != nil {
		return "", fmt.Errorf("wait for sandbox pod ready: %w", err)
	}

	endpoint := fmt.Sprintf("%s:%d", podIP, p.cfg.SandboxGRPCPort)
	p.logger.Info(ctx, "Sandbox pod ready",
		logging.Field{Key: "run_id", Value: run.ID},
		logging.Field{Key: "pod", Value: podName},
		logging.Field{Key: "endpoint", Value: endpoint},
	)
	return endpoint, nil
}

// DeleteSandbox removes the sandbox pod for a run. Missing pod is silently ignored.
func (p *K8sProvisioner) DeleteSandbox(ctx context.Context, runID, namespace string) error {
	podName := fmt.Sprintf("sandbox-%s", runID)
	p.logger.Info(ctx, "Deleting sandbox pod",
		logging.Field{Key: "run_id", Value: runID},
		logging.Field{Key: "pod", Value: podName},
		logging.Field{Key: "namespace", Value: namespace},
	)
	del := metav1.DeletePropagationForeground
	return ignoreNotFound(p.client.CoreV1().Pods(namespace).Delete(ctx, podName, metav1.DeleteOptions{PropagationPolicy: &del}))
}

// waitForPodReady polls until the pod is Ready, returning its pod IP.
// Respects the deadline on ctx (the session-manager enforces 120s).
func (p *K8sProvisioner) waitForPodReady(ctx context.Context, name, namespace string) (string, error) {
	var podIP string
	err := wait.PollUntilContextTimeout(ctx, 3*time.Second, 120*time.Second, true, func(ctx context.Context) (bool, error) {
		pod, err := p.client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if pod.Status.Phase == corev1.PodFailed {
			return false, fmt.Errorf("sandbox pod failed")
		}
		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
				podIP = pod.Status.PodIP
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		return "", err
	}
	if podIP == "" {
		return "", fmt.Errorf("pod ready but no IP assigned")
	}
	return podIP, nil
}

// ── Policy grant management ───────────────────────────────────────────────────

// agentSpiffeID returns the SPIFFE principal ID for an agent type's pods.
// The identity proxy attests pods using their SPIFFE cert; the slug makes the
// ID human-readable (e.g. spiffe://orbit.test/tenant/orbit-tenant-example/agent/coder).
func (p *K8sProvisioner) agentSpiffeID(at AgentType, namespace string) string {
	return fmt.Sprintf("spiffe://%s/tenant/%s/agent/%s", p.cfg.TrustDomain, namespace, at.Slug)
}

// ensureAgentGrant reconciles the policy-service grant for this agent's SPIFFE
// principal toward exactly one grant containing at.GrantScopes.
//
// Reconcile logic:
//   - Keep the first existing grant whose scopes exactly match at.GrantScopes.
//   - Delete all other grants for this principal (duplicates + stale scope sets).
//   - If no matching grant was kept, create one.
//
// This handles first-time provisioning, scope updates, and deduplication in a
// single idempotent call.
// Skipped silently when policyClient is nil (local dev without policy service).
func (p *K8sProvisioner) ensureAgentGrant(ctx context.Context, at AgentType, namespace string) error {
	if p.policyClient == nil {
		return nil
	}
	principalID := p.agentSpiffeID(at, namespace)

	existing, err := p.policyClient.ListGrants(ctx, at.TenantID, "AGENT", principalID)
	if err != nil {
		return fmt.Errorf("list agent grants for %s: %w", principalID, err)
	}

	// Walk existing grants: keep the first with correct scopes, delete the rest.
	var keepID string
	for _, g := range existing {
		if keepID == "" && scopeSetsEqual(g.Scopes, at.GrantScopes) {
			keepID = g.ID
			continue
		}
		if delErr := p.policyClient.DeleteGrant(ctx, g.ID); delErr != nil {
			p.logger.Warn(ctx, "delete stale agent grant failed",
				logging.Field{Key: "grant_id", Value: g.ID},
				logging.Field{Key: "error", Value: delErr.Error()},
			)
		} else {
			p.logger.Info(ctx, "stale agent grant deleted",
				logging.Field{Key: "grant_id", Value: g.ID},
				logging.Field{Key: "principal_id", Value: principalID},
			)
		}
	}

	if keepID != "" {
		p.logger.Info(ctx, "agent grant up to date",
			logging.Field{Key: "principal_id", Value: principalID},
			logging.Field{Key: "grant_id", Value: keepID},
		)
		return nil
	}

	if _, err := p.policyClient.CreateGrant(ctx, at.TenantID, "AGENT", principalID, at.GrantScopes); err != nil {
		return fmt.Errorf("create agent grant for %s: %w", principalID, err)
	}
	p.logger.Info(ctx, "agent grant created",
		logging.Field{Key: "principal_id", Value: principalID},
		logging.Field{Key: "tenant_id", Value: at.TenantID},
	)
	return nil
}

// scopeSetsEqual returns true when a and b contain the same elements (order-independent).
func scopeSetsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	have := make(map[string]struct{}, len(a))
	for _, s := range a {
		have[s] = struct{}{}
	}
	for _, s := range b {
		if _, ok := have[s]; !ok {
			return false
		}
	}
	return true
}

// deleteAgentGrant removes all policy-service grants for this agent's SPIFFE
// principal. Best-effort — logs and returns nil if no grants exist or the
// policy client is unavailable.
func (p *K8sProvisioner) deleteAgentGrant(ctx context.Context, at AgentType, namespace string) error {
	if p.policyClient == nil {
		return nil
	}
	principalID := p.agentSpiffeID(at, namespace)
	grants, err := p.policyClient.ListGrants(ctx, at.TenantID, "AGENT", principalID)
	if err != nil {
		p.logger.Warn(ctx, "list agent grants failed; grant may be orphaned",
			logging.Field{Key: "principal_id", Value: principalID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return nil
	}
	for _, g := range grants {
		if err := p.policyClient.DeleteGrant(ctx, g.ID); err != nil {
			p.logger.Warn(ctx, "delete agent grant failed",
				logging.Field{Key: "grant_id", Value: g.ID},
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
	}
	return nil
}

// ── resource builders ─────────────────────────────────────────────────────────

func (p *K8sProvisioner) ensureServiceAccount(ctx context.Context, name, namespace string, labels map[string]string) error {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels},
	}
	_, err := p.client.CoreV1().ServiceAccounts(namespace).Create(ctx, sa, metav1.CreateOptions{})
	return ignoreAlreadyExists(err)
}

func (p *K8sProvisioner) ensureStatefulSet(ctx context.Context, at AgentType, namespace string, labels map[string]string) error {
	replicas := int32(1)
	uid := int64(100)
	nonRoot := true
	ss := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: at.Slug, Namespace: namespace, Labels: labels},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &replicas,
			ServiceName: at.Slug,
			Selector:    &metav1.LabelSelector{MatchLabels: podLabels(at)},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: podLabels(at),
					Annotations: map[string]string{
						// sidecar,spire: Envoy + SPIRE workload cert (mTLS + SPIFFE identity)
						// identity-proxy: injects a platform JWT on all outbound requests
						// (no opa-policy: session-manager is platform-managed, not a tenant app)
						"inject.istio.io/templates":                     "sidecar,spire,identity-proxy",
						"proxy.istio.io/config":                         `{"holdApplicationUntilProxyStarts": true}`,
						"traffic.sidecar.istio.io/excludeOutboundPorts": "5432,50051",
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: at.Slug,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsUser:    &uid,
						RunAsNonRoot: &nonRoot,
					},
					Containers: []corev1.Container{
						{
							Name:            "session-manager",
							Image:           p.cfg.SessionManagerImage,
							ImagePullPolicy: corev1.PullAlways,
							Ports: []corev1.ContainerPort{
								{Name: "http", ContainerPort: 8090, Protocol: corev1.ProtocolTCP},
							},
							EnvFrom: []corev1.EnvFromSource{
								{
									ConfigMapRef: &corev1.ConfigMapEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: at.TenantID + "-config",
										},
									},
								},
								{
									SecretRef: &corev1.SecretEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "orbit-secrets",
										},
									},
								},
							},
							Env: []corev1.EnvVar{
								{Name: "ANTHROPIC_BASE_URL", Value: "http://orbit-inference-gateway.orbit-system.svc.cluster.local"},
								{Name: "ORBIT_POSTGRES_SSL_MODE", Value: p.cfg.SessionManagerPostgresSSL},
								// orbit-agents URL for live event push (ADR-0054).
								{Name: "ORBIT_AGENTS_URL", Value: fmt.Sprintf("http://orbit-agents.%s.svc.cluster.local", p.cfg.SystemNamespace)},
								// Placeholder key — actual auth is the identity-proxy-issued JWT
								// injected on every outbound request by the Envoy ext_proc filter.
								// Claude Code requires a non-empty value to start in --bare mode.
								{Name: "ANTHROPIC_API_KEY", Value: "orbit-managed"},
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.FromString("http"),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       10,
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("250m"),
									corev1.ResourceMemory: resource.MustParse("512Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("2"),
									corev1.ResourceMemory: resource.MustParse("4Gi"),
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "workspaces", MountPath: "/workspaces"},
							},
						},
					},
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "workspaces"},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("10Gi"),
							},
						},
					},
				},
			},
		},
	}
	_, err := p.client.AppsV1().StatefulSets(namespace).Create(ctx, ss, metav1.CreateOptions{})
	return ignoreAlreadyExists(err)
}

func (p *K8sProvisioner) ensureHeadlessService(ctx context.Context, name, namespace string, labels map[string]string) error {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None",
			Selector:  labels,
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 8090, TargetPort: intstr.FromString("http"), Protocol: corev1.ProtocolTCP},
			},
		},
	}
	_, err := p.client.CoreV1().Services(namespace).Create(ctx, svc, metav1.CreateOptions{})
	return ignoreAlreadyExists(err)
}

func (p *K8sProvisioner) ensureHPA(ctx context.Context, name, namespace string, labels map[string]string) error {
	cpuTarget := int32(70)
	memTarget := int32(80)
	minReplicas := int32(1)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "StatefulSet",
				Name:       name,
			},
			MinReplicas: &minReplicas,
			MaxReplicas: 10,
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceCPU,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: &cpuTarget,
						},
					},
				},
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceMemory,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: &memTarget,
						},
					},
				},
			},
		},
	}
	_, err := p.client.AutoscalingV2().HorizontalPodAutoscalers(namespace).Create(ctx, hpa, metav1.CreateOptions{})
	return ignoreAlreadyExists(err)
}

func (p *K8sProvisioner) ensurePDB(ctx context.Context, name, namespace string, labels map[string]string) error {
	minAvailable := intstr.FromInt32(1)
	pdb := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable: &minAvailable,
			Selector:     &metav1.LabelSelector{MatchLabels: labels},
		},
	}
	_, err := p.client.PolicyV1().PodDisruptionBudgets(namespace).Create(ctx, pdb, metav1.CreateOptions{})
	return ignoreAlreadyExists(err)
}

// ensureDestinationRule creates an Istio DestinationRule in the system namespace
// that tells Istio the exact SPIFFE SAN to expect when orbit-agents connects to
// this agent type's headless service.
//
// Headless services in Istio use ORIGINAL_DST clusters, which cannot perform
// EDS-based endpoint identity lookup. Without this rule, istiod falls back to
// the standard formula (spiffe://<td>/ns/<ns>/sa/<sa>), which doesn't match the
// SPIRE-issued SPIFFE ID (spiffe://<td>/tenant/<ns>/agent/<slug>).
func (p *K8sProvisioner) ensureDestinationRule(ctx context.Context, at AgentType, namespace string) error {
	spiffeID := p.agentSpiffeID(at, namespace)
	host := fmt.Sprintf("%s.%s.svc.cluster.local", at.Slug, namespace)
	dr := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "networking.istio.io/v1beta1",
			"kind":       "DestinationRule",
			"metadata": map[string]any{
				"name":      "agent-" + at.Slug,
				"namespace": p.cfg.SystemNamespace,
			},
			"spec": map[string]any{
				"host": host,
				"trafficPolicy": map[string]any{
					"portLevelSettings": []any{
						map[string]any{
							"port": map[string]any{
								"number": int64(8090),
							},
							"tls": map[string]any{
								"mode":            "ISTIO_MUTUAL",
								"subjectAltNames": []any{spiffeID},
							},
						},
					},
				},
			},
		},
	}
	_, err := p.dynamicClient.Resource(destinationRuleGVR).
		Namespace(p.cfg.SystemNamespace).
		Create(ctx, dr, metav1.CreateOptions{})
	return ignoreAlreadyExists(err)
}

func (p *K8sProvisioner) deleteDestinationRule(ctx context.Context, slug string) error {
	err := p.dynamicClient.Resource(destinationRuleGVR).
		Namespace(p.cfg.SystemNamespace).
		Delete(ctx, "agent-"+slug, metav1.DeleteOptions{})
	return ignoreNotFound(err)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func agentTypeLabels(at AgentType) map[string]string {
	return map[string]string{
		"app":                 at.Slug,
		"orbit.io/agent-type": at.ID,
		"orbit.io/tenant":     at.TenantID,
	}
}

func podLabels(at AgentType) map[string]string {
	return map[string]string{
		"app":                    at.Slug,
		"orbit.io/agent-type":    at.ID,
		"orbit.io/tenant":        at.TenantID,
		"orbit.io/managed-agent": "true",
		// slug used so the SPIFFE ID is human-readable:
		// spiffe://<trust-domain>/tenant/<namespace>/agent/<slug>
		"spiffe.io/agent":                  at.Slug,
		"spiffe.io/spire-managed-identity": "true",
		"orbit.io/identity-proxy":          "enabled",
	}
}

func ignoreNotFound(err error) error {
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func ignoreAlreadyExists(err error) error {
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}
