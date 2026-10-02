package agents

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"time"

	"github.com/clear-route/orbit/internal/agents/manifestpatch"
	"github.com/clear-route/orbit/internal/agents/sandbox/execauth"
	"github.com/clear-route/orbit/internal/agents/sandbox/kubeconfig"
	"github.com/clear-route/orbit/internal/agents/sandbox/runfs"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	policyclient "github.com/clear-route/orbit/internal/platform/clients/policy"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/ports"
)

var k8sTracer = otel.Tracer("orbit.agents.k8s")

var destinationRuleGVR = schema.GroupVersionResource{
	Group:    "networking.istio.io",
	Version:  "v1beta1",
	Resource: "destinationrules",
}

// sessionManagerPort is the port each agent pod's session-manager listens on.
// Must match cmd/session-manager/config/base.yaml server.port and the podPort
// in internal/platform/clients/sessionmanager.
const sessionManagerPort = 8090

var authorizationPolicyGVR = schema.GroupVersionResource{
	Group:    "security.istio.io",
	Version:  "v1beta1",
	Resource: "authorizationpolicies",
}

// K8sProvisionerConfig holds the parameters the provisioner needs that cannot
// be derived from the agent type or run model.
type K8sProvisionerConfig struct {
	SessionManagerImage string
	SandboxImage        string
	SandboxGRPCPort     int32
	// GitAuthorName/GitAuthorEmail set the git identity for commits the agent
	// makes in the sandbox (GIT_AUTHOR_*/GIT_COMMITTER_*). Empty = leave unset.
	GitAuthorName  string
	GitAuthorEmail string
	// OTLPEndpoint is passed to the session-manager so a provisioned agent
	// exports to the same collector as the platform. Empty disables its export.
	OTLPEndpoint string
	// SamplingRate is passed alongside it so a provisioned agent samples at the
	// same rate as the platform. Empty keeps the image's own default.
	SamplingRate string
	// ImagePullSecrets names the Secrets, present in every tenant namespace,
	// that runtime-created pods pull the session-manager and sandbox images with.
	ImagePullSecrets []string
	// TrustDomain is the SPIFFE trust domain (e.g. "orbit.test").
	// Used to build the agent's SPIFFE principal ID for grant creation.
	TrustDomain string
	// SystemNamespace is the namespace where orbit-agents runs.
	// DestinationRules are created here so Istio applies them to outbound
	// traffic from orbit-agents pods targeting agent session-managers.
	// Also used to derive the egress-gateway service URL for sandbox
	// HTTPS_PROXY (the gateway always lives in this namespace).
	SystemNamespace string
	// Patches are kustomize `patches:` entries loaded from disk at startup and
	// applied to each provisioned resource after the default manifest is
	// built. See docs/adr/2026-09-15-manifest-patches-configmap.md.
	Patches []manifestpatch.Patch
	// Workloads customizes the pods provisioned in tenant namespaces: their
	// placement, pod metadata and service account annotations. Applied before
	// Patches, so a patch still wins on a field both set.
	// See docs/adr/2026-09-16-first-class-workload-placement.md.
	Workloads WorkloadsConfig
	// Workspace is the storage backing each agent's workspace volume, which the
	// session-manager and every sandbox of its runs mount.
	Workspace WorkspaceConfig
	// AuthorizationPolicyEnabled creates an Istio AuthorizationPolicy beside
	// each agent StatefulSet restricting its session-manager port to the
	// orbit-agents principal. Requires SPIFFE/mTLS: without attested identities
	// no source matches the rule and every call is denied, so the chart gates
	// this on platform.spiffe.enabled (charts/orbit/templates/agents/deployment.yaml).
	AuthorizationPolicyEnabled bool
	// NodeMaxOldSpaceMB is passed to each session-manager, which caps the heap
	// of Node processes its runs start. Zero disables the cap.
	NodeMaxOldSpaceMB int
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
	scopeResolver *ScopeResolver
}

// SetScopeResolver installs the resolver that compiles an agent type's full
// effective grant scopes; its own scopes plus the egress and connectors
// contributed by its referenced environment, plus connector auto-egress. When
// unset, grant reconciliation falls back to the agent's own configured scopes
// (AgentType.EffectiveGrantScopes), so a partially-wired deployment still works.
func (p *K8sProvisioner) SetScopeResolver(r *ScopeResolver) { p.scopeResolver = r }

// NewK8sProvisioner builds a provisioner using in-cluster config when inCluster
// is true, otherwise falling back to KUBECONFIG (local development).
// policyClient may be nil; grant creation is skipped when unset (local dev).
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
// given namespace. Idempotent; existing resources are left unchanged.
func (p *K8sProvisioner) ProvisionAgentType(ctx context.Context, at AgentType, namespace string) error {
	ctx, span := k8sTracer.Start(ctx, "agents.k8s.ProvisionAgentType",
		trace.WithAttributes(
			attribute.String("slug", at.Slug),
			attribute.String("namespace", namespace),
		),
	)
	defer span.End()

	slug := at.Slug
	labels := agentTypeLabels(at)
	// Owner reference for GitOps observability + k8s-native cascade delete
	// (docs/adr/2026-09-06-owner-references-for-agents-and-sandboxes.md).
	// Zero value when the OrbitTenant CR is unreachable; ensureX no-ops the
	// merge and provisioning proceeds unowned so local-dev without the
	// operator still works.
	tenantRef := p.tenantOwnerRef(ctx, at.TenantID)

	// Patches selected by this agent type's own name or labels match nothing
	// in the startup validation, which renders a stand-in, so a sandbox pinned
	// away from its session-manager over a ReadWriteOnce workspace is caught
	// here instead of on the agent's first run.
	if err := p.checkSandboxPlacement(at, namespace); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "check sandbox placement")
		return err
	}

	if err := p.ensureServiceAccount(ctx, slug, namespace, labels, tenantRef); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "ensure service account")
		return err
	}
	if err := p.ensureStatefulSet(ctx, at, namespace, labels, tenantRef); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "ensure statefulset")
		return err
	}
	if err := p.ensureHeadlessService(ctx, slug, namespace, labels, tenantRef); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "ensure headless service")
		return err
	}
	if err := p.ensureHPA(ctx, slug, namespace, labels, tenantRef); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "ensure hpa")
		return err
	}
	if err := p.ensurePDB(ctx, slug, namespace, labels, tenantRef); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "ensure pdb")
		return err
	}
	if err := p.ensureAgentGrant(ctx, at, namespace); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "ensure agent grant")
		return err
	}
	if err := p.ensureDestinationRule(ctx, at, namespace, tenantRef); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "ensure destination rule")
		return err
	}
	if err := p.ensureAuthorizationPolicy(ctx, at, namespace, tenantRef); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "ensure authorization policy")
		return err
	}
	return nil
}

// DeprovisionAgentType deletes all k8s resources created by ProvisionAgentType.
// Missing resources are silently ignored.
func (p *K8sProvisioner) DeprovisionAgentType(ctx context.Context, at AgentType, namespace string) error {
	ctx, span := k8sTracer.Start(ctx, "agents.k8s.DeprovisionAgentType",
		trace.WithAttributes(
			attribute.String("slug", at.Slug),
			attribute.String("namespace", namespace),
		),
	)
	defer span.End()

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
			// Sweeps claims that predate the retention policy set below.
			return p.deleteWorkspacePVCs(ctx, at, namespace)
		},
		func() error {
			return p.deleteAgentGrant(ctx, at, namespace)
		},
		func() error {
			return p.deleteDestinationRule(ctx, at.Slug)
		},
		func() error {
			return p.deleteAuthorizationPolicy(ctx, at.Slug, namespace)
		},
	}

	var firstErr error
	for _, fn := range resources {
		if err := fn(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		span.RecordError(firstErr)
		span.SetStatus(codes.Error, "deprovision agent type")
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

// WaitForStatefulSetReady polls until the StatefulSet for the given slug has at
// least one ready replica, or the context deadline is reached.
func (p *K8sProvisioner) WaitForStatefulSetReady(ctx context.Context, slug, namespace string) error {
	return wait.PollUntilContextTimeout(ctx, 5*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		ss, err := p.client.AppsV1().StatefulSets(namespace).Get(ctx, slug, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("get statefulset %s/%s: %w", namespace, slug, err)
		}
		return ss.Status.ReadyReplicas >= 1, nil
	})
}

// RolloutStatefulSet triggers a rolling restart of the StatefulSet for the
// given slug by patching the pod template with a restartedAt annotation.
// This is equivalent to `kubectl rollout restart statefulset/<slug>` and
// ensures that pods pick up new configuration (e.g. a changed framework or
// model_id) without requiring a manual recycle.
func (p *K8sProvisioner) RolloutStatefulSet(ctx context.Context, slug, namespace string) error {
	ctx, span := k8sTracer.Start(ctx, "agents.k8s.RolloutStatefulSet",
		trace.WithAttributes(
			attribute.String("slug", slug),
			attribute.String("namespace", namespace),
		),
	)
	defer span.End()

	type patchAnnotations struct {
		Spec struct {
			Template struct {
				Metadata struct {
					Annotations map[string]string `json:"annotations"`
				} `json:"metadata"`
			} `json:"template"`
		} `json:"spec"`
	}
	var p2 patchAnnotations
	p2.Spec.Template.Metadata.Annotations = map[string]string{
		"kubectl.kubernetes.io/restartedAt": time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.Marshal(p2)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "marshal rollout patch")
		return fmt.Errorf("marshal rollout patch: %w", err)
	}
	if _, err := p.client.AppsV1().StatefulSets(namespace).Patch(
		ctx, slug, types.StrategicMergePatchType, data, metav1.PatchOptions{},
	); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "patch statefulset for rollout")
		return fmt.Errorf("patch statefulset %s/%s for rollout: %w", namespace, slug, err)
	}
	return nil
}

