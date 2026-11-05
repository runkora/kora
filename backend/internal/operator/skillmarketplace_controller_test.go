package operator

import (
	"context"
	"errors"
	"io"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

func newTestMarketplaceReconciler(c client.Client) *SkillMarketplaceReconciler {
	logger := logging.NewLoggerWithOutput(io.Discard)
	return &SkillMarketplaceReconciler{Client: c, Aggregator: NewSkillMarketplaceAggregator(c, logger), Logger: logger}
}

func TestSkillMarketplaceReconciler_SetsStatusAndRequeues(t *testing.T) {
	tenant := marketTenant("example", true)
	m := marketSrc(exampleNS, "team-skills", "example-org", "skills", marketplaceT0)
	c := newMarketplaceClient(t, &tenant, &m, appKeySecret(t, exampleNS))

	key := client.ObjectKeyFromObject(&m)
	res, err := newTestMarketplaceReconciler(c).Reconcile(context.Background(), ctrl.Request{NamespacedName: key})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if res.RequeueAfter != credentialRecheckInterval {
		t.Errorf("RequeueAfter = %s, want %s so a Secret change reaches the status", res.RequeueAfter, credentialRecheckInterval)
	}
	assertMarketplaceStatus(t, getMarketplace(t, c, key), SkillMarketplacePhaseAggregated, MarketplaceReasonAggregated, metav1.ConditionTrue)
}

// A Marketplace in the system namespace maps to no tenant, so only this
// reconciler can give it a status.
func TestSkillMarketplaceReconciler_GlobalMarketplaceGetsStatus(t *testing.T) {
	m := marketSrc(systemNamespace, "global", "example-org", "skills", marketplaceT0)
	c := newMarketplaceClient(t, &m)
	key := client.ObjectKeyFromObject(&m)
	if _, err := newTestMarketplaceReconciler(c).Reconcile(context.Background(), ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	assertMarketplaceStatus(t, getMarketplace(t, c, key), SkillMarketplacePhaseInvalid, MarketplaceReasonGlobalSourcesNotSupported, metav1.ConditionFalse)
}

// A delete event names an object that no longer exists. The run still drops it
// from both objects and does not requeue.
func TestSkillMarketplaceReconciler_DeletedMarketplaceStillSyncs(t *testing.T) {
	ctx := context.Background()
	tenant := marketTenant("example", true)
	m := marketSrc(exampleNS, "team-skills", "example-org", "skills", marketplaceT0)
	c := newMarketplaceClient(t, &tenant, &m, appKeySecret(t, exampleNS))
	r := newTestMarketplaceReconciler(c)
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(&m)}
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(ctx, getMarketplace(t, c, req.NamespacedName)); err != nil {
		t.Fatal(err)
	}

	res, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if res.RequeueAfter != 0 {
		t.Errorf("RequeueAfter = %s, want none for a deleted Marketplace", res.RequeueAfter)
	}
	if file := readMarketplaceFile(t, c); len(file.Tenants) != 0 {
		t.Errorf("file = %+v, want no tenants", file)
	}
	if got := readMarketplaceSecret(t, c); len(got) != 0 {
		t.Errorf("Secret holds %d keys, want none", len(got))
	}
}

func TestSkillMarketplaceReconciler_SyncFailureReturnsError(t *testing.T) {
	tenant := marketTenant("example", true)
	m := marketSrc(exampleNS, "team-skills", "example-org", "skills", marketplaceT0)
	c := failingConfigMapWrites{Client: newMarketplaceClient(t, &tenant, &m)}
	key := client.ObjectKeyFromObject(&m)
	if _, err := newTestMarketplaceReconciler(c).Reconcile(context.Background(), ctrl.Request{NamespacedName: key}); !errors.Is(err, errConfigMapRefused) {
		t.Fatalf("Reconcile = %v, want the ConfigMap error", err)
	}
}
