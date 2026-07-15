package devenvironments

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/orbitapi"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/token"
	jwt "github.com/golang-jwt/jwt/v5"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/clientcmd"
)

// ── fakes ────────────────────────────────────────────────────────────────

type fakeRepository struct {
	mu         sync.Mutex
	recs       map[string]*Record
	createCall int
	audit      []AuditEvent
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{recs: map[string]*Record{}}
}

func (f *fakeRepository) InsertAuditEvent(_ context.Context, e AuditEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.audit {
		if existing.EnvironmentID == e.EnvironmentID && existing.Action == e.Action && existing.OccurredAt.Equal(e.OccurredAt) {
			return nil // ON CONFLICT DO NOTHING
		}
	}
	f.audit = append(f.audit, e)
	return nil
}

func (f *fakeRepository) ListAuditEvents(_ context.Context, environmentID string) ([]AuditEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []AuditEvent
	for _, e := range f.audit {
		if e.EnvironmentID == environmentID {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeRepository) HasTerminalAuditEvent(_ context.Context, environmentID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.audit {
		if e.EnvironmentID == environmentID && (e.Action == "destroyed" || e.Action == "force_destroyed") {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeRepository) Create(_ context.Context, rec *Record) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCall++
	cp := *rec
	f.recs[rec.ID] = &cp
	return nil
}

func (f *fakeRepository) Get(_ context.Context, id string) (*Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.recs[id]
	if !ok || rec.DeletedAt != nil {
		return nil, ErrNotFound
	}
	cp := *rec
	return &cp, nil
}

func (f *fakeRepository) ListByOwner(_ context.Context, owner string) ([]Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Record
	for _, rec := range f.recs {
		if rec.Owner == owner && rec.DeletedAt == nil {
			out = append(out, *rec)
		}
	}
	return out, nil
}

func (f *fakeRepository) ListAll(_ context.Context) ([]Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Record
	for _, rec := range f.recs {
		if rec.DeletedAt == nil {
			out = append(out, *rec)
		}
	}
	return out, nil
}

func (f *fakeRepository) MarkDeleted(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.recs[id]
	if !ok || rec.DeletedAt != nil {
		return ErrNotFound
	}
	now := time.Now().UTC()
	rec.DeletedAt = &now
	return nil
}

func (f *fakeRepository) CountActiveByOwner(_ context.Context, owner string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, rec := range f.recs {
		if rec.Owner == owner && rec.DeletedAt == nil {
			count++
		}
	}
	return count, nil
}

// fakeCRClient is an in-memory CRClient. onPatchSpec, if set, runs
// synchronously inside PatchSpec — tests use it to simulate (or deliberately
// omit) the operator's own reconcile loop signing a kubeconfig CSR. Watch
// returns a watch.FakeWatcher per call; tests that need to simulate the
// operator signing asynchronously (rather than synchronously via
// onPatchSpec) can push events onto whichever watcher a test call captured.
type fakeCRClient struct {
	mu          sync.Mutex
	envs        map[string]*orbitapi.OrbitDevEnvironment
	failCreate  bool
	onPatchSpec func(*orbitapi.OrbitDevEnvironment)
	createCall  int
	watchCall   int
}

func newFakeCRClient() *fakeCRClient {
	return &fakeCRClient{envs: map[string]*orbitapi.OrbitDevEnvironment{}}
}

func (f *fakeCRClient) Create(_ context.Context, env *orbitapi.OrbitDevEnvironment) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCall++
	if f.failCreate {
		return errors.New("simulated create failure")
	}
	cp := env.DeepCopy()
	cp.Status.Phase = orbitapi.DevEnvPhasePending
	f.envs[env.Name] = cp
	return nil
}

func (f *fakeCRClient) Get(_ context.Context, id string) (*orbitapi.OrbitDevEnvironment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	env, ok := f.envs[id]
	if !ok {
		return nil, ErrNotFound
	}
	return env.DeepCopy(), nil
}

func (f *fakeCRClient) List(_ context.Context) ([]orbitapi.OrbitDevEnvironment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]orbitapi.OrbitDevEnvironment, 0, len(f.envs))
	for _, env := range f.envs {
		out = append(out, *env.DeepCopy())
	}
	return out, nil
}

func (f *fakeCRClient) PatchSpec(_ context.Context, id string, mutate func(*orbitapi.OrbitDevEnvironmentSpec)) (*orbitapi.OrbitDevEnvironment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	env, ok := f.envs[id]
	if !ok {
		return nil, ErrNotFound
	}
	mutate(&env.Spec)
	if f.onPatchSpec != nil {
		f.onPatchSpec(env)
	}
	return env.DeepCopy(), nil
}

