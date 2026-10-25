package operator

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/runkora/kora/backend/internal/platform/declarative"
	dv1 "github.com/runkora/kora/backend/internal/platform/declarative/v1alpha1"
	"github.com/runkora/kora/backend/internal/platform/logging"
	si "github.com/runkora/kora/backend/internal/platform/sandboximages/v1alpha1"
)

// fakeDeclared records calls and answers with result, or Projected and ready.
type fakeDeclared struct {
	mu       sync.Mutex
	failDel  bool
	failList map[string]bool // by tenant
	applied  []declarative.Item
	deleted  []string
	declared map[string][]declarative.Declared // by tenant
	result   *declarative.Result
}

func (f *fakeDeclared) Apply(_ context.Context, _ string, item declarative.Item) (declarative.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applied = append(f.applied, item)
	if f.result != nil {
		return *f.result, nil
	}
	return declarative.Result{Outcome: declarative.OutcomeProjected, ID: "id-" + item.Name, Ready: true}, nil
}

func (f *fakeDeclared) Delete(_ context.Context, _, tenantID, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failDel {
		return errors.New("in use")
	}
	f.deleted = append(f.deleted, tenantID+"/"+name)
	return nil
}

func (f *fakeDeclared) List(_ context.Context, _, tenantID string) ([]declarative.Declared, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failList[tenantID] {
		return nil, errors.New("unavailable")
	}
	return f.declared[tenantID], nil
}

func (f *fakeDeclared) last(t *testing.T) declarative.Item {
	t.Helper()
	if len(f.applied) == 0 {
		t.Fatal("nothing was applied")
	}
	return f.applied[len(f.applied)-1]
}

var t0 = time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)

func meta0(namespace, name string, age int) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Namespace: namespace, Name: name, Generation: 1,
		UID:               types.UID(namespace + "/" + name),
		CreationTimestamp: metav1.NewTime(t0.Add(time.Duration(age) * time.Minute)),
	}
}

// readyStatus marks a declarative resource Ready with id.
func readyStatus(id string) dv1.Status {
	return dv1.Status{ID: id, Conditions: []metav1.Condition{{
		Type: condReady, Status: metav1.ConditionTrue, Reason: reasonProjected, LastTransitionTime: metav1.NewTime(t0),
	}}}
}

type harness struct {
	c       client.Client
	clients map[string]*fakeDeclared
	recs    map[string]*DeclaredReconciler
}

// newHarness starts with tenant acme in kora-tenant-acme and its namespace.
func newHarness(t *testing.T, objs ...client.Object) *harness {
	t.Helper()
	acme := tenantAt("acme", "", t0)
	base := []client.Object{
		&acme,
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kora-tenant-acme"}},
	}
	statusTypes := []client.Object{&dv1.VirtualModel{}, &dv1.Environment{}, &dv1.Agent{}, &dv1.TriggerSource{}, &dv1.Trigger{}, &dv1.PolicyRole{}}
	c := fake.NewClientBuilder().WithScheme(NewScheme()).WithObjects(append(base, objs...)...).WithStatusSubresource(statusTypes...).Build()
	h := &harness{c: c, clients: map[string]*fakeDeclared{}, recs: map[string]*DeclaredReconciler{}}
	for _, svc := range []string{"agents", "triggers", "policy", "inference"} {
		h.clients[svc] = &fakeDeclared{declared: map[string][]declarative.Declared{}}
	}
	recs := NewDeclaredReconcilers(c, c, DeclaredClients{
		Agents: h.clients["agents"], Triggers: h.clients["triggers"],
		PolicyService: h.clients["policy"], InferenceGateway: h.clients["inference"],
	}, logging.NewLoggerWithOutput(io.Discard))
	for _, r := range recs {
		h.recs[r.kind.kind] = r
	}
	return h
}

func (h *harness) reconcile(t *testing.T, kind string, obj client.Object) ctrl.Result {
	t.Helper()
	res, err := h.recs[kind].Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(obj)})
	if err != nil {
		t.Fatalf("reconcile %s: %v", kind, err)
	}
	if err := h.c.Get(context.Background(), client.ObjectKeyFromObject(obj), obj); err != nil && !isNotFound(err) {
		t.Fatal(err)
	}
	return res
}

