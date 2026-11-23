package agents

import (
	"context"
	"errors"
	"io"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// ownerRefFixture wires a K8sProvisioner backed by both a fake typed client
// (agents create SA/StatefulSet/Service/PDB through it) and a fake dynamic
// client (DestinationRule + AuthorizationPolicy + KoraTenant go through it),
// seeded with an KoraTenant so tenantOwnerRef finds a UID to reference.
func ownerRefFixture(t *testing.T, cfg K8sProvisionerConfig, tenantName string) (*K8sProvisioner, *fake.Clientset, *dynamicfake.FakeDynamicClient) {
	t.Helper()
	typed := fake.NewSimpleClientset()
	dc := ownerRefDynamicClient(tenantName)
	return &K8sProvisioner{
		client:        typed,
		dynamicClient: dc,
		logger:        logging.NewLoggerWithOutput(io.Discard),
		cfg:           cfg,
	}, typed, dc
}

// ownerRefDynamicClient is a fake dynamic client for the Istio objects, seeded with the KoraTenant tenantName.
func ownerRefDynamicClient(tenantName string) *dynamicfake.FakeDynamicClient {
	scheme := runtime.NewScheme()
	kinds := map[schema.GroupVersionResource]string{
		authorizationPolicyGVR: "AuthorizationPolicyList",
		destinationRuleGVR:     "DestinationRuleList",
		koraTenantGVR:          "KoraTenantList",
	}
	tenant := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "runkora.io/v1alpha1",
			"kind":       "KoraTenant",
			"metadata": map[string]any{
				"name": tenantName,
				"uid":  "tenant-uid-" + tenantName,
			},
			"spec": map[string]any{},
		},
	}
	tenant.SetUID(types.UID("tenant-uid-" + tenantName))
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, kinds, tenant)
}

func assertTenantOwnerRef(t *testing.T, refs []metav1.OwnerReference, tenantName string) {
	t.Helper()
	if len(refs) == 0 {
		t.Fatalf("no owner references set; want KoraTenant/%s", tenantName)
	}
	for _, r := range refs {
		if r.Kind == koraTenantKind && r.Name == tenantName {
			if r.APIVersion != koraTenantAPIVersion {
				t.Errorf("apiVersion = %q, want %q", r.APIVersion, koraTenantAPIVersion)
			}
			if r.UID != types.UID("tenant-uid-"+tenantName) {
				t.Errorf("uid = %q, want tenant-uid-%s", r.UID, tenantName)
			}
			if r.Controller == nil || !*r.Controller {
				t.Errorf("Controller = %v, want true (agent objects are tenant-owned)", r.Controller)
			}
			if r.BlockOwnerDeletion == nil || !*r.BlockOwnerDeletion {
				t.Errorf("BlockOwnerDeletion = %v, want true", r.BlockOwnerDeletion)
			}
			return
		}
	}
	t.Fatalf("no KoraTenant/%s ref among %+v", tenantName, refs)
}

func TestProvisionAgentType_SetsTenantOwnerReferenceOnEveryObject(t *testing.T) {
	cfg := K8sProvisionerConfig{
		SystemNamespace:            "kora-system",
		TrustDomain:                "runkora.test",
		AuthorizationPolicyEnabled: true,
	}
	const tenantID = "acme"
	const ns = "kora-tenant-acme"
	p, typed, dc := ownerRefFixture(t, cfg, tenantID)

	at := AgentType{ID: "coder", Slug: "coder", TenantID: tenantID}
	if err := p.ProvisionAgentType(context.Background(), at, ns); err != nil {
		t.Fatalf("ProvisionAgentType: %v", err)
	}

	sa, err := typed.CoreV1().ServiceAccounts(ns).Get(context.Background(), poolName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get pool sa: %v", err)
	}
	assertTenantOwnerRef(t, sa.OwnerReferences, tenantID)

	svc, err := typed.CoreV1().Services(ns).Get(context.Background(), "coder", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get svc: %v", err)
	}
	assertTenantOwnerRef(t, svc.OwnerReferences, tenantID)

	// The tenant pool comes with the agent (docs/adr/2026-11-22-agent-cold-tier.md).
	pool, err := typed.AppsV1().Deployments(ns).Get(context.Background(), poolName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get pool deployment: %v", err)
	}
	assertTenantOwnerRef(t, pool.OwnerReferences, tenantID)

	// The scaler owns the replicas. No HPA is created.
	if _, err := typed.AutoscalingV2().HorizontalPodAutoscalers(ns).Get(context.Background(), "coder", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Errorf("get hpa: err = %v, want not found", err)
	}

	// DestinationRule lives in kora-system; a cluster-scoped KoraTenant owner
	// is legal for a namespaced child in any namespace.
	dr, err := dc.Resource(destinationRuleGVR).Namespace(cfg.SystemNamespace).Get(context.Background(), agentDestinationRuleName("coder", ns), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get destinationrule: %v", err)
	}
	assertTenantOwnerRef(t, dr.GetOwnerReferences(), tenantID)

	ap, err := dc.Resource(authorizationPolicyGVR).Namespace(ns).Get(context.Background(), agentAuthorizationPolicyName("coder"), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get authpolicy: %v", err)
	}
	assertTenantOwnerRef(t, ap.GetOwnerReferences(), tenantID)
}

