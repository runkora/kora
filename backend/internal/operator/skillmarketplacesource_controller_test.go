package operator

import (
	"context"
	"errors"
	"io"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/clear-route/orbit/internal/platform/logging"
)

func TestSkillMarketplaceSourceReconciler_SetsStatus(t *testing.T) {
	tenant := marketTenant("acme", true, "acme-corp/internal-skills")
	src := marketSrc("orbit-tenant-acme", "acme-internal", "Acme Internal Skills", "acme-corp", "internal-skills", marketplaceT0)
	c := newMarketplaceClient(t, &tenant, &src)
	logger := logging.NewLoggerWithOutput(io.Discard)
	r := &SkillMarketplaceSourceReconciler{Aggregator: NewSkillMarketplaceAggregator(c, logger), Logger: logger}

	key := client.ObjectKeyFromObject(&src)
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	assertMarketplaceStatus(t, getMarketplaceSource(t, c, key),
		SkillMarketplaceSourcePhaseAggregated, MarketplaceReasonAggregated, metav1.ConditionTrue)
}

// A source in orbit-system maps to no tenant, so only this reconciler can give
// it a status.
func TestSkillMarketplaceSourceReconciler_GlobalSourceGetsStatus(t *testing.T) {
	src := marketSrc(systemNamespace, "global", "Global", "clear-route", "skills", marketplaceT0)
	c := newMarketplaceClient(t, &src)
	logger := logging.NewLoggerWithOutput(io.Discard)
	r := &SkillMarketplaceSourceReconciler{Aggregator: NewSkillMarketplaceAggregator(c, logger), Logger: logger}

	key := client.ObjectKeyFromObject(&src)
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	assertMarketplaceStatus(t, getMarketplaceSource(t, c, key),
		SkillMarketplaceSourcePhaseInvalid, MarketplaceReasonGlobalSourcesNotSupported, metav1.ConditionFalse)
}

// A delete event names an object that no longer exists; the run must still
// succeed and drop it from the file.
func TestSkillMarketplaceSourceReconciler_DeletedSourceStillSyncs(t *testing.T) {
	tenant := marketTenant("acme", true)
	c := newMarketplaceClient(t, &tenant)
	logger := logging.NewLoggerWithOutput(io.Discard)
	r := &SkillMarketplaceSourceReconciler{Aggregator: NewSkillMarketplaceAggregator(c, logger), Logger: logger}

	req := ctrl.Request{NamespacedName: client.ObjectKey{Namespace: "orbit-tenant-acme", Name: "gone"}}
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if file := readMarketplaceFile(t, c); len(file.Tenants) != 0 {
		t.Errorf("file = %v, want no tenants", file.Tenants)
	}
}

// A failed run is returned so controller-runtime requeues it with backoff.
func TestSkillMarketplaceSourceReconciler_SyncErrorIsReturned(t *testing.T) {
	errList := errors.New("list refused")
	c := fake.NewClientBuilder().WithScheme(NewScheme()).WithInterceptorFuncs(interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*OrbitTenantList); ok {
				return errList
			}
			return c.List(ctx, list, opts...)
		},
	}).Build()
	logger := logging.NewLoggerWithOutput(io.Discard)
	r := &SkillMarketplaceSourceReconciler{Aggregator: NewSkillMarketplaceAggregator(c, logger), Logger: logger}

	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Namespace: "orbit-tenant-acme", Name: "any"}})
	if !errors.Is(err, errList) {
		t.Fatalf("err = %v, want the list error", err)
	}
}

// newMarketplaceTenantReconciler runs every aggregator over c, as main wires
// them, so a tenant reconcile reaches the marketplace step.
func newMarketplaceTenantReconciler(t *testing.T, objs ...client.Object) (*TenantReconciler, client.Client) {
	t.Helper()
	c := fake.NewClientBuilder().
		WithScheme(NewScheme()).
		WithObjects(objs...).
		WithStatusSubresource(&OrbitTenant{}, &SkillMarketplaceSource{}).
		Build()
	logger := logging.NewLoggerWithOutput(io.Discard)
	return &TenantReconciler{
		Client:                c,
		Scheme:                c.Scheme(),
		Registry:              NewTenantRegistry(c, logger),
		Aggregator:            NewClientSecretAggregator(c, logger),
		CAAggregator:          NewTenantCAAggregator(c, logger),
		ConnAggregator:        NewConnectionAggregator(c, logger),
		MCPAggregator:         NewMCPServerAggregator(c, logger),
		OAuthAggregator:       NewOAuthClientAggregator(c, logger),
		MarketplaceAggregator: NewSkillMarketplaceAggregator(c, logger),
		Logger:                logger,
	}, c
}

func TestTenantReconciler_ActiveTenantAggregatesMarketplaceSources(t *testing.T) {
	tenant := marketTenant("acme", true, "acme-corp/internal-skills")
	src := marketSrc("orbit-tenant-acme", "acme-internal", "Acme Internal Skills", "acme-corp", "internal-skills", marketplaceT0)
	r, c := newMarketplaceTenantReconciler(t, &tenant, &src)

	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(&tenant)}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	assertMarketplaceStatus(t, getMarketplaceSource(t, c, client.ObjectKeyFromObject(&src)),
		SkillMarketplaceSourcePhaseAggregated, MarketplaceReasonAggregated, metav1.ConditionTrue)
}

func TestTenantReconciler_SuspendedTenantDropsMarketplaceSources(t *testing.T) {
	tenant := marketTenant("acme", false, "acme-corp/internal-skills")
	src := marketSrc("orbit-tenant-acme", "acme-internal", "Acme Internal Skills", "acme-corp", "internal-skills", marketplaceT0)
	r, c := newMarketplaceTenantReconciler(t, &tenant, &src)

	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(&tenant)}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	assertMarketplaceStatus(t, getMarketplaceSource(t, c, client.ObjectKeyFromObject(&src)),
		SkillMarketplaceSourcePhasePending, MarketplaceReasonTenantNotActive, metav1.ConditionFalse)
	if file := readMarketplaceFile(t, c); len(file.Tenants) != 0 {
		t.Errorf("file = %v, want no tenants", file.Tenants)
	}
}

func TestTenantReconciler_DeletedTenantDropsMarketplaceSources(t *testing.T) {
	src := marketSrc("orbit-tenant-acme", "acme-internal", "Acme Internal Skills", "acme-corp", "internal-skills", marketplaceT0)
	r, c := newMarketplaceTenantReconciler(t, &src)

	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "acme"}}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	assertMarketplaceStatus(t, getMarketplaceSource(t, c, client.ObjectKeyFromObject(&src)),
		SkillMarketplaceSourcePhasePending, MarketplaceReasonTenantNotActive, metav1.ConditionFalse)
	if file := readMarketplaceFile(t, c); len(file.Tenants) != 0 {
		t.Errorf("file = %v, want no tenants", file.Tenants)
	}
}
