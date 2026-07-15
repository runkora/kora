package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	oc "github.com/clear-route/orbit/cmd/operator/config"
	"github.com/clear-route/orbit/internal/operator"
	"github.com/clear-route/orbit/internal/platform/config"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/observability"
)

// devEnvMeterName scopes every OrbitDevEnvironment OTel instrument
// (orbit_devenv_*) under one meter name, matching the ADR §10 metrics table.
const devEnvMeterName = "github.com/clear-route/orbit/internal/operator/devenvironment"

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
	mgr, err := initManager(restCfg, logger)
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

	// Wire up admission webhooks.
	initWebhooks(mgr)

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

// initObservability sets up OTEL tracing and metrics and returns a cleanup
// function that shuts both down.
func initObservability(ctx context.Context) (func(), error) {
	obsCfg := observability.Config{
		ServiceName:     oc.Current.Observability.ServiceName,
		ServiceVersion:  oc.Current.App.Version,
		Environment:     config.Environment(),
		ShutdownTimeout: oc.Current.ShutdownTimeout(),
	}

	shutdownTracing, err := observability.Setup(ctx, obsCfg)
	if err != nil {
		return nil, err
	}
	shutdownMetrics, err := observability.SetupMetrics(ctx, obsCfg)
	if err != nil {
		return nil, err
	}

	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), oc.Current.ShutdownTimeout())
		defer cancel()
		if err := shutdownTracing(shutdownCtx); err != nil {
			log.Printf("error shutting down tracing: %v", err)
		}
		if err := shutdownMetrics(shutdownCtx); err != nil {
			log.Printf("error shutting down metrics: %v", err)
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
func initManager(restCfg *rest.Config, logger logging.Logger) (ctrl.Manager, error) {
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
		WebhookServer: webhook.NewServer(webhook.Options{
			Port:    9443,
			CertDir: oc.Current.Operator.WebhookCertDir,
		}),
	}

	// Wire controller-runtime's own internal logging (leader election,
	// webhook server, cache sync) through our injected logging.Logger instead
	// of discarding it — logr.Discard() previously hid these entirely, with
	// no operator-side signal when e.g. leader election or cache sync failed.
	ctrl.SetLogger(logr.New(newCRLogSink(logger)))

	return ctrl.NewManager(restCfg, opts)
}

// initWebhooks registers the admission webhook handlers with the manager's
// built-in webhook server (port 9443, TLS from WebhookCertDir).
func initWebhooks(mgr ctrl.Manager) {
	decoder := admission.NewDecoder(mgr.GetScheme())
	mgr.GetWebhookServer().Register("/mutate-pod", &webhook.Admission{
		Handler: &operator.PodMutator{
			Client:  mgr.GetClient(),
			Decoder: decoder,
		},
	})
}

// initControllers registers the PlatformReconciler and TenantReconciler with
// the controller-runtime Manager.
func initControllers(mgr ctrl.Manager, restCfg *rest.Config, logger logging.Logger) error {
	registry := operator.NewTenantRegistry(mgr.GetClient(), logger)
	aggregator := operator.NewClientSecretAggregator(mgr.GetClient(), logger)
	caAggregator := operator.NewTenantCAAggregator(mgr.GetClient(), logger)
	connAggregator := operator.NewConnectionAggregator(mgr.GetClient(), logger)
	mcpAggregator := operator.NewMCPServerAggregator(mgr.GetClient(), logger)

	platformReconciler := &operator.PlatformReconciler{
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		RestConfig:  restCfg,
		Logger:      logger,
		ChartDir:    oc.Current.Operator.ChartDir,
		Registry:    registry,
		OPAImage:    oc.Current.Operator.OPAImage,
		OPALogLevel: oc.Current.Operator.OPALogLevel,
	}
	if err := platformReconciler.SetupWithManager(mgr); err != nil {
		return err
	}

	tenantReconciler := &operator.TenantReconciler{
		Client:                mgr.GetClient(),
		Scheme:                mgr.GetScheme(),
		Registry:              registry,
		Aggregator:            aggregator,
		CAAggregator:          caAggregator,
		ConnAggregator:        connAggregator,
		MCPAggregator:         mcpAggregator,
		Logger:                logger,
		BundlePollingMinDelay: oc.Current.Operator.BundlePollingMinDelay,
		BundlePollingMaxDelay: oc.Current.Operator.BundlePollingMaxDelay,
	}
	if err := tenantReconciler.SetupWithManager(mgr); err != nil {
		return err
	}

	mcpServerReconciler := &operator.MCPServerReconciler{
		Client:     mgr.GetClient(),
		Scheme:     mgr.GetScheme(),
		Aggregator: mcpAggregator,
		Logger:     logger,
	}
	if err := mcpServerReconciler.SetupWithManager(mgr); err != nil {
		return err
	}

	devEnvReconciler, err := newDevEnvironmentReconciler(mgr, restCfg, logger)
	if err != nil {
		return err
	}
	if err := devEnvReconciler.SetupWithManager(mgr); err != nil {
		return err
	}
	if err := operator.RegisterDevEnvironmentsGauge(otel.Meter(devEnvMeterName), mgr.GetClient()); err != nil {
		return err
	}

	return nil
}