// ── Sandbox provisioning ──────────────────────────────────────────────────────

// errSandboxToken is the span status for a sandbox that has no usable exec token.
var errSandboxToken = errors.New("sandbox exec token unavailable")

// Sandbox is a ready sandbox pod: where to reach its exec server, and the
// per-run token that server requires.
type Sandbox struct {
	Endpoint  string `json:"endpoint"`
	ExecToken string `json:"exec_token"`
	// CanaryHost is a host the agent may reach, which the session-manager probes
	// before handing the sandbox over. Empty skips the probe.
	CanaryHost string `json:"canary_host,omitempty"`
}

// ProvisionSandbox creates a sandbox pod for the given run and waits for it to
// be Ready. Returns where to reach its exec server ("podIP:port") and the
// per-run token that server requires on every call; a caller holding only the
// endpoint cannot execute anything. The caller (session-manager) writes both
// into the run's workspace (docs/adr/2026-09-18-sandbox-exec-trust-boundary.md).
//
// The sandbox pod mounts the same workspace PVC as the agent StatefulSet pod
// (named "workspaces-{slug}-{ordinal}") so files written by the agent container
// (Edit, Write tools) are immediately visible in the sandbox without copying.
// The PVC is shared by every run scheduled onto that pod ordinal, so the
// mount is scoped with SubPath: run.ID (see sandboxContainer); this run's
// sandbox pod only ever has its own {runID} directory mounted, never a
// sibling run's. See https://github.com/clear-route/orbit/issues/134.
func (p *K8sProvisioner) ProvisionSandbox(ctx context.Context, run Run, agentTypeSlug, namespace string, kubeClusters []kubeconfig.Cluster) (Sandbox, error) {
	ctx, span := k8sTracer.Start(ctx, "agents.k8s.ProvisionSandbox",
		trace.WithAttributes(
			attribute.String("run_id", run.ID),
			attribute.String("agent_type_slug", agentTypeSlug),
			attribute.String("namespace", namespace),
		),
	)
	defer span.End()

	token, err := newSandboxExecToken()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, errSandboxToken.Error())
		return Sandbox{}, fmt.Errorf("mint sandbox exec token: %w", err)
	}

	pod := p.buildSandboxPod(run, agentTypeSlug, namespace, kubeClusters, token)
	podName := pod.Name
	// Owner reference on the parent StatefulSet: sandbox pods share its
	// workspace PVC via subPath, so their lifecycle is bound to it. Deleting
	// the StatefulSet cascades to in-flight sandboxes; the StatefulSet itself
	// is owned by the tenant CR
	// (docs/adr/2026-09-06-owner-references-for-agents-and-sandboxes.md).
	// Best-effort: on any lookup error we log and proceed unowned so
	// sandbox creation is never gated on the ownerRef path.
	applyOwnerRef(&pod.ObjectMeta, p.statefulSetOwnerRef(ctx, agentTypeSlug, namespace))

	podVal, err := manifestpatch.Apply(*pod, "Pod", p.cfg.Patches)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "apply Pod patches")
		return Sandbox{}, fmt.Errorf("apply Pod patches: %w", err)
	}

	_, err = p.client.CoreV1().Pods(namespace).Create(ctx, &podVal, metav1.CreateOptions{})
	switch {
	case err == nil:
		p.logger.Info(ctx, "Sandbox pod created, waiting for ready",
			logging.Field{Key: "run_id", Value: run.ID},
			logging.Field{Key: "pod", Value: podName},
			logging.Field{Key: "namespace", Value: namespace},
		)
	case apierrors.IsAlreadyExists(err):
		// Reuse an existing pod if alive; replace it if dead or terminating.
		replaced, repErr := p.replaceIfDeadSandboxPod(ctx, &podVal, namespace)
		if repErr != nil {
			span.RecordError(repErr)
			span.SetStatus(codes.Error, "replace dead sandbox pod")
			return Sandbox{}, repErr
		}
		p.logger.Info(ctx, "Sandbox pod already exists",
			logging.Field{Key: "run_id", Value: run.ID},
			logging.Field{Key: "pod", Value: podName},
			logging.Field{Key: "replaced_dead_pod", Value: replaced},
		)
	default:
		span.RecordError(err)
		span.SetStatus(codes.Error, "create sandbox pod")
		return Sandbox{}, fmt.Errorf("create sandbox pod: %w", err)
	}

	ready, err := p.waitForPodReady(ctx, podName, namespace)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "wait for sandbox pod ready")
		return Sandbox{}, fmt.Errorf("wait for sandbox pod ready: %w", err)
	}

	// Read the token off the ready pod, not the freshly minted one: a reused pod
	// still holds the token it was created with.
	liveToken := sandboxExecTokenOf(ready)
	if liveToken == "" {
		span.SetStatus(codes.Error, errSandboxToken.Error())
		return Sandbox{}, fmt.Errorf("%w: pod %s carries no %s", errSandboxToken, podName, sandboxExecTokenEnv)
	}

	endpoint := fmt.Sprintf("%s:%d", ready.Status.PodIP, p.cfg.SandboxGRPCPort)
	span.SetAttributes(attribute.String("endpoint", endpoint))
	p.logger.Info(ctx, "Sandbox pod ready",
		logging.Field{Key: "run_id", Value: run.ID},
		logging.Field{Key: "pod", Value: podName},
		logging.Field{Key: "endpoint", Value: endpoint},
	)
	return Sandbox{Endpoint: endpoint, ExecToken: liveToken}, nil
}

// errSandboxUnusable reports a sandbox pod that exists but cannot serve the
// run. Distinct from no pod at all, which is the only state that may route the
// agent's command to a local shell.
var errSandboxUnusable = errors.New("sandbox pod unusable")

// LookupSandbox returns the run's sandbox as its live pod describes it, and
// whether one exists. It creates nothing.
//
// The pod is the durable record: it carries the exec token in its spec and the
// endpoint in its status, and it outlives both the session-manager's memory and
// anything the agent can delete from its workspace. A pod that is not ready has
// no usable endpoint yet, so it reports no sandbox rather than a half one.
func (p *K8sProvisioner) LookupSandbox(ctx context.Context, runID, namespace string) (Sandbox, bool, error) {
	ctx, span := k8sTracer.Start(ctx, "agents.k8s.LookupSandbox",
		trace.WithAttributes(
			attribute.String("run_id", runID),
			attribute.String("namespace", namespace),
		),
	)
	defer span.End()

	pod, err := p.client.CoreV1().Pods(namespace).Get(ctx, fmt.Sprintf("sandbox-%s", runID), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return Sandbox{}, false, nil
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "get sandbox pod")
		return Sandbox{}, false, fmt.Errorf("get sandbox pod: %w", err)
	}
	// A pod that exists but cannot be used is not the same answer as no pod.
	// found=false reaches orbit-shell as "this run never had a sandbox", which
	// sends the command to a local shell, so only a genuinely absent pod earns
	// it. Everything else is an error, and the caller stops.
	switch {
	case pod.DeletionTimestamp != nil:
		return Sandbox{}, false, fmt.Errorf("%w: sandbox pod %s is terminating", errSandboxUnusable, pod.Name)
	case pod.Status.Phase == corev1.PodFailed, pod.Status.Phase == corev1.PodSucceeded:
		return Sandbox{}, false, fmt.Errorf("%w: sandbox pod %s is %s", errSandboxUnusable, pod.Name, pod.Status.Phase)
	case pod.Status.PodIP == "":
		return Sandbox{}, false, fmt.Errorf("%w: sandbox pod %s has no address yet", errSandboxUnusable, pod.Name)
	}
	token := sandboxExecTokenOf(pod)
	if token == "" {
		return Sandbox{}, false, fmt.Errorf("%w: sandbox pod %s carries no %s", errSandboxUnusable, pod.Name, sandboxExecTokenEnv)
	}
	return Sandbox{
		Endpoint:  fmt.Sprintf("%s:%d", pod.Status.PodIP, p.cfg.SandboxGRPCPort),
		ExecToken: token,
	}, true, nil
}

// newSandboxExecToken mints the per-run secret that sandbox-exec requires on
// every Execute. 32 bytes from crypto/rand, hex encoded so it survives an env var.
func newSandboxExecToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// sandboxExecTokenOf returns the exec token the pod was created with, or empty
// if it carries none.
func sandboxExecTokenOf(pod *corev1.Pod) string {
	for _, c := range pod.Spec.Containers {
		if c.Name != sandboxContainerName {
			continue
		}
		for _, e := range c.Env {
			if e.Name == sandboxExecTokenEnv {
				return e.Value
			}
		}
	}
	return ""
}

