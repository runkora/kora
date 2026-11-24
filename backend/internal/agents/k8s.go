package agents

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/runkora/kora/backend/internal/agents/manifestpatch"
	"github.com/runkora/kora/backend/internal/agents/sandbox/execauth"
	sandboxfolder "github.com/runkora/kora/backend/internal/agents/sandbox/folder"
	"github.com/runkora/kora/backend/internal/agents/sandbox/kubeconfig"
	"github.com/runkora/kora/backend/internal/agents/sandbox/landlockmode"
	"github.com/runkora/kora/backend/internal/agents/sandbox/runfs"
	"github.com/runkora/kora/backend/internal/platform/brand"
	platformconns "github.com/runkora/kora/backend/internal/platform/connections"

	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"

	policyclient "github.com/runkora/kora/backend/internal/platform/clients/policy"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/observability"
	"github.com/runkora/kora/backend/internal/platform/ports"
	"github.com/runkora/kora/backend/internal/platform/probes"
	si "github.com/runkora/kora/backend/internal/platform/sandboximages/v1alpha1"
)

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
	// ClientQPS and ClientBurst rate-limit the Kubernetes client. Zero keeps the client-go defaults.
	ClientQPS           float32
	ClientBurst         int
	SessionManagerImage string
	// SandboxImage is the base image reference that rendered previews use. A
	// live sandbox runs the digest the operator pinned on its SandboxImage.
	SandboxImage string
	// DefaultEnvironmentSize sizes rendered previews.
	DefaultEnvironmentSize EnvironmentSize
	// SandboxReadyWait bounds the wait for a sandbox pod to become ready. Zero uses defaultSandboxReadyWait.
	SandboxReadyWait time.Duration
	SandboxGRPCPort  int32
	// SandboxStartupSeconds is how long sandbox-exec may take to start before its
	// startup probe restarts it. Zero uses probes.DefaultStartupSeconds.
	SandboxStartupSeconds int
	// GitAuthorName/GitAuthorEmail set the git identity for commits the agent
	// makes in the sandbox (GIT_AUTHOR_*/GIT_COMMITTER_*). Empty = leave unset.
	GitAuthorName  string
	GitAuthorEmail string
	// OTLPEndpoint is passed to the session-manager so a provisioned agent
	// exports to the same collector as the platform. Empty disables its export.
	OTLPEndpoint string
	// ProductName is passed to the session-manager as KORA_PRODUCT_NAME.
	ProductName string
	// SamplingRate is passed alongside it so a provisioned agent samples at the
	// same rate as the platform. Empty keeps the image's own default.
	SamplingRate string
	// ImagePullSecrets names the Secrets, present in every tenant namespace,
	// that runtime-created pods pull the session-manager and sandbox images with.
	ImagePullSecrets []string
	// TrustDomain is the SPIFFE trust domain (e.g. "runkora.test").
	// Used to build the agent's SPIFFE principal ID for grant creation.
	TrustDomain string
	// SystemNamespace is the namespace where kora-agents runs.
	// DestinationRules are created here so Istio applies them to outbound
	// traffic from kora-agents pods targeting agent session-managers.
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
	// AuthorizationPolicyEnabled creates an Istio AuthorizationPolicy for
	// each agent's pods and the pool's pods restricting the session-manager port to the
	// kora-agents principal. Requires SPIFFE/mTLS: without attested identities
	// no source matches the rule and every call is denied, so the chart gates
	// this on platform.spiffe.enabled (charts/kora/templates/agents/deployment.yaml).
	AuthorizationPolicyEnabled bool
	// Landlock is the session-manager's KORA_LANDLOCK: enforce or bestEffort.
	Landlock string
	// PoolSize is the unbound session-manager pods each tenant keeps ready to bind
	// (docs/adr/2026-11-22-agent-cold-tier.md).
	PoolSize int32
	// MinReplicas is the scaler's floor on each agent's pods.
	MinReplicas int32
	// MaxReplicas is the scaler's ceiling on each agent's pods.
	MaxReplicas int32
	// DrainGracePeriodSecs is how long a terminating session-manager pod may wait for its runs to end.
	DrainGracePeriodSecs int64
	// PressureLimit is the session-manager's KORA_PRESSURE_LIMIT: the fraction of memory.max at which it kills its newest run.
	PressureLimit float64
	// SandboxMount gives each sandbox a working folder on its local disk, which the
	// sandbox-mount node agent mounts into the session-manager pod at the same path
	// (docs/adr/2026-10-08-sandbox-workspace-mount.md).
	SandboxMount bool
}

// K8sProvisioner manages Kubernetes resources on behalf of kora-agents.
// It is the only component with RBAC to bind and delete agent pods and to
// create the pool Deployments, Services, sandbox Pods, and Istio
// DestinationRules in tenant namespaces.
type K8sProvisioner struct {
	client        kubernetes.Interface
	dynamicClient dynamic.Interface
	logger        logging.Logger
	cfg           K8sProvisionerConfig
	policyClient  *policyclient.Client
	scopeResolver *ScopeResolver
	// workloads caches the bound agent pods, and pool the pool Deployments and pool pods. SyncWorkloadCache starts both.
	workloads, pool informers.SharedInformerFactory
	// growTimeout bounds a warm pod's grow before dispatch. Zero means defaultGrowTimeout.
	growTimeout time.Duration
	// poolsApplied maps a tenant namespace to the hash of the pool Deployment last
	// reconciled there. Every agent apply reconciles the pool, and an unchanged
	// render makes no Deployment or ServiceAccount call. A pool Deployment deleted
	// by hand comes back when the render changes or kora-agents restarts.
	poolsApplied sync.Map
	// retiredAccounts holds the namespace/slug of each agent whose StatefulSet ServiceAccount is deleted.
	retiredAccounts sync.Map
	// metrics counts the binds an admission policy refused. Nil records nothing.
	metrics *Metrics
}

// SetMetrics installs the instrument set that counts refused binds.
func (p *K8sProvisioner) SetMetrics(m *Metrics) { p.metrics = m }

// SetScopeResolver installs the resolver that adds the attached environments' egress and connectors to an agent's grant.
// Without it, grant reconciliation uses the agent's own scopes.
func (p *K8sProvisioner) SetScopeResolver(r *ScopeResolver) { p.scopeResolver = r }

// ErrProvisionerProductNameEmpty is returned when the provisioner is built with no product name.
var ErrProvisionerProductNameEmpty = errors.New("k8s provisioner: product name is empty")

// NewK8sProvisioner builds a provisioner using in-cluster config when inCluster
// is true, otherwise falling back to KUBECONFIG (local development).
// policyClient may be nil; grant creation is skipped when unset (local dev).
func NewK8sProvisioner(inCluster bool, logger logging.Logger, cfg K8sProvisionerConfig, policyClient *policyclient.Client) (*K8sProvisioner, error) {
	// An empty name would silently fall back to the image default in the pod.
	if strings.TrimSpace(cfg.ProductName) == "" {
		return nil, ErrProvisionerProductNameEmpty
	}
	restCfg, err := buildRestConfig(inCluster)
	if err != nil {
		return nil, fmt.Errorf("build k8s rest config: %w", err)
	}
	restCfg.QPS, restCfg.Burst = cfg.ClientQPS, cfg.ClientBurst

	cs, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("create k8s clientset: %w", err)
	}

	dc, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("create dynamic k8s client: %w", err)
	}

	return &K8sProvisioner{
		client: cs, dynamicClient: dc, logger: logger, cfg: cfg, policyClient: policyClient,
		workloads: newWorkloadCache(cs, agentTypeLabel), pool: newWorkloadCache(cs, poolLabel),
	}, nil
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

// ProvisionAgentType creates the k8s resources of a managed agent type in the
// given namespace and updates existing ones to the rendered spec. Updating the
// tenant pool's pod template rolls the pool, and the scaler then rolls the agent's pods.
func (p *K8sProvisioner) ProvisionAgentType(ctx context.Context, at AgentType, namespace string) error {
	return p.provision(ctx, at, namespace, true)
}

