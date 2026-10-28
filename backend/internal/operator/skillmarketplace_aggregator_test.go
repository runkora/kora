package operator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/marketplace"
)

var marketplaceT0 = metav1.NewTime(time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC))

// laterBy returns t plus the given number of seconds, the precision of a
// creation timestamp.
func laterBy(t metav1.Time, seconds int) metav1.Time {
	return metav1.NewTime(t.Add(time.Duration(seconds) * time.Second))
}

func marketSrc(namespace, name, owner, repo string, created metav1.Time) SkillMarketplaceSource {
	return SkillMarketplaceSource{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name, CreationTimestamp: created},
		Spec: SkillMarketplaceSourceSpec{
			GitHub: SkillMarketplaceSourceGitHub{Owner: owner, Repo: repo},
		},
	}
}

func marketTenant(name string, active bool, allowed ...string) OrbitTenant {
	return OrbitTenant{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       OrbitTenantSpec{Active: active, AllowedMarketplaceRepos: allowed},
	}
}

// marketplaceLinks flattens the rendered file to tenant → links, in the order
// skills-service will show them.
func marketplaceLinks(file marketplace.SourcesFile) map[string][]string {
	out := map[string][]string{}
	for _, tenant := range file.SkillsMapping.Tenants {
		out[tenant.ID] = tenant.Marketplaces
	}
	return out
}

// wantPhase is the ADR's status table: every reason has exactly one phase.
var wantPhase = map[string]string{
	MarketplaceReasonAggregated:                SkillMarketplaceSourcePhaseAggregated,
	MarketplaceReasonTenantNotActive:           SkillMarketplaceSourcePhasePending,
	MarketplaceReasonNotAllowlisted:            SkillMarketplaceSourcePhasePending,
	MarketplaceReasonGlobalSourcesNotSupported: SkillMarketplaceSourcePhaseInvalid,
	MarketplaceReasonDuplicateRepository:       SkillMarketplaceSourcePhaseInvalid,
}