// newDevEnvironmentReconciler builds the DevEnvironmentReconciler with its
// production factories/instruments wired from operator config and the
// global OTel meter provider (set up by initObservability's SetupMetrics).
func newDevEnvironmentReconciler(mgr ctrl.Manager, restCfg *rest.Config, logger logging.Logger) (*operator.DevEnvironmentReconciler, error) {
	meter := otel.Meter(devEnvMeterName)

	provisionStageDuration, err := meter.Float64Histogram(
		"orbit_devenv_provision_stage_duration_seconds",
		metric.WithDescription("Duration of each named OrbitDevEnvironment reconcile stage"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("create orbit_devenv_provision_stage_duration_seconds histogram: %w", err)
	}
	reconcileFailuresTotal, err := meter.Int64Counter(
		"orbit_devenv_reconcile_failures_total",
		metric.WithDescription("Count of OrbitDevEnvironment reconcile-stage failures, by stage"),
	)
	if err != nil {
		return nil, fmt.Errorf("create orbit_devenv_reconcile_failures_total counter: %w", err)
	}
	wakeDuration, err := meter.Float64Histogram(
		"orbit_devenv_wake_duration_seconds",
		metric.WithDescription("Time from an explicit OrbitDevEnvironment wake to phase Ready"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("create orbit_devenv_wake_duration_seconds histogram: %w", err)
	}

	tolerations := make([]corev1.Toleration, 0, len(oc.Current.Operator.DevEnvTolerations))
	for _, t := range oc.Current.Operator.DevEnvTolerations {
		tolerations = append(tolerations, corev1.Toleration{
			Key:               t.Key,
			Operator:          corev1.TolerationOperator(t.Operator),
			Value:             t.Value,
			Effect:            corev1.TaintEffect(t.Effect),
			TolerationSeconds: t.TolerationSeconds,
		})
	}

	scheme := mgr.GetScheme()
	client := mgr.GetClient()

	return &operator.DevEnvironmentReconciler{
		Client:          client,
		Scheme:          scheme,
		Logger:          logger,
		RestConfig:      restCfg,
		HostJWKSFetcher: operator.NewHostJWKSFetcher(restCfg),
		HelmFactory:     operator.NewHelmClientFactory(restCfg, logger),
		Clock:           operator.NewRealClock(),
		IngressDomain:   oc.Current.Operator.DevEnvIngressDomain,
		AppExternalURL:  oc.Current.Operator.DevEnvAppExternalURL,
		VClusterVersion: oc.Current.Operator.DevEnvVClusterVersion,
		ChartDir:        oc.Current.Operator.ChartDir,
		ImageRegistry:   oc.Current.Operator.DevEnvImageRegistry,
		SelfImageTag:    oc.Current.Operator.DevEnvSelfImageTag,
		Quota: operator.DevEnvQuota{
			CPURequests:    oc.Current.Operator.DevEnvQuota.CPURequests,
			MemoryRequests: oc.Current.Operator.DevEnvQuota.MemoryRequests,
			CPULimits:      oc.Current.Operator.DevEnvQuota.CPULimits,
			MemoryLimits:   oc.Current.Operator.DevEnvQuota.MemoryLimits,
			MaxPods:        oc.Current.Operator.DevEnvQuota.MaxPods,
		},
		LimitRange: operator.DevEnvLimitRange{
			DefaultCPURequest:    oc.Current.Operator.DevEnvLimitRange.DefaultCPURequest,
			DefaultMemoryRequest: oc.Current.Operator.DevEnvLimitRange.DefaultMemoryRequest,
			DefaultCPULimit:      oc.Current.Operator.DevEnvLimitRange.DefaultCPULimit,
			DefaultMemoryLimit:   oc.Current.Operator.DevEnvLimitRange.DefaultMemoryLimit,
		},
		NodeSelector:              oc.Current.Operator.DevEnvNodeSelector,
		Tolerations:               tolerations,
		VClusterRestConfigFactory: operator.NewVClusterRestConfigFactory(client),
		VClusterClientFactory:     operator.NewVClusterClientFactory(scheme),
		InVClusterHelmFactory: func(namespace string, vcRestConfig *rest.Config) operator.HelmInterface {
			return operator.NewHelmClient(namespace, vcRestConfig, logger)
		},
		ProvisionStageDuration: provisionStageDuration,
		ReconcileFailuresTotal: reconcileFailuresTotal,
		WakeDuration:           wakeDuration,
	}, nil
}