// ProvisionMissing creates the agent type's missing k8s resources and leaves
// existing ones unchanged. It still reconciles the agent's grant. Run start uses
// it so that starting a run never rolls pods. The scaler binds the agent's pods.
func (p *K8sProvisioner) ProvisionMissing(ctx context.Context, at AgentType, namespace string) error {
	return p.provision(ctx, at, namespace, false)
}

func (p *K8sProvisioner) provision(ctx context.Context, at AgentType, namespace string, update bool) error {
	spanName := "agents.K8sProvisioner.ProvisionMissing"
	if update {
		spanName = "agents.K8sProvisioner.ProvisionAgentType"
	}
	ctx, span := agentsTracer.Start(ctx, spanName,
		trace.WithAttributes(
			observability.AgentSlugKey.String(at.Slug),
			semconv.K8SNamespaceNameKey.String(namespace),
			observability.AgentUpdateKey.Bool(update),
		),
	)
	defer span.End()

	slug := at.Slug
	labels := agentTypeLabels(at)
	// Owner reference for GitOps observability + k8s-native cascade delete
	// (docs/adr/2026-09-06-owner-references-for-agents-and-sandboxes.md).
	// Zero value when the KoraTenant CR is unreachable; ensureX no-ops the
	// merge and provisioning proceeds unowned so local-dev without the
	// operator still works.
	tenantRef := p.tenantOwnerRef(ctx, at.TenantID)

	// Patches selected by this agent type's own name or labels match nothing
	// in the startup validation, which renders a stand-in, so a sandbox pinned
	// away from its session-manager over a ReadWriteOnce workspace is caught
	// here instead of on the agent's first run.
	if err := p.checkSandboxPlacement(at, namespace); err != nil {
		observability.RecordFailure(span, err, errSpanCheckSandboxPlacement)
		return err
	}

	// The workload an earlier release ran for the agent goes on an apply (docs/adr/2026-11-22-agent-cold-tier.md).
	if update {
		if err := p.retireStatefulSet(ctx, at, namespace); err != nil {
			observability.RecordFailure(span, err, errSpanRetireStatefulSet)
			return err
		}
	}
	if err := p.ensureHeadlessService(ctx, slug, namespace, labels, tenantRef, update); err != nil {
		observability.RecordFailure(span, err, errSpanEnsureHeadlessService)
		return err
	}
	if err := p.ensureAgentGrant(ctx, at, namespace); err != nil {
		observability.RecordFailure(span, err, errSpanEnsureAgentGrant)
		return err
	}
	if err := p.ensureDestinationRule(ctx, at, namespace, tenantRef, update); err != nil {
		observability.RecordFailure(span, err, errSpanEnsureDestinationRule)
		return err
	}
	if err := p.ensureAuthorizationPolicy(ctx, at, namespace, tenantRef, update); err != nil {
		observability.RecordFailure(span, err, errSpanEnsureAuthorizationPolicy)
		return err
	}
	if err := p.ensureTenantPool(ctx, at.TenantID, namespace, tenantRef, update); err != nil {
		observability.RecordFailure(span, err, errSpanEnsureTenantPool)
		return err
	}
	return nil
}

// DeprovisionAgentType deletes all k8s resources created by ProvisionAgentType.
// Missing resources are silently ignored.
func (p *K8sProvisioner) DeprovisionAgentType(ctx context.Context, at AgentType, namespace string) error {
	ctx, span := agentsTracer.Start(ctx, "agents.K8sProvisioner.DeprovisionAgentType",
		trace.WithAttributes(
			observability.AgentSlugKey.String(at.Slug),
			semconv.K8SNamespaceNameKey.String(namespace),
		),
	)
	defer span.End()

	slug := at.Slug
	del := metav1.DeletePropagationForeground
	opts := metav1.DeleteOptions{PropagationPolicy: &del}

	resources := []func() error{
		func() error {
			return p.retireStatefulSet(ctx, at, namespace)
		},
		func() error {
			// The agent's pods go with it, so its ServiceAccount does not wait for them.
			p.retiredAccounts.Delete(namespace + "/" + slug)
			return p.deleteAgentServiceAccount(ctx, at, namespace)
		},
		func() error {
			// The agent is being deleted and its runs end with it. Its pods skip the drain grace period.
			return p.deleteAgentPods(ctx, at, namespace)
		},
		func() error {
			return ignoreNotFound(p.client.CoreV1().Services(namespace).Delete(ctx, slug, opts))
		},
		func() error {
			return p.deleteAgentGrant(ctx, at, namespace)
		},
		func() error {
			return p.deleteDestinationRule(ctx, at.Slug, namespace)
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
		observability.RecordFailure(span, firstErr, errSpanDeprovisionAgentType)
	}
	return firstErr
}

// stuckWaitingReasons mark a container that keeps failing to start, rather than one still starting.
var stuckWaitingReasons = []string{"CrashLoopBackOff", "ImagePullBackOff", "ErrImagePull", "CreateContainerConfigError", "InvalidImageName"}

// podStuck reports whether any init or regular container of the pod is waiting for one of stuckWaitingReasons.
// A Running pod that is not Ready is not stuck; notReadyTooLong limits it by time.
func podStuck(pod *corev1.Pod) bool {
	for _, statuses := range [][]corev1.ContainerStatus{pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses} {
		for _, cs := range statuses {
			if cs.State.Waiting != nil && slices.Contains(stuckWaitingReasons, cs.State.Waiting.Reason) {
				return true
			}
		}
	}
	return false
}

func podReady(pod *corev1.Pod) bool {
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
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
	// Created reports that this call started the pod. It is false for a reused live pod.
	Created bool `json:"-"`
	// Folder is the sandbox pod's UID when the node agent mounts its working folder. The
	// session-manager waits for a mount from that pod. Empty when SandboxMount is off.
	Folder string `json:"folder,omitempty"`
}

// SandboxSpec is what the run's environment decides about its sandbox pod.
type SandboxSpec struct {
	// EnvironmentID is empty for the built-in default environment.
	EnvironmentID string
	Environment   string
	// Image is pinned by digest. A node's cached copy always matches it.
	Image        string
	PullSecret   string
	Size         EnvironmentSize
	KubeClusters []kubeconfig.Cluster
	// PlaceholderEnvs names env vars set to kubeconfig.PlaceholderToken, one per
	// granted apikey connection with config.placeholderEnv.
	PlaceholderEnvs []string
}

// Sandbox pod annotations naming its environment.
const (
	annotationEnvironment   = "runkora.io/environment"
	annotationEnvironmentID = "runkora.io/environment-id"
)

// ProvisionSandbox creates a sandbox pod for the given run and waits for it to
// be Ready. Returns where to reach its exec server ("podIP:port") and the
// per-run token that server requires on every call; a caller holding only the
// endpoint cannot execute anything. The caller (session-manager) writes both
// into the run's workspace (docs/adr/2026-09-18-sandbox-exec-trust-boundary.md).
//
// The sandbox pod mounts the workspace claim of the run's session-manager pod
// (named "{pod}-workspaces") so files written by the agent container
// (Edit, Write tools) are immediately visible in the sandbox without copying.
// The claim is shared by every run placed on that pod, so the
// mount is scoped with SubPath: run.ID (see sandboxContainer); this run's
// sandbox pod only ever has its own {runID} directory mounted, never a
// sibling run's. See https://github.com/runkora/kora/issues/134.
func (p *K8sProvisioner) ProvisionSandbox(ctx context.Context, run Run, agentTypeSlug, namespace string, spec SandboxSpec) (Sandbox, error) {
	ctx, span := agentsTracer.Start(ctx, "agents.K8sProvisioner.ProvisionSandbox",
		trace.WithAttributes(
			observability.AgentSlugKey.String(agentTypeSlug),
			semconv.K8SNamespaceNameKey.String(namespace),
		),
	)
	defer span.End()

	token, err := newSandboxExecToken()
	if err != nil {
		observability.RecordFailure(span, err, errSandboxToken)
		return Sandbox{}, fmt.Errorf("mint sandbox exec token: %w", err)
	}

	pod, err := p.buildSandboxPod(run, agentTypeSlug, namespace, spec, token)
	if err != nil {
		observability.RecordFailure(span, err, errSandboxSize)
		return Sandbox{}, err
	}
	podName := pod.Name
	sessionManager, err := p.client.CoreV1().Pods(namespace).Get(ctx, run.PodName, metav1.GetOptions{})
	if err != nil {
		observability.RecordFailure(span, err, errSpanGetSessionManagerPod)
		return Sandbox{}, fmt.Errorf("get session-manager pod %s: %w", run.PodName, err)
	}
	// Owner reference on the session-manager pod: the sandbox mounts its
	// workspace claim, and deleting the pod cascades to its sandboxes
	// (docs/adr/2026-09-06-owner-references-for-agents-and-sandboxes.md).
	applyOwnerRef(&pod.ObjectMeta, podOwnerRef(sessionManager))
	// Applied after the configured placement, so it cannot drop it: a
	// ReadWriteOnce workspace attaches to one node, the session-manager pod's.
	if !p.cfg.Workspace.crossesNodes() {
		requireCoLocation(&pod.Spec, sessionManager.Spec.NodeName)
	}

	podVal, err := manifestpatch.Apply(*pod, "Pod", p.cfg.Patches)
	if err != nil {
		observability.RecordFailure(span, err, errSpanApplyPodPatches)
		return Sandbox{}, fmt.Errorf("apply Pod patches: %w", err)
	}

	_, err = p.client.CoreV1().Pods(namespace).Create(ctx, &podVal, metav1.CreateOptions{})
	created := err == nil
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
			observability.RecordFailure(span, repErr, errSpanReplaceDeadSandboxPod)
			return Sandbox{}, repErr
		}
		created = replaced
		p.logger.Info(ctx, "Sandbox pod already exists",
			logging.Field{Key: "run_id", Value: run.ID},
			logging.Field{Key: "pod", Value: podName},
			logging.Field{Key: "replaced_dead_pod", Value: replaced},
		)
	default:
		observability.RecordFailure(span, err, errSpanCreateSandboxPod)
		return Sandbox{}, fmt.Errorf("create sandbox pod: %w", err)
	}

	ready, err := p.waitForPodReady(ctx, podName, namespace)
	if err != nil {
		observability.RecordFailure(span, err, errSpanWaitForSandboxPodReady)
		return Sandbox{}, fmt.Errorf("wait for sandbox pod ready (run %s): %w", run.ID, err)
	}

	// Read the token off the ready pod, not the freshly minted one: a reused pod
	// still holds the token it was created with.
	liveToken := sandboxExecTokenOf(ready)
	if liveToken == "" {
		observability.RecordFailure(span, errSandboxToken, errSandboxToken)
		return Sandbox{}, fmt.Errorf("%w: pod %s carries no %s", errSandboxToken, podName, sandboxExecTokenEnv)
	}

	endpoint := fmt.Sprintf("%s:%d", ready.Status.PodIP, p.cfg.SandboxGRPCPort)
	span.SetAttributes(observability.SandboxEndpointKey.String(endpoint))
	p.logger.Info(ctx, "Sandbox pod ready",
		logging.Field{Key: "run_id", Value: run.ID},
		logging.Field{Key: "pod", Value: podName},
		logging.Field{Key: "endpoint", Value: endpoint},
	)
	if ready.Annotations[annotationEnvironmentID] != spec.EnvironmentID {
		observability.RecordFailure(span, ErrEnvironmentSwitch, ErrEnvironmentSwitch)
		return Sandbox{}, fmt.Errorf("%w; call replace_environment to switch to %q", ErrEnvironmentSwitch, spec.Environment)
	}
	sandbox := Sandbox{Endpoint: endpoint, ExecToken: liveToken, Created: created}
	// A pod created before the feature was on has no folder, and nothing will mount it.
	if p.cfg.SandboxMount && hasFolderMount(ready) {
		sandbox.Folder = string(ready.UID)
	}
	return sandbox, nil
}