func TestBuildMarketplaceSources(t *testing.T) {
	const acme = "orbit-tenant-acme"
	link := marketplace.RepoURL
	internal := link("acme-corp", "internal-skills")

	customNamespace := marketTenant("acme", true, "acme-corp/internal-skills")
	customNamespace.Spec.Namespace = "acme-custom"
	messageTenant := marketTenant("globex", true, "initech/gadgets")
	messageTenant.Spec.Namespace = "gx-ns"
	sharedActive := marketTenant("acme", true, "acme-corp/internal-skills")
	sharedActive.Spec.Namespace = "shared-ns"
	sharedInactive := marketTenant("aaa-retired", false, "acme-corp/internal-skills")
	sharedInactive.Spec.Namespace = "shared-ns"
	sharedLowerName := marketTenant("alpha", true)
	sharedLowerName.Spec.Namespace = "shared-ns"
	sharedHigherName := marketTenant("bravo", true, "acme-corp/internal-skills")
	sharedHigherName.Spec.Namespace = "shared-ns"

	cases := []struct {
		name     string
		tenants  []OrbitTenant
		sources  []SkillMarketplaceSource
		want     map[string]string   // namespace/name → reason
		messages map[string][]string // namespace/name → substrings its message must contain
		rendered map[string][]string // tenant → links, in display order
	}{
		{
			name:     "allow-listed source is rendered for its tenant",
			tenants:  []OrbitTenant{marketTenant("acme", true, "acme-corp/internal-skills")},
			sources:  []SkillMarketplaceSource{marketSrc(acme, "acme-internal", "acme-corp", "internal-skills", marketplaceT0)},
			want:     map[string]string{acme + "/acme-internal": MarketplaceReasonAggregated},
			rendered: map[string][]string{"acme": {internal}},
		},
		{
			name:    "source not on the allowlist waits",
			tenants: []OrbitTenant{marketTenant("acme", true)},
			sources: []SkillMarketplaceSource{marketSrc(acme, "acme-internal", "acme-corp", "internal-skills", marketplaceT0)},
			want:    map[string]string{acme + "/acme-internal": MarketplaceReasonNotAllowlisted},
		},
		{
			name:     "allowlist match ignores case and renders lowercase",
			tenants:  []OrbitTenant{marketTenant("acme", true, "Acme-Corp/Internal-Skills")},
			sources:  []SkillMarketplaceSource{marketSrc(acme, "acme-internal", "ACME-corp", "internal-SKILLS", marketplaceT0)},
			want:     map[string]string{acme + "/acme-internal": MarketplaceReasonAggregated},
			rendered: map[string][]string{"acme": {internal}},
		},
		{
			name:    "suspended tenant",
			tenants: []OrbitTenant{marketTenant("acme", false, "acme-corp/internal-skills")},
			sources: []SkillMarketplaceSource{marketSrc(acme, "acme-internal", "acme-corp", "internal-skills", marketplaceT0)},
			want:    map[string]string{acme + "/acme-internal": MarketplaceReasonTenantNotActive},
		},
		{
			name:     "namespace with no tenant",
			tenants:  []OrbitTenant{marketTenant("acme", true, "acme-corp/internal-skills")},
			sources:  []SkillMarketplaceSource{marketSrc("orbit-tenant-ghost", "s", "acme-corp", "internal-skills", marketplaceT0)},
			want:     map[string]string{"orbit-tenant-ghost/s": MarketplaceReasonTenantNotActive},
			messages: map[string][]string{"orbit-tenant-ghost/s": {"orbit-tenant-ghost"}},
		},
		{
			name:     "orbit-system source is invalid",
			sources:  []SkillMarketplaceSource{marketSrc(systemNamespace, "global", "clear-route", "skills", marketplaceT0)},
			want:     map[string]string{systemNamespace + "/global": MarketplaceReasonGlobalSourcesNotSupported},
			messages: map[string][]string{systemNamespace + "/global": {systemNamespace}},
		},
		{
			// One kubectl apply gives sources the same second; the name breaks
			// the tie so the winner is stable across runs.
			name:    "same-second tie goes to the lower name",
			tenants: []OrbitTenant{marketTenant("acme", true, "acme-corp/one")},
			sources: []SkillMarketplaceSource{
				marketSrc(acme, "z-src", "acme-corp", "one", marketplaceT0),
				marketSrc(acme, "a-src", "acme-corp", "one", marketplaceT0),
			},
			want: map[string]string{
				acme + "/a-src": MarketplaceReasonAggregated,
				acme + "/z-src": MarketplaceReasonDuplicateRepository,
			},
			messages: map[string][]string{acme + "/z-src": {"a-src", "acme-corp/one"}},
			rendered: map[string][]string{"acme": {link("acme-corp", "one")}},
		},
		{
			name:    "second source for one repository is invalid",
			tenants: []OrbitTenant{marketTenant("acme", true, "acme-corp/internal-skills")},
			sources: []SkillMarketplaceSource{
				marketSrc(acme, "acme-internal", "acme-corp", "internal-skills", marketplaceT0),
				marketSrc(acme, "second", "acme-corp", "internal-skills", laterBy(marketplaceT0, 1)),
			},
			want: map[string]string{
				acme + "/acme-internal": MarketplaceReasonAggregated,
				acme + "/second":        MarketplaceReasonDuplicateRepository,
			},
			messages: map[string][]string{acme + "/second": {"acme-internal", "acme-corp/internal-skills"}},
			rendered: map[string][]string{"acme": {internal}},
		},
		{
			name: "two tenants may register the same repository",
			tenants: []OrbitTenant{
				marketTenant("acme", true, "shared/skills"),
				marketTenant("globex", true, "shared/skills"),
			},
			sources: []SkillMarketplaceSource{
				marketSrc(acme, "skills", "shared", "skills", marketplaceT0),
				marketSrc("orbit-tenant-globex", "skills", "shared", "skills", marketplaceT0),
			},
			want: map[string]string{
				acme + "/skills":             MarketplaceReasonAggregated,
				"orbit-tenant-globex/skills": MarketplaceReasonAggregated,
			},
			rendered: map[string][]string{
				"acme":   {link("shared", "skills")},
				"globex": {link("shared", "skills")},
			},
		},
		{
			// Otherwise approving the older source would take the repository from
			// a newer one that is already being served.
			name:    "source awaiting approval still holds its repository",
			tenants: []OrbitTenant{marketTenant("acme", true)},
			sources: []SkillMarketplaceSource{
				marketSrc(acme, "older", "acme-corp", "one", marketplaceT0),
				marketSrc(acme, "newer", "acme-corp", "one", laterBy(marketplaceT0, 1)),
			},
			want: map[string]string{
				acme + "/older": MarketplaceReasonNotAllowlisted,
				acme + "/newer": MarketplaceReasonDuplicateRepository,
			},
		},
		{
			name:    "repository clash ignores case",
			tenants: []OrbitTenant{marketTenant("acme", true, "acme/repo")},
			sources: []SkillMarketplaceSource{
				marketSrc(acme, "first", "Acme", "Repo", marketplaceT0),
				marketSrc(acme, "second", "acme", "repo", laterBy(marketplaceT0, 1)),
			},
			want: map[string]string{
				acme + "/first":  MarketplaceReasonAggregated,
				acme + "/second": MarketplaceReasonDuplicateRepository,
			},
			messages: map[string][]string{acme + "/second": {"first", "acme/repo"}},
			rendered: map[string][]string{"acme": {link("acme", "repo")}},
		},
		{
			name:    "sources render oldest first",
			tenants: []OrbitTenant{marketTenant("acme", true, "acme-corp/one", "acme-corp/two", "acme-corp/three")},
			// The names sort in the opposite order to the ages, so ordering by name
			// instead of age would show.
			sources: []SkillMarketplaceSource{
				marketSrc(acme, "a-newest", "acme-corp", "three", laterBy(marketplaceT0, 20)),
				marketSrc(acme, "z-oldest", "acme-corp", "one", marketplaceT0),
				marketSrc(acme, "m-middle", "acme-corp", "two", laterBy(marketplaceT0, 10)),
			},
			want: map[string]string{
				acme + "/z-oldest": MarketplaceReasonAggregated,
				acme + "/m-middle": MarketplaceReasonAggregated,
				acme + "/a-newest": MarketplaceReasonAggregated,
			},
			rendered: map[string][]string{"acme": {link("acme-corp", "one"), link("acme-corp", "two"), link("acme-corp", "three")}},
		},
		{
			name:    "tenant namespace override decides which namespace is served",
			tenants: []OrbitTenant{customNamespace},
			sources: []SkillMarketplaceSource{
				marketSrc("acme-custom", "custom", "acme-corp", "internal-skills", marketplaceT0),
				marketSrc(acme, "default", "acme-corp", "internal-skills", laterBy(marketplaceT0, 1)),
			},
			want: map[string]string{
				"acme-custom/custom": MarketplaceReasonAggregated,
				acme + "/default":    MarketplaceReasonTenantNotActive,
			},
			rendered: map[string][]string{"acme": {internal}},
		},
		{
			name:     "an inactive tenant does not shadow an active one that shares its namespace",
			tenants:  []OrbitTenant{sharedInactive, sharedActive},
			sources:  []SkillMarketplaceSource{marketSrc("shared-ns", "shared", "acme-corp", "internal-skills", marketplaceT0)},
			want:     map[string]string{"shared-ns/shared": MarketplaceReasonAggregated},
			rendered: map[string][]string{"acme": {internal}},
		},
		{
			name:     "of two active tenants sharing a namespace the lower name serves it",
			tenants:  []OrbitTenant{sharedHigherName, sharedLowerName},
			sources:  []SkillMarketplaceSource{marketSrc("shared-ns", "shared", "acme-corp", "internal-skills", marketplaceT0)},
			want:     map[string]string{"shared-ns/shared": MarketplaceReasonNotAllowlisted},
			messages: map[string][]string{"shared-ns/shared": {"alpha"}},
		},
		{
			name:    "messages name the tenant and the repository",
			tenants: []OrbitTenant{messageTenant},
			sources: []SkillMarketplaceSource{
				marketSrc("gx-ns", "gadgets", "initech", "gadgets", marketplaceT0),
				marketSrc("gx-ns", "widgets", "initech", "widgets", laterBy(marketplaceT0, 1)),
			},
			want: map[string]string{
				"gx-ns/gadgets": MarketplaceReasonAggregated,
				"gx-ns/widgets": MarketplaceReasonNotAllowlisted,
			},
			messages: map[string][]string{
				"gx-ns/gadgets": {"globex"},
				"gx-ns/widgets": {"globex", "initech/widgets"},
			},
			rendered: map[string][]string{"globex": {link("initech", "gadgets")}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file, verdicts := buildMarketplaceSources(tc.sources, tc.tenants)

			got := map[string]string{}
			messages := map[string]string{}
			for key, v := range verdicts {
				got[key.String()] = v.reason
				messages[key.String()] = v.message
				if v.phase != wantPhase[v.reason] {
					t.Errorf("%s: phase %q for reason %q, want %q", key, v.phase, v.reason, wantPhase[v.reason])
				}
				if v.message == "" {
					t.Errorf("%s: empty message", key)
				}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("reasons = %v, want %v", got, tc.want)
			}
			for key, substrings := range tc.messages {
				for _, substring := range substrings {
					if !strings.Contains(messages[key], substring) {
						t.Errorf("%s: message %q does not contain %q", key, messages[key], substring)
					}
				}
			}

			rendered := marketplaceLinks(file)
			if (len(rendered) != 0 || len(tc.rendered) != 0) && !reflect.DeepEqual(rendered, tc.rendered) {
				t.Errorf("rendered = %+v, want %+v", rendered, tc.rendered)
			}

			reversedSources := slices.Clone(tc.sources)
			slices.Reverse(reversedSources)
			reversedTenants := slices.Clone(tc.tenants)
			slices.Reverse(reversedTenants)
			reversedFile, reversedVerdicts := buildMarketplaceSources(reversedSources, reversedTenants)
			if !reflect.DeepEqual(reversedFile, file) {
				t.Errorf("reversed run: rendered file = %+v, want %+v, the same as the first run", reversedFile, file)
			}
			if !reflect.DeepEqual(reversedVerdicts, verdicts) {
				t.Errorf("reversed run: verdicts = %v, want %v, the same as the first run", reversedVerdicts, verdicts)
			}
		})
	}
}

