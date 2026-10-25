package operator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/runkora/kora/backend/internal/platform/declarative"
	dv1 "github.com/runkora/kora/backend/internal/platform/declarative/v1alpha1"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

const (
	// declaredRetry is how soon an unresolved or unready resource is reconciled again.
	declaredRetry = 30 * time.Second
	// declaredResync is how often a ready resource is applied again. The
	// controller ignores the cache's resync events. They change no generation.
	declaredResync = 5 * time.Minute
)

// DeclaredClient is the part of clients/declared.Client the projector calls.
type DeclaredClient interface {
	Apply(ctx context.Context, kind string, item declarative.Item) (declarative.Result, error)
	Delete(ctx context.Context, kind, tenantID, name string) error
	List(ctx context.Context, kind, tenantID string) ([]declarative.Declared, error)
}

// DeclaredClients holds one client per owning service.
type DeclaredClients struct {
	Agents, Triggers, PolicyService, InferenceGateway DeclaredClient
}

// prepared is what the operator's own checks decide about a resource before
// the owning service sees it.
type prepared struct {
	tenantID string
	// refusal is a false Accepted condition. The resource is not sent.
	refusal *metav1.Condition
	// missing lists unresolved references. The resource is not sent.
	missing []string
	refs    map[string]string
	secret  []byte
	spec    any
}

// declaredKind is one declarative kind and the service that owns it.
type declaredKind struct {
	kind    string
	newObj  func() client.Object
	newList func() client.ObjectList
	client  DeclaredClient
	prepare func(ctx context.Context, r *DeclaredReconciler, obj client.Object) (prepared, error)
	// watches enqueue this kind when a resource it references changes.
	watches []declaredWatch
}

type declaredWatch struct {
	obj    client.Object
	mapper handler.MapFunc
}

// DeclaredReconciler projects one declarative kind into its owning service.
type DeclaredReconciler struct {
	client.Client
	// Reader reads from the API server. The deletion sweep uses it so a
	// resource the cache has not seen yet is never taken for deleted.
	Reader client.Reader
	Logger logging.Logger
	kind   declaredKind
}

// Reconcile resolves the resource's tenant and references, applies it
// through the owning service and writes the outcome to its status.
func (r *DeclaredReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	obj := r.kind.newObj()
	if err := r.Get(ctx, req.NamespacedName, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return r.reconcileGone(ctx, req.Namespace)
		}
		return ctrl.Result{}, fmt.Errorf("get %s %s: %w", r.kind.kind, req.NamespacedName, err)
	}
	p, err := r.kind.prepare(ctx, r, obj)
	if err != nil {
		return ctrl.Result{}, err
	}

	before := obj.DeepCopyObject().(client.Object)
	status := declaredStatus(obj)
	gen := obj.GetGeneration()
	notProjected := condFalse(reasonPending, "The resource is not projected.")
	ready := false
	switch {
	case p.refusal != nil:
		setConditions(status, gen, *p.refusal, condFalse(reasonPending, "References are not checked."), notProjected)
	case len(p.missing) > 0:
		setConditions(status, gen, condTrue(reasonAccepted, ""),
			condFalse(reasonRefNotFound, strings.Join(p.missing, ", ")+" not found or not Ready."), notProjected)
	default:
		res, err := r.apply(ctx, obj, p)
		if err != nil {
			return ctrl.Result{}, err
		}
		ready = setResult(status, gen, res)
		if ts, ok := obj.(*dv1.TriggerSource); ok && res.ID != "" {
			ts.Status.WebhookPath = "/api/v1/webhooks/" + res.ID
		}
	}
	if err := r.Status().Patch(ctx, obj, client.MergeFrom(before)); err != nil {
		return ctrl.Result{}, fmt.Errorf("patch %s %s status: %w", r.kind.kind, req.NamespacedName, err)
	}
	r.sweepMovedRole(ctx, obj)
	if !ready {
		return ctrl.Result{RequeueAfter: declaredRetry}, nil
	}
	return ctrl.Result{RequeueAfter: declaredResync}, nil
}

// reconcileGone deletes the declared items of a resource that no longer
// exists. A delete the service refuses for now is retried.
func (r *DeclaredReconciler) reconcileGone(ctx context.Context, namespace string) (ctrl.Result, error) {
	deferred, err := r.sweepNamespace(ctx, namespace)
	if err != nil {
		return ctrl.Result{}, err
	}
	if deferred {
		return ctrl.Result{RequeueAfter: declaredRetry}, nil
	}
	return ctrl.Result{}, nil
}