// errSandboxUnusable reports a sandbox pod that exists but cannot serve the
// run. Distinct from no pod at all, which tells the agent to provision one.
var errSandboxUnusable = errors.New("sandbox pod unusable")

// LookupSandbox returns the run's sandbox as its live pod describes it, and
// whether one exists. It creates nothing.
//
// The pod is the durable record: it carries the exec token in its spec and the
// endpoint in its status, and it outlives both the session-manager's memory and
// anything the agent can delete from its workspace. A pod that is not ready has
// no usable endpoint yet, so it reports no sandbox rather than a half one.
func (p *K8sProvisioner) LookupSandbox(ctx context.Context, runID, namespace string) (Sandbox, bool, error) {
	ctx, span := agentsTracer.Start(ctx, "agents.K8sProvisioner.LookupSandbox",
		trace.WithAttributes(semconv.K8SNamespaceNameKey.String(namespace)),
	)
	defer span.End()

	pod, err := p.sandboxPod(ctx, runID, namespace)
	if err != nil {
		observability.RecordFailure(span, err, errSpanGetSandboxPod)
		return Sandbox{}, false, err
	}
	if pod == nil {
		return Sandbox{}, false, nil
	}
	// A pod that exists but cannot be used is not the same answer as no pod.
	// found=false reaches kora-shell as "this run never had a sandbox", so only
	// a genuinely absent pod earns it. Everything else is an error, and the
	// caller stops.
	switch {
	case !podLive(pod):
		return Sandbox{}, false, fmt.Errorf("%w: sandbox pod %s is terminating, finished or its sandbox-exec exited", errSandboxUnusable, pod.Name)
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
func (p *K8sProvisioner) buildSandboxPod(run Run, agentTypeSlug, namespace string, spec SandboxSpec, execToken string) (*corev1.Pod, error) {
	// The container is built first to reject a size that is not a quantity.
	container, err := sandboxContainer(p.cfg, spec, run.ID, execToken)
	if err != nil {
		return nil, err
	}
	podName := sandboxfolder.SandboxPodName(run.ID)
	// The sandbox mounts the workspace claim of the run's session-manager pod.
	workspacesPVC := sandboxfolder.WorkspaceClaim(run.PodName)

	// Labels carry the pod's identity for the operator pod webhook and SPIRE.
	//   - runkora.io/role=sandbox: namespace-policy targeting and the future
	//     tenant NetworkPolicy locking egress to the gateway.
	//   - runkora.io/run-id, runkora.io/agent-slug: matched by the
	//     kora-tenant-sandbox ClusterSPIFFEID template (Phase 4b) so the
	//     pod is attested as
	//     spiffe://<td>/tenant/<ns>/agent/<slug>/sandbox/<run-id>.
	//     The principal extractor (internal/identity/principal_extractor.go)
	//     normalizes this back to the agent principal for authorization
	//     while lifting run_id into claims for attribution.
	//
	// We deliberately NO LONGER set runkora.io/skip-injection: per
	// docs/adr/2026-06-06-jit-credential-brokering-egress-gateway.md the
	// sandbox rejoins the mesh; the operator pod webhook adds the
	// identity-proxy + opa-policy + spire injection templates, giving the
	// sandbox the same outbound enforcement chain as the session-manager
	// pod. Without injection the credential broker would be unreachable.
	labels := map[string]string{
		sandboxfolder.RoleLabel:  sandboxfolder.RoleSandbox,
		sandboxfolder.RunIDLabel: run.ID,
		"runkora.io/agent-slug":  agentTypeSlug,
	}

	// Sandbox-exec is a gRPC server on port 50051 with no other inbound or
	// outbound traffic on its own (kora-shell connects directly via plain
	// TCP); however the sandbox pod ALSO hosts the agent's tooling
	// (git/curl/SDKs) which need outbound HTTPS through the broker. Istio
	// injection is therefore required; see comment on the labels block.
	annotations := map[string]string{
		// Exclude the gRPC port from the inbound mesh so kora-shell still
		// connects over plain TCP, matching the existing pre-injection
		// behavior. Outbound to the egress-gateway intentionally STAYS in
		// the mesh; the sandbox's identity-proxy ext_proc filter must see
		// the CONNECT and inject the platform Bearer token, which is the
		// gateway's only signal of caller identity. The CONNECT method is
		// passed through HCM via the egress-gateway EnvoyFilter
		// (charts/kora/templates/egress-gateway/envoyfilter.yaml).
		"traffic.sidecar.istio.io/excludeInboundPorts": fmt.Sprintf("%d", p.cfg.SandboxGRPCPort),
		// Holds the other containers until istio-proxy is ready on clusters
		// where Istio does not inject it as a native sidecar. Same wiring the
		// session-manager pod uses.
		"proxy.istio.io/config": sidecarProxyConfig,
		annotationEnvironment:   spec.Environment,
		annotationEnvironmentID: spec.EnvironmentID,
	}

	pullSecrets := slices.Clone(p.cfg.ImagePullSecrets)
	if spec.PullSecret != "" && !slices.Contains(pullSecrets, spec.PullSecret) {
		pullSecrets = append(pullSecrets, spec.PullSecret)
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
			ImagePullSecrets:   imagePullSecretRefs(pullSecrets),
			SecurityContext:    restrictedPodSecurityContext(),
			Volumes:            sandboxVolumes(workspacesPVC, p.cfg, spec.Size),
			Containers:         []corev1.Container{container},
		},
	}

	sandbox := p.cfg.Workloads.ForSandbox()
	sandbox.applyToPodMeta(&pod.ObjectMeta)
	sandbox.applyToPodSpec(&pod.Spec)

	return pod, nil
}

// hasFolderMount reports a sandbox pod whose container mounts its working folder.
func hasFolderMount(pod *corev1.Pod) bool {
	for _, c := range pod.Spec.Containers {
		if c.Name != sandboxContainerName {
			continue
		}
		return slices.ContainsFunc(c.VolumeMounts, func(m corev1.VolumeMount) bool {
			return sandboxfolder.IsFolderMount(m.Name, m.SubPath)
		})
	}
	return false
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
// if it is not live (see podLive), deletes it, waits for it to disappear, and
// creates a fresh one. It returns true when it replaced the pod. A live pod is
// left untouched so warm-start and idempotent re-provision calls keep reusing it.
//
// A pod whose sandbox-exec ran out of memory is replaced, and the replacement is
// reported as ErrSandboxOutOfMemory unless it has more memory.
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

	if podLive(existing) {
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
	// The same size would run out again, so the agent is told to pick a larger one.
	limit, next := sandboxMemoryLimit(existing), sandboxMemoryLimit(pod)
	if t := sandboxExecTerminated(existing); t != nil && t.Reason == "OOMKilled" && next.Cmp(limit) <= 0 {
		return true, fmt.Errorf("%w at its %s limit and was restarted. %s "+
			"Call replace_environment with a larger environment, or use less memory", ErrSandboxOutOfMemory, limit.String(), sandboxLossNote(existing))
	}
	return true, nil
}

// sandboxLossNote says which files a replaced sandbox took with it.
// The sandbox folder is on the sandbox's own disk, so it is lost with the pod.
func sandboxLossNote(pod *corev1.Pod) string {
	workspace := runWorkspaceDir(pod.Labels[sandboxfolder.RunIDLabel])
	if hasFolderMount(pod) {
		return fmt.Sprintf("The sandbox folder %s was lost with it. Other files under %s are intact.", filepath.Join(workspace, sandboxfolder.Dir), workspace)
	}
	return fmt.Sprintf("Files outside %s are lost.", workspace)
}

// ErrSandboxOutOfMemory reports a sandbox replaced because sandbox-exec ran out of memory.
var ErrSandboxOutOfMemory = errors.New("the sandbox ran out of memory")

// sandboxExecTerminated returns the sandbox-exec container's terminated state, or nil while it runs.
// Sandbox pods never restart containers, so a terminated sandbox-exec stays dead.
func sandboxExecTerminated(pod *corev1.Pod) *corev1.ContainerStateTerminated {
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name == sandboxContainerName {
			return cs.State.Terminated
		}
	}
	return nil
}

// sandboxMemoryLimit returns the sandbox-exec container's memory limit.
func sandboxMemoryLimit(pod *corev1.Pod) resource.Quantity {
	for _, c := range pod.Spec.Containers {
		if c.Name == sandboxContainerName {
			return c.Resources.Limits.Memory().DeepCopy()
		}
	}
	return resource.Quantity{}
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
func (p *K8sProvisioner) DeleteSandbox(ctx context.Context, runID, namespace string) (bool, error) {
	ctx, span := agentsTracer.Start(observability.WithRun(ctx, runID), "agents.K8sProvisioner.DeleteSandbox",
		trace.WithAttributes(semconv.K8SNamespaceNameKey.String(namespace)),
	)
	defer span.End()

	podName := sandboxfolder.SandboxPodName(runID)
	p.logger.Info(ctx, "Deleting sandbox pod",
		logging.Field{Key: "run_id", Value: runID},
		logging.Field{Key: "pod", Value: podName},
		logging.Field{Key: "namespace", Value: namespace},
	)
	del := metav1.DeletePropagationForeground
	err := p.client.CoreV1().Pods(namespace).Delete(ctx, podName, metav1.DeleteOptions{PropagationPolicy: &del})
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		observability.RecordFailure(span, err, errSpanDeleteSandboxPod)
		return false, err
	}
	return true, nil
}

// defaultSandboxReadyWait bounds the pod wait when the install sets no provision guardrails.
const defaultSandboxReadyWait = 240 * time.Second

// waitForPodReady polls until the pod is Ready, returning the ready pod.
// The wait ends inside the session-manager's attempt, so a pod that never schedules reports why.
func (p *K8sProvisioner) waitForPodReady(ctx context.Context, name, namespace string) (*corev1.Pod, error) {
	timeout := p.cfg.SandboxReadyWait
	if timeout <= 0 {
		timeout = defaultSandboxReadyWait
	}
	var ready *corev1.Pod
	err := wait.PollUntilContextTimeout(ctx, 3*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		pod, err := p.client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if pod.Status.Phase == corev1.PodFailed {
			return false, fmt.Errorf("sandbox pod failed: %s", podFailureReason(pod))
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
		if reason := p.unschedulableReason(ctx, name, namespace); reason != "" {
			return nil, &unschedulableError{reason: reason, err: err}
		}
		return nil, err
	}
	if ready.Status.PodIP == "" {
		return nil, fmt.Errorf("pod ready but no IP assigned")
	}
	return ready, nil
}

// podFailureReason summarizes why a pod failed: its status reason and message, the scheduling
// condition when it was not placed, and each container's waiting or terminated reason.
func podFailureReason(pod *corev1.Pod) string {
	var parts []string
	if pod.Status.Reason != "" || pod.Status.Message != "" {
		parts = append(parts, strings.TrimSpace(pod.Status.Reason+" "+pod.Status.Message))
	}
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionFalse {
			parts = append(parts, strings.TrimSpace(fmt.Sprintf("scheduling %s %s", cond.Reason, cond.Message)))
		}
	}
	statuses := append(append([]corev1.ContainerStatus{}, pod.Status.InitContainerStatuses...), pod.Status.ContainerStatuses...)
	for _, cs := range statuses {
		switch {
		case cs.State.Waiting != nil && cs.State.Waiting.Reason != "":
			parts = append(parts, fmt.Sprintf("container %s waiting: %s %s", cs.Name, cs.State.Waiting.Reason, cs.State.Waiting.Message))
		case cs.State.Terminated != nil && cs.State.Terminated.ExitCode != 0:
			t := cs.State.Terminated
			parts = append(parts, fmt.Sprintf("container %s terminated: %s %s (exit %d)", cs.Name, t.Reason, t.Message, t.ExitCode))
		}
	}
	if len(parts) == 0 {
		return "no reason reported"
	}
	return strings.Join(parts, "; ")
}

// errSandboxUnschedulable reports a sandbox pod the scheduler could not place before the wait ended.
var errSandboxUnschedulable = errors.New("sandbox pod unschedulable")

// unschedulableError carries the scheduler's reason for a sandbox pod it could not place.
type unschedulableError struct {
	reason string
	err    error
}

func (e *unschedulableError) Error() string {
	return fmt.Sprintf("%s: %s: %v", errSandboxUnschedulable, e.reason, e.err)
}

func (e *unschedulableError) Is(target error) bool { return target == errSandboxUnschedulable }

func (e *unschedulableError) Unwrap() error { return e.err }

// unschedulableReason returns the scheduler's message for a pod it has not placed, or "" when it was placed.
// The read survives the caller's cancelled context so a timed-out wait can still report why.
func (p *K8sProvisioner) unschedulableReason(ctx context.Context, name, namespace string) string {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	pod, err := p.client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return ""
	}
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionFalse {
			return cond.Message
		}
	}
	return ""
}