// Tenants render in ID order whatever order the API lists them in, so the same
// sources always give the same text and an unchanged list costs no write.
func TestBuildMarketplaceSources_TenantsRenderInIDOrder(t *testing.T) {
	tenants := []OrbitTenant{
		marketTenant("globex", true, "initech/gadgets"),
		marketTenant("acme", true, "acme-corp/internal-skills"),
	}
	sources := []SkillMarketplaceSource{
		marketSrc("orbit-tenant-globex", "gadgets", "initech", "gadgets", marketplaceT0),
		marketSrc("orbit-tenant-acme", "internal", "acme-corp", "internal-skills", marketplaceT0),
	}

	file, _ := buildMarketplaceSources(sources, tenants)

	var ids []string
	for _, tenant := range file.SkillsMapping.Tenants {
		ids = append(ids, tenant.ID)
	}
	if want := []string{"acme", "globex"}; !slices.Equal(ids, want) {
		t.Errorf("tenant order = %v, want %v", ids, want)
	}
}

func TestMarketplaceVerdict_Aggregated(t *testing.T) {
	cases := []struct {
		phase string
		want  bool
	}{
		{SkillMarketplaceSourcePhaseAggregated, true},
		{SkillMarketplaceSourcePhasePending, false},
		{SkillMarketplaceSourcePhaseInvalid, false},
	}
	for _, tc := range cases {
		t.Run(tc.phase, func(t *testing.T) {
			if got := (marketplaceVerdict{phase: tc.phase}).aggregated(); got != tc.want {
				t.Errorf("aggregated() for phase %q = %v, want %v", tc.phase, got, tc.want)
			}
		})
	}
}

