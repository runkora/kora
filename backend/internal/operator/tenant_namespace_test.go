package operator

import (
	"context"
	"io"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	sigsyaml "sigs.k8s.io/yaml"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

func tenantAt(name, namespace string, created time.Time) KoraTenant {
	return KoraTenant{
		ObjectMeta: metav1.ObjectMeta{Name: name, CreationTimestamp: metav1.NewTime(created)},
		Spec:       KoraTenantSpec{DisplayName: name, Active: true, Namespace: namespace},
	}
}

func TestNamespaceConflict(t *testing.T) {
	t0 := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	older := tenantAt("acme", "shared", t0)
	newer := tenantAt("beta", "shared", t0.Add(time.Hour))
	sameTime := tenantAt("aaa", "shared", t0)
	derived := tenantAt("gamma", "", t0)
	squatter := tenantAt("delta", "kora-tenant-gamma", t0.Add(time.Hour))
	system := tenantAt("sys", systemNamespace, t0)

	all := []KoraTenant{older, newer, derived, squatter, system}
	cases := []struct {
		name     string
		tenant   KoraTenant
		tenants  []KoraTenant
		conflict bool
	}{
		{"older keeps the namespace", older, all, false},
		{"newer is refused", newer, all, true},
		{"same time: lower name wins", older, []KoraTenant{older, sameTime}, true},
		{"derived namespace is owned", derived, all, false},
		{"explicit copy of a derived namespace", squatter, all, true},
		{"system namespace", system, all, true},
		{"alone", newer, []KoraTenant{newer}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := namespaceConflict(&tc.tenant, tc.tenants)
			if (got != "") != tc.conflict {
				t.Errorf("namespaceConflict = %q, want conflict %v", got, tc.conflict)
			}
		})
	}
}

func TestBuildRegistryYAMLDeclarativeFields(t *testing.T) {
	t0 := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	locked := tenantAt("acme", "", t0)
	locked.Spec.APIAuthoring = APIAuthoringDisabled
	locked.Spec.Budget = &TenantBudget{LimitUSD: 1000, ResetCadence: "monthly"}
	refused := tenantAt("beta", "kora-tenant-acme", t0.Add(time.Hour))
	suspended := tenantAt("gamma", "", t0)
	suspended.Spec.Active = false

	r := &TenantRegistry{}
	out, err := r.buildRegistryYAML([]KoraTenant{locked, refused, suspended})
	if err != nil {
		t.Fatal(err)
	}
	var reg registryFile
	if err := sigsyaml.Unmarshal([]byte(out), &reg); err != nil {
		t.Fatal(err)
	}
	if len(reg.Tenants) != 2 {
		t.Fatalf("want 2 active tenants, got %+v", reg.Tenants)
	}
	acme, beta := reg.Tenants[0], reg.Tenants[1]
	if acme.APIAuthoring != APIAuthoringDisabled {
		t.Errorf("acme = %+v", acme)
	}
	if acme.Budget == nil || acme.Budget.LimitUSD != 1000 || acme.Budget.ResetCadence != "monthly" {
		t.Errorf("acme budget = %+v", acme.Budget)
	}
	if beta.APIAuthoring != APIAuthoringEnabled || beta.Budget != nil {
		t.Errorf("beta defaults = %+v", beta)
	}
}

func TestReconcileRefusesNamespaceConflict(t *testing.T) {
	t0 := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	owner := tenantAt("acme", "shared", t0)
	intruder := tenantAt("beta", "shared", t0.Add(time.Hour))
	system := tenantAt("sys", systemNamespace, t0)
	c := fake.NewClientBuilder().WithScheme(NewScheme()).
		WithObjects(&owner, &intruder, &system).
		WithStatusSubresource(&owner, &intruder, &system).Build()
	logger := logging.NewLoggerWithOutput(io.Discard)
	r := &TenantReconciler{Client: c, Registry: NewTenantRegistry(c, logger), Logger: logger}

	for _, name := range []string{"beta", "sys"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: name}})
			if err != nil {
				t.Fatal(err)
			}
			if res.RequeueAfter != time.Minute {
				t.Errorf("RequeueAfter = %v, want 1m", res.RequeueAfter)
			}
			got := &KoraTenant{}
			if err := c.Get(ctx, client.ObjectKey{Name: name}, got); err != nil {
				t.Fatal(err)
			}
			if got.Status.Phase != TenantPhaseDegraded {
				t.Errorf("phase = %q, want Degraded", got.Status.Phase)
			}
			cond := findCondition(got.Status.Conditions, ConditionNamespaceReady)
			if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != "NamespaceConflict" {
				t.Errorf("NamespaceReady = %+v", cond)
			}
			// A refused tenant never touches the namespace.
			if err := c.Get(ctx, client.ObjectKey{Name: got.Spec.Namespace}, &corev1.Namespace{}); err == nil {
				t.Errorf("namespace %q was created", got.Spec.Namespace)
			}
		})
	}

	cm := &corev1.ConfigMap{}
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: systemNamespace, Name: tenantsConfigMapName}, cm); err != nil {
		t.Fatalf("a refused tenant still syncs the registry: %v", err)
	}
	var reg registryFile
	if err := sigsyaml.Unmarshal([]byte(cm.Data[tenantsConfigMapKey]), &reg); err != nil {
		t.Fatal(err)
	}
	listed := map[string]string{}
	for _, e := range reg.Tenants {
		listed[e.ID] = e.APIAuthoring
	}
	for _, id := range []string{"acme", "beta", "sys"} {
		if listed[id] != APIAuthoringEnabled {
			t.Errorf("registry entry for %s = %q, want listed with apiAuthoring Enabled", id, listed[id])
		}
	}
}

func findCondition(conds []metav1.Condition, condType string) *metav1.Condition {
	for i := range conds {
		if conds[i].Type == condType {
			return &conds[i]
		}
	}
	return nil
}

func TestKoraTenantSpecDeepCopy(t *testing.T) {
	in := KoraTenantSpec{
		OIDC:   TenantOIDCSpec{Scopes: []string{"openid"}, EmailDomains: []string{"acme.com"}, ClientSecretRef: &SecretRef{Name: "s"}},
		Budget: &TenantBudget{LimitUSD: 10},
	}
	var out KoraTenantSpec
	in.DeepCopyInto(&out)
	out.OIDC.Scopes[0] = "x"
	out.OIDC.EmailDomains[0] = "x"
	out.OIDC.ClientSecretRef.Name = "x"
	out.Budget.LimitUSD = 99
	if in.OIDC.Scopes[0] != "openid" || in.OIDC.EmailDomains[0] != "acme.com" || in.OIDC.ClientSecretRef.Name != "s" || in.Budget.LimitUSD != 10 {
		t.Errorf("DeepCopyInto shares state: %+v", in)
	}
}

func TestServedTenants(t *testing.T) {
	t0 := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	owner := tenantAt("acme", "shared", t0)
	refused := tenantAt("beta", "shared", t0.Add(time.Hour))
	suspended := tenantAt("gamma", "", t0)
	suspended.Spec.Active = false
	got := servedTenants([]KoraTenant{refused, owner, suspended})
	if len(got) != 1 || got[0].Name != "acme" {
		t.Errorf("servedTenants = %v, want only acme", got)
	}
}