// sandboxContainerEnv builds the env block for the sandbox-exec container.
//
// SANDBOX_EXEC_PORT is consumed by sandbox-exec itself.
//
// Egress is TRANSPARENT; the sandbox has NO proxy env. The sandbox Envoy
// rewrites the agent's raw outbound TLS into a CONNECT to the per-tenant egress
// gateway and runs OPA posture + platform-JWT injection on it; see
// docs/adr/2026-06-06-jit-credential-brokering-egress-gateway.md (Amendment 2)
// and charts/kora/templates/egress-gateway/transparent-egress.yaml. We
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
func sandboxContainerEnv(cfg K8sProvisionerConfig, kubeClusters []kubeconfig.Cluster, placeholderEnvs []string, workspaceDir, execToken string) []corev1.EnvVar {
	env := []corev1.EnvVar{
		{Name: "SANDBOX_EXEC_PORT", Value: fmt.Sprintf("%d", cfg.SandboxGRPCPort)},
		// This run's exec token. The agent can read it, which grants nothing it
		// does not already have; it cannot read another run's.
		{Name: sandboxExecTokenEnv, Value: execToken},
		// WORKSPACE is this run's subPath-scoped workspace. sandbox-exec derives
		// the run's local dir from it for HOME and the caches.
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
		corev1.EnvVar{Name: "GH_TOKEN", Value: kubeconfig.PlaceholderToken},
	)
	// apikey connections with config.placeholderEnv: the tool needs a non-empty
	// token variable to issue the request, and the gateway injects the real one
	// on the wire. A name already set above is not overridden.
	for _, name := range placeholderEnvs {
		if !platformconns.ValidPlaceholderEnvName(name) {
			continue
		}
		if !slices.ContainsFunc(env, func(e corev1.EnvVar) bool { return e.Name == name }) {
			env = append(env, corev1.EnvVar{Name: name, Value: kubeconfig.PlaceholderToken})
		}
	}
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
// matching charts/kora/templates/egress-gateway/service.yaml); there's no
// configurable knob because there's no version of the topology where it
// could be different.
func egressGatewayURL(cfg K8sProvisionerConfig) string {
	if cfg.SystemNamespace == "" {
		return ""
	}
	return fmt.Sprintf("http://kora-egress-gateway.%s.svc:%d", cfg.SystemNamespace, ports.EgressGatewayCONNECT)
}

// sandboxTenantCAPath is where the tenant CA root is mounted inside the
// sandbox container. The runtime env hints in sandboxContainerEnv all point
// here.
const sandboxTenantCAPath = "/var/run/kora/tenant-ca/ca.crt"

// osTrustBundlePath is the OS-wide CA bundle (Debian/Ubuntu base images) that
// tools falling back to the system trust store read. Mounting the tenant CA
// here makes git, wget, the curl CLI, Go binaries (gh/kubectl/helm), ruby and
// apt trust the interception cert with no per-tool env var. The sandbox only
// ever sees tenant-CA-signed certs (all egress is intercepted), so replacing
// the bundle with the tenant CA alone is correct, not a downgrade; the
// sandbox never needs to verify a real upstream certificate.
const osTrustBundlePath = "/etc/ssl/certs/ca-certificates.crt"

// tenantCAVolumeName is the projected-volume name for the tenant CA.
const tenantCAVolumeName = "kora-tenant-ca"

// sandboxVolumes builds the sandbox pod's volume list. The workspaces PVC
// always mounts; the per-tenant CA Secret is added only when an egress
// gateway URL can be derived (otherwise there's nothing to trust).
//
// The CA Secret (kora-tenant-ca) is created by the operator's
// ensureTenantCA step in the same tenant namespace as the sandbox pod, so
// no cross-namespace projection is needed.
func sandboxVolumes(workspacesPVC string, cfg K8sProvisionerConfig, size EnvironmentSize) []corev1.Volume {
	vols := []corev1.Volume{
		{
			Name: sandboxfolder.WorkspaceVolume,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: workspacesPVC,
				},
			},
		},
		scratchVolume(scratchVarTmpVolume, "256Mi"),
		scratchVolume(scratchHomeVolume, "64Mi"),
		scratchVolume(localDiskVolume, size.Disk),
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
				SecretName:  "kora-tenant-ca",
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
	// localDiskVolume holds the sandbox's HOME, TMPDIR, build caches, tool installs and its working folder on the node's disk.
	localDiskVolume = sandboxfolder.Volume
	// localTmpSubPath is the local disk's directory mounted at /tmp.
	localTmpSubPath = ".tmp"
	// localHomeSubPath is the local disk's directory mounted as HOME. It sits beside
	// the working folder, so clearing HOME leaves the agent's work in place.
	localHomeSubPath = "home"
)