// skills-service parses a list, so an empty registry must not render as null.
func TestBuildMarketplaceSources_EmptyRendersEmptyList(t *testing.T) {
	file, verdicts := buildMarketplaceSources(nil, nil)
	raw, err := yaml.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	if want := "skillsMapping:\n  tenants: []\n"; string(raw) != want {
		t.Errorf("empty file = %q, want %q", raw, want)
	}
	if len(verdicts) != 0 {
		t.Errorf("verdicts = %v, want none", verdicts)
	}
}

func newMarketplaceClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	return fake.NewClientBuilder().
		WithScheme(NewScheme()).
		WithObjects(objs...).
		WithStatusSubresource(&SkillMarketplaceSource{}).
		Build()
}

func newTestMarketplaceAggregator(c client.Client) *SkillMarketplaceAggregator {
	return NewSkillMarketplaceAggregator(c, logging.NewLoggerWithOutput(io.Discard))
}

func readMarketplaceFile(t *testing.T, c client.Client) marketplace.SourcesFile {
	t.Helper()
	cm := &corev1.ConfigMap{}
	key := client.ObjectKey{Namespace: systemNamespace, Name: marketplace.SourcesConfigMapName}
	if err := c.Get(context.Background(), key, cm); err != nil {
		t.Fatalf("get %s: %v", marketplace.SourcesConfigMapName, err)
	}
	var file marketplace.SourcesFile
	if err := yaml.Unmarshal([]byte(cm.Data[marketplace.SourcesFileName]), &file); err != nil {
		t.Fatalf("parse %s: %v", marketplace.SourcesFileName, err)
	}
	return file
}

