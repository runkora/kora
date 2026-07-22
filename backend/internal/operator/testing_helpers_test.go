package operator

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// newTestEnv builds a minimal, valid OrbitDevEnvironment fixture shared
// across this package's test files (devenvironment_csr_test.go and,
// eventually, devenvironment_controller_test.go — do not redefine this
// there; reuse this one). mutate, if non-nil, is applied after the
// defaults below are set.
func newTestEnv(name string, mutate func(*OrbitDevEnvironment)) *OrbitDevEnvironment {
	env := &OrbitDevEnvironment{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			CreationTimestamp: metav1.Now(),
		},
		Spec: OrbitDevEnvironmentSpec{
			Owner: "dev@example.com",
			Awake: true,
			TTL:   metav1.Duration{Duration: 24 * time.Hour},
		},
	}
	if mutate != nil {
		mutate(env)
	}
	return env
}

// fakeClock is a controllable Clock for tests. Zero value is usable; Set/Advance
// mutate the current time under a lock so it's safe from concurrent reconciles.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock(start time.Time) *fakeClock {
	return &fakeClock{now: start}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// fakeHelm is a HelmInterface fake for tests. Keyed by release name; every
// method call is recorded so tests can assert on install/uninstall counts
// without a real Helm/Kubernetes backend.
type fakeHelm struct {
	mu sync.Mutex

	releases map[string]*ReleaseState

	// InstallOrUpgradeErr, when non-nil, is returned by every InstallOrUpgrade call.
	InstallOrUpgradeErr error
	// UninstallErr, when non-nil, is returned by every Uninstall call.
	UninstallErr error

	InstallOrUpgradeCalls int
	UninstallCalls        int
}

func newFakeHelm() *fakeHelm {
	return &fakeHelm{releases: make(map[string]*ReleaseState)}
}

func (f *fakeHelm) InstallOrUpgrade(_ context.Context, releaseName, _, _, chartVersion string, _ map[string]any) (*ReleaseState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.InstallOrUpgradeCalls++
	if f.InstallOrUpgradeErr != nil {
		return nil, f.InstallOrUpgradeErr
	}
	state := &ReleaseState{
		Name:         releaseName,
		ChartVersion: chartVersion,
		Deployed:     true,
	}
	f.releases[releaseName] = state
	return state, nil
}

func (f *fakeHelm) Status(_ context.Context, releaseName string) (*ReleaseState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	state, ok := f.releases[releaseName]
	if !ok {
		return nil, ErrReleaseNotFound
	}
	return state, nil
}

