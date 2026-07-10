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
	"sync"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/orbitapi"
	"github.com/clear-route/orbit/internal/platform/token"
	jwt "github.com/golang-jwt/jwt/v5"
	"k8s.io/client-go/tools/clientcmd"
)

// ── fakes ────────────────────────────────────────────────────────────────

type fakeRepository struct {
	mu   sync.Mutex
	recs map[string]*Record
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{recs: map[string]*Record{}}
}

func (f *fakeRepository) Create(_ context.Context, rec *Record) error {
	f.mu.Lock()
	defer f.mu.Unlock()
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
// omit) the operator's own reconcile loop signing a kubeconfig CSR.
type fakeCRClient struct {
	mu          sync.Mutex
	envs        map[string]*orbitapi.OrbitDevEnvironment
	failCreate  bool
	onPatchSpec func(*orbitapi.OrbitDevEnvironment)
}

func newFakeCRClient() *fakeCRClient {
	return &fakeCRClient{envs: map[string]*orbitapi.OrbitDevEnvironment{}}
}

func (f *fakeCRClient) Create(_ context.Context, env *orbitapi.OrbitDevEnvironment) error {
	f.mu.Lock()
	defer f.mu.Unlock()
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

func newTestService(repo Repository, cr CRClient, cfg *Config) *Service {
	if cfg == nil {
		cfg = testConfig()
	}
	return NewService(repo, cr, cfg, logging.NewDevelopmentLogger())
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
}

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

// TestAssembleKubeconfig_ServerAndSNIAreDecoupled verifies the ADR's resolved
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