func getMarketplaceSource(t *testing.T, c client.Client, key client.ObjectKey) *SkillMarketplaceSource {
	t.Helper()
	src := &SkillMarketplaceSource{}
	if err := c.Get(context.Background(), key, src); err != nil {
		t.Fatalf("get %s: %v", key, err)
	}
	return src
}

func assertMarketplaceStatus(t *testing.T, src *SkillMarketplaceSource, phase, reason string, status metav1.ConditionStatus) {
	t.Helper()
	if src.Status.Phase != phase {
		t.Errorf("phase = %q, want %q", src.Status.Phase, phase)
	}
	cond := meta.FindStatusCondition(src.Status.Conditions, conditionAggregated)
	if cond == nil {
		t.Fatal("no Aggregated condition")
	}
	if cond.Reason != reason || cond.Status != status {
		t.Errorf("condition = %s/%s, want %s/%s", cond.Status, cond.Reason, status, reason)
	}
	if src.Status.LastReconciledAt == nil {
		t.Error("LastReconciledAt not set")
	}
}

// The ADR's registration flow: a source applied before approval waits, and the
// platform admin's allowlist edit admits it on the next run.
func TestSkillMarketplaceAggregator_AllowlistEditAdmitsPendingSource(t *testing.T) {
	ctx := context.Background()
	tenant := marketTenant("acme", true)
	src := marketSrc("orbit-tenant-acme", "acme-internal", "acme-corp", "internal-skills", marketplaceT0)
	c := newMarketplaceClient(t, &tenant, &src)
	a := newTestMarketplaceAggregator(c)
	key := client.ObjectKeyFromObject(&src)

	if err := a.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	assertMarketplaceStatus(t, getMarketplaceSource(t, c, key),
		SkillMarketplaceSourcePhasePending, MarketplaceReasonNotAllowlisted, metav1.ConditionFalse)
	if file := readMarketplaceFile(t, c); len(file.SkillsMapping.Tenants) != 0 {
		t.Errorf("before approval the file has %v, want no tenants", file.SkillsMapping.Tenants)
	}

	current := &OrbitTenant{}
	if err := c.Get(ctx, client.ObjectKey{Name: "acme"}, current); err != nil {
		t.Fatal(err)
	}
	current.Spec.AllowedMarketplaceRepos = []string{"acme-corp/internal-skills"}
	if err := c.Update(ctx, current); err != nil {
		t.Fatal(err)
	}

	if err := a.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
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

// Both reconcilers run Sync on every event and the tenant requeue; an
// unchanged verdict must not write the status again.
func TestSkillMarketplaceAggregator_UnchangedVerdictSkipsStatusWrite(t *testing.T) {
	ctx := context.Background()
	tenant := marketTenant("acme", true, "acme-corp/internal-skills")
	src := marketSrc("orbit-tenant-acme", "acme-internal", "acme-corp", "internal-skills", marketplaceT0)
	c := newMarketplaceClient(t, &tenant, &src)
	a := newTestMarketplaceAggregator(c)
	key := client.ObjectKeyFromObject(&src)

	if err := a.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	before := getMarketplaceSource(t, c, key).ResourceVersion
	if err := a.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if after := getMarketplaceSource(t, c, key).ResourceVersion; after != before {
		t.Errorf("resourceVersion moved from %s to %s on an unchanged verdict", before, after)
	}
}

func TestSkillMarketplaceAggregator_DeletedSourceLeavesTheFile(t *testing.T) {
	ctx := context.Background()
	tenant := marketTenant("acme", true, "acme-corp/internal-skills")
	src := marketSrc("orbit-tenant-acme", "acme-internal", "acme-corp", "internal-skills", marketplaceT0)
	c := newMarketplaceClient(t, &tenant, &src)
	a := newTestMarketplaceAggregator(c)

	if err := a.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if err := c.Delete(ctx, getMarketplaceSource(t, c, client.ObjectKeyFromObject(&src))); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if file := readMarketplaceFile(t, c); len(file.SkillsMapping.Tenants) != 0 {
		t.Errorf("after delete the file has %v, want no tenants", file.SkillsMapping.Tenants)
	}
}

var errConfigMapRefused = errors.New("configmap write refused")

// failingConfigMapWrites rejects every ConfigMap create.
type failingConfigMapWrites struct{ client.Client }

func (c failingConfigMapWrites) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if _, ok := obj.(*corev1.ConfigMap); ok {
		return errConfigMapRefused
	}
	return c.Client.Create(ctx, obj, opts...)
}