func TestProvisionAgentType_MissingTenantCRDegradesGracefully(t *testing.T) {
	cfg := K8sProvisionerConfig{
		SystemNamespace: "kora-system",
		TrustDomain:     "runkora.test",
	}
	// Fixture seeded with a DIFFERENT tenant name — Get(tenantID) returns NotFound.
	p, typed, _ := ownerRefFixture(t, cfg, "someone-else")

	const tenantID = "acme"
	const ns = "kora-tenant-acme"
	at := AgentType{ID: "coder", Slug: "coder", TenantID: tenantID}
	if err := p.ProvisionAgentType(context.Background(), at, ns); err != nil {
		t.Fatalf("ProvisionAgentType must not fail when KoraTenant is missing: %v", err)
	}

	pool, err := typed.AppsV1().Deployments(ns).Get(context.Background(), poolName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get pool deployment: %v", err)
	}
	if len(pool.OwnerReferences) != 0 {
		t.Errorf("pool deployment carries owner refs %+v when the tenant CR is absent; want none", pool.OwnerReferences)
	}
}

// A sandbox is owned by the session-manager pod whose workspace it mounts, and the reference only takes part
// in garbage collection.
func TestPodOwnerRef(t *testing.T) {
	ref := podOwnerRef(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pool-a", UID: types.UID("pod-uid")}})
	if ref.APIVersion != "v1" || ref.Kind != "Pod" || ref.Name != "pool-a" || ref.UID != "pod-uid" {
		t.Fatalf("ref = %+v, want v1 Pod/pool-a", ref)
	}
	if ref.Controller == nil || *ref.Controller || ref.BlockOwnerDeletion == nil || *ref.BlockOwnerDeletion {
		t.Errorf("Controller = %v, BlockOwnerDeletion = %v; want both false", ref.Controller, ref.BlockOwnerDeletion)
	}
}

func TestIsNoKindMatchError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"no matches for kind", errors.New(`no matches for kind "KoraTenant" in group "runkora.io"`), true},
		{"server could not find", errors.New("the server could not find the requested resource (get koratenants.runkora.io)"), true},
		{"unrelated", errors.New("boom"), false},
		{"apierrors NotFound is separate", apierrors.NewNotFound(schema.GroupResource{Group: "runkora.io", Resource: "koratenants"}, "acme"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isNoKindMatchError(c.err); got != c.want {
				t.Errorf("isNoKindMatchError(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}

func TestTenantOwnerRef_CRDMissingDegradesGracefully(t *testing.T) {
	// Fake dynamic client with the KoraTenant list-kind unregistered — Get
	// returns an "invalid API kind" error that isNoKindMatchError catches,
	// mirroring a cluster where the operator's CRD is not installed.
	scheme := runtime.NewScheme()
	dc := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{})
	p := &K8sProvisioner{
		dynamicClient: dc,
		logger:        logging.NewLoggerWithOutput(io.Discard),
	}

	ref := p.tenantOwnerRef(context.Background(), "acme")
	if ref.Kind != "" {
		t.Fatalf("ref = %+v, want zero-value when the KoraTenant CRD is unregistered", ref)
	}
}

func TestTenantOwnerRef_EmptyTenantIDIsZero(t *testing.T) {
	p := &K8sProvisioner{
		logger: logging.NewLoggerWithOutput(io.Discard),
	}
	if ref := p.tenantOwnerRef(context.Background(), ""); ref.Kind != "" {
		t.Fatalf("ref = %+v, want zero-value on empty tenant id", ref)
	}
}

func TestApplyOwnerRef_ZeroValueIsNoop(t *testing.T) {
	meta := metav1.ObjectMeta{}
	applyOwnerRef(&meta, metav1.OwnerReference{})
	if len(meta.OwnerReferences) != 0 {
		t.Fatalf("owner refs = %+v, want none for zero-value ref", meta.OwnerReferences)
	}
}

func TestApplyOwnerRef_ReplacesExistingEntry(t *testing.T) {
	first := metav1.OwnerReference{APIVersion: koraTenantAPIVersion, Kind: koraTenantKind, Name: "acme", UID: "old"}
	second := first
	second.UID = "new"

	meta := metav1.ObjectMeta{}
	applyOwnerRef(&meta, first)
	applyOwnerRef(&meta, second)
	if len(meta.OwnerReferences) != 1 || meta.OwnerReferences[0].UID != "new" {
		t.Fatalf("owner refs = %+v, want single ref with UID=new", meta.OwnerReferences)
	}
}

// TestProvisionAgentType_BackfillsOwnerRefOnExistingObjects seeds the cluster
// with unowned copies of every agent-type resource (the shape upgraded
// clusters land in on first run of this feature) and asserts a re-provision
// stamps the KoraTenant owner reference onto each one. The ADR's
// "existing objects gain owner references on next reconcile" claim depends
// on this behaviour.
func TestProvisionAgentType_BackfillsOwnerRefOnExistingObjects(t *testing.T) {
	cfg := K8sProvisionerConfig{
		SystemNamespace:            "kora-system",
		TrustDomain:                "runkora.test",
		AuthorizationPolicyEnabled: true,
	}
	const tenantID = "acme"
	const ns = "kora-tenant-acme"

	// Seed unowned versions of every object ProvisionAgentType manages.
	typed := fake.NewSimpleClientset(
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: poolName, Namespace: ns}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "coder", Namespace: ns}},
		&autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: "coder", Namespace: ns}},
	)

	scheme := runtime.NewScheme()
	kinds := map[schema.GroupVersionResource]string{
		authorizationPolicyGVR: "AuthorizationPolicyList",
		destinationRuleGVR:     "DestinationRuleList",
		koraTenantGVR:          "KoraTenantList",
	}
	tenant := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "runkora.io/v1alpha1",
		"kind":       "KoraTenant",
		"metadata":   map[string]any{"name": tenantID, "uid": "tenant-uid-" + tenantID},
		"spec":       map[string]any{},
	}}
	tenant.SetUID(types.UID("tenant-uid-" + tenantID))
	// Pre-existing (unowned) Istio objects — mirror the pre-upgrade state.
	dr := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "networking.istio.io/v1beta1",
		"kind":       "DestinationRule",
		"metadata":   map[string]any{"name": agentDestinationRuleName("coder", ns), "namespace": cfg.SystemNamespace},
		"spec":       map[string]any{"host": agentServiceHost("coder", ns)},
	}}
	ap := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "security.istio.io/v1beta1",
		"kind":       "AuthorizationPolicy",
		"metadata":   map[string]any{"name": agentAuthorizationPolicyName("coder"), "namespace": ns},
	}}
	dc := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, kinds, tenant, dr, ap)

	p := &K8sProvisioner{
		client:        typed,
		dynamicClient: dc,
		logger:        logging.NewLoggerWithOutput(io.Discard),
		cfg:           cfg,
	}

	at := AgentType{ID: "coder", Slug: "coder", TenantID: tenantID}
	if err := p.ProvisionAgentType(context.Background(), at, ns); err != nil {
		t.Fatalf("ProvisionAgentType on pre-existing objects: %v", err)
	}

	sa, _ := typed.CoreV1().ServiceAccounts(ns).Get(context.Background(), poolName, metav1.GetOptions{})
	assertTenantOwnerRef(t, sa.OwnerReferences, tenantID)

	svc, _ := typed.CoreV1().Services(ns).Get(context.Background(), "coder", metav1.GetOptions{})
	assertTenantOwnerRef(t, svc.OwnerReferences, tenantID)

	// An HPA left by an earlier release is deleted on apply.
	if _, err := typed.AutoscalingV2().HorizontalPodAutoscalers(ns).Get(context.Background(), "coder", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Errorf("stale hpa: err = %v, want not found", err)
	}

	drOut, _ := dc.Resource(destinationRuleGVR).Namespace(cfg.SystemNamespace).Get(context.Background(), agentDestinationRuleName("coder", ns), metav1.GetOptions{})
	assertTenantOwnerRef(t, drOut.GetOwnerReferences(), tenantID)

	apOut, _ := dc.Resource(authorizationPolicyGVR).Namespace(ns).Get(context.Background(), agentAuthorizationPolicyName("coder"), metav1.GetOptions{})
	assertTenantOwnerRef(t, apOut.GetOwnerReferences(), tenantID)
}

func TestMergeOwnerRefs(t *testing.T) {
	unrelated := metav1.OwnerReference{APIVersion: "v1", Kind: "ConfigMap", Name: "other"}
	first := metav1.OwnerReference{APIVersion: koraTenantAPIVersion, Kind: koraTenantKind, Name: "acme", UID: "old"}
	updated := first
	updated.UID = "new"

	got := mergeOwnerRefs([]metav1.OwnerReference{unrelated, first}, updated)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2 (unrelated preserved, matching entry updated)", len(got))
	}
	if got[0] != unrelated {
		t.Errorf("unrelated ref clobbered: %+v", got[0])
	}
	if got[1].UID != "new" {
		t.Errorf("matching ref not updated: %+v", got[1])
	}

	appended := mergeOwnerRefs([]metav1.OwnerReference{unrelated}, first)
	if len(appended) != 2 || appended[1].Name != "acme" {
		t.Fatalf("expected append when no match; got %+v", appended)
	}
}
