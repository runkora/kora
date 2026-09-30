package operator

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// InferenceProviderReconciler re-renders the provider aggregate on every
// InferenceProvider change and records each resource's outcome in its status.
// It requeues every Resync; the operator does not watch Secrets, and the
// requeue is how a rotated credential reaches the gateway.
type InferenceProviderReconciler struct {
	client.Client
	Aggregator *InferenceProviderAggregator
	Resync     time.Duration
	Logger     logging.Logger
}

// Reconcile runs the aggregator and patches the reconciled resource's status.
func (r *InferenceProviderReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	results, err := r.Aggregator.Sync(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	p := &InferenceProvider{}
	if err := r.Get(ctx, req.NamespacedName, p); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get InferenceProvider %q: %w", req.NamespacedName, err)
	}

	res, ok := results[req.NamespacedName]
	if !ok {
		// Created after the aggregator listed; the create event reconciles it.
		return ctrl.Result{Requeue: true}, nil
	}
	if err := r.setStatus(ctx, p, res); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: r.Resync}, nil
}

func (r *InferenceProviderReconciler) setStatus(ctx context.Context, p *InferenceProvider, res providerResult) error {
	patch := client.MergeFrom(p.DeepCopy())
	now := metav1.Now()
	phase, cond := InferenceProviderPhaseReady, metav1.ConditionTrue
	if !res.ok() {
		phase, cond = InferenceProviderPhaseInvalid, metav1.ConditionFalse
	}
	p.Status.Phase = phase
	p.Status.ProviderName = res.providerName
	p.Status.ObservedGeneration = p.Generation
	p.Status.LastReconciledAt = &now
	meta.SetStatusCondition(&p.Status.Conditions, metav1.Condition{
		Type:               conditionAggregated,
		Status:             cond,
		ObservedGeneration: p.Generation,
		Reason:             res.reason,
		Message:            res.message,
	})
	if err := r.Status().Patch(ctx, p, patch); err != nil {
		return fmt.Errorf("patch InferenceProvider %q status: %w", p.Name, err)
	}
	return nil
}

// SetupWithManager registers the controller. Annotation changes reconcile too:
// bumping an annotation forces an immediate re-render after a Secret rotation.
// Status patches change neither generation nor annotations and do not requeue.
func (r *InferenceProviderReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&InferenceProvider{}, builder.WithPredicates(predicate.Or(
			predicate.GenerationChangedPredicate{},
			predicate.AnnotationChangedPredicate{},
		))).
		Complete(r)
}
