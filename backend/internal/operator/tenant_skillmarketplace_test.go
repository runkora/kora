package operator

import (
	"context"
	"io"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// Tenant changes must re-evaluate sources even without a source event.
func TestTenantReconciler_MarketplaceSourcesFollowTenantChanges(t *testing.T) {
	ctx := context.Background()
	tenant := marketTenant("acme", true)
	src := marketSrc("orbit-tenant-acme", "acme-internal", "Acme Internal Skills", "acme-corp", "internal-skills", marketplaceT0)
	otherTenant := marketTenant("other", true, "other-corp/skills")
	otherSrc := marketSrc("orbit-tenant-other", "other", "Other Skills", "other-corp", "skills", marketplaceT0)
	scheme := NewScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(&tenant, &src, &otherTenant, &otherSrc).
		WithStatusSubresource(&OrbitTenant{}, &SkillMarketplaceSource{}).
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
		allowRepo  bool
		delete     bool
		wantReason string
	}{
		{name: "before approval", active: true, wantReason: MarketplaceReasonNotAllowlisted},
		{name: "allowlist edit admits source", active: true, allowRepo: true, wantReason: MarketplaceReasonAggregated},
		{name: "allowlist removal revokes source", active: true, wantReason: MarketplaceReasonNotAllowlisted},
		{name: "restoring approval admits source", active: true, allowRepo: true, wantReason: MarketplaceReasonAggregated},
		{name: "suspension removes source", allowRepo: true, wantReason: MarketplaceReasonTenantNotActive},
		{name: "reactivation restores source", active: true, allowRepo: true, wantReason: MarketplaceReasonAggregated},
		{name: "deletion removes source", delete: true, wantReason: MarketplaceReasonTenantNotActive},
	}
	for _, step := range steps {
		if !t.Run(step.name, func(t *testing.T) {
			current := &OrbitTenant{}
			if err := c.Get(ctx, key, current); err != nil {
				t.Fatal(err)
			}
			if step.delete {
				if err := c.Delete(ctx, current); err != nil {
					t.Fatal(err)
				}
			} else {
				current.Spec.Active = step.active
				current.Spec.AllowedMarketplaceRepos = nil
				if step.allowRepo {
					current.Spec.AllowedMarketplaceRepos = []string{"acme-corp/internal-skills"}
				}
				current.Generation++
				if err := c.Update(ctx, current); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
				t.Fatalf("Reconcile: %v", err)
			}

			wantPhase := SkillMarketplaceSourcePhasePending
			wantStatus := metav1.ConditionFalse
			wantSources := 0
			if step.wantReason == MarketplaceReasonAggregated {
				wantPhase = SkillMarketplaceSourcePhaseAggregated
				wantStatus = metav1.ConditionTrue
				wantSources = 1
			}
			assertMarketplaceStatus(t, getMarketplaceSource(t, c, client.ObjectKeyFromObject(&src)),
				wantPhase, step.wantReason, wantStatus)
			file := readMarketplaceFile(t, c)
			if got := len(file.Tenants["acme"].Sources); got != wantSources {
				t.Errorf("published acme sources = %d, want %d", got, wantSources)
			}
			if got := len(file.Tenants["other"].Sources); got != 1 {
				t.Errorf("published other sources = %d, want 1", got)
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
