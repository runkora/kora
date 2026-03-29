package main

import (
	"context"
	"log"
	"os"

	"github.com/go-logr/logr"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	oc "github.com/clear-route/orbit/cmd/operator/config"
	"github.com/clear-route/orbit/internal/operator"
	"github.com/clear-route/orbit/internal/platform/config"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/observability"
)

func main() {
	ctx := context.Background()

	// Initialize observability with deferred cleanup.
	shutdownObservability, err := initObservability(ctx)
	if err != nil {
		log.Fatalf("failed to setup observability: %v", err)
	}
	defer shutdownObservability()

	// Create logger.
	logger := initLogger()
	logger.Info(ctx, "orbit-operator starting",
		logging.Field{Key: "version", Value: oc.Current.App.Version},
		logging.Field{Key: "environment", Value: config.Environment()},
	)

	// Build Kubernetes REST config.
	restCfg, err := buildRestConfig(ctx, logger)
	if err != nil {
		logger.Error(ctx, "failed to build Kubernetes REST config",
			logging.Field{Key: "error", Value: err.Error()},
		)
		shutdownObservability()
		os.Exit(1) //nolint:gocritic // exitAfterDefer: explicit cleanup before exit
	}

	// Create controller-runtime manager.
	mgr, err := initManager(restCfg)
	if err != nil {
		logger.Error(ctx, "failed to create controller-runtime manager",
			logging.Field{Key: "error", Value: err.Error()},
		)
		shutdownObservability()
		os.Exit(1)
	}

	// Wire up reconcilers.
	if err := initControllers(mgr, restCfg, logger); err != nil {
		logger.Error(ctx, "failed to register controllers",
			logging.Field{Key: "error", Value: err.Error()},
		)
		shutdownObservability()
		os.Exit(1)
	}

	// Register health + readiness probes (served by controller-runtime).
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		logger.Error(ctx, "failed to register healthz check",
			logging.Field{Key: "error", Value: err.Error()},
		)
		shutdownObservability()
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		logger.Error(ctx, "failed to register readyz check",
			logging.Field{Key: "error", Value: err.Error()},
		)
		shutdownObservability()
		os.Exit(1)
	}

	logger.Info(ctx, "starting controller-runtime manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Error(ctx, "manager exited with error",
			logging.Field{Key: "error", Value: err.Error()},
		)
		shutdownObservability()
		os.Exit(1)
	}

	logger.Info(ctx, "orbit-operator shutdown complete")
}

// initObservability sets up OTEL tracing and returns a cleanup function.
func initObservability(ctx context.Context) (func(), error) {
	shutdown, err := observability.Setup(ctx, observability.Config{
		ServiceName:     oc.Current.Observability.ServiceName,
		ServiceVersion:  oc.Current.App.Version,
		Environment:     config.Environment(),
		ShutdownTimeout: oc.Current.ShutdownTimeout(),
	})
	if err != nil {
		return nil, err
	}

	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), oc.Current.ShutdownTimeout())
		defer cancel()
		if err := shutdown(shutdownCtx); err != nil {
			log.Printf("error shutting down observability: %v", err)
		}
	}, nil
}

// initLogger creates the appropriate logger based on environment.
func initLogger() logging.Logger {
	if observability.DefaultConfig.LogFormat == "console" {
		return logging.NewDevelopmentLogger()
	}
	return logging.NewLogger()
}

// buildRestConfig builds the Kubernetes REST config. Inside the cluster it
// uses the in-cluster config; outside (local dev) it falls back to KUBECONFIG.
func buildRestConfig(ctx context.Context, logger logging.Logger) (*rest.Config, error) {
	// Try in-cluster first — this is the production path (operator runs as a pod).
	cfg, err := rest.InClusterConfig()
	if err == nil {
		logger.Info(ctx, "using in-cluster Kubernetes config")
		return cfg, nil
	}

	// Fall back to kubeconfig — local dev path.
	kubeconfig := clientcmd.NewDefaultClientConfigLoadingRules().GetLoadingPrecedence()
	logger.Info(ctx, "using kubeconfig",
		logging.Field{Key: "paths", Value: kubeconfig},
	)
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		configOverrides,
	).ClientConfig()
}

// initManager creates the controller-runtime Manager with our CRD scheme and
// options derived from operator config.
func initManager(restCfg *rest.Config) (ctrl.Manager, error) {
	scheme := operator.NewScheme()

	syncPeriod := oc.Current.SyncPeriod()

	opts := ctrl.Options{
		Scheme: scheme,
		Cache: cache.Options{
			SyncPeriod: &syncPeriod,
		},
		LeaderElection:          oc.Current.Operator.LeaderElection,
		LeaderElectionNamespace: oc.Current.Operator.Namespace,
		LeaderElectionID:        "orbit-operator-leader",
		HealthProbeBindAddress:  ":8081",
	}

	// Wire a discard logr into controller-runtime so it doesn't emit the
	// "log.SetLogger was never called" warning. All meaningful logging goes
	// through our own injected logging.Logger in the reconcilers.
	ctrl.SetLogger(logr.Discard())

	return ctrl.NewManager(restCfg, opts)
}

// initControllers registers the PlatformReconciler and TenantReconciler with
// the controller-runtime Manager.
func initControllers(mgr ctrl.Manager, restCfg *rest.Config, logger logging.Logger) error {
	registry := operator.NewTenantRegistry(mgr.GetClient(), logger)

	platformReconciler := &operator.PlatformReconciler{
		Client:     mgr.GetClient(),
		Scheme:     mgr.GetScheme(),
		RestConfig: restCfg,
		Logger:     logger,
		ChartDir:   oc.Current.Operator.ChartDir,
		Registry:   registry,
	}
	if err := platformReconciler.SetupWithManager(mgr); err != nil {
		return err
	}

	tenantReconciler := &operator.TenantReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Registry: registry,
		Logger:   logger,
	}
	if err := tenantReconciler.SetupWithManager(mgr); err != nil {
		return err
	}

	return nil
}