// imageHomeDir is the kora user's home in both images, used by tools that read it from passwd.
const imageHomeDir = "/home/kora"

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
// the session-manager pod's shared claim is scoped to its own run directory only:
// sibling runs placed on the same pod share the underlying claim
// but are not mounted into each other's sandbox pods at all. See
// https://github.com/runkora/kora/issues/134.
func sandboxContainer(cfg K8sProvisionerConfig, spec SandboxSpec, runID, execToken string) (corev1.Container, error) {
	resources, err := sandboxResources(spec.Size)
	if err != nil {
		return corev1.Container{}, err
	}
	workspaceDir := runWorkspaceDir(runID)
	tmpSubPath, _ := filepath.Rel(workspacesRoot, runfs.TmpDir(workspaceDir))
	mounts := []corev1.VolumeMount{
		{Name: sandboxfolder.WorkspaceVolume, MountPath: workspaceDir, SubPath: runID},
		// The agent pod's TMPDIR, for files its harness names in commands, such as the working directory file.
		{Name: sandboxfolder.WorkspaceVolume, MountPath: runfs.TmpDir(workspaceDir), SubPath: tmpSubPath},
		// Writable paths outside the workspace under a read-only root, for tools that ignore HOME and TMPDIR.
		{Name: scratchVarTmpVolume, MountPath: "/var/tmp"},
		{Name: scratchHomeVolume, MountPath: imageHomeDir},
		// The sandbox's HOME, caches and installs. It sits at the agent pod's run local dir, so the PATH the agent replays finds the installs.
		{Name: localDiskVolume, MountPath: runfs.LocalDir(workspaceDir), SubPath: localHomeSubPath},
		// The sandbox's TMPDIR, on the local disk with HOME.
		{Name: localDiskVolume, MountPath: "/tmp", SubPath: localTmpSubPath},
	}
	if cfg.SandboxMount {
		// The working folder, which the node agent mounts at the same path in the session-manager pod.
		mounts = append(mounts, corev1.VolumeMount{Name: localDiskVolume, MountPath: filepath.Join(workspaceDir, sandboxfolder.Dir), SubPath: sandboxfolder.Dir})
	}
	if egressGatewayURL(cfg) != "" {
		mounts = append(mounts,
			corev1.VolumeMount{
				Name:      tenantCAVolumeName,
				MountPath: "/var/run/kora/tenant-ca",
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
		Image: spec.Image,
		// A sandbox image built on the base image may change CMD.
		Command: []string{sandboxExecCommand},
		// The image is pinned by digest. A cached copy always matches it.
		ImagePullPolicy: corev1.PullIfNotPresent,
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
		Env:             sandboxContainerEnv(cfg, spec.KubeClusters, spec.PlaceholderEnvs, workspaceDir, execToken),
		VolumeMounts:    mounts,
		WorkingDir:      workspaceDir,
		// The startup probe checks every second so the pod is Ready soon after
		// the port opens. Its budget is the ceiling for a stuck container, not
		// the expected start time. The readiness probe keeps its steady-state period.
		StartupProbe: &corev1.Probe{
			ProbeHandler:     sandboxProbeHandler(cfg.SandboxGRPCPort),
			PeriodSeconds:    probes.StartupPeriodSeconds,
			FailureThreshold: probes.StartupFailureThreshold(cfg.SandboxStartupSeconds),
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler:  sandboxProbeHandler(cfg.SandboxGRPCPort),
			PeriodSeconds: 3,
		},
		Resources: resources,
	}, nil
}

func sandboxProbeHandler(port int32) corev1.ProbeHandler {
	return corev1.ProbeHandler{
		TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(port)},
	}
}

// sandboxResources reserves the environment's size for the sandbox, with requests equal to limits.
// Ephemeral storage adds the scratch allowance to the local disk.
func sandboxResources(size EnvironmentSize) (corev1.ResourceRequirements, error) {
	var cpu, memory, disk resource.Quantity
	for _, d := range []struct {
		dim   string
		value string
		dst   *resource.Quantity
	}{{"cpu", size.CPU, &cpu}, {"memory", size.Memory, &memory}, {"disk", size.Disk, &disk}} {
		q, err := resource.ParseQuantity(d.value)
		if err != nil {
			return corev1.ResourceRequirements{}, fmt.Errorf("%w: size.%s %q: %w", errSandboxSize, d.dim, d.value, err)
		}
		*d.dst = q
	}
	ephemeral := resource.MustParse(appEphemeralLimit)
	ephemeral.Add(disk)
	limits := corev1.ResourceList{
		corev1.ResourceCPU:              cpu,
		corev1.ResourceMemory:           memory,
		corev1.ResourceEphemeralStorage: ephemeral,
	}
	return corev1.ResourceRequirements{Requests: limits.DeepCopy(), Limits: limits}, nil
}

// errSandboxSize reports an environment size that is not a quantity.
var errSandboxSize = errors.New("invalid sandbox size")

// ── Policy grant management ───────────────────────────────────────────────────

// agentSpiffeID returns the SPIFFE principal ID for an agent type's pods.
// The identity proxy attests pods using their SPIFFE cert; the slug makes the
// ID human-readable (e.g. spiffe://runkora.test/tenant/kora-tenant-example/agent/coder).
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

	ctx, span := agentsTracer.Start(observability.WithTenant(ctx, at.TenantID), "agents.K8sProvisioner.ensureAgentGrant",
		trace.WithAttributes(
			observability.PrincipalIDKey.String(principalID),
		),
	)
	defer span.End()

	existing, err := p.policyClient.ListGrants(ctx, grantTenantID, "AGENT", principalID)
	if err != nil {
		observability.RecordFailure(span, err, errSpanListAgentGrants)
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
			observability.RecordFailure(span, rErr, errSpanResolveEffectiveScopes)
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
	// match (budget is reconciled separately); the rest are stale. Remember the
	// kept grant's budget so an unspecified desired budget survives.
	var keepID string
	var keptBudget *policyclient.BudgetAssignment
	var stale []policyclient.Grant
	for _, g := range existing {
		if keepID == "" && grantMatchesDesired(g, effectiveScopes, delegationRequired) {
			keepID = g.ID
			keptBudget = g.Budget
			continue
		}
		stale = append(stale, g)
	}

	// No scope-matching grant: create one before deleting the stale ones, so a
	// refused create leaves the agent its previous authorization. It carries the
	// desired budget or, when unspecified, the budget of a grant it replaces.
	if keepID == "" {
		createBudget := clientBudget(at.Budget)
		if at.Budget == nil {
			createBudget = firstBudget(existing)
		}
		if _, err := p.policyClient.CreateGrant(ctx, grantTenantID, "AGENT", principalID, effectiveScopes, createBudget, delegationRequired); err != nil {
			observability.RecordFailure(span, err, errSpanCreateAgentGrant)
			return fmt.Errorf("create agent grant for %s: %w", principalID, err)
		}
		p.logger.Info(ctx, "agent grant created",
			logging.Field{Key: "principal_id", Value: principalID},
			logging.Field{Key: "tenant_id", Value: grantTenantID},
		)
		p.deleteStaleGrants(ctx, principalID, stale)
		return nil
	}
	p.deleteStaleGrants(ctx, principalID, stale)

	// A scope-matching grant exists: reconcile only the budget, in place. This
	// avoids a delete+create window that would briefly leave the agent with no
	// authorization, and preserves the grant ID (and its attribution counters).
	// at.Budget == nil means "leave budget unchanged".
	if at.Budget != nil && !budgetMatches(at.Budget, keptBudget) {
		if err := p.policyClient.UpdateGrantBudget(ctx, keepID, clientBudget(at.Budget)); err != nil {
			observability.RecordFailure(span, err, errSpanUpdateAgentGrantBudget)
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

// deleteStaleGrants removes grants a reconcile replaced. A failed delete is
// logged and retried by the next reconcile.
func (p *K8sProvisioner) deleteStaleGrants(ctx context.Context, principalID string, stale []policyclient.Grant) {
	for _, g := range stale {
		if delErr := p.policyClient.DeleteGrant(ctx, g.ID); delErr != nil {
			p.logger.Warn(ctx, "delete stale agent grant failed",
				logging.Field{Key: "grant_id", Value: g.ID},
				logging.Field{Key: "error", Value: delErr.Error()},
			)
			continue
		}
		p.logger.Info(ctx, "stale agent grant deleted",
			logging.Field{Key: "grant_id", Value: g.ID},
			logging.Field{Key: "principal_id", Value: principalID},
		)
	}
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

// deleteWorkspacePVCs removes the claims an agent StatefulSet left, by label.
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
		},
	}
}

func (p *K8sProvisioner) ensureServiceAccount(ctx context.Context, name, namespace string, labels map[string]string, owner metav1.OwnerReference, update bool) error {
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
	if !update {
		return nil
	}
	// Sync patched labels/annotations onto an existing ServiceAccount so a
	// patch (e.g. an IRSA role-arn annotation) takes effect on reconcile, not
	// only for freshly-created agent types.
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
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
	})
}

