package operator

import (
	"context"
	"io"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// Tenant changes must re-evaluate sources even without a source event.
func TestTenantReconciler_MarketplacesFollowTenantChanges(t *testing.T) {
	ctx := context.Background()
	tenant := marketTenant("example", true)
	src := marketSrc(exampleNS, "team-skills", "example-org", "skills", marketplaceT0)
	otherTenant := marketTenant("other", true)
	otherSrc := marketSrc("kora-tenant-other", "other", "other-org", "skills", marketplaceT0)
	scheme := NewScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(&tenant, &src, &otherTenant, &otherSrc, appKeySecret(t, exampleNS), appKeySecret(t, "kora-tenant-other")).
		WithStatusSubresource(&KoraTenant{}, &SkillMarketplace{}).
		Build()
	logger := logging.NewLoggerWithOutput(io.Discard)
	r := &TenantReconciler{
		Client:                c,
		Scheme:                scheme,
		Registry:              NewTenantRegistry(c, logger),
		Aggregator:            NewClientSecretAggregator(c, logger),
		CAAggregator:          NewTenantCAAggregator(c, logger),
		ConnAggregator:        NewConnectionAggregator(c, logger),
		MCPAggregator:         NewMCPServerAggregator(c, logger),
		OAuthAggregator:       NewOAuthClientAggregator(c, logger),
		MarketplaceAggregator: NewSkillMarketplaceAggregator(c, logger),
		Logger:                logger,
	}
	key := client.ObjectKeyFromObject(&tenant)
	steps := []struct {
		name       string
		active     bool
		delete     bool
		wantReason string
	}{
		{name: "active tenant serves source", active: true, wantReason: MarketplaceReasonAggregated},
		{name: "suspension removes source", wantReason: MarketplaceReasonTenantNotActive},
		{name: "reactivation restores source", active: true, wantReason: MarketplaceReasonAggregated},
		{name: "deletion removes source", delete: true, wantReason: MarketplaceReasonTenantNotActive},
	}
	for _, step := range steps {
		if !t.Run(step.name, func(t *testing.T) {
			current := &KoraTenant{}
			if err := c.Get(ctx, key, current); err != nil {
				t.Fatal(err)
			}
			if step.delete {
				if err := c.Delete(ctx, current); err != nil {
					t.Fatal(err)
				}
			} else {
				current.Spec.Active = step.active
				current.Generation++
				if err := c.Update(ctx, current); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
				t.Fatalf("Reconcile: %v", err)
			}

			wantPhase := SkillMarketplacePhasePending
			wantStatus := metav1.ConditionFalse
			wantSources := 0
			if step.wantReason == MarketplaceReasonAggregated {
				wantPhase = SkillMarketplacePhaseAggregated
				wantStatus = metav1.ConditionTrue
				wantSources = 1
			}
			assertMarketplaceStatus(t, getMarketplace(t, c, client.ObjectKeyFromObject(&src)),
				wantPhase, step.wantReason, wantStatus)
			links := slugs(readMarketplaceFile(t, c))
			if got := len(links["example"]); got != wantSources {
				t.Errorf("published example Marketplaces = %d, want %d", got, wantSources)
			}
			if got := len(links["other"]); got != 1 {
				t.Errorf("published other Marketplaces = %d, want 1", got)
			}
			if !step.delete {
				if err := c.Get(ctx, key, current); err != nil {
					t.Fatal(err)
				}
				wantTenantPhase := TenantPhaseReady
				if !step.active {
					wantTenantPhase = TenantPhaseSuspended
				}
				if current.Status.Phase != wantTenantPhase {
					t.Errorf("tenant phase = %q, want %q", current.Status.Phase, wantTenantPhase)
				}
			}
		}) {
			return
		}
	}
}