func isNotFound(err error) bool { return client.IgnoreNotFound(err) == nil }

func declaredCondition(t *testing.T, obj client.Object, condType string) metav1.Condition {
	t.Helper()
	c := meta.FindStatusCondition(declaredStatus(obj).Conditions, condType)
	if c == nil {
		t.Fatalf("no %s condition", condType)
	}
	return *c
}

func TestProjectEnvironment(t *testing.T) {
	env := &dv1.Environment{ObjectMeta: meta0("kora-tenant-acme", "go-build", 0), Spec: dv1.EnvironmentSpec{DisplayName: "Go", SandboxImage: "kora-go"}}
	img := &si.SandboxImage{ObjectMeta: metav1.ObjectMeta{Namespace: "kora-tenant-acme", Name: "kora-go"}}
	h := newHarness(t, env, img)

	res := h.reconcile(t, declarative.KindEnvironments, env)
	item := h.clients["agents"].last(t)
	if item.TenantID != "acme" || item.Name != "go-build" || item.UID != "kora-tenant-acme/go-build" || item.Generation != 1 {
		t.Errorf("item = %+v", item)
	}
	var spec dv1.EnvironmentSpec
	if err := json.Unmarshal(item.Spec, &spec); err != nil || spec.SandboxImage != "kora-go" {
		t.Errorf("spec = %s (%v)", item.Spec, err)
	}
	if env.Status.ID != "id-go-build" || declaredCondition(t, env, condReady).Status != metav1.ConditionTrue {
		t.Errorf("status = %+v", env.Status)
	}
	if res.RequeueAfter != declaredResync {
		t.Errorf("a ready resource resyncs, got %v", res.RequeueAfter)
	}
}

func TestUnknownTenant(t *testing.T) {
	env := &dv1.Environment{ObjectMeta: meta0("nowhere", "e", 0), Spec: dv1.EnvironmentSpec{DisplayName: "E"}}
	h := newHarness(t, env)
	res := h.reconcile(t, declarative.KindEnvironments, env)
	if c := declaredCondition(t, env, condAccepted); c.Reason != reasonUnknownTenant {
		t.Errorf("Accepted = %+v", c)
	}
	if len(h.clients["agents"].applied) != 0 || res.RequeueAfter != declaredRetry {
		t.Errorf("applied %d, requeue %v", len(h.clients["agents"].applied), res.RequeueAfter)
	}
}