// concurrentConfigMapWriter has a second controller rewrite the ConfigMap right
// after every read of it, so the caller's copy is stale when it writes.
type concurrentConfigMapWriter struct{ client.Client }

func (c concurrentConfigMapWriter) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if err := c.Client.Get(ctx, key, obj, opts...); err != nil {
		return err
	}
	if cm, ok := obj.(*corev1.ConfigMap); ok {
		other := cm.DeepCopy()
		other.Data = map[string]string{marketplace.SourcesFileName: "skillsMapping:\n  tenants:\n  - id: other\n    marketplaces: []\n"}
		return c.Client.Update(ctx, other)
	}
	return nil
}

// A status must never read Aggregated before the ConfigMap holds the source.
func TestSkillMarketplaceAggregator_ConfigMapFailureLeavesStatusesUntouched(t *testing.T) {
	ctx := context.Background()
	tenant := marketTenant("acme", true, "acme-corp/internal-skills")
	src := marketSrc("orbit-tenant-acme", "acme-internal", "acme-corp", "internal-skills", marketplaceT0)
	base := newMarketplaceClient(t, &tenant, &src)
	a := newTestMarketplaceAggregator(failingConfigMapWrites{base})

	if err := a.Sync(ctx); !errors.Is(err, errConfigMapRefused) {
		t.Fatalf("Sync = %v, want the ConfigMap write error", err)
	}
	got := getMarketplaceSource(t, base, client.ObjectKeyFromObject(&src))
	if got.Status.Phase != "" || len(got.Status.Conditions) != 0 {
		t.Errorf("status = %+v, want it untouched while the ConfigMap is unwritten", got.Status)
	}
}