func (f *fakeCRClient) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.envs, id)
	return nil
}

func (f *fakeCRClient) Watch(_ context.Context, _ string) (watch.Interface, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.watchCall++
	return watch.NewFake(), nil
}

func (f *fakeCRClient) WatchAll(_ context.Context) (watch.Interface, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.watchCall++
	return watch.NewFake(), nil
}

// fakeEvaluator is a minimal policy.Evaluator stub — DestroyEnvironment's
// admin-bypass check is the only thing in this package that consults an
// Evaluator. allow controls every Check call's result.
type fakeEvaluator struct {
	policy.Evaluator // embed nil; only Check is ever called in this package
	allow            bool
}

func (f *fakeEvaluator) Check(context.Context, policy.CheckInput) (policy.Result, error) {
	return policy.Result{Allow: f.allow}, nil
}

// ── test helpers ─────────────────────────────────────────────────────────

func ctxWithOwner(owner string) context.Context {
	claims := &token.PlatformClaims{RegisteredClaims: jwt.RegisteredClaims{Subject: owner}}
	return token.WithClaims(context.Background(), claims)
}

func testConfig() *Config {
	return &Config{
		Quota:      QuotaConfig{MaxActivePerOwner: 2},
		Kubeconfig: KubeconfigConfig{SignTimeoutSeconds: 2},
		APIServer:  APIServerConfig{ExternalAddress: "localhost:16443"},
	}
}

// newTestService builds a Service with a deny-all policy evaluator — every
// existing test's non-owner calls are expected to see ErrNotFound, never an
// admin bypass. Tests exercising the admin bypass itself use
// newTestServiceWithPolicy.
func newTestService(repo Repository, cr CRClient, cfg *Config) *Service {
	return newTestServiceWithPolicy(repo, cr, cfg, &fakeEvaluator{allow: false})
}

func newTestServiceWithPolicy(repo Repository, cr CRClient, cfg *Config, eval policy.Evaluator) *Service {
	if cfg == nil {
		cfg = testConfig()
	}
	return NewService(repo, cr, cfg, eval, logging.NewDevelopmentLogger())
}

// selfSignedTestCert returns a throwaway self-signed certificate PEM block,
// standing in for what the operator would sign against the vcluster CA —
// assembleKubeconfig only parses it, it never verifies it against a CA.
func selfSignedTestCert(t *testing.T, cn string) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// autoSignCSR is an onPatchSpec hook simulating a fast operator reconcile:
// as soon as spec.KubeconfigCSR changes, it immediately signs and publishes
// status, exactly like reconcileKubeconfigCSR but without a live cluster.
func autoSignCSR(t *testing.T) func(*orbitapi.OrbitDevEnvironment) {
	return func(env *orbitapi.OrbitDevEnvironment) {
		if env.Spec.KubeconfigCSR == "" {
			return
		}
		fp := orbitapi.CSRFingerprint([]byte(env.Spec.KubeconfigCSR))
		if fp == env.Status.KubeconfigCSRFingerprint {
			return
		}
		env.Status.KubeconfigCertificate = string(selfSignedTestCert(t, env.Spec.Owner))
		env.Status.KubeconfigCABundle = "fake-ca-bundle"
		env.Status.KubeconfigCSRFingerprint = fp
	}
}

// ── CreateEnvironment ────────────────────────────────────────────────────

func TestCreateEnvironment_Success(t *testing.T) {
	repo, cr := newFakeRepository(), newFakeCRClient()
	svc := newTestService(repo, cr, nil)
	ctx := ctxWithOwner("dev-alice")

	env, err := svc.CreateEnvironment(ctx, CreateRequest{Ref: "feature-branch"})
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}
	if env.Owner != "dev-alice" || env.Ref != "feature-branch" {
		t.Errorf("unexpected environment: %+v", env)
	}
	if _, err := cr.Get(ctx, env.ID); err != nil {
		t.Errorf("CR was not created: %v", err)
	}
	if _, err := repo.Get(ctx, env.ID); err != nil {
		t.Errorf("registry row was not created: %v", err)
	}
}

func TestCreateEnvironment_NoOwnerInContext(t *testing.T) {
	svc := newTestService(newFakeRepository(), newFakeCRClient(), nil)
	if _, err := svc.CreateEnvironment(context.Background(), CreateRequest{}); !errors.Is(err, ErrValidation) {
		t.Errorf("CreateEnvironment with no claims = %v, want ErrValidation", err)
	}
}