func TestAgentReferences(t *testing.T) {
	vm := &dv1.VirtualModel{ObjectMeta: meta0(systemNamespace, "sonnet", 0), Spec: dv1.VirtualModelSpec{Family: "anthropic", Name: "sonnet"}, Status: readyStatus("vm1")}
	env := &dv1.Environment{ObjectMeta: meta0("kora-tenant-acme", "go-build", 0)}
	globalMCP := &MCPServer{ObjectMeta: metav1.ObjectMeta{Namespace: systemNamespace, Name: "github"}}
	conn := &Connection{ObjectMeta: metav1.ObjectMeta{Namespace: "kora-tenant-acme", Name: "gh"}}
	agent := &dv1.Agent{ObjectMeta: meta0("kora-tenant-acme", "reviewer", 0), Spec: dv1.AgentSpec{
		DisplayName:  "R",
		Model:        dv1.ModelRef{Family: "anthropic", Name: "sonnet"},
		Environments: []dv1.AgentEnvironment{{Name: "go-build", Default: true}},
		Connectors:   []dv1.Connector{{Connection: "gh", Actions: []string{"read"}}},
		MCPTools:     []dv1.MCPTools{{Server: "github", Tools: []string{"t"}}},
	}}
	h := newHarness(t, vm, env, globalMCP, conn, agent)

	h.reconcile(t, declarative.KindAgents, agent)
	if c := declaredCondition(t, agent, condResolvedRefs); c.Reason != reasonRefNotFound || c.Message != `Environment "go-build" not found or not Ready.` {
		t.Errorf("ResolvedRefs = %+v", c)
	}
	if len(h.clients["agents"].applied) != 0 {
		t.Fatal("an agent with an unready environment was applied")
	}

	env.Status = readyStatus("env1")
	if err := h.c.Status().Update(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	h.reconcile(t, declarative.KindAgents, agent)
	if got := h.clients["agents"].last(t).Refs; got["environment/go-build"] != "env1" {
		t.Errorf("refs = %v", got)
	}
}

func TestTriggerReferences(t *testing.T) {
	ns := "kora-tenant-acme"
	vm := &dv1.VirtualModel{ObjectMeta: meta0(systemNamespace, "d1", 0), Spec: dv1.VirtualModelSpec{Family: "liquid", Name: "d1"}, Status: readyStatus("vm")}
	src := &dv1.TriggerSource{ObjectMeta: meta0(ns, "gh", 0), Status: dv1.TriggerSourceStatus{Status: readyStatus("s1")}}
	a := &dv1.Agent{ObjectMeta: meta0(ns, "a", 0), Status: readyStatus("a1")}
	b := &dv1.Agent{ObjectMeta: meta0(ns, "b", 0), Status: readyStatus("b1")}
	trig := &dv1.Trigger{ObjectMeta: meta0(ns, "t", 0), Spec: dv1.TriggerSpec{
		Kind: "webhook", Source: "gh", EventType: "push", PromptTemplate: "p",
		Routing: &dv1.Routing{Candidates: []string{"a"}, Fallback: "b", Model: dv1.ModelRef{Family: "liquid", Name: "d1"}},
	}}
	h := newHarness(t, vm, src, a, b, trig)
	h.reconcile(t, declarative.KindTriggers, trig)
	want := map[string]string{"source/gh": "s1", "agent/a": "a1", "agent/b": "b1"}
	got := h.clients["triggers"].last(t).Refs
	for k, v := range want {
		if got[k] != v {
			t.Errorf("refs = %v, want %v", got, want)
		}
	}

	missing := &dv1.Trigger{ObjectMeta: meta0(ns, "t2", 0), Spec: dv1.TriggerSpec{Kind: "manual", Agent: "ghost", PromptTemplate: "p"}}
	if err := h.c.Create(context.Background(), missing); err != nil {
		t.Fatal(err)
	}
	h.reconcile(t, declarative.KindTriggers, missing)
	if c := declaredCondition(t, missing, condResolvedRefs); c.Message != `Agent "ghost" not found or not Ready.` {
		t.Errorf("ResolvedRefs = %+v", c)
	}
}

func TestVirtualModelRules(t *testing.T) {
	older := &dv1.VirtualModel{ObjectMeta: meta0(systemNamespace, "a", 0), Spec: dv1.VirtualModelSpec{Family: "f", Name: "n"}}
	newer := &dv1.VirtualModel{ObjectMeta: meta0(systemNamespace, "b", 5), Spec: dv1.VirtualModelSpec{Family: "f", Name: "n"}}
	h := newHarness(t, older, newer)

	h.reconcile(t, declarative.KindVirtualModels, older)
	if item := h.clients["inference"].last(t); item.TenantID != declarative.PlatformTenant || item.Name != "a" {
		t.Errorf("item = %+v", item)
	}
	h.reconcile(t, declarative.KindVirtualModels, newer)
	if c := declaredCondition(t, newer, condAccepted); c.Reason != reasonNameConflict {
		t.Errorf("newer Accepted = %+v", c)
	}
	if n := len(h.clients["inference"].applied); n != 1 {
		t.Errorf("applied %d, want only the older model", n)
	}
}

func TestPolicyRoleRules(t *testing.T) {
	ns := "kora-tenant-acme"
	role := func(namespace, name string, age int, spec dv1.PolicyRoleSpec) *dv1.PolicyRole {
		return &dv1.PolicyRole{ObjectMeta: meta0(namespace, name, age), Spec: spec}
	}
	cases := []struct {
		name   string
		role   *dv1.PolicyRole
		reason string
	}{
		{"platform scope in a tenant namespace", role(ns, "p", 0, dv1.PolicyRoleSpec{Scopes: []string{"platform:catalog:write"}}), reasonInvalid},
		{"tenant in a tenant namespace", role(ns, "t", 0, dv1.PolicyRoleSpec{Tenant: "acme", Scopes: []string{"a"}}), reasonInvalid},
		{"system role without a tenant", role(systemNamespace, "s", 0, dv1.PolicyRoleSpec{Scopes: []string{"a"}}), reasonInvalid},
		{"system role for an unknown tenant", role(systemNamespace, "u", 0, dv1.PolicyRoleSpec{Tenant: "ghost", Scopes: []string{"a"}}), reasonUnknownTenant},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, tc.role)
			h.reconcile(t, declarative.KindPolicyRoles, tc.role)
			if c := declaredCondition(t, tc.role, condAccepted); c.Reason != tc.reason {
				t.Errorf("Accepted = %+v, want %s", c, tc.reason)
			}
			if len(h.clients["policy"].applied) != 0 {
				t.Error("a refused role was applied")
			}
		})
	}

	t.Run("system role with platform scopes, and older wins across namespaces", func(t *testing.T) {
		sys := role(systemNamespace, "admins", 0, dv1.PolicyRoleSpec{Tenant: "acme", Role: "Admins", Scopes: []string{"platform:catalog:write"}})
		dup := role(ns, "admins-dup", 5, dv1.PolicyRoleSpec{Role: "Admins", Scopes: []string{"a"}})
		plain := role(ns, "reviewers", 0, dv1.PolicyRoleSpec{Scopes: []string{"a"}})
		h := newHarness(t, sys, dup, plain)

		h.reconcile(t, declarative.KindPolicyRoles, sys)
		if item := h.clients["policy"].last(t); item.TenantID != "acme" {
			t.Errorf("item = %+v", item)
		}
		h.reconcile(t, declarative.KindPolicyRoles, dup)
		if c := declaredCondition(t, dup, condAccepted); c.Reason != reasonNameConflict {
			t.Errorf("dup Accepted = %+v", c)
		}
		h.reconcile(t, declarative.KindPolicyRoles, plain)
		var spec dv1.PolicyRoleSpec
		if err := json.Unmarshal(h.clients["policy"].last(t).Spec, &spec); err != nil || spec.Role != "reviewers" {
			t.Errorf("spec.role defaults to metadata.name, got %+v (%v)", spec, err)
		}
	})
}

