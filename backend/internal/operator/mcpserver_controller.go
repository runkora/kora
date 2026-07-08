// MCPServer reconciler (Phase 3 — MCP Tool Gateway).
//
// Unlike Connection (which has no status and is aggregated off the tenant
// reconcile), an MCPServer carries its own Status and can live in orbit-system
// (global) where there is no owning OrbitTenant — so it gets its own
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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// conditionAggregated is the MCPServer status condition reporting whether the CR
// was accepted into the registry.
const conditionAggregated = "Aggregated"

// MCPServerReconciler reconciles MCPServer CRs: it sets each CR's reconciliation
// Status and re-aggregates the orbit-mcp-servers registry ConfigMap.
type MCPServerReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	Aggregator *MCPServerAggregator
	Logger     logging.Logger
}

// Reconcile is called by controller-runtime whenever an MCPServer is created,
// updated, or deleted.
func (r *MCPServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	srv := &MCPServer{}
	if err := r.Get(ctx, req.NamespacedName, srv); err != nil {
		if apierrors.IsNotFound(err) {
			// CR deleted — re-aggregate so the removed server drops out of the
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
	vErr := validateMCPServerSpec(srv.Spec, srv.Namespace)

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

	return ctrl.Result{}, r.setStatus(ctx, srv, MCPServerPhaseAggregated,
		metav1.ConditionTrue, "Aggregated", "rendered into the orbit-mcp-servers registry")
}

// setStatus patches the MCPServer phase + Aggregated condition. It is a no-op
// when the phase and condition are already in the desired state, which prevents
// a status-update event from triggering an endless reconcile loop (the operator
// uses no controller-runtime predicates).
func (r *MCPServerReconciler) setStatus(
	ctx context.Context,
	srv *MCPServer,
	phase string,
	condStatus metav1.ConditionStatus,
	reason, message string,
) error {
	existing := meta.FindStatusCondition(srv.Status.Conditions, conditionAggregated)
	condCurrent := existing != nil &&
		existing.Status == condStatus &&
		existing.Reason == reason &&
		existing.Message == message &&
		existing.ObservedGeneration == srv.Generation
	if srv.Status.Phase == phase && condCurrent {
		return nil
	}

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
		For(&MCPServer{}).
		Complete(r)
}