// The lock turns a writer holding a stale copy into a conflict instead of a
// silent overwrite of the other controller's file.
func TestSkillMarketplaceAggregator_StaleConfigMapWriteConflicts(t *testing.T) {
	ctx := context.Background()
	tenant := marketTenant("acme", true, "acme-corp/internal-skills")
	src := marketSrc("orbit-tenant-acme", "acme-internal", "acme-corp", "internal-skills", marketplaceT0)
	existing := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: marketplace.SourcesConfigMapName, Namespace: systemNamespace},
		Data:       map[string]string{marketplace.SourcesFileName: "skillsMapping:\n  tenants: []\n"},
	}
	base := newMarketplaceClient(t, &tenant, &src, existing)
	a := newTestMarketplaceAggregator(concurrentConfigMapWriter{base})

	if err := a.Sync(ctx); !apierrors.IsConflict(err) {
		t.Fatalf("Sync = %v, want a conflict", err)
	}
	if _, ok := marketplaceLinks(readMarketplaceFile(t, base))["other"]; !ok {
		t.Error("the other controller's write was overwritten")
	}
}

// An existing ConfigMap, such as the one the chart creates, keeps its labels and
// annotations: only the data key is patched.
func TestSkillMarketplaceAggregator_PatchKeepsTheExistingMetadata(t *testing.T) {
	ctx := context.Background()
	tenant := marketTenant("acme", true, "acme-corp/internal-skills")
	src := marketSrc("orbit-tenant-acme", "acme-internal", "acme-corp", "internal-skills", marketplaceT0)
	existing := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        marketplace.SourcesConfigMapName,
			Namespace:   systemNamespace,
			Labels:      map[string]string{"app.kubernetes.io/managed-by": "Helm"},
			Annotations: map[string]string{"meta.helm.sh/release-name": "orbit"},
		},
		Data: map[string]string{marketplace.SourcesFileName: "skillsMapping:\n  tenants: []\n"},
	}
	c := newMarketplaceClient(t, &tenant, &src, existing)

	if err := newTestMarketplaceAggregator(c).Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	got := &corev1.ConfigMap{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(existing), got); err != nil {
		t.Fatal(err)
	}
	if got.Labels["app.kubernetes.io/managed-by"] != "Helm" || got.Annotations["meta.helm.sh/release-name"] != "orbit" {
		t.Errorf("metadata = labels %v annotations %v, want the chart's kept", got.Labels, got.Annotations)
	}
	if !strings.Contains(got.Data[marketplace.SourcesFileName], "https://github.com/acme-corp/internal-skills") {
		t.Errorf("data = %q, want the approved source rendered", got.Data[marketplace.SourcesFileName])
	}
}

