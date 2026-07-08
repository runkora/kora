package operator

import (
	"context"
	"errors"
	"sync"
	"time"
)

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
