package operator

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/marketplace"
)

const exampleNS = "kora-tenant-example"

var marketplaceT0 = metav1.NewTime(time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC))

func laterBy(t metav1.Time, seconds int) metav1.Time {
	return metav1.NewTime(t.Add(time.Duration(seconds) * time.Second))
}

func marketSrc(namespace, name, owner, repo string, created metav1.Time) SkillMarketplace {
	return SkillMarketplace{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name, CreationTimestamp: created},
		Spec: SkillMarketplaceSpec{GitHub: SkillMarketplaceGitHub{
			Owner: owner, Repo: repo,
			App: SkillMarketplaceApp{
				AppID: "4015347", InstallationID: 139299720,
				PrivateKeySecretRef: SkillMarketplaceSecretRef{Name: "skills-github-app", Key: "private-key.pem"},
			},
		}},
	}
}

func marketTenant(name string, active bool) KoraTenant {
	return KoraTenant{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: KoraTenantSpec{Active: active}}
}

var (
	testKeyOnce sync.Once
	testKeyPEM  []byte
)

// appKeyPEM is one RSA private key, generated once for the package's tests.
func appKeyPEM(t *testing.T) []byte {
	t.Helper()
	testKeyOnce.Do(func() {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		testKeyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	})
	return testKeyPEM
}

func appKeySecret(t *testing.T, namespace string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "skills-github-app"},
		Data:       map[string][]byte{"private-key.pem": appKeyPEM(t)},
	}
}

// slugs flattens a rendered file to tenant -> Marketplace slugs in order.
func slugs(file marketplace.File) map[string][]string {
	out := map[string][]string{}
	for _, tenant := range file.Tenants {
		for _, m := range tenant.Marketplaces {
			out[tenant.ID] = append(out[tenant.ID], m.Slug)
		}
	}
	return out
}

var wantPhase = map[string]string{
	MarketplaceReasonAggregated:                SkillMarketplacePhaseAggregated,
	MarketplaceReasonTenantNotActive:           SkillMarketplacePhasePending,
	MarketplaceReasonGlobalSourcesNotSupported: SkillMarketplacePhaseInvalid,
	MarketplaceReasonDuplicateRepository:       SkillMarketplacePhaseInvalid,
}