// sweepMovedRole sweeps after a system-namespace PolicyRole changes. It can
// move to another tenant, and the one it left keeps its row until swept.
func (r *DeclaredReconciler) sweepMovedRole(ctx context.Context, obj client.Object) {
	if _, ok := obj.(*dv1.PolicyRole); !ok || obj.GetNamespace() != systemNamespace {
		return
	}
	if _, err := r.sweepNamespace(ctx, systemNamespace); err != nil {
		r.Logger.Warn(ctx, "sweep after a system PolicyRole change", logging.Field{Key: "error", Value: err.Error()})
	}
}

func (r *DeclaredReconciler) apply(ctx context.Context, obj client.Object, p prepared) (declarative.Result, error) {
	spec, err := json.Marshal(p.spec)
	if err != nil {
		return declarative.Result{}, fmt.Errorf("encode %s spec: %w", r.kind.kind, err)
	}
	res, err := r.kind.client.Apply(ctx, r.kind.kind, declarative.Item{
		TenantID:   p.tenantID,
		Name:       obj.GetName(),
		UID:        string(obj.GetUID()),
		Generation: obj.GetGeneration(),
		Spec:       spec,
		Refs:       p.refs,
		Secret:     p.secret,
	})
	if err != nil {
		return res, fmt.Errorf("apply %s %s/%s: %w", r.kind.kind, obj.GetNamespace(), obj.GetName(), err)
	}
	return res, nil
}

// setResult writes the service's outcome as conditions and reports whether
// the item is ready.
func setResult(status *dv1.Status, gen int64, res declarative.Result) bool {
	accepted := condTrue(reasonAccepted, "")
	resolved := condTrue(reasonResolved, "")
	notProjected := condFalse(reasonPending, "The resource is not projected.")
	switch res.Outcome {
	case declarative.OutcomeProjected:
		status.ID = res.ID
		ready := condTrue(reasonProjected, res.Message)
		if !res.Ready {
			ready = condFalse(reasonPending, nonEmpty(res.Message, "The item is not live yet."))
		}
		setConditions(status, gen, accepted, resolved, ready)
		return res.Ready
	case declarative.OutcomeNameConflict:
		setConditions(status, gen, condFalse(reasonNameConflict, res.Message), resolved, notProjected)
	case declarative.OutcomePending:
		setConditions(status, gen, accepted, condFalse(reasonPending, res.Message), notProjected)
	default:
		setConditions(status, gen, condFalse(reasonInvalid, res.Message), resolved, notProjected)
	}
	return false
}

func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// SetupWithManager registers one controller for the kind. Spec changes and
// changes to referenced resources reconcile it. Its own status patches do not.
func (r *DeclaredReconciler) SetupWithManager(mgr ctrl.Manager) error {
	b := ctrl.NewControllerManagedBy(mgr).
		Named("declared-"+r.kind.kind).
		For(r.kind.newObj(), builder.WithPredicates(predicate.GenerationChangedPredicate{}))
	for _, w := range r.kind.watches {
		b = b.Watches(w.obj, handler.EnqueueRequestsFromMapFunc(w.mapper))
	}
	return b.Complete(r)
}

// sweepNamespace deletes the declared items whose resource is gone, for the
// tenants a resource in namespace could belong to.
func (r *DeclaredReconciler) sweepNamespace(ctx context.Context, namespace string) (bool, error) {
	tenants, err := r.sweepTenants(ctx, namespace)
	if err != nil {
		return false, err
	}
	return r.sweep(ctx, tenants)
}

