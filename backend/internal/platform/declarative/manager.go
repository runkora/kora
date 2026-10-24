package declarative

import (
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/runkora/kora/backend/internal/platform/declarative/v1alpha1"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

const (
	// ResyncPeriod is how often every cached resource is reconciled again.
	ResyncPeriod = 5 * time.Minute
	// RetryUnresolved is how soon a resource with a missing reference or an
	// unknown tenant is reconciled again.
	RetryUnresolved = 30 * time.Second
)

// ErrManagerOptions is returned when ManagerOptions lacks a required field.
var ErrManagerOptions = errors.New("declarative manager options")

// ManagerOptions configures NewManager.
type ManagerOptions struct {
	// Service names the Lease <Service>-declarative.
	Service string
	// SystemNamespace holds the Lease.
	SystemNamespace string
	// Namespaces limits the cache to these namespaces. Empty caches every
	// namespace.
	Namespaces []string
	Logger     logging.Logger
}

// NewManager builds a service's controller-runtime manager for its
// declarative kinds. Only the Lease holder runs its controllers. It sets
// controller-runtime's global logger to opts.Logger. The scheme
// knows the core kinds and the declarative kinds. Secrets are never cached:
// the client reads them from the API server, as does mgr.GetAPIReader(). The
// manager serves no metrics or health endpoints of its own.
func NewManager(cfg *rest.Config, opts ManagerOptions) (ctrl.Manager, error) {
	o, err := managerOptions(opts)
	if err != nil {
		return nil, err
	}
	// Sources and handlers log through controller-runtime's global logger.
	ctrl.SetLogger(o.Logger)
	mgr, err := ctrl.NewManager(cfg, o)
	if err != nil {
		return nil, fmt.Errorf("create %s declarative manager: %w", opts.Service, err)
	}
	return mgr, nil
}

func managerOptions(opts ManagerOptions) (ctrl.Options, error) {
	if opts.Service == "" || opts.SystemNamespace == "" || opts.Logger == nil {
		return ctrl.Options{}, fmt.Errorf("%w: Service, SystemNamespace and Logger are required", ErrManagerOptions)
	}
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return ctrl.Options{}, fmt.Errorf("register core types: %w", err)
	}
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		return ctrl.Options{}, fmt.Errorf("register declarative types: %w", err)
	}
	resync := ResyncPeriod
	cacheOpts := cache.Options{SyncPeriod: &resync}
	if len(opts.Namespaces) > 0 {
		cacheOpts.DefaultNamespaces = map[string]cache.Config{}
		for _, ns := range opts.Namespaces {
			cacheOpts.DefaultNamespaces[ns] = cache.Config{}
		}
	}
	return ctrl.Options{
		Scheme: scheme,
		Logger: logr.New(logging.NewLogrSink(opts.Logger)),
		Cache:  cacheOpts,
		Client: client.Options{Cache: &client.CacheOptions{DisableFor: []client.Object{&corev1.Secret{}}}},
		// The service already serves metrics; "0" turns off the manager's listener.
		Metrics:                       metricsserver.Options{BindAddress: "0"},
		LeaderElection:                true,
		LeaderElectionID:              opts.Service + "-declarative",
		LeaderElectionNamespace:       opts.SystemNamespace,
		LeaderElectionReleaseOnCancel: true,
	}, nil
}