// buildSandboxPod renders the sandbox pod for a run before manifest patches
// are applied.
func (p *K8sProvisioner) buildSandboxPod(run Run, agentTypeSlug, namespace string, kubeClusters []kubeconfig.Cluster, execToken string) *corev1.Pod {
	podName := fmt.Sprintf("sandbox-%s", run.ID)
	// PVC name follows StatefulSet volumeClaimTemplate convention:
	// {volumeName}-{statefulSetName}-{ordinal}
	workspacesPVC := fmt.Sprintf("workspaces-%s-%d", agentTypeSlug, run.PodOrdinal)

	// Labels carry the pod's identity for the operator pod webhook and SPIRE.
	//   - orbit.io/role=sandbox: namespace-policy targeting and the future
	//     tenant NetworkPolicy locking egress to the gateway.
	//   - orbit.io/run-id, orbit.io/agent-slug: matched by the
	//     orbit-tenant-sandbox ClusterSPIFFEID template (Phase 4b) so the
	//     pod is attested as
	//     spiffe://<td>/tenant/<ns>/agent/<slug>/sandbox/<run-id>.
	//     The principal extractor (internal/identity/principal_extractor.go)
	//     normalizes this back to the agent principal for authorization
	//     while lifting run_id into claims for attribution.
	//
	// We deliberately NO LONGER set orbit.io/skip-injection: per
	// docs/adr/2026-06-06-jit-credential-brokering-egress-gateway.md the
	// sandbox rejoins the mesh; the operator pod webhook adds the
	// identity-proxy + opa-policy + spire injection templates, giving the
	// sandbox the same outbound enforcement chain as the agent StatefulSet
	// pod. Without injection the credential broker would be unreachable.
	labels := map[string]string{
		"orbit.io/role":       "sandbox",
		"orbit.io/run-id":     run.ID,
		"orbit.io/agent-slug": agentTypeSlug,
	}

	// Sandbox-exec is a gRPC server on port 50051 with no other inbound or
	// outbound traffic on its own (orbit-shell connects directly via plain
	// TCP); however the sandbox pod ALSO hosts the agent's tooling
	// (git/curl/SDKs) which need outbound HTTPS through the broker. Istio
	// injection is therefore required; see comment on the labels block.
	annotations := map[string]string{
		// Exclude the gRPC port from the inbound mesh so orbit-shell still
		// connects over plain TCP, matching the existing pre-injection
		// behavior. Outbound to the egress-gateway intentionally STAYS in
		// the mesh; the sandbox's identity-proxy ext_proc filter must see
		// the CONNECT and inject the platform Bearer token, which is the
		// gateway's only signal of caller identity. The CONNECT method is
		// passed through HCM via the egress-gateway EnvoyFilter
		// (charts/orbit/templates/egress-gateway/envoyfilter.yaml).
		"traffic.sidecar.istio.io/excludeInboundPorts": fmt.Sprintf("%d", p.cfg.SandboxGRPCPort),
		// Hold every other container until istio-proxy is ready. Without
		// this, identity-proxy starts immediately and times out after 60s
		// waiting for Envoy, since istio-proxy is the LAST container
		// kubelet starts (it follows the largest image, sandbox-exec, whose
		// pull alone can take ~50s on a cold node). Same wiring the
		// session-manager StatefulSet pod uses.
		"proxy.istio.io/config": sidecarProxyConfig,
	}

	pod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
		ObjectMeta: metav1.ObjectMeta{
			Name:        podName,
			Namespace:   namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			// Nothing in the sandbox calls the Kubernetes API. A mounted token would only give a compromised agent a credential to steal.
			AutomountServiceAccountToken: new(false),
			// Service links would list every Service in the tenant namespace in each command's environment.
			EnableServiceLinks: new(false),
			ImagePullSecrets:   imagePullSecretRefs(p.cfg.ImagePullSecrets),
			SecurityContext:    restrictedPodSecurityContext(),
			Volumes:            sandboxVolumes(workspacesPVC, p.cfg),
			Containers:         []corev1.Container{sandboxContainer(p.cfg, kubeClusters, run.ID, execToken)},
		},
	}

	sandbox := p.cfg.Workloads.ForSandbox()
	sandbox.applyToPodMeta(&pod.ObjectMeta)
	sandbox.applyToPodSpec(&pod.Spec)

	// Applied last, so configured placement cannot drop it: a ReadWriteOnce
	// workspace attaches to one node, and this sandbox mounts the workspace of
	// session-manager pod {slug}-{ordinal}.
	if !p.cfg.Workspace.crossesNodes() {
		requireCoLocation(&pod.Spec, fmt.Sprintf("%s-%d", agentTypeSlug, run.PodOrdinal))
	}

	return pod
}

// imagePullSecretRefs turns configured Secret names into pod references, or
// nil so an unconfigured install leaves the field off the spec entirely.
func imagePullSecretRefs(names []string) []corev1.LocalObjectReference {
	if len(names) == 0 {
		return nil
	}
	refs := make([]corev1.LocalObjectReference, 0, len(names))
	for _, n := range names {
		refs = append(refs, corev1.LocalObjectReference{Name: n})
	}
	return refs
}

// replaceIfDeadSandboxPod inspects an existing sandbox pod of the same name and,
// if it is dead (PodFailed/PodSucceeded) or already terminating
// (DeletionTimestamp set), deletes it, waits for it to disappear, and creates a
// fresh one. It returns true when it replaced the pod. A live pod
// (Running/Pending and not terminating) is left untouched so warm-start and
// idempotent re-provision calls keep reusing it.
func (p *K8sProvisioner) replaceIfDeadSandboxPod(ctx context.Context, pod *corev1.Pod, namespace string) (bool, error) {
	existing, err := p.client.CoreV1().Pods(namespace).Get(ctx, pod.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		// Vanished between Create and Get; just create it fresh.
		if _, cErr := p.client.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{}); cErr != nil && !apierrors.IsAlreadyExists(cErr) {
			return true, fmt.Errorf("create sandbox pod: %w", cErr)
		}
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("get existing sandbox pod: %w", err)
	}

	alive := existing.DeletionTimestamp == nil &&
		existing.Status.Phase != corev1.PodFailed &&
		existing.Status.Phase != corev1.PodSucceeded
	if alive {
		return false, nil
	}

	del := metav1.DeletePropagationForeground
	if err := ignoreNotFound(p.client.CoreV1().Pods(namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{PropagationPolicy: &del})); err != nil {
		return false, fmt.Errorf("delete dead sandbox pod: %w", err)
	}
	if err := p.waitForPodGone(ctx, pod.Name, namespace); err != nil {
		return false, err
	}
	if _, err := p.client.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return false, fmt.Errorf("recreate sandbox pod: %w", err)
	}
	return true, nil
}

// waitForPodGone polls until the named pod no longer exists.
func (p *K8sProvisioner) waitForPodGone(ctx context.Context, name, namespace string) error {
	return wait.PollUntilContextTimeout(ctx, 2*time.Second, 60*time.Second, true, func(ctx context.Context) (bool, error) {
		_, err := p.client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		return false, nil
	})
}

// DeleteSandbox removes the sandbox pod for a run. Missing pod is silently ignored.
func (p *K8sProvisioner) DeleteSandbox(ctx context.Context, runID, namespace string) error {
	ctx, span := k8sTracer.Start(ctx, "agents.k8s.DeleteSandbox",
		trace.WithAttributes(
			attribute.String("run_id", runID),
			attribute.String("namespace", namespace),
		),
	)
	defer span.End()

	podName := fmt.Sprintf("sandbox-%s", runID)
	p.logger.Info(ctx, "Deleting sandbox pod",
		logging.Field{Key: "run_id", Value: runID},
		logging.Field{Key: "pod", Value: podName},
		logging.Field{Key: "namespace", Value: namespace},
	)
	del := metav1.DeletePropagationForeground
	if err := ignoreNotFound(p.client.CoreV1().Pods(namespace).Delete(ctx, podName, metav1.DeleteOptions{PropagationPolicy: &del})); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "delete sandbox pod")
		return err
	}
	return nil
}

