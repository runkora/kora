// MCPServer reconciler (Phase 3: MCP Tool Gateway).
//
// Unlike Connection (which has no status and is aggregated off the tenant
// reconcile), an MCPServer carries its own Status and can live in orbit-system
// (global) where there is no owning OrbitTenant. It gets its own
// reconciler. On each MCPServer change this validates the spec, sets the CR's
// reconciliation Status (Pending|Aggregated|Invalid), and re-renders the
// orbit-mcp-servers registry ConfigMap via the shared MCPServerAggregator.
//
// Tenant active/suspend changes that affect which servers are included are
// handled by the TenantReconciler, which runs the same aggregator.
//
// See docs/adr/2026-06-29-mcp-server-registration-and-introspection.md.
package operator

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// conditionAggregated is the MCPServer status condition reporting whether the CR
// was accepted into the registry.
const conditionAggregated = "Aggregated"

// mcpPolicyResult is what reconciling one server's policies produced. It rides
// on the Aggregated condition's message, so a selector matching no pods is
// visible in kubectl describe rather than silently inert.
type mcpPolicyResult struct {
	// applies is false for a server with no workload selector, which has no pods
	// in this namespace to front.
	applies bool
	// pods is how many pods the selector matched.
	pods int
}

func (res mcpPolicyResult) message() string {
	const base = "rendered into the orbit-mcp-servers registry"
	switch {
	case !res.applies:
		return base
	case res.pods == 0:
		return base + "; policies applied, but spec.workload.selector matches no pods"
	default:
		return fmt.Sprintf("%s; tool-gateway admitted to %d pod(s), all other callers denied", base, res.pods)
	}
}

// MCPServerReconciler reconciles MCPServer CRs: it sets each CR's reconciliation
// Status and re-aggregates the orbit-mcp-servers registry ConfigMap.
type MCPServerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	// PodReader counts the pods a workload selector matches. It reads through
	// the API server rather than the manager's cache: caching pods would start
	// a cluster-wide Pod informer, which needs a watch on every pod in the
	// cluster to report one number per MCPServer.
	PodReader  client.Reader
	Aggregator *MCPServerAggregator
	Logger     logging.Logger
}

// Reconcile is called by controller-runtime whenever an MCPServer is created,
// updated, or deleted.
func (r *MCPServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	srv := &MCPServer{}
	if err := r.Get(ctx, req.NamespacedName, srv); err != nil {
		if apierrors.IsNotFound(err) {
			// CR deleted: re-aggregate so the removed server drops out of the
			// registry (and its scopes are pruned by the gateway on next sync).
			return ctrl.Result{}, r.Aggregator.Sync(ctx)
		}
		return ctrl.Result{}, fmt.Errorf("get MCPServer %q: %w", req.NamespacedName, err)
	}

	r.Logger.Info(ctx, "reconciling MCPServer",
		logging.Field{Key: "mcpserver", Value: srv.Name},
		logging.Field{Key: "namespace", Value: srv.Namespace},
		logging.Field{Key: "phase", Value: srv.Status.Phase},
	)

	// Validate the spec for this CR's Status. Whether valid or not we still run
	// the aggregator: a valid server must be (re)included, and an invalid one
	// must be excluded (e.g. it was valid before and the spec just regressed).
	vErr := validateMCPServerSpec(srv.Spec, srv.Name, srv.Namespace)

	if syncErr := r.Aggregator.Sync(ctx); syncErr != nil {
		return ctrl.Result{}, syncErr
	}

	if vErr != nil {
		r.Logger.Warn(ctx, "MCPServer spec invalid",
			logging.Field{Key: "mcpserver", Value: srv.Name},
			logging.Field{Key: "namespace", Value: srv.Namespace},
			logging.Field{Key: "reason", Value: vErr.Error()},
		)
		return ctrl.Result{}, r.setStatus(ctx, srv, MCPServerPhaseInvalid,
			metav1.ConditionFalse, "SpecInvalid", vErr.Error())
	}

	policies, pErr := r.reconcilePolicies(ctx, srv)
	if pErr != nil {
		return ctrl.Result{}, pErr
	}

	return ctrl.Result{}, r.setStatus(ctx, srv, MCPServerPhaseAggregated,
		metav1.ConditionTrue, "Aggregated", policies.message())
}