// sweepTenants maps a namespace to the tenants whose declared items a change
// there can affect. A system-namespace PolicyRole names its tenant in its spec.
// That spec is gone after a delete and may have moved after an update. Every
// tenant is swept.
func (r *DeclaredReconciler) sweepTenants(ctx context.Context, namespace string) ([]string, error) {
	if r.kind.kind == declarative.KindVirtualModels {
		return []string{declarative.PlatformTenant}, nil
	}
	tenants, err := r.liveTenants(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, t := range tenants {
		if namespace == systemNamespace || t.namespace == namespace {
			out = append(out, t.id)
		}
	}
	return out, nil
}

// heldItems records, per tenant, the UIDs and names of the live resources.
type heldItems map[string]struct{ uids, names map[string]bool }

func (h heldItems) add(tenantID, uid, name string) {
	e, ok := h[tenantID]
	if !ok {
		e.uids, e.names = map[string]bool{}, map[string]bool{}
		h[tenantID] = e
	}
	e.uids[uid], e.names[name] = true, true
}

// holds reports whether a live resource of the tenant has the item's UID or
// name. The name keeps an item that a resource recreated under the same name
// has just applied again.
func (h heldItems) holds(tenantID string, d declarative.Declared) bool {
	e := h[tenantID]
	return e.uids[d.UID] || e.names[d.Name]
}

// sweep deletes each tenant's declared items of this kind that no live
// resource of that tenant holds. It reports whether a service refused a
// delete for now, such as an environment an agent still uses.
//
// Every tenant is listed from its service before the resources are listed
// from the API server. An item a service holds was applied from a resource
// that existed by then. A resource created during the sweep cannot be taken
// for a deleted one.
func (r *DeclaredReconciler) sweep(ctx context.Context, tenants []string) (bool, error) {
	declared := map[string][]declarative.Declared{}
	var errs []error
	for _, tenantID := range tenants {
		items, err := r.kind.client.List(ctx, r.kind.kind, tenantID)
		if err != nil {
			errs = append(errs, fmt.Errorf("list declared %s for tenant %q: %w", r.kind.kind, tenantID, err))
			continue
		}
		if len(items) > 0 {
			declared[tenantID] = items
		}
	}
	if len(declared) == 0 {
		return false, errors.Join(errs...)
	}
	held, err := r.heldItems(ctx)
	if err != nil {
		return false, errors.Join(append(errs, err)...)
	}
	deferred := false
	for tenantID, items := range declared {
		for _, d := range items {
			if held.holds(tenantID, d) {
				continue
			}
			if err := r.kind.client.Delete(ctx, r.kind.kind, tenantID, d.Name); err != nil {
				r.Logger.Warn(ctx, "declared item delete deferred",
					logging.Field{Key: "kind", Value: r.kind.kind},
					logging.Field{Key: "tenant", Value: tenantID},
					logging.Field{Key: "name", Value: d.Name},
					logging.Field{Key: "error", Value: err.Error()},
				)
				deferred = true
				continue
			}
			r.Logger.Info(ctx, "deleted declared item whose resource is gone",
				logging.Field{Key: "kind", Value: r.kind.kind},
				logging.Field{Key: "tenant", Value: tenantID},
				logging.Field{Key: "name", Value: d.Name},
			)
		}
	}
	return deferred, errors.Join(errs...)
}

// heldItems lists the kind's resources from the API server and maps each to
// the tenant it projects into now.
func (r *DeclaredReconciler) heldItems(ctx context.Context) (heldItems, error) {
	list := r.kind.newList()
	if err := r.Reader.List(ctx, list); err != nil {
		return nil, fmt.Errorf("list %s: %w", r.kind.kind, err)
	}
	byNamespace, err := r.servedNamespaces(ctx)
	if err != nil {
		return nil, err
	}
	held := heldItems{}
	err = meta.EachListItem(list, func(o runtime.Object) error {
		obj := o.(client.Object)
		tenantID := byNamespace[obj.GetNamespace()]
		switch v := obj.(type) {
		case *dv1.VirtualModel:
			tenantID = declarative.PlatformTenant
		case *dv1.PolicyRole:
			if v.Namespace == systemNamespace {
				tenantID = v.Spec.Tenant
			}
		}
		held.add(tenantID, string(obj.GetUID()), obj.GetName())
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", r.kind.kind, err)
	}
	return held, nil
}

// tenantRef is a served tenant whose namespace exists.
type tenantRef struct{ id, namespace string }

// liveTenants returns the active tenants that hold their namespace and whose
// namespace exists. A tenant whose namespace is gone is never swept.
func (r *DeclaredReconciler) liveTenants(ctx context.Context) ([]tenantRef, error) {
	served, err := r.servedNamespaces(ctx)
	if err != nil {
		return nil, err
	}
	var out []tenantRef
	for ns, id := range served {
		if err := r.Reader.Get(ctx, client.ObjectKey{Name: ns}, &corev1.Namespace{}); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return nil, fmt.Errorf("get namespace %q: %w", ns, err)
		}
		out = append(out, tenantRef{id: id, namespace: ns})
	}
	return out, nil
}

// SweepAll runs the deletion sweep for every kind and tenant, once at start
// and then every interval. It catches resources deleted while the operator or
// a service was down. Errors are logged and retried on the next pass.
func SweepAll(ctx context.Context, reconcilers []*DeclaredReconciler, interval time.Duration, logger logging.Logger) error {
	wait.UntilWithContext(ctx, func(ctx context.Context) { sweepOnce(ctx, reconcilers, logger) }, interval)
	return nil
}

func sweepOnce(ctx context.Context, reconcilers []*DeclaredReconciler, logger logging.Logger) {
	for _, r := range reconcilers {
		// The system namespace reaches every tenant.
		if _, err := r.sweepNamespace(ctx, systemNamespace); err != nil {
			logger.Warn(ctx, "declared sweep failed", logging.Field{Key: "error", Value: err.Error()})
		}
	}
}