func TestBuildMarketplaces(t *testing.T) {
	custom := marketTenant("example", true)
	custom.Spec.Namespace = "example-custom"
	older := marketTenant("older", true)
	older.CreationTimestamp = marketplaceT0
	older.Spec.Namespace = "shared-ns"
	newer := marketTenant("newer", true)
	newer.CreationTimestamp = laterBy(marketplaceT0, 1)
	newer.Spec.Namespace = "shared-ns"

	cases := []struct {
		name     string
		tenants  []KoraTenant
		items    []SkillMarketplace
		want     map[string]string
		rendered map[string][]string
	}{
		{
			name:     "served for its tenant",
			tenants:  []KoraTenant{marketTenant("example", true)},
			items:    []SkillMarketplace{marketSrc(exampleNS, "team-skills", "example-org", "skills", marketplaceT0)},
			want:     map[string]string{exampleNS + "/team-skills": MarketplaceReasonAggregated},
			rendered: map[string][]string{"example": {"team-skills"}},
		},
		{
			name:    "suspended tenant",
			tenants: []KoraTenant{marketTenant("example", false)},
			items:   []SkillMarketplace{marketSrc(exampleNS, "team-skills", "example-org", "skills", marketplaceT0)},
			want:    map[string]string{exampleNS + "/team-skills": MarketplaceReasonTenantNotActive},
		},
		{
			name:  "namespace with no tenant",
			items: []SkillMarketplace{marketSrc("kora-tenant-ghost", "s", "example-org", "skills", marketplaceT0)},
			want:  map[string]string{"kora-tenant-ghost/s": MarketplaceReasonTenantNotActive},
		},
		{
			name:  "kora-system Marketplace is invalid",
			items: []SkillMarketplace{marketSrc(systemNamespace, "global", "example-org", "skills", marketplaceT0)},
			want:  map[string]string{systemNamespace + "/global": MarketplaceReasonGlobalSourcesNotSupported},
		},
		{
			name:    "second Marketplace for one repository is invalid, ignoring case",
			tenants: []KoraTenant{marketTenant("example", true)},
			items: []SkillMarketplace{
				marketSrc(exampleNS, "second", "example-org", "skills", laterBy(marketplaceT0, 1)),
				marketSrc(exampleNS, "first", "Example-Org", "Skills", marketplaceT0),
			},
			want: map[string]string{
				exampleNS + "/first":  MarketplaceReasonAggregated,
				exampleNS + "/second": MarketplaceReasonDuplicateRepository,
			},
			rendered: map[string][]string{"example": {"first"}},
		},
		{
			name:    "same-second tie goes to the lower name",
			tenants: []KoraTenant{marketTenant("example", true)},
			items: []SkillMarketplace{
				marketSrc(exampleNS, "z", "example-org", "skills", marketplaceT0),
				marketSrc(exampleNS, "a", "example-org", "skills", marketplaceT0),
			},
			want: map[string]string{
				exampleNS + "/a": MarketplaceReasonAggregated,
				exampleNS + "/z": MarketplaceReasonDuplicateRepository,
			},
			rendered: map[string][]string{"example": {"a"}},
		},
		{
			name:    "two tenants may register the same repository",
			tenants: []KoraTenant{marketTenant("example", true), marketTenant("other", true)},
			items: []SkillMarketplace{
				marketSrc(exampleNS, "skills", "example-org", "skills", marketplaceT0),
				marketSrc("kora-tenant-other", "skills", "example-org", "skills", marketplaceT0),
			},
			want: map[string]string{
				exampleNS + "/skills":      MarketplaceReasonAggregated,
				"kora-tenant-other/skills": MarketplaceReasonAggregated,
			},
			rendered: map[string][]string{"example": {"skills"}, "other": {"skills"}},
		},
		{
			name:    "Marketplaces from several organizations render oldest first",
			tenants: []KoraTenant{marketTenant("example", true)},
			items: []SkillMarketplace{
				marketSrc(exampleNS, "a-newest", "third-org", "skills", laterBy(marketplaceT0, 20)),
				marketSrc(exampleNS, "z-oldest", "example-org", "skills", marketplaceT0),
				marketSrc(exampleNS, "m-middle", "other-org", "skills", laterBy(marketplaceT0, 10)),
			},
			want: map[string]string{
				exampleNS + "/a-newest": MarketplaceReasonAggregated,
				exampleNS + "/z-oldest": MarketplaceReasonAggregated,
				exampleNS + "/m-middle": MarketplaceReasonAggregated,
			},
			rendered: map[string][]string{"example": {"z-oldest", "m-middle", "a-newest"}},
		},
		{
			name:    "tenant namespace override decides which namespace is served",
			tenants: []KoraTenant{custom},
			items: []SkillMarketplace{
				marketSrc("example-custom", "custom", "example-org", "skills", marketplaceT0),
				marketSrc(exampleNS, "default", "example-org", "skills", marketplaceT0),
			},
			want: map[string]string{
				"example-custom/custom": MarketplaceReasonAggregated,
				exampleNS + "/default":  MarketplaceReasonTenantNotActive,
			},
			rendered: map[string][]string{"example": {"custom"}},
		},
		{
			// servedTenants gives a shared namespace to the tenant created first.
			name:     "a shared namespace is served to the older tenant",
			tenants:  []KoraTenant{newer, older},
			items:    []SkillMarketplace{marketSrc("shared-ns", "skills", "example-org", "skills", marketplaceT0)},
			want:     map[string]string{"shared-ns/skills": MarketplaceReasonAggregated},
			rendered: map[string][]string{"older": {"skills"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file, verdicts := buildMarketplaces(tc.items, tc.tenants)
			got := map[string]string{}
			for key, v := range verdicts {
				got[key.String()] = v.reason
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
			if r := slugs(file); (len(r) != 0 || len(tc.rendered) != 0) && !reflect.DeepEqual(r, tc.rendered) {
				t.Errorf("rendered = %v, want %v", r, tc.rendered)
			}

			items, tenants := slices.Clone(tc.items), slices.Clone(tc.tenants)
			slices.Reverse(items)
			slices.Reverse(tenants)
			reversedFile, reversedVerdicts := buildMarketplaces(items, tenants)
			if !reflect.DeepEqual(reversedFile, file) || !reflect.DeepEqual(reversedVerdicts, verdicts) {
				t.Error("reversed input changed the result")
			}
		})
	}
}

func TestBuildMarketplaces_RendersTheSpec(t *testing.T) {
	m := marketSrc(exampleNS, "team-skills", "example-org", "skills", marketplaceT0)
	m.Spec.GitHub.Ref = "main"
	file, _ := buildMarketplaces([]SkillMarketplace{m}, []KoraTenant{marketTenant("example", true)})
	want := marketplace.File{Tenants: []marketplace.Tenant{{ID: "example", Marketplaces: []marketplace.Marketplace{{
		Slug: "team-skills", Owner: "example-org", Repo: "skills", Ref: "main",
		AppID: "4015347", InstallationID: 139299720,
		PrivateKeySecretKey: "kora-tenant-example.team-skills",
	}}}}}
	if !reflect.DeepEqual(file, want) {
		t.Errorf("file = %+v, want %+v", file, want)
	}
}

func newMarketplaceClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	return fake.NewClientBuilder().
		WithScheme(NewScheme()).
		WithObjects(objs...).
		WithStatusSubresource(&SkillMarketplace{}).
		Build()
}

func newTestMarketplaceAggregator(c client.Client) *SkillMarketplaceAggregator {
	return NewSkillMarketplaceAggregator(c, logging.NewLoggerWithOutput(io.Discard))
}

func readMarketplaceFile(t *testing.T, c client.Client) marketplace.File {
	t.Helper()
	cm := &corev1.ConfigMap{}
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: systemNamespace, Name: "kora-skill-marketplaces"}, cm); err != nil {
		t.Fatalf("get ConfigMap: %v", err)
	}
	var file marketplace.File
	if err := json.Unmarshal([]byte(cm.Data["marketplaces.json"]), &file); err != nil {
		t.Fatalf("parse marketplaces.json: %v", err)
	}
	return file
}

