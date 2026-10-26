package operator

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

func domainTenant(name, issuer string, created time.Time, binding *ClaimBinding, domains ...string) KoraTenant {
	t := tenantAt(name, "", created)
	t.Spec.OIDC = TenantOIDCSpec{IssuerURL: issuer, ClientID: "kora", EmailDomains: domains, ClaimBinding: binding}
	return t
}

func TestEmailDomainConflict(t *testing.T) {
	t0 := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	eng := &ClaimBinding{Claim: "groups", Values: []string{"engineering"}}
	sre := &ClaimBinding{Claim: "groups", Values: []string{"sre"}}
	plain := domainTenant("acme", "https://idp", t0, nil, "acme.com")
	tests := []struct {
		name     string
		tenant   KoraTenant
		tenants  []KoraTenant
		conflict bool
	}{
		{"unique domain", domainTenant("beta", "https://idp", t0.Add(time.Hour), nil, "beta.com"), []KoraTenant{plain}, false},
		{"same domain unbound", domainTenant("beta", "https://idp", t0.Add(time.Hour), nil, "acme.com"), []KoraTenant{plain}, true},
		{"older tenant keeps the domain", plain, []KoraTenant{domainTenant("beta", "https://idp", t0.Add(time.Hour), nil, "acme.com")}, false},
		{"both bound on one issuer", domainTenant("sre", "https://idp", t0.Add(time.Hour), sre, "acme.com"), []KoraTenant{domainTenant("eng", "https://idp", t0, eng, "acme.com")}, false},
		{"bound on another issuer", domainTenant("sre", "https://other", t0.Add(time.Hour), sre, "acme.com"), []KoraTenant{domainTenant("eng", "https://idp", t0, eng, "acme.com")}, true},
		{"only the newer is bound", domainTenant("sre", "https://idp", t0.Add(time.Hour), sre, "acme.com"), []KoraTenant{plain}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := emailDomainConflict(&tc.tenant, append([]KoraTenant{tc.tenant}, tc.tenants...))
			if (got != "") != tc.conflict {
				t.Errorf("emailDomainConflict = %q, want conflict %v", got, tc.conflict)
			}
		})
	}
}

func TestReconcileMarksEmailDomainConflictInvalid(t *testing.T) {
	t0 := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	owner := domainTenant("acme", "https://idp", t0, nil, "acme.com")
	intruder := domainTenant("beta", "https://idp", t0.Add(time.Hour), nil, "acme.com")
	c := fake.NewClientBuilder().WithScheme(NewScheme()).
		WithObjects(&owner, &intruder).WithStatusSubresource(&owner, &intruder).Build()
	logger := logging.NewLoggerWithOutput(io.Discard)
	r := &TenantReconciler{Client: c, Registry: NewTenantRegistry(c, logger), Logger: logger}

	ctx := context.Background()
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "beta"}}); err != nil {
		t.Fatal(err)
	}
	got := &KoraTenant{}
	if err := c.Get(ctx, client.ObjectKey{Name: "beta"}, got); err != nil {
		t.Fatal(err)
	}
	if got.Status.Phase != TenantPhaseInvalid {
		t.Errorf("phase = %q, want Invalid", got.Status.Phase)
	}
	cond := findCondition(got.Status.Conditions, ConditionEmailDomainsValid)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != "EmailDomainConflict" {
		t.Errorf("EmailDomainsValid = %+v", cond)
	}
}

func TestBuildRegistryYAMLClaimBinding(t *testing.T) {
	t0 := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	eng := domainTenant("eng", "https://idp", t0, &ClaimBinding{Claim: "groups", Values: []string{"engineering"}}, "acme.com")
	sre := domainTenant("sre", "https://idp", t0.Add(time.Hour), &ClaimBinding{Claim: "groups", Values: []string{"sre"}}, "acme.com")
	dup := domainTenant("dup", "https://idp", t0.Add(2*time.Hour), nil, "acme.com")
	r := &TenantRegistry{}
	out, err := r.buildRegistryYAML([]KoraTenant{eng, sre, dup})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"id: eng", "id: sre", "claim: groups", "- engineering", "- sre"} {
		if !strings.Contains(out, want) {
			t.Errorf("registry lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "id: dup") {
		t.Errorf("registry lists the conflicting tenant:\n%s", out)
	}
}
