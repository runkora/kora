package operator

import (
	"context"

	"k8s.io/client-go/rest"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// HelmInterface is the subset of *HelmClient's behavior reconcilers depend on.
// Extracted so reconcilers can be unit tested against a fake implementation
// instead of a real Helm/Kubernetes backend — HelmClient itself talks to a
// live cluster via its embedded rest.Config and cannot be exercised in
// process. See docs/adr/2026-07-07-ephemeral-development-environments-via-vcluster.md,
// Implementation §Ring 1.
type HelmInterface interface {
	InstallOrUpgrade(ctx context.Context, releaseName, chartPath, repoURL, chartVersion string, values map[string]any) (*ReleaseState, error)
	Status(ctx context.Context, releaseName string) (*ReleaseState, error)
	ReleaseExists(ctx context.Context, releaseName string) (bool, error)
	Uninstall(ctx context.Context, releaseName string) error
}

var _ HelmInterface = (*HelmClient)(nil)

// HelmClientFactory constructs a HelmInterface scoped to a single namespace.
// Production wiring (cmd/operator/main.go) supplies a factory that wraps
// NewHelmClient; tests inject a factory returning a fake so reconcile logic
// can be exercised without a live cluster.
type HelmClientFactory func(namespace string) HelmInterface

// NewHelmClientFactory returns the production HelmClientFactory: each call
// constructs a real *HelmClient targeting the requested namespace.
func NewHelmClientFactory(restConfig *rest.Config, logger logging.Logger) HelmClientFactory {
	return func(namespace string) HelmInterface {
		return NewHelmClient(namespace, restConfig, logger)
	}
}
