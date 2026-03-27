package operator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	helmaction "helm.sh/helm/v3/pkg/action"
	helmchart "helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	helmcli "helm.sh/helm/v3/pkg/cli"
	helmrelease "helm.sh/helm/v3/pkg/release"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// HelmClient wraps the Helm Go SDK action package. Each instance targets a
// single Kubernetes namespace — install/upgrade operations are scoped there.
// The client uses the in-cluster rest.Config directly so it does not depend
// on kubeconfig files or the Helm CLI environment.
type HelmClient struct {
	namespace  string
	restConfig *rest.Config
	logger     logging.Logger
}

// NewHelmClient creates a HelmClient targeting the given namespace.
func NewHelmClient(namespace string, restConfig *rest.Config, logger logging.Logger) *HelmClient {
	return &HelmClient{
		namespace:  namespace,
		restConfig: restConfig,
		logger:     logger,
	}
}

// ReleaseState describes the observed state of a Helm release.
type ReleaseState struct {
	Name         string
	Namespace    string
	Version      int
	ChartVersion string
	Status       helmrelease.Status
	Deployed     bool
}

// ErrReleaseNotFound is returned by Status and ReleaseExists when no release
// with the requested name exists in the client's namespace.
var ErrReleaseNotFound = errors.New("helm release not found")

// InstallOrUpgrade installs the chart if the release does not exist, or
// upgrades it if one is already present.
//
//   - releaseName:   Helm release name
//   - chartPath:     local filesystem path OR chart name (when repoURL is set)
//   - repoURL:       Helm repo URL; empty means chartPath is a local path
//   - chartVersion:  chart semver (only used with repoURL)
//   - values:        values to merge with chart defaults
func (h *HelmClient) InstallOrUpgrade(
	ctx context.Context,
	releaseName, chartPath, repoURL, chartVersion string,
	values map[string]any,
) (*ReleaseState, error) {
	cfg, err := h.actionConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("helm action config: %w", err)
	}

	chrt, err := h.loadChart(chartPath, repoURL, chartVersion)
	if err != nil {
		return nil, fmt.Errorf("load chart %q: %w", chartPath, err)
	}

	existing, err := getRelease(cfg, releaseName)
	if err != nil && !errors.Is(err, ErrReleaseNotFound) {
		return nil, fmt.Errorf("get release %q: %w", releaseName, err)
	}

	if existing == nil {
		return h.install(ctx, cfg, releaseName, chrt, values)
	}

	// Recover from stuck pending states left by a previous interrupted
	// install/upgrade (e.g. Helm waited for a LoadBalancer IP that never came).
	// Uninstall clears the stale Helm secret so we can do a clean install.
	state := releaseToState(existing)
	if state.Status == helmrelease.StatusPendingInstall ||
		state.Status == helmrelease.StatusPendingUpgrade ||
		state.Status == helmrelease.StatusPendingRollback {
		h.logger.Info(ctx, "helm release stuck in pending state, cleaning up before reinstall",
			logging.Field{Key: "release", Value: releaseName},
			logging.Field{Key: "status", Value: string(state.Status)},
		)
		if err := h.Uninstall(ctx, releaseName); err != nil {
			return nil, fmt.Errorf("cleanup stuck release %q: %w", releaseName, err)
		}
		return h.install(ctx, cfg, releaseName, chrt, values)
	}

	// Skip upgrade when the release is already deployed at exactly the target
	// chart version. This prevents SPIRE/Istio pre/post-upgrade hooks from
	// running on every reconcile loop and stops the install-loop cascade.
	// For local charts (chartVersion == "") we always upgrade so that local
	// edits are picked up immediately.
	if state.Deployed && chrt.Metadata.Version != "" && state.ChartVersion == chrt.Metadata.Version {
		h.logger.Debug(ctx, "helm release already at target version, skipping upgrade",
			logging.Field{Key: "release", Value: releaseName},
			logging.Field{Key: "version", Value: chrt.Metadata.Version},
		)
		return state, nil
	}

	return h.upgrade(ctx, cfg, releaseName, chrt, values)
}