// waitForPodReady polls until the pod is Ready, returning the ready pod.
// Respects the deadline on ctx (the session-manager enforces 300s). The poll
// budget allows for a cold sandbox-exec image pull on a fresh orbit node plus
// istio/sidecar startup; keep it below the session-manager client timeout.
func (p *K8sProvisioner) waitForPodReady(ctx context.Context, name, namespace string) (*corev1.Pod, error) {
	var ready *corev1.Pod
	err := wait.PollUntilContextTimeout(ctx, 3*time.Second, 240*time.Second, true, func(ctx context.Context) (bool, error) {
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
				ready = pod
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	if ready.Status.PodIP == "" {
		return nil, fmt.Errorf("pod ready but no IP assigned")
	}
	return ready, nil
}

// sandboxContainerEnv builds the env block for the sandbox-exec container.
//
// SANDBOX_EXEC_PORT is consumed by sandbox-exec itself.
//
// Egress is TRANSPARENT; the sandbox has NO proxy env. The sandbox Envoy
// rewrites the agent's raw outbound TLS into a CONNECT to the per-tenant egress
// gateway and runs OPA posture + platform-JWT injection on it; see
// docs/adr/2026-06-06-jit-credential-brokering-egress-gateway.md (Amendment 2)
// and charts/orbit/templates/egress-gateway/transparent-egress.yaml. We
// deliberately do NOT set HTTPS_PROXY: it was agent-cooperative (a hostile
// agent ignores it and uses a raw socket), so it was never an enforcement
// boundary. The interception now happens below the agent, at Envoy.
//
// SSL_CERT_FILE / CURL_CA_BUNDLE / NODE_EXTRA_CA_CERTS / REQUESTS_CA_BUNDLE
// point common runtimes at the per-tenant CA root mounted at
// sandboxTenantCAPath. The gateway presents a leaf for whatever upstream the
// tool dialed, signed by this CA; without this hint, every TLS handshake
// would fail because the leaves chain to a CA that's not in the system trust
// store.
//
// When SystemNamespace is empty (defensive; should never happen in
// production) the CA env is omitted so the sandbox simply has no governed
// egress.
func sandboxContainerEnv(cfg K8sProvisionerConfig, kubeClusters []kubeconfig.Cluster, workspaceDir, execToken string) []corev1.EnvVar {
	env := []corev1.EnvVar{
		{Name: "SANDBOX_EXEC_PORT", Value: fmt.Sprintf("%d", cfg.SandboxGRPCPort)},
		// This run's exec token. The agent can read it, which grants nothing it
		// does not already have; it cannot read another run's.
		{Name: sandboxExecTokenEnv, Value: execToken},
		// WORKSPACE is this run's subPath-scoped workspace (see sandboxVolumes/
		// sandboxContainer). sandbox-exec uses it to derive a per-run HOME
		// (mirroring orbit-session-runner's {workDir}/.runhome, see
		// docs/adr/2026-06-04-per-session-home-isolation.md) so kubeconfig and
		// tool caches (npm, pip, go, etc.) never land in the pod's shared
		// default home.
		{Name: "WORKSPACE", Value: workspaceDir},
	}
	// Git identity for commits the agent makes in the sandbox. Setting both
	// GIT_AUTHOR_* and GIT_COMMITTER_* makes the platform identity the author and
	// committer; these override any repo or global git config the agent inherits.
	if cfg.GitAuthorName != "" && cfg.GitAuthorEmail != "" {
		env = append(env,
			corev1.EnvVar{Name: "GIT_AUTHOR_NAME", Value: cfg.GitAuthorName},
			corev1.EnvVar{Name: "GIT_AUTHOR_EMAIL", Value: cfg.GitAuthorEmail},
			corev1.EnvVar{Name: "GIT_COMMITTER_NAME", Value: cfg.GitAuthorName},
			corev1.EnvVar{Name: "GIT_COMMITTER_EMAIL", Value: cfg.GitAuthorEmail},
		)
	}
	// egressGatewayURL is empty only when SystemNamespace is unset; in that
	// case there is no gateway and no per-tenant CA to trust.
	if egressGatewayURL(cfg) == "" {
		return env
	}
	env = append(env,
		// Trust-store hints for the per-tenant CA root. Different runtimes honor
		// different env vars; we set all the common ones so a tool finds the CA
		// wherever it looks. The file itself is mounted by sandboxVolumes below.
		//   SSL_CERT_FILE:       OpenSSL apps (and Go's crypto/tls)
		//   CURL_CA_BUNDLE:      the curl CLI
		//   NODE_EXTRA_CA_CERTS: Node.js
		//   REQUESTS_CA_BUNDLE:  Python requests
		//   GIT_SSL_CAINFO:      git over HTTPS (git manages CAINFO itself and
		//     does NOT read SSL_CERT_FILE/CURL_CA_BUNDLE).
		//   PIP_CERT:            pip (ships its own bundle, ignores
		//     REQUESTS_CA_BUNDLE).
		//
		// These cover runtimes that ship their own CA bundle and ignore the OS
		// trust store. Tools that DO fall back to the OS bundle (wget, the curl
		// CLI, Go binaries like gh/kubectl, ruby, apt) are covered instead by
		// mounting the CA over osTrustBundlePath in sandboxContainer. We do not
		// have to enumerate an env var per tool.
		corev1.EnvVar{Name: "SSL_CERT_FILE", Value: sandboxTenantCAPath},
		corev1.EnvVar{Name: "CURL_CA_BUNDLE", Value: sandboxTenantCAPath},
		corev1.EnvVar{Name: "NODE_EXTRA_CA_CERTS", Value: sandboxTenantCAPath},
		corev1.EnvVar{Name: "REQUESTS_CA_BUNDLE", Value: sandboxTenantCAPath},
		corev1.EnvVar{Name: "GIT_SSL_CAINFO", Value: sandboxTenantCAPath},
		corev1.EnvVar{Name: "PIP_CERT", Value: sandboxTenantCAPath},
		// gh refuses to run without a token, yet the agent never holds a real one:
		// the egress gateway injects a short-lived GitHub credential on the wire and
		// strips this header. A non-empty, obviously-non-functional placeholder lets
		// gh issue the request (which the gateway then authenticates); the value
		// never reaches GitHub and reads clearly as a non-secret to credential scanners.
		corev1.EnvVar{Name: "GH_TOKEN", Value: "placeholder-credential-injected-by-orbit-egress-gateway"},
	)
	// Kubernetes connections the agent is granted. sandbox-exec renders these to
	// $HOME/.kube/config at startup, so the agent runs `kubectl get pods` with no
	// server URL, no kubeconfig and no token; the gateway injects the brokered
	// ServiceAccount credential on the wire. Omitted entirely when the agent has
	// no Kubernetes connector.
	if encoded, err := kubeconfig.Encode(kubeClusters); err == nil && encoded != "" {
		env = append(env, corev1.EnvVar{Name: kubeconfig.ClustersEnvVar, Value: encoded})
	}
	return env
}

// egressGatewayURL returns the in-cluster URL of the egress gateway, or empty
// if SystemNamespace is unset. The gateway always lives in SystemNamespace
// under the well-known service name + port (the shared ports.EgressGatewayCONNECT,
// matching charts/orbit/templates/egress-gateway/service.yaml); there's no
// configurable knob because there's no version of the topology where it
// could be different.
func egressGatewayURL(cfg K8sProvisionerConfig) string {
	if cfg.SystemNamespace == "" {
		return ""
	}
	return fmt.Sprintf("http://orbit-egress-gateway.%s.svc:%d", cfg.SystemNamespace, ports.EgressGatewayCONNECT)
}

// sandboxTenantCAPath is where the tenant CA root is mounted inside the
// sandbox container. The runtime env hints in sandboxContainerEnv all point
// here.
const sandboxTenantCAPath = "/var/run/orbit/tenant-ca/ca.crt"

// osTrustBundlePath is the OS-wide CA bundle (Debian/Ubuntu base images) that
// tools falling back to the system trust store read. Mounting the tenant CA
// here makes git, wget, the curl CLI, Go binaries (gh/kubectl/helm), ruby and
// apt trust the interception cert with no per-tool env var. The sandbox only
// ever sees tenant-CA-signed certs (all egress is intercepted), so replacing
// the bundle with the tenant CA alone is correct, not a downgrade; the
// sandbox never needs to verify a real upstream certificate.
const osTrustBundlePath = "/etc/ssl/certs/ca-certificates.crt"

// tenantCAVolumeName is the projected-volume name for the tenant CA.
const tenantCAVolumeName = "orbit-tenant-ca"

// sandboxVolumes builds the sandbox pod's volume list. The workspaces PVC
// always mounts; the per-tenant CA Secret is added only when an egress
// gateway URL can be derived (otherwise there's nothing to trust).
//
// The CA Secret (orbit-tenant-ca) is created by the operator's
// ensureTenantCA step in the same tenant namespace as the sandbox pod, so
// no cross-namespace projection is needed.
func sandboxVolumes(workspacesPVC string, cfg K8sProvisionerConfig) []corev1.Volume {
	vols := []corev1.Volume{
		{
			Name: "workspaces",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: workspacesPVC,
				},
			},
		},
		scratchVolume(scratchVarTmpVolume, "256Mi"),
		scratchVolume(scratchHomeVolume, "64Mi"),
	}
	if egressGatewayURL(cfg) == "" {
		return vols
	}
	// 0o444: world-readable. The sandbox container runs as a non-root UID
	// the operator does not pre-coordinate with the Secret mount's UID, so
	// any-user-readable is the safest default for a public CA cert. The
	// CA's *private key* is intentionally NOT projected here (Items below
	// only includes ca.crt); the key never leaves the egress gateway pod.
	mode := int32(0o444)
	vols = append(vols, corev1.Volume{
		Name: tenantCAVolumeName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName:  "orbit-tenant-ca",
				DefaultMode: &mode,
				Items: []corev1.KeyToPath{
					// Project ca.crt only; the private key never reaches
					// the sandbox; that lives on the egress gateway pod.
					{Key: "ca.crt", Path: "ca.crt"},
				},
			},
		},
	})
	return vols
}

// Scratch volumes give a read-only root the few writable paths tools expect outside the workspace.
const (
	scratchTmpVolume    = "scratch-tmp"
	scratchVarTmpVolume = "scratch-var-tmp"
	scratchHomeVolume   = "scratch-home"
)

// imageHomeDir is the orbit user's home in both images, used by tools that read it from passwd.
const imageHomeDir = "/home/orbit"

// Ephemeral storage for the app containers. With a read-only root only the scratch volumes and
// container logs use node disk, so 1Gi fits a normal run and stops one pod filling a node.
const (
	appEphemeralRequest = "128Mi"
	appEphemeralLimit   = "1Gi"
)