func TestTriggerSourceSecret(t *testing.T) {
	ns := "kora-tenant-acme"
	src := &dv1.TriggerSource{ObjectMeta: meta0(ns, "gh", 0), Spec: dv1.TriggerSourceSpec{Provider: "github", SigningSecretRef: dv1.SecretKeyRef{Name: "hook", Key: "secret"}}}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "hook"}, Data: map[string][]byte{"secret": []byte("s3cret")}}
	h := newHarness(t, src, secret)

	h.reconcile(t, declarative.KindTriggerSources, src)
	if c := declaredCondition(t, src, condResolvedRefs); c.Reason != reasonRefNotFound {
		t.Errorf("an unlabelled Secret resolves, got %+v", c)
	}
	if len(h.clients["triggers"].applied) != 0 {
		t.Fatal("applied without a labelled Secret")
	}

	secret.Labels = map[string]string{TriggerSourceSecretLabel: "true"}
	if err := h.c.Update(context.Background(), secret); err != nil {
		t.Fatal(err)
	}
	h.reconcile(t, declarative.KindTriggerSources, src)
	if item := h.clients["triggers"].last(t); string(item.Secret) != "s3cret" {
		t.Errorf("secret = %q", item.Secret)
	}
	if src.Status.WebhookPath != "/api/v1/webhooks/id-gh" || src.Status.ID != "id-gh" {
		t.Errorf("status = %+v", src.Status)
	}
}

