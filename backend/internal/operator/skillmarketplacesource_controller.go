// SkillMarketplaceSource reconciler.
//
// A source's verdict depends on its siblings and on its tenant's allowlist, so
// this reconciler does not judge the one source it was called for: it re-runs
// the aggregator, which judges every source and patches each status that
// changed. It has its own controller rather than mapping onto the tenant
// reconcile as Connection does, because a source in orbit-system or in a
// namespace with no tenant maps to no tenant and would never get a status.
//
// See docs/adr/2026-10-13-skill-marketplace-source-registration.md.
package operator

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// SkillMarketplaceSourceReconciler re-aggregates marketplace sources whenever
// one is created, deleted, or changes generation.
type SkillMarketplaceSourceReconciler struct {
	Aggregator *SkillMarketplaceAggregator
	Logger     logging.Logger
}

// Reconcile runs the aggregator, which covers the source in req whether it
// still exists or was deleted.
func (r *SkillMarketplaceSourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger.Debug(ctx, "reconciling SkillMarketplaceSource",
		logging.Field{Key: "source", Value: req.Name},
		logging.Field{Key: "namespace", Value: req.Namespace},
	)
	return ctrl.Result{}, r.Aggregator.Sync(ctx)
}

// SetupWithManager registers the controller. GenerationChangedPredicate drops
// the status-only updates the aggregator writes, so a status patch never
// triggers another run.
func (r *SkillMarketplaceSourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&SkillMarketplaceSource{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}