// scratchVolume is a node-disk emptyDir capped at size.
func scratchVolume(name, size string) corev1.Volume {
	limit := resource.MustParse(size)
	return corev1.Volume{
		Name:         name,
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: &limit}},
	}
}

// workspacesRoot is where the agent pod mounts its workspace PVC; each run's
// workspace is the directory named for it.
const workspacesRoot = "/workspaces"

// runWorkspaceDir is the run's workspace path, the same in the agent pod and its sandbox.
func runWorkspaceDir(runID string) string {
	return filepath.Join(workspacesRoot, runID)
}

// sandboxContainer builds the sandbox-exec container. Mirrors sandboxVolumes:
// when an egress gateway URL can be derived, a read-only mount of the tenant
// CA root is added so the env vars in sandboxContainerEnv point at a real
// file.
//
// The workspaces volume is mounted with SubPath: runID so this pod's view of
// the (StatefulSet-shared) PVC is scoped to its own run directory only:
// sibling runs co-located on the same pod ordinal share the underlying PVC
// but are not mounted into each other's sandbox pods at all. See
// https://github.com/clear-route/orbit/issues/134.
func sandboxContainer(cfg K8sProvisionerConfig, kubeClusters []kubeconfig.Cluster, runID, execToken string) corev1.Container {
	workspaceDir := runWorkspaceDir(runID)
	tmpSubPath, _ := filepath.Rel(workspacesRoot, runfs.TmpDir(workspaceDir))
	mounts := []corev1.VolumeMount{
		{Name: "workspaces", MountPath: workspaceDir, SubPath: runID},
		// The run's TMPDIR at the agent pod's path, and at /tmp for tools that ignore TMPDIR.
		{Name: "workspaces", MountPath: runfs.TmpDir(workspaceDir), SubPath: tmpSubPath},
		{Name: "workspaces", MountPath: "/tmp", SubPath: tmpSubPath},
		// Writable paths outside the workspace under a read-only root, for tools that ignore HOME and TMPDIR.
		{Name: scratchVarTmpVolume, MountPath: "/var/tmp"},
		{Name: scratchHomeVolume, MountPath: imageHomeDir},
	}
	if egressGatewayURL(cfg) != "" {
		mounts = append(mounts,
			corev1.VolumeMount{
				Name:      tenantCAVolumeName,
				MountPath: "/var/run/orbit/tenant-ca",
				ReadOnly:  true,
			},
			// Overlay the OS trust bundle so OS-store tools (wget, curl CLI, Go
			// binaries, ruby, apt) trust the interception cert without a per-tool
			// env var. SubPath mounts a single file and does not auto-update on
			// CA rotation; fine for per-run sandboxes, each of which gets the
			// current CA at provision time.
			corev1.VolumeMount{
				Name:      tenantCAVolumeName,
				MountPath: osTrustBundlePath,
				SubPath:   "ca.crt",
				ReadOnly:  true,
			},
		)
	}
	return corev1.Container{
		Name:  sandboxContainerName,
		Image: cfg.SandboxImage,
		// Always so a freshly built :latest image (new toolchain/versions) reaches
		// new sandboxes; PullIfNotPresent would keep serving the node's cached
		// image. The digest check short-circuits when the image is already cached.
		ImagePullPolicy: corev1.PullAlways,
		Ports: []corev1.ContainerPort{
			{Name: "grpc", ContainerPort: cfg.SandboxGRPCPort, Protocol: corev1.ProtocolTCP},
		},
		// The agent is untrusted code. Dropping all Linux capabilities,
		// critically NET_ADMIN and NET_RAW, is what makes the transparent
		// egress redirect un-bypassable: without them the agent cannot rewrite
		// the pod's iptables or open raw sockets to escape the sidecar
		// interception (docs/adr/2026-06-06-jit-credential-brokering-egress-
		// gateway.md, Amendment 2). Combined with the pod's RunAsNonRoot, the
		// agent has no path back to the network layer Envoy controls.
		SecurityContext: restrictedContainerSecurityContext(),
		Env:             sandboxContainerEnv(cfg, kubeClusters, workspaceDir, execToken),
		VolumeMounts:    mounts,
		WorkingDir:      workspaceDir,
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt32(cfg.SandboxGRPCPort),
				},
			},
			InitialDelaySeconds: 2,
			PeriodSeconds:       3,
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:              resource.MustParse("100m"),
				corev1.ResourceMemory:           resource.MustParse("256Mi"),
				corev1.ResourceEphemeralStorage: resource.MustParse(appEphemeralRequest),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:              resource.MustParse("2"),
				corev1.ResourceMemory:           resource.MustParse("4Gi"),
				corev1.ResourceEphemeralStorage: resource.MustParse(appEphemeralLimit),
			},
		},
	}
}

// ── Policy grant management ───────────────────────────────────────────────────

// agentSpiffeID returns the SPIFFE principal ID for an agent type's pods.
// The identity proxy attests pods using their SPIFFE cert; the slug makes the
// ID human-readable (e.g. spiffe://orbit.test/tenant/orbit-tenant-example/agent/coder).
func (p *K8sProvisioner) agentSpiffeID(at AgentType, namespace string) string {
	return fmt.Sprintf("spiffe://%s/tenant/%s/agent/%s", p.cfg.TrustDomain, namespace, at.Slug)
}

// EnsureAgentGrant reconciles the policy-service grant for the agent type.
// Called on update when model/grant scopes change.
func (p *K8sProvisioner) EnsureAgentGrant(ctx context.Context, at AgentType, namespace string) error {
	return p.ensureAgentGrant(ctx, at, namespace)
}

// ensureAgentGrant reconciles the policy-service grant for this agent's SPIFFE
// principal toward exactly one grant containing at.EffectiveGrantScopes()
// (capability scopes + compiled egress scopes) and, when at.Budget is set,
// the desired budget assignment.
//
// Reconcile logic:
//   - Keep the first existing grant whose scopes (and, when a budget is
//     specified, budget) exactly match the desired state.
//   - Delete all other grants for this principal (duplicates + stale state).
//   - If no matching grant was kept, create one, carrying the desired budget,
//     or preserving the previous grant's budget when none was specified.
//
// Budget semantics: at.Budget == nil means "leave the budget as it is";
// at.Budget with both limits zero means "remove the budget". Budget changes
// recreate the grant (the agent path holds platform:grants:write, not
// platform:budgets:write); per-grant attribution counters restart, while
// enforcement counters (principal/tenant/run) are unaffected.
//
// This handles first-time provisioning, scope and budget updates, and
// deduplication in a single idempotent call.
// Skipped silently when policyClient is nil (local dev without policy service).
func (p *K8sProvisioner) ensureAgentGrant(ctx context.Context, at AgentType, namespace string) error {
	if p.policyClient == nil {
		return nil
	}
	principalID := p.agentSpiffeID(at, namespace)
	grantTenantID := at.TenantID

	ctx, span := k8sTracer.Start(ctx, "agents.k8s.EnsureAgentGrant",
		trace.WithAttributes(
			attribute.String("principal_id", principalID),
			attribute.String("tenant_id", grantTenantID),
		),
	)
	defer span.End()

	existing, err := p.policyClient.ListGrants(ctx, grantTenantID, "AGENT", principalID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "list agent grants")
		return fmt.Errorf("list agent grants for %s: %w", principalID, err)
	}

	// The reconciled scope set is the agent's capability scopes PLUS the egress
	// scopes compiled from its connectivity posture, so egress survives every
	// re-provision (a hand-added egress grant would be deleted as "stale" here).
	// When a scope resolver is wired it also folds in the referenced
	// environment's egress/connectors and each connector's adapter-declared
	// auto-egress; without one it falls back to the agent's own scopes.
	effectiveScopes := at.EffectiveGrantScopes()
	if p.scopeResolver != nil {
		resolved, rErr := p.scopeResolver.EffectiveScopes(ctx, at)
		if rErr != nil {
			span.RecordError(rErr)
			span.SetStatus(codes.Error, "resolve effective scopes")
			return fmt.Errorf("resolve effective scopes for %s: %w", principalID, rErr)
		}
		effectiveScopes = resolved
	}

	// The delegation_required marker mirrors the agent's identity mode: a
	// delegated agent's grant is a ceiling, never a standing entitlement
	// (delegated-agent-identity ADR, Decision 2). Reconciled like scopes; a
	// grant whose marker drifted from the identity mode is stale and replaced.
	delegationRequired := at.IsDelegated()

	// Walk existing grants: keep the first whose SCOPES and delegation marker
	// match (budget is reconciled separately), delete the rest. Remember the
	// kept grant's budget so an unspecified desired budget survives.
	var keepID string
	var keptBudget *policyclient.BudgetAssignment
	for _, g := range existing {
		if keepID == "" && grantMatchesDesired(g, effectiveScopes, delegationRequired) {
			keepID = g.ID
			keptBudget = g.Budget
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

	// A scope-matching grant exists: reconcile only the budget, in place. This
	// avoids a delete+create window that would briefly leave the agent with no
	// authorization, and preserves the grant ID (and its attribution counters).
	// at.Budget == nil means "leave budget unchanged".
	if keepID != "" {
		if at.Budget != nil && !budgetMatches(at.Budget, keptBudget) {
			if err := p.policyClient.UpdateGrantBudget(ctx, keepID, clientBudget(at.Budget)); err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, "update agent grant budget")
				return fmt.Errorf("update agent grant budget for %s: %w", principalID, err)
			}
			p.logger.Info(ctx, "agent grant budget updated",
				logging.Field{Key: "principal_id", Value: principalID},
				logging.Field{Key: "grant_id", Value: keepID},
			)
			return nil
		}
		p.logger.Info(ctx, "agent grant up to date",
			logging.Field{Key: "principal_id", Value: principalID},
			logging.Field{Key: "grant_id", Value: keepID},
		)
		return nil
	}

	// No scope-matching grant: create one, carrying the desired budget (or, when
	// unspecified, preserving the budget of a grant we just deleted).
	createBudget := clientBudget(at.Budget)
	if at.Budget == nil {
		createBudget = firstBudget(existing)
	}
	if _, err := p.policyClient.CreateGrant(ctx, grantTenantID, "AGENT", principalID, effectiveScopes, createBudget, delegationRequired); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "create agent grant")
		return fmt.Errorf("create agent grant for %s: %w", principalID, err)
	}
	p.logger.Info(ctx, "agent grant created",
		logging.Field{Key: "principal_id", Value: principalID},
		logging.Field{Key: "tenant_id", Value: grantTenantID},
	)
	return nil
}