// deletedAgentPodGrace is the grace period of an agent pod deleted with its agent.
const deletedAgentPodGrace = int64(30)

// deleteAgentPods deletes the agent's session-manager pods with a short grace period.
func (p *K8sProvisioner) deleteAgentPods(ctx context.Context, at AgentType, namespace string) error {
	pods, err := p.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: labels.SelectorFromSet(podLabels(at)).String()})
	if err != nil {
		return fmt.Errorf("list agent pods: %w", err)
	}
	grace := deletedAgentPodGrace
	for _, pod := range pods.Items {
		if err := p.client.CoreV1().Pods(namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{GracePeriodSeconds: &grace}); ignoreNotFound(err) != nil {
			return fmt.Errorf("delete agent pod %s: %w", pod.Name, err)
		}
	}
	return nil
}

// drainGracePeriod is the pod's termination grace period. Zero keeps the Kubernetes default.
func drainGracePeriod(secs int64) *int64 {
	if secs <= 0 {
		return nil
	}
	return &secs
}

// sessionManagerContainerName is the session-manager container in every pool and agent pod.
const sessionManagerContainerName = "session-manager"

// sandboxContainerName is the sandbox-exec container in every sandbox pod.
const sandboxContainerName = "sandbox-exec"

// sandboxExecCommand is where the base image installs sandbox-exec.
const sandboxExecCommand = "/usr/local/bin/sandbox-exec"

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
// Pod Security Standard, plus a read-only root for the agent and sandbox containers.
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