func readMarketplaceSecret(t *testing.T, c client.Client) map[string][]byte {
	t.Helper()
	s := &corev1.Secret{}
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: systemNamespace, Name: "kora-skill-marketplace-secrets"}, s); err != nil {
		t.Fatalf("get Secret: %v", err)
	}
	return s.Data
}

func getMarketplace(t *testing.T, c client.Client, key client.ObjectKey) *SkillMarketplace {
	t.Helper()
	m := &SkillMarketplace{}
	if err := c.Get(context.Background(), key, m); err != nil {
		t.Fatalf("get %s: %v", key, err)
	}
	return m
}

func assertMarketplaceStatus(t *testing.T, m *SkillMarketplace, phase, reason string, status metav1.ConditionStatus) *metav1.Condition {
	t.Helper()
	if m.Status.Phase != phase {
		t.Errorf("phase = %q, want %q", m.Status.Phase, phase)
	}
	cond := meta.FindStatusCondition(m.Status.Conditions, conditionAggregated)
	if cond == nil {
		t.Fatal("no Aggregated condition")
	}
	if cond.Reason != reason || cond.Status != status {
		t.Errorf("condition = %s/%s (%s), want %s/%s", cond.Status, cond.Reason, cond.Message, status, reason)
	}
	if m.Status.LastReconciledAt == nil {
		t.Error("LastReconciledAt not set")
	}
	return cond
}