// grantMatchesDesired reports whether an existing grant already reflects the
// desired state: the exact scope set and the delegation_required marker
// matching the agent's identity mode. A grant whose marker drifted is stale
// and gets replaced, exactly like a scope drift.
func grantMatchesDesired(g policyclient.Grant, scopes []string, delegationRequired bool) bool {
	return scopeSetsEqual(g.Scopes, scopes) && g.DelegationRequired == delegationRequired
}

// firstBudget returns the budget of the first grant carrying one, or nil.
func firstBudget(grants []policyclient.Grant) *policyclient.BudgetAssignment {
	for _, g := range grants {
		if g.Budget != nil {
			return g.Budget
		}
	}
	return nil
}

// GetAgentBudget returns the budget currently carried by the agent's grant,
// or nil when no grant or budget exists. Used to enrich agent type reads:
// the grant is the source of truth for budget configuration.
func (p *K8sProvisioner) GetAgentBudget(ctx context.Context, at AgentType, namespace string) (*AgentBudget, error) {
	if p.policyClient == nil {
		return nil, nil
	}
	grants, err := p.policyClient.ListGrants(ctx, at.TenantID, "AGENT", p.agentSpiffeID(at, namespace))
	if err != nil {
		return nil, fmt.Errorf("list agent grants: %w", err)
	}
	for _, g := range grants {
		if g.Budget != nil {
			return &AgentBudget{
				LimitUSD:       g.Budget.LimitUSD,
				ResetCadence:   g.Budget.ResetCadence,
				PerRunLimitUSD: g.Budget.PerRunLimitUSD,
			}, nil
		}
	}
	return nil, nil
}

// clientBudget converts the desired AgentBudget to the policy client shape.
// Returns nil for "no budget" (nil input or both limits zero).
func clientBudget(b *AgentBudget) *policyclient.BudgetAssignment {
	if b == nil || (b.LimitUSD <= 0 && b.PerRunLimitUSD <= 0) {
		return nil
	}
	cadence := b.ResetCadence
	if cadence == "" {
		cadence = "monthly"
	}
	return &policyclient.BudgetAssignment{
		LimitUSD:       b.LimitUSD,
		ResetCadence:   cadence,
		PerRunLimitUSD: b.PerRunLimitUSD,
	}
}

// budgetMatches reports whether an existing grant's budget satisfies the
// desired agent budget. A nil desired budget matches anything (unspecified);
// a zero desired budget matches only grants with no budget.
func budgetMatches(desired *AgentBudget, actual *policyclient.BudgetAssignment) bool {
	if desired == nil {
		return true
	}
	want := clientBudget(desired)
	if want == nil || actual == nil {
		return want == nil && actual == nil
	}
	return want.LimitUSD == actual.LimitUSD &&
		want.ResetCadence == actual.ResetCadence &&
		want.PerRunLimitUSD == actual.PerRunLimitUSD
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

// deleteWorkspacePVCs removes the agent's workspace claims by label.
// DeleteCollection isn't used because the fake clientset used in tests
// doesn't implement it.
func (p *K8sProvisioner) deleteWorkspacePVCs(ctx context.Context, at AgentType, namespace string) error {
	selector := labels.SelectorFromSet(agentTypeLabels(at)).String()
	list, err := p.client.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return ignoreNotFound(err)
	}
	var firstErr error
	for _, pvc := range list.Items {
		if err := ignoreNotFound(p.client.CoreV1().PersistentVolumeClaims(namespace).Delete(ctx, pvc.Name, metav1.DeleteOptions{})); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// deleteAgentGrant removes all policy-service grants for this agent's SPIFFE
// principal. Best-effort; logs and returns nil if no grants exist or the
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

func (p *K8sProvisioner) buildServiceAccount(name, namespace string, labels map[string]string) corev1.ServiceAccount {
	return corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ServiceAccount"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
			// Cloud identity for the session-manager, which is the only pod
			// running under this account.
			Annotations: cloneOrNil(p.cfg.Workloads.SessionManager.ServiceAccount.Annotations),
		},
	}
}

func (p *K8sProvisioner) ensureServiceAccount(ctx context.Context, name, namespace string, labels map[string]string, owner metav1.OwnerReference) error {
	sa := p.buildServiceAccount(name, namespace, labels)
	applyOwnerRef(&sa.ObjectMeta, owner)
	var err error
	sa, err = manifestpatch.Apply(sa, "ServiceAccount", p.cfg.Patches)
	if err != nil {
		return fmt.Errorf("apply ServiceAccount patches: %w", err)
	}
	_, err = p.client.CoreV1().ServiceAccounts(namespace).Create(ctx, &sa, metav1.CreateOptions{})
	if err == nil || !apierrors.IsAlreadyExists(err) {
		return err
	}
	// Sync patched labels/annotations onto an existing ServiceAccount so a
	// patch (e.g. an IRSA role-arn annotation) takes effect on reconcile, not
	// only for freshly-created agent types.
	existing, err := p.client.CoreV1().ServiceAccounts(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get existing serviceaccount: %w", err)
	}
	existing.Labels = sa.Labels
	existing.Annotations = sa.Annotations
	// Backfill owner refs on the existing object so upgraded clusters gain
	// GitOps observability + native GC on the next reconcile
	// (docs/adr/2026-09-06-owner-references-for-agents-and-sandboxes.md).
	applyOwnerRef(&existing.ObjectMeta, owner)
	_, err = p.client.CoreV1().ServiceAccounts(namespace).Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

func (p *K8sProvisioner) ensureStatefulSet(ctx context.Context, at AgentType, namespace string, labels map[string]string, owner metav1.OwnerReference) error {
	ss := p.buildSessionManagerStatefulSet(at, namespace, labels)
	applyOwnerRef(&ss.ObjectMeta, owner)

	ss, err := p.applyStatefulSetPatches(ss)
	if err != nil {
		return err
	}

	_, err = p.client.AppsV1().StatefulSets(namespace).Create(ctx, &ss, metav1.CreateOptions{})
	if err == nil || !apierrors.IsAlreadyExists(err) {
		return err
	}
	// Replace the existing pod template with the rendered one so a patch change
	// reaches existing agents on the next reconcile, keeping only the live
	// session-manager image, which redeploy pins by digest. Replicas belong to
	// the HPA, and the selector and VolumeClaimTemplates are immutable, so
	// those stay create-only.
	existing, err := p.client.AppsV1().StatefulSets(namespace).Get(ctx, ss.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get existing statefulset: %w", err)
	}
	var liveImage string
	for _, c := range existing.Spec.Template.Spec.Containers {
		if c.Name == sessionManagerContainerName {
			liveImage = c.Image
		}
	}
	existing.Spec.Template = ss.Spec.Template
	for i, c := range existing.Spec.Template.Spec.Containers {
		if c.Name == sessionManagerContainerName && liveImage != "" {
			existing.Spec.Template.Spec.Containers[i].Image = liveImage
		}
	}
	// Backfill the OrbitTenant owner reference on the existing StatefulSet.
	// See docs/adr/2026-09-06-owner-references-for-agents-and-sandboxes.md.
	applyOwnerRef(&existing.ObjectMeta, owner)
	_, err = p.client.AppsV1().StatefulSets(namespace).Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

// sessionManagerContainerName is the session-manager container in every agent StatefulSet pod.
const sessionManagerContainerName = "session-manager"

// sandboxContainerName is the sandbox-exec container in every sandbox pod.
const sandboxContainerName = "sandbox-exec"

// sandboxExecTokenEnv carries the per-run secret sandbox-exec requires on every
// Execute (docs/adr/2026-09-18-sandbox-exec-trust-boundary.md).
const sandboxExecTokenEnv = execauth.TokenEnv

// restrictedPodSecurityContext is the pod half of the restricted Pod Security
// Standard, shared by the agent pod and the sandboxes that mount its workspace
// claim. fsGroup is what makes that claim writable on storage classes which
// format volumes root-owned, and OnRootMismatch keeps a large workspace from
// being re-chowned on every start. The seccomp profile is inherited by
// istio-proxy, which sets none of its own.
func restrictedPodSecurityContext() *corev1.PodSecurityContext {
	uid := int64(100)
	// Both agent images run as uid 100, but their primary groups differ
	// (session-manager 101, sandbox 100). Pinning the group makes files on the
	// shared claim look the same from either pod.
	gid := int64(100)
	nonRoot := true
	onRootMismatch := corev1.FSGroupChangeOnRootMismatch
	return &corev1.PodSecurityContext{
		RunAsUser:           &uid,
		RunAsGroup:          &gid,
		RunAsNonRoot:        &nonRoot,
		FSGroup:             &gid,
		FSGroupChangePolicy: &onRootMismatch,
		SeccompProfile:      &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		SELinuxOptions:      &corev1.SELinuxOptions{Level: "s0"},
	}
}

// restrictedContainerSecurityContext is the container half of the restricted
// Pod Security Standard for processes that still need a writable root filesystem.
func restrictedContainerSecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: new(false),
		ReadOnlyRootFilesystem:   new(true),
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
	}
}

