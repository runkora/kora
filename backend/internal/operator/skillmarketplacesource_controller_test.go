package operator

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/marketplace"
)

func newTestMarketplaceReconciler(c client.Client) *SkillMarketplaceSourceReconciler {
	logger := logging.NewLoggerWithOutput(io.Discard)
	return &SkillMarketplaceSourceReconciler{Aggregator: NewSkillMarketplaceAggregator(c, logger), Logger: logger}
}

func TestSkillMarketplaceSourceReconciler_SetsStatus(t *testing.T) {
	tenant := marketTenant("acme", true, "acme-corp/internal-skills")
	src := marketSrc("orbit-tenant-acme", "acme-internal", "acme-corp", "internal-skills", marketplaceT0)
	c := newMarketplaceClient(t, &tenant, &src)
	r := newTestMarketplaceReconciler(c)

	key := client.ObjectKeyFromObject(&src)
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	assertMarketplaceStatus(t, getMarketplaceSource(t, c, key),
		SkillMarketplaceSourcePhaseAggregated, MarketplaceReasonAggregated, metav1.ConditionTrue)
	want := marketplace.SourcesFile{SkillsMapping: marketplace.SkillsMapping{Tenants: []marketplace.TenantMarketplaces{
		{ID: "acme", Marketplaces: []string{"https://github.com/acme-corp/internal-skills"}},
	}}}
	if got := readMarketplaceFile(t, c); !reflect.DeepEqual(got, want) {
		t.Errorf("file = %+v, want %+v", got, want)
	}
}

// A source in the system namespace maps to no tenant, so only this reconciler
// can give it a status.
func TestSkillMarketplaceSourceReconciler_GlobalSourceGetsStatus(t *testing.T) {
	src := marketSrc(systemNamespace, "global", "clear-route", "skills", marketplaceT0)
	c := newMarketplaceClient(t, &src)
	r := newTestMarketplaceReconciler(c)

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
	ctx := context.Background()
	tenant := marketTenant("acme", true, "acme-corp/internal-skills")
	src := marketSrc("orbit-tenant-acme", "acme-internal", "acme-corp", "internal-skills", marketplaceT0)
	c := newMarketplaceClient(t, &tenant, &src)
	r := newTestMarketplaceReconciler(c)

	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(&src)}
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatalf("initial Reconcile: %v", err)
	}
	if file := readMarketplaceFile(t, c); len(marketplaceLinks(file)["acme"]) != 1 {
		t.Fatalf("before delete the file has %v, want one source", file.SkillsMapping.Tenants)
	}
	if err := c.Delete(ctx, getMarketplaceSource(t, c, req.NamespacedName)); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if file := readMarketplaceFile(t, c); len(file.SkillsMapping.Tenants) != 0 {
		t.Errorf("file = %v, want no tenants", file.SkillsMapping.Tenants)
	}
}

func TestSkillMarketplaceSourceReconciler_SourceEditRevokesUnapprovedRepository(t *testing.T) {
	ctx := context.Background()
	tenant := marketTenant("acme", true, "acme-corp/internal-skills")
	src := marketSrc("orbit-tenant-acme", "acme-internal", "acme-corp", "internal-skills", marketplaceT0)
	c := newMarketplaceClient(t, &tenant, &src)
	r := newTestMarketplaceReconciler(c)
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(&src)}
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatalf("initial Reconcile: %v", err)
	}

	current := getMarketplaceSource(t, c, req.NamespacedName)
	current.Spec.GitHub.Repo = "unapproved-skills"
	current.Generation++
	if err := c.Update(ctx, current); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatalf("Reconcile after edit: %v", err)
	}
	assertMarketplaceStatus(t, getMarketplaceSource(t, c, req.NamespacedName),
		SkillMarketplaceSourcePhasePending, MarketplaceReasonNotAllowlisted, metav1.ConditionFalse)
	if file := readMarketplaceFile(t, c); len(file.SkillsMapping.Tenants) != 0 {
		t.Errorf("after edit the file has %v, want no tenants", file.SkillsMapping.Tenants)
	}
}

func TestSkillMarketplaceSourceReconciler_SyncFailureReturnsError(t *testing.T) {
	tenant := marketTenant("acme", true, "acme-corp/internal-skills")
	src := marketSrc("orbit-tenant-acme", "acme-internal", "acme-corp", "internal-skills", marketplaceT0)
	c := failingConfigMapWrites{Client: newMarketplaceClient(t, &tenant, &src)}
	r := newTestMarketplaceReconciler(c)
	key := client.ObjectKeyFromObject(&src)
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: key}); !errors.Is(err, errConfigMapRefused) {
		t.Fatalf("Reconcile error = %v, want %v", err, errConfigMapRefused)
	}
	if got := getMarketplaceSource(t, c, key).Status.Phase; got != "" {
		t.Errorf("phase = %q after ConfigMap failure, want unset", got)
	}
}