func (f *fakeHelm) ReleaseExists(ctx context.Context, releaseName string) (bool, error) {
	_, err := f.Status(ctx, releaseName)
	if errors.Is(err, ErrReleaseNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (f *fakeHelm) Uninstall(_ context.Context, releaseName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.UninstallCalls++
	if f.UninstallErr != nil {
		return f.UninstallErr
	}
	delete(f.releases, releaseName)
	return nil
}

var _ HelmInterface = (*fakeHelm)(nil)

// newDevEnvTestReconciler is newDevEnvTestReconcilerFull without the two
// in-vcluster-install seams most tests never need to assert on directly.
func newDevEnvTestReconciler(t *testing.T, clock *fakeClock, objs ...client.Object) (*DevEnvironmentReconciler, *fakeHelm) {
	t.Helper()
	r, helm, _, _ := newDevEnvTestReconcilerFull(t, clock, objs...)
	return r, helm
}

// newDevEnvTestReconcilerFull builds a DevEnvironmentReconciler wired
// entirely to fakes — no real cluster, Helm, or registry — for tests that
// drive a full Reconcile() pass through to a Ready environment. Returns the
// reconciler, the fake HOST-side Helm client (vcluster release), the fake
// vcluster API server client, and the fake in-vcluster Helm client
// (orbit-operator release installed inside the vcluster).
//
// Pre-seeds the vcluster's own fake client with the in-vcluster
// OrbitPlatform already Ready: most tests are about the OUTER
// OrbitDevEnvironment's own lifecycle (namespace/vcluster/sleep/wake/TTL/
// CSR), not about waiting on the nested install to converge, and
// reconcileInVClusterPlatform's "existing found" branch only ever patches
// .Spec, so this seeded .Status survives every reconcile untouched. Tests
// that specifically exercise the platform-readiness gate should build their
// own vcClient instead of calling this helper.
func newDevEnvTestReconcilerFull(t *testing.T, clock *fakeClock, objs ...client.Object) (*DevEnvironmentReconciler, *fakeHelm, client.Client, *fakeHelm) {
	t.Helper()
	scheme := NewScheme()
	helm := newFakeHelm()
	inVClusterHelm := newFakeHelm()

	hostPlatform := &OrbitPlatform{
		ObjectMeta: metav1.ObjectMeta{Name: "orbit"},
		Spec:       OrbitPlatformSpec{TrustDomain: "orbit.test"},
	}
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&OrbitDevEnvironment{}).
		WithObjects(append(objs, hostPlatform)...).
		Build()

	// The vcluster client is a second, independent fake — standing in for
	// the vcluster's own API server, which is why applying
	// OrbitPlatform/OrbitTenant here doesn't affect the host fake client c
	// above.
	vcClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(&OrbitPlatform{
			ObjectMeta: metav1.ObjectMeta{Name: inVClusterOrbitPlatformName},
			Status:     OrbitPlatformStatus{Phase: PhaseReady},
		}).
		Build()

	r := &DevEnvironmentReconciler{
		Client:          c,
		Scheme:          scheme,
		Logger:          logging.NewLoggerWithOutput(io.Discard),
		HelmFactory:     func(string) HelmInterface { return helm },
		Clock:           clock,
		IngressDomain:   "dev.orbit.test",
		AppExternalURL:  "http://devenv.orbit.test:8880",
		VClusterVersion: "0.35.1",
		ChartDir:        "/etc/orbit/charts",
		ImageRegistry:   "orbit-registry:5001",
		SelfImageTag:    "main-abc1234",
		Quota: DevEnvQuota{
			CPURequests: "6", MemoryRequests: "12Gi",
			CPULimits: "48", MemoryLimits: "32Gi", MaxPods: "60",
		},
		LimitRange: DevEnvLimitRange{
			DefaultCPURequest: "100m", DefaultMemoryRequest: "128Mi",
			DefaultCPULimit: "2", DefaultMemoryLimit: "3Gi",
		},
		VClusterRestConfigFactory: func(context.Context, string) (*rest.Config, error) {
			return &rest.Config{Host: "https://vcluster.fake"}, nil
		},
		VClusterClientFactory: func(*rest.Config) (client.Client, error) { return vcClient, nil },
		InVClusterHelmFactory: func(string, *rest.Config) HelmInterface { return inVClusterHelm },
		HostJWKSFetcher: func(context.Context) (string, string, error) {
			return "https://kubernetes.default.svc.cluster.local", `{"keys":[]}`, nil
		},
	}
	return r, helm, vcClient, inVClusterHelm
}

// reconcileOnce drives exactly one Reconcile pass for name and fails the
// test on any returned error.
func reconcileOnce(t *testing.T, r *DevEnvironmentReconciler, name string) ctrl.Result {
	t.Helper()
	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: name}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	return res
}

// getEnv fetches the current state of the named OrbitDevEnvironment,
// failing the test if it cannot be read.
func getEnv(t *testing.T, r *DevEnvironmentReconciler, name string) *OrbitDevEnvironment {
	t.Helper()
	env := &OrbitDevEnvironment{}
	if err := r.Get(context.Background(), types.NamespacedName{Name: name}, env); err != nil {
		t.Fatalf("get env %q: %v", name, err)
	}
	return env
}