// sidecarProxyConfig holds the agent's containers until istio-proxy is ready
// and keeps Istio peer metadata headers off traffic that leaves the mesh. The
// operator webhook enforces the same mode; carrying it here rolls existing pods.
const sidecarProxyConfig = `{"holdApplicationUntilProxyStarts":true,"proxyHeaders":{"metadataExchangeHeaders":{"mode":"IN_MESH"}}}`

// buildSessionManagerStatefulSet renders an agent type's StatefulSet before
// manifest patches are applied.
func (p *K8sProvisioner) buildSessionManagerStatefulSet(at AgentType, namespace string, labels map[string]string) appsv1.StatefulSet {
	replicas := int32(1)
	podAnnotations := map[string]string{
		// The operator's pod webhook adds the SPIRE and identity-proxy
		// wiring; Istio itself injects only the sidecar.
		"proxy.istio.io/config": sidecarProxyConfig,
		// Only the sandbox exec port skips Envoy; the agent-egress NetworkPolicy
		// limits it to the tenant's sandboxes.
		"traffic.sidecar.istio.io/excludeOutboundPorts": "50051",
	}
	// Rolls the pods when the ServiceAccount's cloud identity changes, which
	// they would otherwise pick up only on an unrelated restart.
	if hash := serviceAccountAnnotationHash(p.cfg.Workloads.SessionManager.ServiceAccount.Annotations); hash != "" {
		podAnnotations[serviceAccountHashAnnotation] = hash
	}
	ss := appsv1.StatefulSet{
		TypeMeta:   metav1.TypeMeta{APIVersion: "apps/v1", Kind: "StatefulSet"},
		ObjectMeta: metav1.ObjectMeta{Name: at.Slug, Namespace: namespace, Labels: labels},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &replicas,
			ServiceName: at.Slug,
			Selector:    &metav1.LabelSelector{MatchLabels: podLabels(at)},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      podLabels(at),
					Annotations: podAnnotations,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: at.Slug,
					// The agent pod makes no Kubernetes API calls, so it gets no token an agent could read.
					// Cloud identity webhooks project their own token and are unaffected.
					AutomountServiceAccountToken: new(false),
					// Service links would list every Service in the tenant namespace in the agent's environment.
					EnableServiceLinks: new(false),
					ImagePullSecrets:   imagePullSecretRefs(p.cfg.ImagePullSecrets),
					SecurityContext:    restrictedPodSecurityContext(),
					Containers: []corev1.Container{
						{
							Name:            sessionManagerContainerName,
							Image:           p.cfg.SessionManagerImage,
							ImagePullPolicy: corev1.PullAlways,
							SecurityContext: restrictedContainerSecurityContext(),
							Ports: []corev1.ContainerPort{
								{Name: "http", ContainerPort: sessionManagerPort, Protocol: corev1.ProtocolTCP},
							},
							// No envFrom: the session-manager reads no Secret and no
							// ConfigMap. It reaches Postgres through orbit-agents, so the
							// tenant pod holds no database credential for an agent to read
							// out of its own environment.
							Env: []corev1.EnvVar{
								{Name: "ANTHROPIC_BASE_URL", Value: fmt.Sprintf("http://orbit-inference-gateway.%s.svc.cluster.local", p.cfg.SystemNamespace)},
								{Name: "ORBIT_OTLP_ENDPOINT", Value: p.cfg.OTLPEndpoint},
								{Name: "ORBIT_SAMPLING_RATE", Value: p.cfg.SamplingRate},
								// orbit-agents URL for live event push (docs/adr/2026-05-11-agent-event-streaming-protocol.md).
								{Name: "ORBIT_AGENTS_URL", Value: fmt.Sprintf("http://orbit-agents.%s.svc.cluster.local", p.cfg.SystemNamespace)},
								// Placeholder key; actual auth is the identity-proxy-issued JWT
								// injected on every outbound request by the Envoy ext_proc filter.
								// Claude Code requires a non-empty value to start in --bare mode.
								{Name: "ANTHROPIC_API_KEY", Value: "orbit-managed"},
								{Name: "ORBIT_NODE_MAX_OLD_SPACE_MB", Value: strconv.Itoa(p.cfg.NodeMaxOldSpaceMB)},
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
									corev1.ResourceCPU:              resource.MustParse("250m"),
									corev1.ResourceMemory:           resource.MustParse("512Mi"),
									corev1.ResourceEphemeralStorage: resource.MustParse(appEphemeralRequest),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:              resource.MustParse("2"),
									corev1.ResourceMemory:           resource.MustParse("4Gi"),
									corev1.ResourceEphemeralStorage: resource.MustParse(appEphemeralLimit),
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "workspaces", MountPath: workspacesRoot},
								{Name: scratchTmpVolume, MountPath: "/tmp"},
								{Name: scratchHomeVolume, MountPath: imageHomeDir},
							},
						},
					},
					// Runs keep HOME and TMPDIR on the workspace. These cover tools that ignore both, under a read-only root.
					Volumes: []corev1.Volume{
						scratchVolume(scratchTmpVolume, "256Mi"),
						scratchVolume(scratchHomeVolume, "64Mi"),
					},
				},
			},
			// WhenScaled stays Retain: an HPA scale-down is not agent deletion.
			PersistentVolumeClaimRetentionPolicy: &appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy{
				WhenDeleted: appsv1.DeletePersistentVolumeClaimRetentionPolicyType,
				WhenScaled:  appsv1.RetainPersistentVolumeClaimRetentionPolicyType,
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					// Labeled so DeprovisionAgentType can select it by agent type.
					ObjectMeta: metav1.ObjectMeta{Name: "workspaces", Labels: labels},
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

	sessionManager := p.cfg.Workloads.ForSessionManager()
	sessionManager.applyToPodMeta(&ss.Spec.Template.ObjectMeta)
	sessionManager.applyToPodSpec(&ss.Spec.Template.Spec)
	p.cfg.Workspace.applyToClaim(&ss.Spec.VolumeClaimTemplates[0])

	return ss
}

func (p *K8sProvisioner) buildHeadlessService(name, namespace string, labels map[string]string) corev1.Service {
	return corev1.Service{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None",
			Selector:  labels,
			Ports: []corev1.ServicePort{
				{Name: "http", Port: sessionManagerPort, TargetPort: intstr.FromString("http"), Protocol: corev1.ProtocolTCP},
			},
		},
	}
}

func (p *K8sProvisioner) ensureHeadlessService(ctx context.Context, name, namespace string, labels map[string]string, owner metav1.OwnerReference) error {
	svc := p.buildHeadlessService(name, namespace, labels)
	applyOwnerRef(&svc.ObjectMeta, owner)
	var err error
	svc, err = manifestpatch.Apply(svc, "Service", p.cfg.Patches)
	if err != nil {
		return fmt.Errorf("apply Service patches: %w", err)
	}
	_, err = p.client.CoreV1().Services(namespace).Create(ctx, &svc, metav1.CreateOptions{})
	if err == nil || !apierrors.IsAlreadyExists(err) {
		return err
	}
	existing, err := p.client.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get existing service: %w", err)
	}
	existing.Labels = svc.Labels
	existing.Annotations = svc.Annotations
	// ClusterIP and the selector are immutable or protected; ports are the
	// mutable part of the spec a patch can change.
	existing.Spec.Ports = svc.Spec.Ports
	applyOwnerRef(&existing.ObjectMeta, owner)
	_, err = p.client.CoreV1().Services(namespace).Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