func TestServiceOutcomes(t *testing.T) {
	cases := []struct {
		res            declarative.Result
		condType       string
		reason         string
		wantRequeue    time.Duration
		wantReadyTrue  bool
		wantStatusIDOK bool
	}{
		{declarative.Result{Outcome: declarative.OutcomeNameConflict, Message: "taken"}, condAccepted, reasonNameConflict, declaredRetry, false, false},
		{declarative.Result{Outcome: declarative.OutcomeInvalid, Message: "bad"}, condAccepted, reasonInvalid, declaredRetry, false, false},
		{declarative.Result{Outcome: declarative.OutcomePending, Message: "scope x unknown"}, condResolvedRefs, reasonPending, declaredRetry, false, false},
		{declarative.Result{Outcome: declarative.OutcomeProjected, ID: "x"}, condReady, reasonPending, declaredRetry, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.res.Outcome, func(t *testing.T) {
			env := &dv1.Environment{ObjectMeta: meta0("kora-tenant-acme", "e", 0)}
			h := newHarness(t, env)
			h.clients["agents"].result = &tc.res
			res := h.reconcile(t, declarative.KindEnvironments, env)
			if c := declaredCondition(t, env, tc.condType); c.Status != metav1.ConditionFalse || c.Reason != tc.reason || (tc.res.Message != "" && c.Message != tc.res.Message) {
				t.Errorf("%s = %+v", tc.condType, c)
			}
			if res.RequeueAfter != tc.wantRequeue {
				t.Errorf("requeue = %v", res.RequeueAfter)
			}
			if (env.Status.ID != "") != tc.wantStatusIDOK {
				t.Errorf("status.id = %q", env.Status.ID)
			}
		})
	}
}

func TestDeletionSweepsItemsWhoseResourceIsGone(t *testing.T) {
	live := &dv1.Agent{ObjectMeta: meta0("kora-tenant-acme", "live", 0)}
	h := newHarness(t, live)
	h.clients["agents"].declared["acme"] = []declarative.Declared{
		{Name: "live", UID: string(live.UID)},
		{Name: "gone", UID: "kora-tenant-acme/gone"},
	}
	gone := &dv1.Agent{ObjectMeta: meta0("kora-tenant-acme", "gone", 0)}
	h.reconcile(t, declarative.KindAgents, gone)
	if got := h.clients["agents"].deleted; !slices.Equal(got, []string{"acme/gone"}) {
		t.Errorf("deleted %v", got)
	}
}

func TestDeletingASystemRoleSweepsEveryTenant(t *testing.T) {
	beta := tenantAt("beta", "", t0)
	h := newHarness(t, &beta, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kora-tenant-beta"}})
	h.clients["policy"].declared["acme"] = []declarative.Declared{{Name: "admins", UID: "kora-system/admins"}}
	h.clients["policy"].declared["beta"] = []declarative.Declared{{Name: "ops", UID: "kora-system/ops"}}
	h.reconcile(t, declarative.KindPolicyRoles, &dv1.PolicyRole{ObjectMeta: meta0(systemNamespace, "admins", 0)})
	got := h.clients["policy"].deleted
	slices.Sort(got)
	if !slices.Equal(got, []string{"acme/admins", "beta/ops"}) {
		t.Errorf("deleted %v", got)
	}
}

func TestSweepSkipsATenantWhoseNamespaceIsGone(t *testing.T) {
	beta := tenantAt("beta", "", t0)
	h := newHarness(t, &beta)
	h.clients["agents"].declared["beta"] = []declarative.Declared{{Name: "a", UID: "x"}}
	h.clients["agents"].declared["acme"] = []declarative.Declared{{Name: "b", UID: "y"}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := SweepAll(ctx, []*DeclaredReconciler{h.recs[declarative.KindAgents]}, time.Hour, logging.NewLoggerWithOutput(io.Discard)); err != nil {
		t.Fatal(err)
	}
	if got := h.clients["agents"].deleted; !slices.Equal(got, []string{"acme/b"}) {
		t.Errorf("deleted %v, want only acme's item", got)
	}
}

func TestDependentWatches(t *testing.T) {
	ns := "kora-tenant-acme"
	a := &dv1.Agent{ObjectMeta: meta0(ns, "a", 0)}
	routed := &dv1.Trigger{ObjectMeta: meta0(ns, "t", 0), Spec: dv1.TriggerSpec{Routing: &dv1.Routing{Model: dv1.ModelRef{Family: "f", Name: "n"}}}}
	other := &dv1.Trigger{ObjectMeta: meta0(ns, "o", 0), Spec: dv1.TriggerSpec{Routing: &dv1.Routing{Model: dv1.ModelRef{Family: "g", Name: "n"}}}}
	h := newHarness(t, a, routed, other)
	vm := &dv1.VirtualModel{ObjectMeta: meta0(systemNamespace, "m", 0), Spec: dv1.VirtualModelSpec{Family: "f", Name: "n"}}

	reqs := h.recs[declarative.KindTriggers].enqueueModelUsers(context.Background(), vm)
	if len(reqs) != 1 || reqs[0].Name != "t" {
		t.Errorf("model users = %v", reqs)
	}
	reqs = h.recs[declarative.KindTriggers].enqueueNamespace(&dv1.TriggerList{})(context.Background(), a)
	if len(reqs) != 2 {
		t.Errorf("namespace dependents = %v", reqs)
	}
}

func TestRefusedDeleteIsRetried(t *testing.T) {
	h := newHarness(t)
	h.clients["agents"].declared["acme"] = []declarative.Declared{{Name: "go-build", UID: "gone"}}
	h.clients["agents"].failDel = true
	res, err := h.recs[declarative.KindEnvironments].Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "kora-tenant-acme", Name: "go-build"}})
	if err != nil || res.RequeueAfter != declaredRetry {
		t.Errorf("result %+v, err %v, want a retry in %v", res, err, declaredRetry)
	}
}

