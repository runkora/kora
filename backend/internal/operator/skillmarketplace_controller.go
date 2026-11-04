// SkillMarketplace reconciler.
//
// A Marketplace's verdict depends on its siblings, so this reconciler re-runs
// the SkillMarketplaceAggregator, which judges every Marketplace and patches
// each status that changed. It has its own controller because a Marketplace in
// the system namespace, or in a namespace with no tenant, maps to no tenant and
// would never get a status. Tenant suspends and deletes reach the aggregator
// through the TenantReconciler.
//
// See docs/adr/2026-08-17-agent-skills.md.
package operator

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// SkillMarketplaceReconciler re-aggregates Marketplaces whenever one is
// created, deleted, or changes generation.
type SkillMarketplaceReconciler struct {
	Client     client.Reader
	Aggregator *SkillMarketplaceAggregator
	Logger     logging.Logger
}

// Reconcile runs the aggregator. A Marketplace that still exists requeues so
// a Secret change, which no watch reports, reaches its status.
func (r *SkillMarketplaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger.Debug(ctx, "reconciling SkillMarketplace",
		logging.Field{Key: "marketplace", Value: req.Name},
		logging.Field{Key: "namespace", Value: req.Namespace},
	)
	if err := r.Aggregator.Sync(ctx); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.Client.Get(ctx, req.NamespacedName, &SkillMarketplace{}); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get SkillMarketplace %s: %w", req.NamespacedName, err)
	}
	return ctrl.Result{RequeueAfter: credentialRecheckInterval}, nil
}

// SetupWithManager registers the controller. GenerationChangedPredicate drops
// the status-only updates the aggregator writes.
func (r *SkillMarketplaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&SkillMarketplace{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}