func TestCreateEnvironment_QuotaExceeded(t *testing.T) {
	repo, cr := newFakeRepository(), newFakeCRClient()
	cfg := testConfig()
	cfg.Quota.MaxActivePerOwner = 1
	svc := newTestService(repo, cr, cfg)
	ctx := ctxWithOwner("dev-bob")

	if _, err := svc.CreateEnvironment(ctx, CreateRequest{}); err != nil {
		t.Fatalf("first CreateEnvironment: %v", err)
	}
	if _, err := svc.CreateEnvironment(ctx, CreateRequest{}); !errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("second CreateEnvironment = %v, want ErrQuotaExceeded", err)
	}
}

func TestCreateEnvironment_RollsBackRegistryRowOnCRFailure(t *testing.T) {
	repo := newFakeRepository()
	cr := newFakeCRClient()
	cr.failCreate = true
	svc := newTestService(repo, cr, nil)
	ctx := ctxWithOwner("dev-carol")

	if _, err := svc.CreateEnvironment(ctx, CreateRequest{}); err == nil {
		t.Fatal("want error when CR creation fails")
	}

	active, err := repo.CountActiveByOwner(ctx, "dev-carol")
	if err != nil {
		t.Fatalf("CountActiveByOwner: %v", err)
	}
	if active != 0 {
		t.Errorf("active count after rollback = %d, want 0 (registry row must not survive a failed CR create)", active)
	}
}

// TestCreateEnvironment_RefusesWhenRunningInsideDevEnvironment guards the
// backend-enforced nested-provisioning guard: Config.DevEnvironmentID set
// means this identity service is itself running inside a dev environment's
// own vcluster, and CreateEnvironment must refuse before quota, before any
// write — zero repo/CR calls.
func TestCreateEnvironment_RefusesWhenRunningInsideDevEnvironment(t *testing.T) {
	repo, cr := newFakeRepository(), newFakeCRClient()
	cfg := testConfig()
	cfg.DevEnvironmentID = "nested-env-id"
	svc := newTestService(repo, cr, cfg)
	ctx := ctxWithOwner("dev-nested")

	if _, err := svc.CreateEnvironment(ctx, CreateRequest{}); !errors.Is(err, ErrNestedProvisioning) {
		t.Errorf("CreateEnvironment inside a dev environment = %v, want ErrNestedProvisioning", err)
	}
	if repo.createCall != 0 {
		t.Errorf("repo.Create called %d time(s), want 0 — guard must run before any write", repo.createCall)
	}
	if cr.createCall != 0 {
		t.Errorf("cr.Create called %d time(s), want 0 — guard must run before any write", cr.createCall)
	}
}

// TestCreateEnvironment_RefValidation guards normalizeRef: the symbolic
// default passes through verbatim, and a garbage ref is rejected before
// anything is created — never surfaced later as an operator-side
// non-retryable Failed environment the caller has no way to explain.
func TestCreateEnvironment_RefValidation(t *testing.T) {
	tests := []struct {
		name    string
		ref     string
		wantErr bool
	}{
		{name: "empty ref defaults to main", ref: "", wantErr: false},
		{name: "explicit main", ref: "main", wantErr: false},
		{name: "valid immutable tag", ref: "main-abc1234", wantErr: false},
		{name: "valid tag with dots and dashes", ref: "v1.2.3-rc.1", wantErr: false},
		{name: "uppercase rejected", ref: "Main-ABC1234", wantErr: true},
		{name: "spaces rejected", ref: "not a tag", wantErr: true},
		{name: "leading dash rejected", ref: "-leading-dash", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, cr := newFakeRepository(), newFakeCRClient()
			svc := newTestService(repo, cr, nil)
			_, err := svc.CreateEnvironment(ctxWithOwner("dev-ref-"+tt.name), CreateRequest{Ref: tt.ref})
			if tt.wantErr && !errors.Is(err, ErrValidation) {
				t.Errorf("ref %q: err = %v, want ErrValidation", tt.ref, err)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ref %q: unexpected error: %v", tt.ref, err)
			}
		})
	}
}

// ── GetEnvironment ───────────────────────────────────────────────────────

func TestGetEnvironment_WrongOwnerIsNotFound(t *testing.T) {
	repo, cr := newFakeRepository(), newFakeCRClient()
	svc := newTestService(repo, cr, nil)

	env, err := svc.CreateEnvironment(ctxWithOwner("dev-owner"), CreateRequest{})
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}

	if _, err := svc.GetEnvironment(ctxWithOwner("dev-someone-else"), env.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetEnvironment by non-owner = %v, want ErrNotFound", err)
	}
}