// The ADR's primary friction signal: every excluded source is logged with its
// reason on every run, whether or not its status needed a write.
func TestSkillMarketplaceAggregator_LogsEveryExcludedSource(t *testing.T) {
	ctx := context.Background()
	tenant := marketTenant("acme", true, "acme-corp/approved")
	approved := marketSrc("orbit-tenant-acme", "approved", "acme-corp", "approved", marketplaceT0)
	waiting := marketSrc("orbit-tenant-acme", "waiting", "acme-corp", "waiting", marketplaceT0)
	c := newMarketplaceClient(t, &tenant, &approved, &waiting)
	var logs bytes.Buffer
	a := NewSkillMarketplaceAggregator(c, logging.NewLoggerWithOutput(&logs))

	if err := a.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	logs.Reset()
	if err := a.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	var excluded []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(logs.String()), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		if entry["message"] == "SkillMarketplaceSource excluded from marketplace sources" {
			excluded = append(excluded, entry)
		}
	}
	if len(excluded) != 1 {
		t.Fatalf("logged %d excluded sources, want 1 (the waiting one): %s", len(excluded), logs.String())
	}
	got := excluded[0]
	if got["source"] != "waiting" || got["namespace"] != "orbit-tenant-acme" || got["reason"] != MarketplaceReasonNotAllowlisted {
		t.Errorf("excluded log = %v, want source waiting, namespace orbit-tenant-acme, reason %s", got, MarketplaceReasonNotAllowlisted)
	}
	if detail, _ := got["detail"].(string); !strings.Contains(detail, "acme-corp/waiting") {
		t.Errorf("excluded log detail = %v, want the verdict message naming acme-corp/waiting: %v", got["detail"], got)
	}
}

// A verdict can keep its reason and change only its message, here when the
// older source that holds a repository is replaced by another one.
func TestSkillMarketplaceAggregator_ChangedMessageRewritesStatus(t *testing.T) {
	ctx := context.Background()
	const ns = "orbit-tenant-acme"
	tenant := marketTenant("acme", true, "acme-corp/one")
	first := marketSrc(ns, "first", "acme-corp", "one", marketplaceT0)
	clash := marketSrc(ns, "clash", "acme-corp", "one", laterBy(marketplaceT0, 5))
	c := newMarketplaceClient(t, &tenant, &first, &clash)
	a := newTestMarketplaceAggregator(c)
	key := client.ObjectKeyFromObject(&clash)

	if err := a.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	cond := meta.FindStatusCondition(getMarketplaceSource(t, c, key).Status.Conditions, conditionAggregated)
	if cond == nil || cond.Reason != MarketplaceReasonDuplicateRepository || !strings.Contains(cond.Message, `"first"`) {
		t.Fatalf("condition = %+v, want DuplicateRepository held by first", cond)
	}

	if err := c.Delete(ctx, &first); err != nil {
		t.Fatal(err)
	}
	second := marketSrc(ns, "second", "acme-corp", "one", laterBy(marketplaceT0, 1))
	if err := c.Create(ctx, &second); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	cond = meta.FindStatusCondition(getMarketplaceSource(t, c, key).Status.Conditions, conditionAggregated)
	if cond == nil || cond.Reason != MarketplaceReasonDuplicateRepository || !strings.Contains(cond.Message, `"second"`) {
		t.Errorf("condition = %+v, want DuplicateRepository now held by second", cond)
	}
}

// With nothing registered the ConfigMap is still written, as an empty list
// skills-service can parse. The literals are the name and key skills-service
// mounts and reads, so a rename of the marketplace constants alone cannot go
// unnoticed.
func TestSkillMarketplaceAggregator_NoSourcesStillWritesEmptyList(t *testing.T) {
	c := newMarketplaceClient(t)
	if err := newTestMarketplaceAggregator(c).Sync(context.Background()); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	cm := &corev1.ConfigMap{}
	key := client.ObjectKey{Namespace: systemNamespace, Name: "orbit-skill-marketplace-sources"}
	if err := c.Get(context.Background(), key, cm); err != nil {
		t.Fatalf("get %s: %v", key, err)
	}
	if len(cm.Data) != 1 {
		t.Errorf("data keys = %v, want only skills-mapping.yaml", cm.Data)
	}
	if want := "skillsMapping:\n  tenants: []\n"; cm.Data["skills-mapping.yaml"] != want {
		t.Errorf("skills-mapping.yaml = %q, want %q", cm.Data["skills-mapping.yaml"], want)
	}
}