// buildSessionManagerStatefulSet renders the session-manager pod template and
// workspace claim as a StatefulSet before manifest patches are applied. Agent
// manifest patches target this shape, and buildTenantPool turns it into the
// tenant pool Deployment.
func (p *K8sProvisioner) buildSessionManagerStatefulSet(at AgentType, namespace string, labels map[string]string) appsv1.StatefulSet {
	sessionManager := p.cfg.Workloads.ForSessionManager()
	podAnnotations := map[string]string{
		// The operator's pod webhook adds the SPIRE and identity-proxy
		// wiring; Istio itself injects only the sidecar.
		"proxy.istio.io/config": sidecarProxyConfig,
		// Only the sandbox exec port skips Envoy; the agent-egress NetworkPolicy
		// limits it to the tenant's sandboxes.
		"traffic.sidecar.istio.io/excludeOutboundPorts": "50051",
	}
	inferenceURL := fmt.Sprintf("http://kora-inference-gateway.%s.svc.cluster.local", p.cfg.SystemNamespace)
	ss := appsv1.StatefulSet{
		TypeMeta:   metav1.TypeMeta{APIVersion: "apps/v1", Kind: "StatefulSet"},
		ObjectMeta: metav1.ObjectMeta{Name: at.Slug, Namespace: namespace, Labels: labels},
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      podLabels(at),
					Annotations: podAnnotations,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: at.Slug,
					// The session-manager drains on SIGTERM and exits when its runs end, up to this cap.
					TerminationGracePeriodSeconds: drainGracePeriod(p.cfg.DrainGracePeriodSecs),
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
							// ConfigMap. It reaches Postgres through kora-agents, so the
							// tenant pod holds no database credential for an agent to read
							// out of its own environment.
							Env: []corev1.EnvVar{
								{Name: "ANTHROPIC_BASE_URL", Value: inferenceURL},
								{Name: "KORA_OTLP_ENDPOINT", Value: p.cfg.OTLPEndpoint},
								{Name: "KORA_SAMPLING_RATE", Value: p.cfg.SamplingRate},
								{Name: brand.EnvProductName, Value: p.cfg.ProductName},
								// kora-agents URL for live event push (docs/adr/2026-05-11-agent-event-streaming-protocol.md).
								{Name: "KORA_AGENTS_URL", Value: fmt.Sprintf("http://kora-agents.%s.svc.cluster.local", p.cfg.SystemNamespace)},
								// Codex, OpenCode and MCP URLs follow the install's system namespace.
								{Name: "KORA_INFERENCE_GATEWAY_URL", Value: inferenceURL},
								{Name: "KORA_TOOL_GATEWAY_URL", Value: fmt.Sprintf("http://kora-tool-gateway.%s.svc.cluster.local", p.cfg.SystemNamespace)},
								// Placeholder key; actual auth is the identity-proxy-issued JWT
								// injected on every outbound request by the Envoy ext_proc filter.
								// Claude Code requires a non-empty value to start in --bare mode.
								{Name: "ANTHROPIC_API_KEY", Value: "kora-managed"},
								{Name: landlockmode.EnvVar, Value: p.cfg.Landlock},
								// The session-manager ends its drain before the kubelet stops the pod.
								{Name: "KORA_TERMINATION_GRACE_PERIOD_SECS", Value: strconv.FormatInt(p.cfg.DrainGracePeriodSecs, 10)},
								{Name: "KORA_PRESSURE_LIMIT", Value: strconv.FormatFloat(p.cfg.PressureLimit, 'f', -1, 64)},
							},
							// The pod turns Ready within a second of the session-manager serving
							// (docs/adr/2026-11-22-agent-cold-tier.md).
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.FromString("http"),
									},
								},
								PeriodSeconds:    1,
								TimeoutSeconds:   2,
								FailureThreshold: 10,
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:              *sessionManager.CPU,
									corev1.ResourceMemory:           *sessionManager.Memory,
									corev1.ResourceEphemeralStorage: resource.MustParse(appEphemeralRequest),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:              resource.MustParse(sessionManagerCPULimit),
									corev1.ResourceMemory:           *sessionManager.Memory,
									corev1.ResourceEphemeralStorage: resource.MustParse(appEphemeralLimit),
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: sandboxfolder.WorkspaceVolume, MountPath: workspacesRoot, MountPropagation: workspacePropagation(p.cfg)},
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
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: sandboxfolder.WorkspaceVolume},
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

	sessionManager.applyToPodMeta(&ss.Spec.Template.ObjectMeta)
	sessionManager.applyToPodSpec(&ss.Spec.Template.Spec)
	p.cfg.Workspace.applyToClaim(&ss.Spec.VolumeClaimTemplates[0])

	return ss
}

// workspacePropagation lets mounts the sandbox-mount node agent makes under the
// workspace volume on the host reach the running session-manager container.
func workspacePropagation(cfg K8sProvisionerConfig) *corev1.MountPropagationMode {
	if !cfg.SandboxMount {
		return nil
	}
	mode := corev1.MountPropagationHostToContainer
	return &mode
}

func (p *K8sProvisioner) buildHeadlessService(name, namespace string, labels map[string]string) corev1.Service {
	return corev1.Service{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None",
			// A draining pod fails readiness and must stay reachable by its DNS name until its runs end.
			PublishNotReadyAddresses: true,
			Selector:                 labels,
			Ports: []corev1.ServicePort{
				{Name: "http", Port: sessionManagerPort, TargetPort: intstr.FromString("http"), Protocol: corev1.ProtocolTCP},
			},
		},
	}
}

func (p *K8sProvisioner) ensureHeadlessService(ctx context.Context, name, namespace string, labels map[string]string, owner metav1.OwnerReference, update bool) error {
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
	if !update {
		return nil
	}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		existing, err := p.client.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get existing service: %w", err)
		}
		existing.Labels = svc.Labels
		existing.Annotations = svc.Annotations
		// ClusterIP and the selector are immutable or protected; ports are the
		// mutable part of the spec a patch can change.
		existing.Spec.Ports = svc.Spec.Ports
		existing.Spec.PublishNotReadyAddresses = svc.Spec.PublishNotReadyAddresses
		applyOwnerRef(&existing.ObjectMeta, owner)
		_, err = p.client.CoreV1().Services(namespace).Update(ctx, existing, metav1.UpdateOptions{})
		return err
	})
}