func TestSkillMarketplaceAggregator_CopiesTheKey(t *testing.T) {
	tenant := marketTenant("example", true)
	m := marketSrc(exampleNS, "team-skills", "example-org", "skills", marketplaceT0)
	c := newMarketplaceClient(t, &tenant, &m, appKeySecret(t, exampleNS))

	if err := newTestMarketplaceAggregator(c).Sync(context.Background()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	assertMarketplaceStatus(t, getMarketplace(t, c, client.ObjectKeyFromObject(&m)),
		SkillMarketplacePhaseAggregated, MarketplaceReasonAggregated, metav1.ConditionTrue)
	if got := readMarketplaceSecret(t, c); !reflect.DeepEqual(got, map[string][]byte{"kora-tenant-example.team-skills": appKeyPEM(t)}) {
		t.Errorf("Secret data keys = %v, want only kora-tenant-example.team-skills with the key", slices.Collect(maps.Keys(got)))
	}
	if got := slugs(readMarketplaceFile(t, c)); !reflect.DeepEqual(got, map[string][]string{"example": {"team-skills"}}) {
		t.Errorf("file = %v", got)
	}
}

// A missing or invalid key keeps the Marketplace listed and leaves its key out
// of the Secret, so browsing fails closed.
func TestSkillMarketplaceAggregator_CredentialUnavailable(t *testing.T) {
	managed := appKeySecret(t, exampleNS)
	managed.Labels = map[string]string{"app.kubernetes.io/managed-by": "kora-operator"}
	notPEM := appKeySecret(t, exampleNS)
	notPEM.Data["private-key.pem"] = []byte("not a key")
	wrongKey := appKeySecret(t, exampleNS)
	wrongKey.Data = map[string][]byte{"key.pem": appKeyPEM(t)}

	cases := []struct {
		name   string
		secret *corev1.Secret
		want   []string
	}{
		{"missing Secret", nil, []string{`Secret "skills-github-app" not found`}},
		{"missing key", wrongKey, []string{`"skills-github-app"`, `"private-key.pem"`}},
		{"not a PEM key", notPEM, []string{`"private-key.pem"`, "not a PEM RSA private key"}},
		{"operator-managed Secret", managed, []string{"managed by the operator"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tenant := marketTenant("example", true)
			m := marketSrc(exampleNS, "team-skills", "example-org", "skills", marketplaceT0)
			objs := []client.Object{&tenant, &m}
			if tc.secret != nil {
				objs = append(objs, tc.secret)
			}
			c := newMarketplaceClient(t, objs...)

			if err := newTestMarketplaceAggregator(c).Sync(context.Background()); err != nil {
				t.Fatalf("Sync: %v", err)
			}
			cond := assertMarketplaceStatus(t, getMarketplace(t, c, client.ObjectKeyFromObject(&m)),
				SkillMarketplacePhaseAggregated, MarketplaceReasonCredentialUnavailable, metav1.ConditionTrue)
			for _, s := range tc.want {
				if !strings.Contains(cond.Message, s) {
					t.Errorf("message %q does not contain %q", cond.Message, s)
				}
			}
			if got := readMarketplaceSecret(t, c); len(got) != 0 {
				t.Errorf("Secret holds %d keys, want none", len(got))
			}
			if got := slugs(readMarketplaceFile(t, c)); len(got["example"]) != 1 {
				t.Errorf("file = %v, want the Marketplace still listed", got)
			}
		})
	}
}

// Restoring the key returns the Marketplace to Aggregated on the next run.
func TestSkillMarketplaceAggregator_RestoredKeyClearsTheReason(t *testing.T) {
	ctx := context.Background()
	tenant := marketTenant("example", true)
	m := marketSrc(exampleNS, "team-skills", "example-org", "skills", marketplaceT0)
	c := newMarketplaceClient(t, &tenant, &m)
	a := newTestMarketplaceAggregator(c)
	key := client.ObjectKeyFromObject(&m)

	if err := a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	assertMarketplaceStatus(t, getMarketplace(t, c, key), SkillMarketplacePhaseAggregated, MarketplaceReasonCredentialUnavailable, metav1.ConditionTrue)
	if err := c.Create(ctx, appKeySecret(t, exampleNS)); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	assertMarketplaceStatus(t, getMarketplace(t, c, key), SkillMarketplacePhaseAggregated, MarketplaceReasonAggregated, metav1.ConditionTrue)
	if len(readMarketplaceSecret(t, c)) != 1 {
		t.Error("restored key not copied")
	}
}

var errSecretRead = errors.New("API server unavailable")

// failingKeyReads fails reads of tenant Secrets, as a transient API error would.
type failingKeyReads struct{ client.Client }

func (c failingKeyReads) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if _, ok := obj.(*corev1.Secret); ok && key.Namespace != systemNamespace {
		return errSecretRead
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

// A transient read failure keeps the key already aggregated and fails the run
// so it is retried.
func TestSkillMarketplaceAggregator_FailedReadKeepsThePreviousKey(t *testing.T) {
	ctx := context.Background()
	tenant := marketTenant("example", true)
	m := marketSrc(exampleNS, "team-skills", "example-org", "skills", marketplaceT0)
	base := newMarketplaceClient(t, &tenant, &m, appKeySecret(t, exampleNS))
	if err := newTestMarketplaceAggregator(base).Sync(ctx); err != nil {
		t.Fatal(err)
	}

	if err := newTestMarketplaceAggregator(failingKeyReads{base}).Sync(ctx); !errors.Is(err, errSecretRead) {
		t.Fatalf("Sync = %v, want the read error", err)
	}
	if got := readMarketplaceSecret(t, base); len(got["kora-tenant-example.team-skills"]) == 0 {
		t.Error("previous key dropped after a failed read")
	}
}

// Both reconcilers run Sync often; an unchanged verdict must not write again.
func TestSkillMarketplaceAggregator_UnchangedVerdictSkipsStatusWrite(t *testing.T) {
	ctx := context.Background()
	tenant := marketTenant("example", true)
	m := marketSrc(exampleNS, "team-skills", "example-org", "skills", marketplaceT0)
	c := newMarketplaceClient(t, &tenant, &m, appKeySecret(t, exampleNS))
	a := newTestMarketplaceAggregator(c)
	key := client.ObjectKeyFromObject(&m)

	if err := a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	first := getMarketplace(t, c, key).ResourceVersion
	if err := a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if got := getMarketplace(t, c, key).ResourceVersion; got != first {
		t.Errorf("resourceVersion %s -> %s, want no status write", first, got)
	}
}

var errConfigMapRefused = errors.New("configmap write refused")

type failingConfigMapWrites struct{ client.Client }

func (c failingConfigMapWrites) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if _, ok := obj.(*corev1.ConfigMap); ok {
		return errConfigMapRefused
	}
	return c.Client.Create(ctx, obj, opts...)
}

// A status must never read Aggregated before the ConfigMap holds the Marketplace.
func TestSkillMarketplaceAggregator_ConfigMapFailureLeavesStatusesUntouched(t *testing.T) {
	tenant := marketTenant("example", true)
	m := marketSrc(exampleNS, "team-skills", "example-org", "skills", marketplaceT0)
	base := newMarketplaceClient(t, &tenant, &m, appKeySecret(t, exampleNS))

	if err := newTestMarketplaceAggregator(failingConfigMapWrites{base}).Sync(context.Background()); !errors.Is(err, errConfigMapRefused) {
		t.Fatalf("Sync = %v, want the ConfigMap write error", err)
	}
	if got := getMarketplace(t, base, client.ObjectKeyFromObject(&m)); got.Status.Phase != "" {
		t.Errorf("status = %+v, want it untouched", got.Status)
	}
}

var errSecretRefused = errors.New("secret write refused")

type failingSecretWrites struct{ client.Client }

func (c failingSecretWrites) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	if _, ok := obj.(*corev1.Secret); ok {
		return errSecretRefused
	}
	return c.Client.Patch(ctx, obj, patch, opts...)
}