func TestGetEnvironment_CRGoneSelfHealsRegistry(t *testing.T) {
	repo, cr := newFakeRepository(), newFakeCRClient()
	svc := newTestService(repo, cr, nil)
	ctx := ctxWithOwner("dev-dana")

	env, err := svc.CreateEnvironment(ctx, CreateRequest{})
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}

	// Simulate TTL expiry: the operator deletes the CR without the registry
	// ever being told.
	if err := cr.Delete(ctx, env.ID); err != nil {
		t.Fatalf("delete CR: %v", err)
	}

	if _, err := svc.GetEnvironment(ctx, env.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetEnvironment after CR deleted = %v, want ErrNotFound", err)
	}
	if _, err := repo.Get(ctx, env.ID); !errors.Is(err, ErrNotFound) {
		t.Error("registry row was not self-healed to deleted after the CR vanished")
	}
}

// ── ExtendEnvironment ────────────────────────────────────────────────────

func TestExtendEnvironment(t *testing.T) {
	repo, cr := newFakeRepository(), newFakeCRClient()
	svc := newTestService(repo, cr, nil)
	ctx := ctxWithOwner("dev-erin")
	env, err := svc.CreateEnvironment(ctx, CreateRequest{})
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}

	if _, err := svc.ExtendEnvironment(ctx, env.ID, ExtendRequest{TTL: "not-a-duration"}); !errors.Is(err, ErrValidation) {
		t.Errorf("invalid ttl = %v, want ErrValidation", err)
	}
	if _, err := svc.ExtendEnvironment(ctx, env.ID, ExtendRequest{TTL: "200h"}); !errors.Is(err, ErrTTLExceedsHardCap) {
		t.Errorf("ttl over hard cap = %v, want ErrTTLExceedsHardCap", err)
	}

	if _, err := svc.ExtendEnvironment(ctx, env.ID, ExtendRequest{TTL: "48h"}); err != nil {
		t.Fatalf("valid extend: %v", err)
	}

	live, err := cr.Get(ctx, env.ID)
	if err != nil {
		t.Fatalf("get CR: %v", err)
	}
	if live.Spec.TTL.Duration != 48*time.Hour {
		t.Errorf("spec.ttl = %v, want 48h", live.Spec.TTL.Duration)
	}
}

// ── WakeEnvironment ──────────────────────────────────────────────────────

func TestWakeEnvironment(t *testing.T) {
	repo, cr := newFakeRepository(), newFakeCRClient()
	svc := newTestService(repo, cr, nil)
	ctx := ctxWithOwner("dev-heidi")
	env, err := svc.CreateEnvironment(ctx, CreateRequest{})
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}

	if _, err := cr.PatchSpec(ctx, env.ID, func(spec *orbitapi.OrbitDevEnvironmentSpec) {
		spec.Awake = false
	}); err != nil {
		t.Fatalf("simulate reconciler-driven sleep: %v", err)
	}

	if _, err := svc.WakeEnvironment(ctxWithOwner("someone-else"), env.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("wake by non-owner = %v, want ErrNotFound", err)
	}

	woken, err := svc.WakeEnvironment(ctx, env.ID)
	if err != nil {
		t.Fatalf("WakeEnvironment: %v", err)
	}
	if !woken.Awake {
		t.Error("Environment.Awake = false after WakeEnvironment, want true")
	}

	live, err := cr.Get(ctx, env.ID)
	if err != nil {
		t.Fatalf("get CR: %v", err)
	}
	if !live.Spec.Awake {
		t.Error("spec.awake = false after WakeEnvironment, want true")
	}
}

// ── DestroyEnvironment ───────────────────────────────────────────────────