// ensureDestinationRule creates an Istio DestinationRule in the system namespace
// that tells Istio the exact SPIFFE SAN to expect when kora-agents connects to
// this agent type's headless service.
//
// Headless services in Istio use ORIGINAL_DST clusters, which cannot perform
// EDS-based endpoint identity lookup. Without this rule, istiod falls back to
// the standard formula (spiffe://<td>/ns/<ns>/sa/<sa>), which doesn't match the
// SPIRE-issued SPIFFE ID (spiffe://<td>/tenant/<ns>/agent/<slug>).
func (p *K8sProvisioner) ensureDestinationRule(ctx context.Context, at AgentType, namespace string, owner metav1.OwnerReference, update bool) error {
	spiffeID := p.agentSpiffeID(at, namespace)
	host := agentServiceHost(at.Slug, namespace)
	name := agentDestinationRuleName(at.Slug, namespace)
	metadata := map[string]any{
		"name":      name,
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
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	if err != nil {
		existing, getErr := p.dynamicClient.Resource(destinationRuleGVR).
			Namespace(p.cfg.SystemNamespace).
			Get(ctx, name, metav1.GetOptions{})
		if getErr != nil {
			return fmt.Errorf("get existing destinationrule: %w", getErr)
		}
		// A rule under this name for another host was not made by kora. Refuse
		// rather than leave this agent unreachable behind it.
		if existingHost, _, _ := unstructured.NestedString(existing.Object, "spec", "host"); existingHost != host {
			return fmt.Errorf("destinationrule %s is for %s, not %s", name, existingHost, host)
		}
		if !update || owner.Kind == "" {
			return p.deleteLegacyDestinationRule(ctx, at.Slug, namespace)
		}
		existing.SetOwnerReferences(mergeOwnerRefs(existing.GetOwnerReferences(), owner))
		if _, err = p.dynamicClient.Resource(destinationRuleGVR).
			Namespace(p.cfg.SystemNamespace).
			Update(ctx, existing, metav1.UpdateOptions{}); err != nil {
			return err
		}
	}
	// Istio applies one DestinationRule per host, so the rule under the earlier
	// tenant-less name must go once this one exists.
	return p.deleteLegacyDestinationRule(ctx, at.Slug, namespace)
}

// agentServiceHost is the FQDN of an agent type's headless service.
func agentServiceHost(slug, namespace string) string {
	return fmt.Sprintf("%s.%s.svc.cluster.local", slug, namespace)
}

// agentDestinationRuleName is the DestinationRule for an agent type. Every
// tenant's rule lives in the system namespace, so the name carries the tenant
// namespace: two tenants can have an agent with the same slug. Slugs and
// namespaces are DNS labels and hold no dots, so the dot keeps every pair
// distinct and apart from the earlier agent-<slug> names.
func agentDestinationRuleName(slug, namespace string) string {
	return "agent-" + slug + "." + namespace
}

// deleteLegacyDestinationRule deletes the rule an earlier release named
// agent-<slug>, when it is this agent's. Another tenant's agent with the same
// slug may hold that name, so the rule's host decides.
func (p *K8sProvisioner) deleteLegacyDestinationRule(ctx context.Context, slug, namespace string) error {
	return p.deleteOwnDestinationRule(ctx, "agent-"+slug, slug, namespace)
}

// agentAuthorizationPolicyName is the AuthorizationPolicy over an agent's pods, or over a
// tenant's pool pods for the pool name.
func agentAuthorizationPolicyName(slug string) string { return "agent-" + slug + "-callers" }

// ensureAuthorizationPolicy restricts who may reach an agent pod's
// session-manager through the mesh.
//
// The session-manager API can start, message, interrupt, and kill any run on
// the pod, and its only legitimate caller is kora-agents
// (internal/platform/clients/sessionmanager). The application now authenticates
// that caller, but the mesh is the cheaper place to say it: the check runs in
// the sidecar, before the request is ever seen by the application, and it holds
// even if the application's own check regresses.
//
// It lives in the TENANT namespace, not kora-system, because that is where the
// agent pods run and an AuthorizationPolicy selects workloads in its own
// namespace. The DestinationRule above is the mirror image; it configures the
// CLIENT side (kora-agents' outbound mTLS expectations) so it belongs in
// kora-system. Neither can be templated in a chart: the agents these govern
// are created imperatively, per agent type, at provision time, so their
// policies are created the same way and torn down with them.
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
func (p *K8sProvisioner) ensureAuthorizationPolicy(ctx context.Context, at AgentType, namespace string, owner metav1.OwnerReference, update bool) error {
	return p.ensureSessionManagerPolicy(ctx, agentAuthorizationPolicyName(at.Slug), namespace, map[string]any{"app": at.Slug}, owner, update)
}

// ensureSessionManagerPolicy lets only kora-agents call the session-manager
// port of the pods selector matches, and anyone read GET /health.
func (p *K8sProvisioner) ensureSessionManagerPolicy(ctx context.Context, name, namespace string, selector map[string]any, owner metav1.OwnerReference, update bool) error {
	if !p.cfg.AuthorizationPolicyEnabled {
		return nil
	}
	metadata := map[string]any{
		"name":      name,
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
					"matchLabels": selector,
				},
				"action": "ALLOW",
				"rules": []any{
					map[string]any{
						"from": []any{
							map[string]any{
								"source": map[string]any{
									"principals": []any{
										fmt.Sprintf("%s/system/kora-agents", p.cfg.TrustDomain),
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
	if !update {
		return nil
	}
	if owner.Kind == "" {
		return nil
	}
	existing, err := p.dynamicClient.Resource(authorizationPolicyGVR).
		Namespace(namespace).
		Get(ctx, name, metav1.GetOptions{})
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

func (p *K8sProvisioner) deleteDestinationRule(ctx context.Context, slug, namespace string) error {
	if err := p.deleteOwnDestinationRule(ctx, agentDestinationRuleName(slug, namespace), slug, namespace); err != nil {
		return err
	}
	return p.deleteLegacyDestinationRule(ctx, slug, namespace)
}

// deleteOwnDestinationRule deletes the named rule when its host is this
// agent's service, so a rule that belongs to another agent is never removed.
func (p *K8sProvisioner) deleteOwnDestinationRule(ctx context.Context, name, slug, namespace string) error {
	rules := p.dynamicClient.Resource(destinationRuleGVR).Namespace(p.cfg.SystemNamespace)
	rule, err := rules.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get destinationrule %s: %w", name, err)
	}
	if host, _, _ := unstructured.NestedString(rule.Object, "spec", "host"); host != agentServiceHost(slug, namespace) {
		return nil
	}
	return ignoreNotFound(rules.Delete(ctx, name, metav1.DeleteOptions{}))
}

// ── helpers ───────────────────────────────────────────────────────────────────

func agentTypeLabels(at AgentType) map[string]string {
	return map[string]string{
		"app":               at.Slug,
		agentTypeLabel:      at.ID,
		"runkora.io/tenant": at.TenantID,
	}
}

func podLabels(at AgentType) map[string]string {
	return map[string]string{
		"app":                           at.Slug,
		agentTypeLabel:                  at.ID,
		"runkora.io/tenant":             at.TenantID,
		sandboxfolder.ManagedAgentLabel: "true",
		// slug used so the SPIFFE ID is human-readable:
		// spiffe://<trust-domain>/tenant/<namespace>/agent/<slug>
		"spiffe.io/agent":                  at.Slug,
		"spiffe.io/spire-managed-identity": "true",
		"runkora.io/identity-proxy":        "enabled",
	}
}

func ignoreNotFound(err error) error {
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

// SandboxEnvironment reports the environment of the run's sandbox pod while it
// is starting or running. live is false when the pod is missing, dead or terminating.
func (p *K8sProvisioner) SandboxEnvironment(ctx context.Context, runID, namespace string) (envID string, live bool, err error) {
	pod, err := p.sandboxPod(ctx, runID, namespace)
	if err != nil || pod == nil || !podLive(pod) {
		return "", false, err
	}
	return pod.Annotations[annotationEnvironmentID], true, nil
}

// sandboxPod returns the run's sandbox pod, or nil when there is none.
func (p *K8sProvisioner) sandboxPod(ctx context.Context, runID, namespace string) (*corev1.Pod, error) {
	pod, err := p.client.CoreV1().Pods(namespace).Get(ctx, sandboxfolder.SandboxPodName(runID), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get sandbox pod: %w", err)
	}
	return pod, nil
}

// podLive reports a sandbox pod that is neither terminating nor finished and whose sandbox-exec has not exited.
// The status of sandbox-exec catches a dead pod before its phase changes.
func podLive(pod *corev1.Pod) bool {
	return pod.DeletionTimestamp == nil && pod.Status.Phase != corev1.PodFailed && pod.Status.Phase != corev1.PodSucceeded &&
		sandboxExecTerminated(pod) == nil
}

// SandboxImage reads a SandboxImage in a tenant namespace. It returns ErrNotFound when there is none.
func (p *K8sProvisioner) SandboxImage(ctx context.Context, namespace, name string) (*si.SandboxImage, error) {
	u, err := p.dynamicClient.Resource(si.Resource).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get SandboxImage %s/%s: %w", namespace, name, err)
	}
	img := &si.SandboxImage{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, img); err != nil {
		return nil, fmt.Errorf("decode SandboxImage %s/%s: %w", namespace, name, err)
	}
	return img, nil
}

// SandboxImages lists the SandboxImages in a tenant namespace.
func (p *K8sProvisioner) SandboxImages(ctx context.Context, namespace string) ([]si.SandboxImage, error) {
	list, err := p.dynamicClient.Resource(si.Resource).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list SandboxImages in %s: %w", namespace, err)
	}
	out := make([]si.SandboxImage, 0, len(list.Items))
	for _, u := range list.Items {
		var img si.SandboxImage
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &img); err != nil {
			return nil, fmt.Errorf("decode SandboxImage %s/%s: %w", namespace, u.GetName(), err)
		}
		out = append(out, img)
	}
	return out, nil
}
