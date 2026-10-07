package webhookcert

import (
	"context"
	"time"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// Refresher keeps webhook certificates and configuration current on every replica.
type Refresher struct {
	Manager  *Manager
	Interval time.Duration
}

// NeedLeaderElection reports false so every replica serves a valid certificate.
func (r *Refresher) NeedLeaderElection() bool { return false }

func (r *Refresher) Start(ctx context.Context) error {
	tick := time.NewTicker(r.Interval)
	defer tick.Stop()
	for {
		if err := r.Manager.Reconcile(ctx); err != nil {
			r.Manager.Logger.Error(ctx, "webhook certificate refresh failed", logging.Field{Key: "error", Value: err.Error()})
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

// Rotator runs on the leader only and renews the CA and serving certificate
// ahead of expiry.
type Rotator struct {
	Manager  *Manager
	Interval time.Duration
}

// NeedLeaderElection reports true: one writer for the shared Secret.
func (r *Rotator) NeedLeaderElection() bool { return true }

func (r *Rotator) Start(ctx context.Context) error {
	tick := time.NewTicker(r.Interval)
	defer tick.Stop()
	for {
		if err := r.Manager.Rotate(ctx); err != nil {
			r.Manager.Logger.Error(ctx, "webhook certificate rotation failed", logging.Field{Key: "error", Value: err.Error()})
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

// DriftReconciler re-patches a webhook configuration the moment something else
// (a GitOps sync reapplying the chart) rewrites it.
type DriftReconciler struct {
	Manager *Manager
}

// Reconcile re-wires both configurations; the trigger names one of them but the
// patch is idempotent and cheap.
func (d *DriftReconciler) Reconcile(ctx context.Context, _ reconcile.Request) (reconcile.Result, error) {
	if err := d.Manager.Reconcile(ctx); err != nil {
		return reconcile.Result{RequeueAfter: 15 * time.Second}, err
	}
	return reconcile.Result{}, nil
}

// SetupWithManager watches only kora's two webhook configurations.
func (d *DriftReconciler) SetupWithManager(mgr ctrl.Manager) error {
	own := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		return obj.GetName() == Webhooks.Mutating || obj.GetName() == Webhooks.Validating
	})
	return ctrl.NewControllerManagedBy(mgr).
		Named("webhook-drift").
		For(&admissionv1.MutatingWebhookConfiguration{}, builder.WithPredicates(own)).
		Watches(&admissionv1.ValidatingWebhookConfiguration{},
			handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []reconcile.Request {
				return []reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(obj)}}
			}),
			builder.WithPredicates(own)).
		Complete(d)
}