func TestPolicyRoleNameIsUniquePerTenant(t *testing.T) {
	sys := &dv1.PolicyRole{ObjectMeta: meta0(systemNamespace, "admins", 0), Spec: dv1.PolicyRoleSpec{Tenant: "acme", Role: "Platform", Scopes: []string{"a"}}}
	same := &dv1.PolicyRole{ObjectMeta: meta0("kora-tenant-acme", "admins", 5), Spec: dv1.PolicyRoleSpec{Role: "Other", Scopes: []string{"a"}}}
	h := newHarness(t, sys, same)
	h.reconcile(t, declarative.KindPolicyRoles, same)
	if c := declaredCondition(t, same, condAccepted); c.Reason != reasonNameConflict {
		t.Errorf("Accepted = %+v", c)
	}
	if len(h.clients["policy"].applied) != 0 {
		t.Error("the newer role was applied")
	}
}

func TestTriggerRefusesDelegatedAgents(t *testing.T) {
	ns := "kora-tenant-acme"
	auto := &dv1.Agent{ObjectMeta: meta0(ns, "auto", 0), Spec: dv1.AgentSpec{IdentityMode: "autonomous"}, Status: readyStatus("a1")}
	del := &dv1.Agent{ObjectMeta: meta0(ns, "del", 0), Spec: dv1.AgentSpec{IdentityMode: "delegated"}, Status: readyStatus("d1")}
	for name, spec := range map[string]dv1.TriggerSpec{
		"agent":     {Kind: "manual", Agent: "del", PromptTemplate: "p"},
		"candidate": {Kind: "webhook", Source: "s", EventType: "e", PromptTemplate: "p", Routing: &dv1.Routing{Candidates: []string{"auto", "del"}}},
		"fallback":  {Kind: "webhook", Source: "s", EventType: "e", PromptTemplate: "p", Routing: &dv1.Routing{Candidates: []string{"auto"}, Fallback: "del"}},
	} {
		t.Run(name, func(t *testing.T) {
			trig := &dv1.Trigger{ObjectMeta: meta0(ns, "t", 0), Spec: spec}
			h := newHarness(t, auto.DeepCopy(), del.DeepCopy(), trig)
			h.reconcile(t, declarative.KindTriggers, trig)
			if c := declaredCondition(t, trig, condAccepted); c.Reason != reasonInvalid {
				t.Errorf("Accepted = %+v", c)
			}
			if len(h.clients["triggers"].applied) != 0 {
				t.Error("a trigger bound to a delegated agent was applied")
			}
		})
	}
}