// While the Secret cannot be written, a removed Marketplace still leaves the
// ConfigMap and no status moves to Aggregated.
func TestSkillMarketplaceAggregator_SecretFailure(t *testing.T) {
	ctx := context.Background()
	tenant := marketTenant("example", true)
	kept := marketSrc(exampleNS, "kept", "example-org", "kept", marketplaceT0)
	removed := marketSrc(exampleNS, "removed", "example-org", "removed", marketplaceT0)
	base := newMarketplaceClient(t, &tenant, &kept, &removed, appKeySecret(t, exampleNS))
	if err := newTestMarketplaceAggregator(base).Sync(ctx); err != nil {
		t.Fatal(err)
	}

	if err := base.Delete(ctx, getMarketplace(t, base, client.ObjectKeyFromObject(&removed))); err != nil {
		t.Fatal(err)
	}
	added := marketSrc(exampleNS, "added", "example-org", "added", laterBy(marketplaceT0, 5))
	if err := base.Create(ctx, &added); err != nil {
		t.Fatal(err)
	}

	if err := newTestMarketplaceAggregator(failingSecretWrites{base}).Sync(ctx); !errors.Is(err, errSecretRefused) {
		t.Fatalf("Sync = %v, want the Secret write error", err)
	}
	if got := slugs(readMarketplaceFile(t, base)); !reflect.DeepEqual(got, map[string][]string{"example": {"kept", "added"}}) {
		t.Errorf("file = %v, want kept and added, without removed", got)
	}
	if got := getMarketplace(t, base, client.ObjectKeyFromObject(&added)); got.Status.Phase != "" {
		t.Errorf("added status = %+v, want it untouched while the Secret write fails", got.Status)
	}
}