// Status returns the current state of a named release.
// Returns ErrReleaseNotFound if the release does not exist.
func (h *HelmClient) Status(ctx context.Context, releaseName string) (*ReleaseState, error) {
	cfg, err := h.actionConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("helm action config: %w", err)
	}
	rel, err := getRelease(cfg, releaseName)
	if err != nil {
		return nil, err
	}
	return releaseToState(rel), nil
}

// ReleaseExists returns true when a release with the given name exists.
func (h *HelmClient) ReleaseExists(ctx context.Context, releaseName string) (bool, error) {
	_, err := h.Status(ctx, releaseName)
	if errors.Is(err, ErrReleaseNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// Uninstall removes a named release. Returns nil when the release is absent.
func (h *HelmClient) Uninstall(ctx context.Context, releaseName string) error {
	cfg, err := h.actionConfig(ctx)
	if err != nil {
		return fmt.Errorf("helm action config: %w", err)
	}
	act := helmaction.NewUninstall(cfg)
	act.Timeout = 5 * time.Minute
	if _, err := act.Run(releaseName); err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("uninstall release %q: %w", releaseName, err)
	}
	return nil
}

// -----------------------------------------------------------------------------
// private helpers
// -----------------------------------------------------------------------------

// actionConfig builds the Helm action.Configuration that connects to the
// cluster via the operator's in-cluster rest.Config.
func (h *HelmClient) actionConfig(ctx context.Context) (*helmaction.Configuration, error) {
	getter := &restConfigGetter{cfg: h.restConfig, namespace: h.namespace}
	actionCfg := new(helmaction.Configuration)
	logFn := func(format string, v ...any) {
		h.logger.Debug(ctx, fmt.Sprintf(format, v...))
	}
	if err := actionCfg.Init(getter, h.namespace, "secret", logFn); err != nil {
		return nil, fmt.Errorf("init helm action config for namespace %q: %w", h.namespace, err)
	}
	return actionCfg, nil
}

func (h *HelmClient) loadChart(chartPath, repoURL, version string) (*helmchart.Chart, error) {
	if repoURL == "" {
		// Local path — used in local dev where charts/ directory is present.
		chrt, err := loader.Load(chartPath)
		if err != nil {
			return nil, fmt.Errorf("load chart from path %q: %w", chartPath, err)
		}
		return chrt, nil
	}
	return h.pullChart(chartPath, repoURL, version)
}

// pullChart downloads a chart from a remote Helm/OCI repository into a
// local cache directory, then loads it.
func (h *HelmClient) pullChart(chartName, repoURL, version string) (*helmchart.Chart, error) {
	cacheDir := helmChartCacheDir()
	if err := os.MkdirAll(cacheDir, 0o750); err != nil {
		return nil, fmt.Errorf("create chart cache dir %q: %w", cacheDir, err)
	}

	settings := helmcli.New()
	settings.RepositoryCache = cacheDir

	pull := helmaction.NewPullWithOpts(helmaction.WithConfig(&helmaction.Configuration{}))
	pull.RepoURL = repoURL
	pull.Version = version
	pull.Settings = settings
	pull.DestDir = cacheDir
	pull.Untar = false

	if _, err := pull.Run(chartName); err != nil {
		return nil, fmt.Errorf("pull chart %q@%s: %w", chartName, version, err)
	}

	// Find the downloaded archive.
	pattern := filepath.Join(cacheDir, fmt.Sprintf("%s-*.tgz", chartName))
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("glob chart archive: %w", err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no chart archive found matching %q after pull", pattern)
	}

	chrt, err := loader.Load(matches[0])
	if err != nil {
		return nil, fmt.Errorf("load chart archive %q: %w", matches[0], err)
	}
	return chrt, nil
}

func (h *HelmClient) install(
	ctx context.Context,
	cfg *helmaction.Configuration,
	releaseName string,
	chrt *helmchart.Chart,
	values map[string]any,
) (*ReleaseState, error) {
	act := helmaction.NewInstall(cfg)
	act.ReleaseName = releaseName
	act.Namespace = h.namespace
	act.CreateNamespace = true
	act.Timeout = 10 * time.Minute
	act.Wait = true

	h.logger.Info(ctx, "installing helm release",
		logging.Field{Key: "release", Value: releaseName},
		logging.Field{Key: "namespace", Value: h.namespace},
	)

	rel, err := act.Run(chrt, values)
	if err != nil {
		return nil, fmt.Errorf("install release %q: %w", releaseName, err)
	}
	return releaseToState(rel), nil
}

func (h *HelmClient) upgrade(
	ctx context.Context,
	cfg *helmaction.Configuration,
	releaseName string,
	chrt *helmchart.Chart,
	values map[string]any,
) (*ReleaseState, error) {
	act := helmaction.NewUpgrade(cfg)
	act.Namespace = h.namespace
	act.Timeout = 10 * time.Minute
	act.Wait = true
	act.ReuseValues = false

	h.logger.Info(ctx, "upgrading helm release",
		logging.Field{Key: "release", Value: releaseName},
		logging.Field{Key: "namespace", Value: h.namespace},
	)

	rel, err := act.Run(releaseName, chrt, values)
	if err != nil {
		return nil, fmt.Errorf("upgrade release %q: %w", releaseName, err)
	}
	return releaseToState(rel), nil
}

func getRelease(cfg *helmaction.Configuration, name string) (*helmrelease.Release, error) {
	act := helmaction.NewGet(cfg)
	rel, err := act.Run(name)
	if err != nil {
		if isNotFound(err) {
			return nil, ErrReleaseNotFound
		}
		return nil, fmt.Errorf("get release %q: %w", name, err)
	}
	return rel, nil
}

func releaseToState(rel *helmrelease.Release) *ReleaseState {
	chartVer := ""
	if rel.Chart != nil && rel.Chart.Metadata != nil {
		chartVer = rel.Chart.Metadata.Version
	}
	return &ReleaseState{
		Name:         rel.Name,
		Namespace:    rel.Namespace,
		Version:      rel.Version,
		ChartVersion: chartVer,
		Status:       rel.Info.Status,
		Deployed:     rel.Info.Status == helmrelease.StatusDeployed,
	}
}

func isNotFound(err error) bool {
	return err != nil && err.Error() == "release: not found"
}

// helmChartCacheDir is the directory used to cache pulled chart archives.
// The operator Deployment mounts an emptyDir at /tmp so this is always writable.
func helmChartCacheDir() string {
	return "/tmp/orbit-helm-cache"
}

// -----------------------------------------------------------------------------
// restConfigGetter — adapts rest.Config to Helm's RESTClientGetter interface
// -----------------------------------------------------------------------------

// restConfigGetter implements genericclioptions.RESTClientGetter using an
// in-cluster rest.Config. This allows helm's action.Configuration to use the
// operator's service-account credentials rather than a kubeconfig file.
type restConfigGetter struct {
	cfg       *rest.Config
	namespace string
}

func (r *restConfigGetter) ToRESTConfig() (*rest.Config, error) {
	return r.cfg, nil
}

func (r *restConfigGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	dc, err := discovery.NewDiscoveryClientForConfig(r.cfg)
	if err != nil {
		return nil, fmt.Errorf("discovery client: %w", err)
	}
	return memory.NewMemCacheClient(dc), nil
}

func (r *restConfigGetter) ToRESTMapper() (meta.RESTMapper, error) {
	dc, err := r.ToDiscoveryClient()
	if err != nil {
		return nil, err
	}
	return restmapper.NewDeferredDiscoveryRESTMapper(dc), nil
}

// ToRawKubeConfigLoader returns a ClientConfig that supplies the namespace to
// Helm's kubeClient. We embed a minimal in-memory kubeconfig built from the
// rest.Config rather than reading any file on disk.
func (r *restConfigGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	kubeConfig := clientcmdapi.NewConfig()
	kubeConfig.Clusters["default"] = &clientcmdapi.Cluster{
		Server:                   r.cfg.Host,
		CertificateAuthorityData: r.cfg.CAData,
	}
	kubeConfig.AuthInfos["default"] = &clientcmdapi.AuthInfo{
		Token:     r.cfg.BearerToken,
		TokenFile: r.cfg.BearerTokenFile,
	}
	kubeConfig.Contexts["default"] = &clientcmdapi.Context{
		Cluster:   "default",
		AuthInfo:  "default",
		Namespace: r.namespace,
	}
	kubeConfig.CurrentContext = "default"

	return clientcmd.NewDefaultClientConfig(
		*kubeConfig,
		&clientcmd.ConfigOverrides{},
	)
}