func TestSystemRoleTenantMoveSweepsTheOldTenant(t *testing.T) {
	beta := tenantAt("beta", "", t0)
	role := &dv1.PolicyRole{ObjectMeta: meta0(systemNamespace, "admins", 0), Spec: dv1.PolicyRoleSpec{Tenant: "beta", Scopes: []string{"platform:catalog:write"}}}
	h := newHarness(t, &beta, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kora-tenant-beta"}}, role)
	h.clients["policy"].declared["acme"] = []declarative.Declared{{Name: "admins", UID: string(role.UID)}}
	h.clients["policy"].declared["beta"] = []declarative.Declared{{Name: "admins", UID: string(role.UID)}}
	h.reconcile(t, declarative.KindPolicyRoles, role)
	if got := h.clients["policy"].deleted; !slices.Equal(got, []string{"acme/admins"}) {
		t.Errorf("deleted %v, want only the tenant the role left", got)
	}
}

func TestSweepKeepsAnItemRecreatedUnderTheSameName(t *testing.T) {
	recreated := &dv1.Agent{ObjectMeta: meta0("kora-tenant-acme", "a", 0)}
	recreated.UID = "uid-2"
	h := newHarness(t, recreated)
	h.clients["agents"].declared["acme"] = []declarative.Declared{{Name: "a", UID: "uid-1"}}
	gone := &dv1.Agent{ObjectMeta: meta0("kora-tenant-acme", "other", 0)}
	h.reconcile(t, declarative.KindAgents, gone)
	if got := h.clients["agents"].deleted; len(got) != 0 {
		t.Errorf("deleted %v; the recreated resource holds the item", got)
	}
}

func TestOlderRefusedModelDoesNotHoldTheKey(t *testing.T) {
	older := &dv1.VirtualModel{ObjectMeta: meta0(systemNamespace, "a", 0), Spec: dv1.VirtualModelSpec{Family: "f", Name: "n"},
		Status: dv1.Status{Conditions: []metav1.Condition{{Type: condAccepted, Status: metav1.ConditionFalse, Reason: reasonInvalid, LastTransitionTime: metav1.NewTime(t0)}}}}
	newer := &dv1.VirtualModel{ObjectMeta: meta0(systemNamespace, "b", 5), Spec: dv1.VirtualModelSpec{Family: "f", Name: "n"}}
	h := newHarness(t, older, newer)
	h.reconcile(t, declarative.KindVirtualModels, newer)
	if len(h.clients["inference"].applied) != 1 {
		t.Errorf("the newer model was not applied: %+v", newer.Status)
	}
}

func TestSweepContinuesPastATenantError(t *testing.T) {
	beta := tenantAt("beta", "", t0)
	h := newHarness(t, &beta, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kora-tenant-beta"}})
	h.clients["agents"].failList = map[string]bool{"acme": true}
	h.clients["agents"].declared["beta"] = []declarative.Declared{{Name: "x", UID: "gone"}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := SweepAll(ctx, []*DeclaredReconciler{h.recs[declarative.KindAgents]}, time.Hour, logging.NewLoggerWithOutput(io.Discard)); err != nil {
		t.Fatal(err)
	}
	if got := h.clients["agents"].deleted; !slices.Equal(got, []string{"beta/x"}) {
		t.Errorf("deleted %v", got)
	}
}

func TestAMissingAgentNamedTwiceIsReportedOnce(t *testing.T) {
	ns := "kora-tenant-acme"
	src := &dv1.TriggerSource{ObjectMeta: meta0(ns, "s", 0), Status: dv1.TriggerSourceStatus{Status: readyStatus("s1")}}
	vm := &dv1.VirtualModel{ObjectMeta: meta0(systemNamespace, "d1", 0), Spec: dv1.VirtualModelSpec{Family: "f", Name: "d1"}, Status: readyStatus("vm")}
	trig := &dv1.Trigger{ObjectMeta: meta0(ns, "t", 0), Spec: dv1.TriggerSpec{Kind: "webhook", Source: "s", EventType: "e", PromptTemplate: "p",
		Routing: &dv1.Routing{Candidates: []string{"ghost"}, Fallback: "ghost", Model: dv1.ModelRef{Family: "f", Name: "d1"}}}}
	h := newHarness(t, src, vm, trig)
	h.reconcile(t, declarative.KindTriggers, trig)
	if c := declaredCondition(t, trig, condResolvedRefs); c.Message != `Agent "ghost" not found or not Ready.` {
		t.Errorf("ResolvedRefs = %q", c.Message)
	}
}