func (p *K8sProvisioner) buildHPA(name, namespace string, labels map[string]string) autoscalingv2.HorizontalPodAutoscaler {
	cpuTarget := int32(70)
	memTarget := int32(80)
	minReplicas := int32(1)
	return autoscalingv2.HorizontalPodAutoscaler{
		TypeMeta:   metav1.TypeMeta{APIVersion: "autoscaling/v2", Kind: "HorizontalPodAutoscaler"},
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
}

// ensureHPA creates an HPA targeting the agent StatefulSet.
// Note: when the HPA scales the StatefulSet down, Kubernetes does NOT delete the
// PVCs created by the StatefulSet's VolumeClaimTemplate. Orphaned workspace PVCs
// accumulate silently. A cleanup controller or CronJob should prune PVCs whose
// owning pod no longer exists. Track in the sandbox hardening story.
func (p *K8sProvisioner) ensureHPA(ctx context.Context, name, namespace string, labels map[string]string, owner metav1.OwnerReference) error {
	hpa := p.buildHPA(name, namespace, labels)
	applyOwnerRef(&hpa.ObjectMeta, owner)
	var err error
	hpa, err = manifestpatch.Apply(hpa, "HorizontalPodAutoscaler", p.cfg.Patches)
	if err != nil {
		return fmt.Errorf("apply HorizontalPodAutoscaler patches: %w", err)
	}
	_, err = p.client.AutoscalingV2().HorizontalPodAutoscalers(namespace).Create(ctx, &hpa, metav1.CreateOptions{})
	if err == nil || !apierrors.IsAlreadyExists(err) {
		return err
	}
	existing, err := p.client.AutoscalingV2().HorizontalPodAutoscalers(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get existing hpa: %w", err)
	}
	existing.Labels = hpa.Labels
	existing.Annotations = hpa.Annotations
	existing.Spec = hpa.Spec
	applyOwnerRef(&existing.ObjectMeta, owner)
	_, err = p.client.AutoscalingV2().HorizontalPodAutoscalers(namespace).Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

func (p *K8sProvisioner) buildPDB(name, namespace string, labels map[string]string) policyv1.PodDisruptionBudget {
	minAvailable := intstr.FromInt32(1)
	return policyv1.PodDisruptionBudget{
		TypeMeta:   metav1.TypeMeta{APIVersion: "policy/v1", Kind: "PodDisruptionBudget"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable: &minAvailable,
			Selector:     &metav1.LabelSelector{MatchLabels: labels},
		},
	}
}

func (p *K8sProvisioner) ensurePDB(ctx context.Context, name, namespace string, labels map[string]string, owner metav1.OwnerReference) error {
	pdb := p.buildPDB(name, namespace, labels)
	applyOwnerRef(&pdb.ObjectMeta, owner)
	var err error
	pdb, err = manifestpatch.Apply(pdb, "PodDisruptionBudget", p.cfg.Patches)
	if err != nil {
		return fmt.Errorf("apply PodDisruptionBudget patches: %w", err)
	}
	_, err = p.client.PolicyV1().PodDisruptionBudgets(namespace).Create(ctx, &pdb, metav1.CreateOptions{})
	if err == nil || !apierrors.IsAlreadyExists(err) {
		return err
	}
	existing, err := p.client.PolicyV1().PodDisruptionBudgets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get existing pdb: %w", err)
	}
	existing.Labels = pdb.Labels
	existing.Annotations = pdb.Annotations
	existing.Spec.MinAvailable = pdb.Spec.MinAvailable
	// spec.selector is immutable on PodDisruptionBudget; a patch changing it
	// only takes effect on a fresh PDB (delete + recreate), not on this
	// update path.
	applyOwnerRef(&existing.ObjectMeta, owner)
	_, err = p.client.PolicyV1().PodDisruptionBudgets(namespace).Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

// ensureDestinationRule creates an Istio DestinationRule in the system namespace
// that tells Istio the exact SPIFFE SAN to expect when orbit-agents connects to
// this agent type's headless service.
//
// Headless services in Istio use ORIGINAL_DST clusters, which cannot perform
// EDS-based endpoint identity lookup. Without this rule, istiod falls back to
// the standard formula (spiffe://<td>/ns/<ns>/sa/<sa>), which doesn't match the
// SPIRE-issued SPIFFE ID (spiffe://<td>/tenant/<ns>/agent/<slug>).
func (p *K8sProvisioner) ensureDestinationRule(ctx context.Context, at AgentType, namespace string, owner metav1.OwnerReference) error {
	spiffeID := p.agentSpiffeID(at, namespace)
	host := fmt.Sprintf("%s.%s.svc.cluster.local", at.Slug, namespace)
	metadata := map[string]any{
		"name":      "agent-" + at.Slug,
		"namespace": p.cfg.SystemNamespace,
	}
	setUnstructuredOwnerRef(metadata, owner)
	dr := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "networking.istio.io/v1beta1",
			"kind":       "DestinationRule",
			"metadata":   metadata,
			"spec": map[string]any{
				"host": host,
				"trafficPolicy": map[string]any{
					"portLevelSettings": []any{
						map[string]any{
							"port": map[string]any{
								"number": int64(sessionManagerPort),
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
	if err == nil || !apierrors.IsAlreadyExists(err) {
		return err
	}
	if owner.Kind == "" {
		return nil
	}
	existing, err := p.dynamicClient.Resource(destinationRuleGVR).
		Namespace(p.cfg.SystemNamespace).
		Get(ctx, "agent-"+at.Slug, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get existing destinationrule: %w", err)
	}
	existing.SetOwnerReferences(mergeOwnerRefs(existing.GetOwnerReferences(), owner))
	_, err = p.dynamicClient.Resource(destinationRuleGVR).
		Namespace(p.cfg.SystemNamespace).
		Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

// agentAuthorizationPolicyName is the AuthorizationPolicy created beside each
// agent StatefulSet.
func agentAuthorizationPolicyName(slug string) string { return "agent-" + slug + "-callers" }

// ensureAuthorizationPolicy restricts who may reach an agent pod's
// session-manager through the mesh.
//
// The session-manager API can start, message, interrupt, and kill any run on
// the pod, and its only legitimate caller is orbit-agents
// (internal/platform/clients/sessionmanager). The application now authenticates
// that caller, but the mesh is the cheaper place to say it: the check runs in
// the sidecar, before the request is ever seen by the application, and it holds
// even if the application's own check regresses.
//
// It lives in the TENANT namespace, not orbit-system, because that is where the
// agent pods run and an AuthorizationPolicy selects workloads in its own
// namespace. The DestinationRule above is the mirror image; it configures the
// CLIENT side (orbit-agents' outbound mTLS expectations) so it belongs in
// orbit-system. Neither can be templated in a chart: the StatefulSet these
// govern is created imperatively, per agent type, at provision time, so its
// policy is created the same way and torn down with it.
//
// ALLOW, not DENY. A DENY listing the callers to exclude is a list that has to
// be kept complete forever; an ALLOW listing the callers that exist fails
// closed when something new appears, which is the direction this should fail in.
//
// The second rule keeps GET /health reachable from any source. The kubelet's
// readiness probe carries no SPIFFE identity, and while Istio's probe rewrite
// normally routes it via pilot-agent on 15020 (outside Envoy's RBAC listener),
// making readiness depend on that rewrite staying enabled would trade a
// security fix for an outage. /health returns liveness and a session count.
//
// NOTE: Istio prepends "spiffe://" when translating an AuthorizationPolicy to
// Envoy RBAC. Principals are written WITHOUT the prefix; writing it produces
// "spiffe://spiffe://..." in the generated rule and never matches.
func (p *K8sProvisioner) ensureAuthorizationPolicy(ctx context.Context, at AgentType, namespace string, owner metav1.OwnerReference) error {
	if !p.cfg.AuthorizationPolicyEnabled {
		return nil
	}
	metadata := map[string]any{
		"name":      agentAuthorizationPolicyName(at.Slug),
		"namespace": namespace,
	}
	setUnstructuredOwnerRef(metadata, owner)
	ap := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "security.istio.io/v1beta1",
			"kind":       "AuthorizationPolicy",
			"metadata":   metadata,
			"spec": map[string]any{
				"selector": map[string]any{
					"matchLabels": map[string]any{"app": at.Slug},
				},
				"action": "ALLOW",
				"rules": []any{
					map[string]any{
						"from": []any{
							map[string]any{
								"source": map[string]any{
									"principals": []any{
										fmt.Sprintf("%s/system/orbit-agents", p.cfg.TrustDomain),
									},
								},
							},
						},
						"to": []any{
							map[string]any{
								"operation": map[string]any{
									"ports": []any{strconv.Itoa(sessionManagerPort)},
								},
							},
						},
					},
					map[string]any{
						"to": []any{
							map[string]any{
								"operation": map[string]any{
									"methods": []any{"GET"},
									"paths":   []any{"/health"},
								},
							},
						},
					},
				},
			},
		},
	}
	_, err := p.dynamicClient.Resource(authorizationPolicyGVR).
		Namespace(namespace).
		Create(ctx, ap, metav1.CreateOptions{})
	if err == nil || !apierrors.IsAlreadyExists(err) {
		return err
	}
	if owner.Kind == "" {
		return nil
	}
	existing, err := p.dynamicClient.Resource(authorizationPolicyGVR).
		Namespace(namespace).
		Get(ctx, agentAuthorizationPolicyName(at.Slug), metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get existing authorizationpolicy: %w", err)
	}
	existing.SetOwnerReferences(mergeOwnerRefs(existing.GetOwnerReferences(), owner))
	_, err = p.dynamicClient.Resource(authorizationPolicyGVR).
		Namespace(namespace).
		Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

func (p *K8sProvisioner) deleteAuthorizationPolicy(ctx context.Context, slug, namespace string) error {
	err := p.dynamicClient.Resource(authorizationPolicyGVR).
		Namespace(namespace).
		Delete(ctx, agentAuthorizationPolicyName(slug), metav1.DeleteOptions{})
	return ignoreNotFound(err)
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