// concurrentConfigMapWriter lets another writer update the ConfigMap between
// this writer's read and its patch.
type concurrentConfigMapWriter struct{ client.Client }

func (c concurrentConfigMapWriter) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if err := c.Client.Get(ctx, key, obj, opts...); err != nil {
		return err
	}
	if cm, ok := obj.(*corev1.ConfigMap); ok {
		other := cm.DeepCopy()
		other.Data = map[string]string{"marketplaces.json": `{"tenants":[{"id":"other","marketplaces":[]}]}`}
		return c.Client.Update(ctx, other)
	}
	return nil
}

func TestSkillMarketplaceAggregator_StaleConfigMapWriteConflicts(t *testing.T) {
	tenant := marketTenant("example", true)
	m := marketSrc(exampleNS, "team-skills", "example-org", "skills", marketplaceT0)
	existing := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "kora-skill-marketplaces", Namespace: systemNamespace},
		Data:       map[string]string{"marketplaces.json": `{"tenants":[]}`},
	}
	base := newMarketplaceClient(t, &tenant, &m, existing)

	if err := newTestMarketplaceAggregator(concurrentConfigMapWriter{base}).Sync(context.Background()); !apierrors.IsConflict(err) {
		t.Fatalf("Sync = %v, want a conflict", err)
	}
	if file := readMarketplaceFile(t, base); len(file.Tenants) != 1 || file.Tenants[0].ID != "other" {
		t.Errorf("the other writer's file was overwritten: %+v", file)
	}
}

// With nothing registered both objects still exist, so the mounts resolve.
func TestSkillMarketplaceAggregator_NoMarketplacesWritesEmptyObjects(t *testing.T) {
	c := newMarketplaceClient(t)
	if err := newTestMarketplaceAggregator(c).Sync(context.Background()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if file := readMarketplaceFile(t, c); file.Tenants == nil || len(file.Tenants) != 0 {
		t.Errorf("file = %+v, want an empty tenants list", file)
	}
	if got := readMarketplaceSecret(t, c); len(got) != 0 {
		t.Errorf("Secret = %v, want empty", got)
	}
}

// Every Marketplace not served cleanly is logged with its reason on every run.
func TestSkillMarketplaceAggregator_LogsEveryUnservedMarketplace(t *testing.T) {
	ctx := context.Background()
	tenant := marketTenant("example", true)
	served := marketSrc(exampleNS, "served", "example-org", "skills", marketplaceT0)
	dup := marketSrc(exampleNS, "dup", "example-org", "skills", laterBy(marketplaceT0, 1))
	c := newMarketplaceClient(t, &tenant, &served, &dup, appKeySecret(t, exampleNS))
	var logs bytes.Buffer
	a := NewSkillMarketplaceAggregator(c, logging.NewLoggerWithOutput(&logs))
	if err := a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	logs.Reset()
	if err := a.Sync(ctx); err != nil {
		t.Fatal(err)
	}

	var logged []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(logs.String()), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		if entry["message"] == "SkillMarketplace not served" {
			logged = append(logged, entry)
		}
	}
	if len(logged) != 1 || logged[0]["marketplace"] != "dup" || logged[0]["reason"] != MarketplaceReasonDuplicateRepository {
		t.Errorf("logged = %v, want only dup with DuplicateRepository", logged)
	}
}