func TestDestroyEnvironment(t *testing.T) {
	repo, cr := newFakeRepository(), newFakeCRClient()
	svc := newTestService(repo, cr, nil)
	ctx := ctxWithOwner("dev-frank")
	env, err := svc.CreateEnvironment(ctx, CreateRequest{})
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}

	if err := svc.DestroyEnvironment(ctxWithOwner("someone-else"), env.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("destroy by non-owner = %v, want ErrNotFound", err)
	}

	if err := svc.DestroyEnvironment(ctx, env.ID); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if _, err := cr.Get(ctx, env.ID); !errors.Is(err, ErrNotFound) {
		t.Error("CR still exists after destroy")
	}
	if _, err := svc.GetEnvironment(ctx, env.ID); !errors.Is(err, ErrNotFound) {
		t.Error("registry row still active after destroy")
	}

	events, err := repo.ListAuditEvents(ctx, env.ID)
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	var sawCreated, sawDestroyed bool
	for _, e := range events {
		switch e.Action {
		case "created":
			sawCreated = true
		case "destroyed":
			sawDestroyed = true
			if e.ActorType != "user" || e.Actor != "dev-frank" {
				t.Errorf("destroyed event = %+v, want actorType=user actor=dev-frank", e)
			}
			if e.Details["owner"] != "dev-frank" {
				t.Errorf("destroyed event details = %v, want owner=dev-frank", e.Details)
			}
		}
	}
	if !sawCreated || !sawDestroyed {
		t.Errorf("audit events = %+v, want both created and destroyed", events)
	}
}

// TestDestroyEnvironment_AdminBypass guards the admin force-destroy path:
// DELETE /api/v1/dev-environments/{id} is the same route for an owner's own
// destroy and an admin's force-destroy of someone else's — the branch is
// decided inside the service by consulting the policy evaluator directly.
func TestDestroyEnvironment_AdminBypass(t *testing.T) {
	repo, cr := newFakeRepository(), newFakeCRClient()
	ownerCtx := ctxWithOwner("dev-owner")
	ownerSvc := newTestService(repo, cr, nil)
	env, err := ownerSvc.CreateEnvironment(ownerCtx, CreateRequest{})
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}

	// A non-owner, non-admin caller must still be refused.
	noAdminSvc := newTestServiceWithPolicy(repo, cr, nil, &fakeEvaluator{allow: false})
	if err := noAdminSvc.DestroyEnvironment(ctxWithOwner("someone-else"), env.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("destroy by non-owner, non-admin = %v, want ErrNotFound", err)
	}
	if _, getErr := cr.Get(ownerCtx, env.ID); getErr != nil {
		t.Fatalf("CR should still exist after a refused destroy attempt: %v", getErr)
	}

	// A non-owner WITH devenvironments:admin succeeds as a force-destroy.
	adminSvc := newTestServiceWithPolicy(repo, cr, nil, &fakeEvaluator{allow: true})
	adminCtx := ctxWithOwner("admin-alice")
	if err := adminSvc.DestroyEnvironment(adminCtx, env.ID); err != nil {
		t.Fatalf("admin force-destroy: %v", err)
	}
	if _, err := cr.Get(adminCtx, env.ID); !errors.Is(err, ErrNotFound) {
		t.Error("CR still exists after admin force-destroy")
	}

	events, err := repo.ListAuditEvents(adminCtx, env.ID)
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	var found bool
	for _, e := range events {
		if e.Action != "force_destroyed" {
			continue
		}
		found = true
		if e.Actor != "admin-alice" || e.ActorType != "user" {
			t.Errorf("force_destroyed event = %+v, want actor=admin-alice actorType=user", e)
		}
		if e.Details["owner"] != "dev-owner" || e.Details["admin"] != "admin-alice" {
			t.Errorf("force_destroyed details = %v, want owner=dev-owner admin=admin-alice", e.Details)
		}
	}
	if !found {
		t.Errorf("audit events = %+v, want a force_destroyed event", events)
	}
}

// ── Kubeconfig ───────────────────────────────────────────────────────────

func TestKubeconfig_SuccessWhenOperatorSignsPromptly(t *testing.T) {
	repo, cr := newFakeRepository(), newFakeCRClient()
	cr.onPatchSpec = autoSignCSR(t)
	svc := newTestService(repo, cr, nil)
	ctx := ctxWithOwner("dev-grace")
	env, err := svc.CreateEnvironment(ctx, CreateRequest{})
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}

	resp, err := svc.Kubeconfig(ctx, env.ID)
	if err != nil {
		t.Fatalf("Kubeconfig: %v", err)
	}
	if resp.Kubeconfig == "" {
		t.Error("empty kubeconfig")
	}
	if resp.ExpiresAt.Before(time.Now()) {
		t.Error("kubeconfig ExpiresAt is already in the past")
	}
	// The CSR was already signed (synchronously, inside PatchSpec) by the
	// time waitForSignedCertificate's first Get runs — it must resolve from
	// that current-state check without ever opening a watch.
	if cr.watchCall != 0 {
		t.Errorf("Watch called %d time(s), want 0 (already signed on first Get)", cr.watchCall)
	}
}