// reconcilePolicies applies the policies fronting a server's workload, both in
// the CR's namespace and owned by it: ingress admitting the tool-gateway, and an
// AuthorizationPolicy naming its principal. The gateway's own egress is a single
// chart rule, so there is nothing to reconcile in the system namespace. A server
// with no workload selector has no pods here, so nothing is applied.
func (r *MCPServerReconciler) reconcilePolicies(ctx context.Context, srv *MCPServer) (mcpPolicyResult, error) {
	if srv.Spec.Workload == nil || len(srv.Spec.Workload.Selector) == 0 {
		return mcpPolicyResult{}, nil
	}

	port, err := srv.endpointPort()
	if err != nil {
		return mcpPolicyResult{}, err
	}

	// Taken before the policies are written, so the condition reports whether
	// anything is actually being fronted.
	pods := &corev1.PodList{}
	if err := r.PodReader.List(ctx, pods,
		client.InNamespace(srv.Namespace),
		client.MatchingLabels(srv.Spec.Workload.Selector),
	); err != nil {
		return mcpPolicyResult{}, fmt.Errorf("list pods for MCPServer %q: %w", srv.Name, err)
	}

	ingress := srv.ingressPolicy(port)
	if err := controllerutil.SetControllerReference(srv, ingress, r.Scheme); err != nil {
		return mcpPolicyResult{}, fmt.Errorf("own ingress policy for %q: %w", srv.Name, err)
	}
	if err := r.apply(ctx, ingress); err != nil {
		return mcpPolicyResult{}, err
	}

	// The NetworkPolicy alone admits the gateway's address, not its identity.
	// Applying it without the AuthorizationPolicy would leave the server open to
	// anything that reaches the port, so an unset trust domain fails the
	// reconcile rather than half-applying.
	if trustDomain == "" {
		return mcpPolicyResult{}, fmt.Errorf(
			"MCPServer %q needs a trust domain to name the tool-gateway's identity: set global.trustDomain", srv.Name)
	}
	authz := srv.authorizationPolicy(port)
	if err := controllerutil.SetControllerReference(srv, authz, r.Scheme); err != nil {
		return mcpPolicyResult{}, fmt.Errorf("own authorization policy for %q: %w", srv.Name, err)
	}
	if err := r.apply(ctx, authz); err != nil {
		return mcpPolicyResult{}, err
	}

	// Without this the caller expects the SAN Istio derives from the
	// destination's ServiceAccount, which no SPIRE-issued identity matches, and
	// the tool-gateway's handshake fails with a 503.
	host, hostErr := srv.endpointHost()
	if hostErr != nil {
		return mcpPolicyResult{}, hostErr
	}
	rule := srv.destinationRule(port, host)
	if err := controllerutil.SetControllerReference(srv, rule, r.Scheme); err != nil {
		return mcpPolicyResult{}, fmt.Errorf("own destination rule for %q: %w", srv.Name, err)
	}
	if err := r.apply(ctx, rule); err != nil {
		return mcpPolicyResult{}, err
	}

	return mcpPolicyResult{applies: true, pods: len(pods.Items)}, nil
}

// apply creates an operator-owned object, or merges the desired state onto the
// one already there. A merge patch needs the object to exist, hence the create
// first, and it leaves fields the API server owns alone.
func (r *MCPServerReconciler) apply(ctx context.Context, obj client.Object) error {
	err := r.Create(ctx, obj)
	if err == nil {
		return nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create %T %q in %q: %w", obj, obj.GetName(), obj.GetNamespace(), err)
	}
	if err := r.Patch(ctx, obj, client.Merge, client.FieldOwner("orbit-operator")); err != nil {
		return fmt.Errorf("patch %T %q in %q: %w", obj, obj.GetName(), obj.GetNamespace(), err)
	}
	return nil
}

// setStatus patches the MCPServer phase, LastReconciledAt and Aggregated
// condition on every call. SetupWithManager's GenerationChangedPredicate
// filters out the resulting status-only update; this never re-triggers its
// own reconcile.
func (r *MCPServerReconciler) setStatus(
	ctx context.Context,
	srv *MCPServer,
	phase string,
	condStatus metav1.ConditionStatus,
	reason, message string,
) error {
	patch := client.MergeFrom(srv.DeepCopy())
	now := metav1.Now()
	srv.Status.Phase = phase
	srv.Status.LastReconciledAt = &now
	meta.SetStatusCondition(&srv.Status.Conditions, metav1.Condition{
		Type:               conditionAggregated,
		Status:             condStatus,
		ObservedGeneration: srv.Generation,
		Reason:             reason,
		Message:            message,
	})
	if err := r.Status().Patch(ctx, srv, patch); err != nil {
		return fmt.Errorf("patch MCPServer %q status: %w", srv.Name, err)
	}
	return nil
}

// SetupWithManager registers the controller with the controller-runtime Manager.
func (r *MCPServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&MCPServer{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}