// TestKubeconfig_TimesOutWhenNeverSigned guards the bounded single-object
// watch: with no signer ever running, waitForSignedCertificate must open a
// watch (current-state check finds nothing signed yet) and time out at
// SignTimeoutSeconds, not hang forever.
func TestKubeconfig_TimesOutWhenNeverSigned(t *testing.T) {
	repo, cr := newFakeRepository(), newFakeCRClient() // no onPatchSpec: never signs
	cfg := testConfig()
	cfg.Kubeconfig.SignTimeoutSeconds = 1
	svc := newTestService(repo, cr, cfg)
	ctx := ctxWithOwner("dev-henry")
	env, err := svc.CreateEnvironment(ctx, CreateRequest{})
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}

	if _, err := svc.Kubeconfig(ctx, env.ID); !errors.Is(err, ErrKubeconfigSigningTimedOut) {
		t.Errorf("Kubeconfig with no signer = %v, want ErrKubeconfigSigningTimedOut", err)
	}
	if cr.watchCall != 1 {
		t.Errorf("Watch called %d time(s), want exactly 1", cr.watchCall)
	}
}

func TestKubeconfig_WrongOwnerIsNotFound(t *testing.T) {
	repo, cr := newFakeRepository(), newFakeCRClient()
	cr.onPatchSpec = autoSignCSR(t)
	svc := newTestService(repo, cr, nil)
	env, err := svc.CreateEnvironment(ctxWithOwner("dev-irene"), CreateRequest{})
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}

	if _, err := svc.Kubeconfig(ctxWithOwner("someone-else"), env.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Kubeconfig by non-owner = %v, want ErrNotFound", err)
	}
}

// TestGenerateKubeconfigCSR_UsesDevEnvAdminOrganization guards decision E2:
// the minted client certificate's Subject Organization must be
// "orbit:devenv-admin" (matching the operator's ensureInVClusterDevEnvAdminBinding
// group exactly), never the built-in "system:masters" bypass-everything group.
func TestGenerateKubeconfigCSR_UsesDevEnvAdminOrganization(t *testing.T) {
	_, csrPEM, err := generateKubeconfigCSR("dev-jules")
	if err != nil {
		t.Fatalf("generateKubeconfigCSR: %v", err)
	}
	block, _ := pem.Decode(csrPEM)
	if block == nil {
		t.Fatal("csr is not valid PEM")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		t.Fatalf("parse csr: %v", err)
	}
	if !slices.Contains(csr.Subject.Organization, "orbit:devenv-admin") {
		t.Errorf("csr Subject.Organization = %v, want [orbit:devenv-admin]", csr.Subject.Organization)
	}
	if slices.Contains(csr.Subject.Organization, "system:masters") {
		t.Error("csr Subject.Organization still carries system:masters — the bypass-everything group must be gone")
	}
	if csr.Subject.CommonName != "dev-jules" {
		t.Errorf("csr Subject.CommonName = %q, want %q", csr.Subject.CommonName, "dev-jules")
	}
}

// TestAssembleKubeconfig_ServerAndSNIAreDecoupled verifies the resolved
// external-reachability design: server: is the configured, genuinely
// reachable gateway address, while the per-environment ingress host travels
// only as TLSServerName (SNI) — it is never required to be DNS-resolvable.
func TestAssembleKubeconfig_ServerAndSNIAreDecoupled(t *testing.T) {
	certPEM := selfSignedTestCert(t, "dev-jordan")
	yamlOut, _, err := assembleKubeconfig(
		"jordan-env", "localhost:16443", "jordan-env.dev.orbit.local",
		certPEM, []byte("fake-key-der-placeholder"), []byte("fake-ca-bundle"))
	if err != nil {
		t.Fatalf("assembleKubeconfig: %v", err)
	}

	cfg, err := clientcmd.Load([]byte(yamlOut))
	if err != nil {
		t.Fatalf("parse generated kubeconfig: %v", err)
	}
	cluster := cfg.Clusters[cfg.CurrentContext]
	if cluster == nil {
		t.Fatalf("no cluster for current context %q", cfg.CurrentContext)
	}
	if cluster.Server != "https://localhost:16443" {
		t.Errorf("server = %q, want the configured external address, not the ingress host", cluster.Server)
	}
	if cluster.TLSServerName != "jordan-env.dev.orbit.local" {
		t.Errorf("tls-server-name = %q, want the ingress host (SNI routing)", cluster.TLSServerName)
	}
}
