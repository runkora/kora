package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	oc "github.com/clear-route/orbit/cmd/operator/config"
	"github.com/clear-route/orbit/internal/operator"
	"github.com/clear-route/orbit/internal/operator/preflight"
	"github.com/clear-route/orbit/internal/operator/webhookcert"
	"github.com/clear-route/orbit/internal/platform/config"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/observability"
)

const (
	webhookServiceName = "orbit-operator-webhook"
	spiffeSocket       = "unix:///run/secrets/workload-spiffe-uds/socket"
)

func main() {
	ctx := context.Background()

	shutdownObservability, err := initObservability(ctx)
	if err != nil {
		log.Fatalf("failed to setup observability: %v", err)
	}
	defer shutdownObservability()

	logger := initLogger()

	for _, parseErr := range oc.ResourceParseErrors {
		logger.Warn(ctx, "malformed resource requirements env var; using empty requirements",
			logging.Field{Key: "error", Value: parseErr.Error()})
	}

	operator.Apply(operator.Settings{
		SystemNamespace: oc.Current.Install.SystemNamespace,
		IstioNamespace:  oc.Current.Install.IstioNamespace,
		TrustDomain:     oc.Current.Install.TrustDomain,
	})

	restCfg, err := buildRestConfig(ctx, logger)
	if err != nil {
		logger.Error(ctx, "failed to build Kubernetes REST config", logging.Field{Key: "error", Value: err.Error()})
		shutdownObservability()
		os.Exit(1) //nolint:gocritic // exitAfterDefer: explicit cleanup before exit
	}

	// `orbit preflight` runs the prerequisite checks once and exits non-zero on
	// failure; the chart's helm test uses it.
	if len(os.Args) > 1 && os.Args[1] == "preflight" {
		code := runPreflightOnce(ctx, restCfg, logger)
		shutdownObservability()
		os.Exit(code)
	}

	logger.Info(ctx, "orbit-operator starting",
		logging.Field{Key: "version", Value: oc.Current.App.Version},
		logging.Field{Key: "environment", Value: config.Environment()},
		logging.Field{Key: "systemNamespace", Value: oc.Current.Install.SystemNamespace},
	)

	if err := run(ctx, restCfg, logger); err != nil {
		logger.Error(ctx, "orbit-operator exited with error", logging.Field{Key: "error", Value: err.Error()})
		shutdownObservability()
		os.Exit(1)
	}
	logger.Info(ctx, "orbit-operator shutdown complete")
}

func run(ctx context.Context, restCfg *rest.Config, logger logging.Logger) error {
	install := oc.Current.Install

	// controller-runtime logs Reconcile errors and webhook TLS failures itself;
	// route them through the injected logger instead of discarding them.
	ctrl.SetLogger(logr.New(operator.NewLogSink(logger)))

	mgr, err := initManager(restCfg)
	if err != nil {
		return fmt.Errorf("create manager: %w", err)
	}

	// Use a direct client before the manager cache starts.
	direct, err := client.New(restCfg, client.Options{Scheme: mgr.GetScheme()})
	if err != nil {
		return fmt.Errorf("create bootstrap client: %w", err)
	}
	certs := &webhookcert.Manager{
		Client:      direct,
		Logger:      logger,
		Namespace:   install.SystemNamespace,
		ServiceName: webhookServiceName,
		CertDir:     oc.Current.Operator.WebhookCertDir,
		External:    install.WebhookCertManager,
	}
	if err := certs.Bootstrap(ctx); err != nil {
		return fmt.Errorf("bootstrap webhook certificate: %w", err)
	}
	if err := mgr.Add(&webhookcert.Refresher{Manager: certs, Interval: 5 * time.Minute}); err != nil {
		return err
	}
	if err := mgr.Add(&webhookcert.Rotator{Manager: certs, Interval: time.Hour}); err != nil {
		return err
	}
	if err := (&webhookcert.DriftReconciler{Manager: certs}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("register webhook drift reconciler: %w", err)
	}

	checks := &preflight.Runner{Client: mgr.GetAPIReader(), Logger: logger, Config: preflightConfig()}
	if err := mgr.Add(&preflight.Loop{Runner: checks, Interval: install.Preflight.Interval}); err != nil {
		return err
	}

	if err := initControllers(mgr, logger, checks); err != nil {
		return fmt.Errorf("register controllers: %w", err)
	}
	initWebhooks(mgr)

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return err
	}
	// Readiness depends only on the process. The preflight gates tenants, not
	// the webhooks; a transient prerequisite failure never blocks admission.
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return err
	}

	logger.Info(ctx, "starting controller-runtime manager")
	return mgr.Start(ctrl.SetupSignalHandler())
}

func runPreflightOnce(ctx context.Context, restCfg *rest.Config, logger logging.Logger) int {
	direct, err := client.New(restCfg, client.Options{Scheme: operator.NewScheme()})
	if err != nil {
		logger.Error(ctx, "create client", logging.Field{Key: "error", Value: err.Error()})
		return 1
	}
	res := (&preflight.Runner{Client: direct, Logger: logger, Config: preflightConfig()}).Run(ctx)
	for _, c := range res.Checks {
		state := "ok  "
		if !c.OK {
			state = "FAIL"
		}
		fmt.Printf("%s %-28s %s\n", state, c.Name, c.Message)
	}
	if res.OK() {
		return 0
	}
	return 1
}

func preflightConfig() preflight.Config {
	install := oc.Current.Install
	return preflight.Config{
		TrustDomain:          install.TrustDomain,
		SystemNamespace:      install.SystemNamespace,
		IstioNamespace:       install.IstioNamespace,
		IstioRevision:        install.IstioRevision,
		SPIREClassName:       install.SPIREClassName,
		SupportedIstioMinors: install.Preflight.SupportedIstioMinors,
		SPIFFESocket:         spiffeSocket,
		WebhookNames:         []string{webhookcert.Webhooks.Mutating, webhookcert.Webhooks.Validating},
	}
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

func initLogger() logging.Logger {
	if observability.DefaultConfig.LogFormat == "console" {
		return logging.NewDevelopmentLogger()
	}
	return logging.NewLogger()
}

// buildRestConfig uses the in-cluster config when running as a pod and falls
// back to KUBECONFIG for local development.
func buildRestConfig(ctx context.Context, logger logging.Logger) (*rest.Config, error) {
	cfg, err := rest.InClusterConfig()
	if err == nil {
		logger.Info(ctx, "using in-cluster Kubernetes config")
		return cfg, nil
	}
	kubeconfig := clientcmd.NewDefaultClientConfigLoadingRules().GetLoadingPrecedence()
	logger.Info(ctx, "using kubeconfig", logging.Field{Key: "paths", Value: kubeconfig})
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
}

func initManager(restCfg *rest.Config) (ctrl.Manager, error) {
	syncPeriod := oc.Current.SyncPeriod()
	install := oc.Current.Install

	opts := ctrl.Options{
		Scheme: operator.NewScheme(),
		Cache: cache.Options{
			SyncPeriod: &syncPeriod,
		},
		// Bypass the cache to avoid a cluster-wide Secret informer.
		Client: client.Options{
			Cache: &client.CacheOptions{
				DisableFor: []client.Object{&corev1.Secret{}, &corev1.ConfigMap{}},
			},
		},
		LeaderElection:          oc.Current.Operator.LeaderElection,
		LeaderElectionNamespace: install.SystemNamespace,
		LeaderElectionID:        "orbit-operator-leader",
		HealthProbeBindAddress:  ":8081",
		WebhookServer: webhook.NewServer(webhook.Options{
			Port:    9443,
			CertDir: oc.Current.Operator.WebhookCertDir,
		}),
	}

	return ctrl.NewManager(restCfg, opts)
}

func initWebhooks(mgr ctrl.Manager) {
	install := oc.Current.Install
	decoder := admission.NewDecoder(mgr.GetScheme())
	mgr.GetWebhookServer().Register("/mutate-pod", &webhook.Admission{
		Handler: &operator.PodMutator{
			Client:  mgr.GetClient(),
			Decoder: decoder,
			Config: operator.InjectionConfig{
				SystemNamespace:         install.SystemNamespace,
				ImagePullSecrets:        install.ImagePullSecrets,
				OTLPEndpoint:            observability.TenantOTLPEndpoint(),
				SamplingRate:            observability.SamplingRateString(),
				IdentityProxyImage:      install.Injection.IdentityProxyImage,
				IdentityProxyPullPolicy: corev1.PullPolicy(install.Injection.IdentityProxyPullPolicy),
				IdentityProxyResources:  install.Injection.IdentityProxyResources,
				OPAImage:                install.Injection.OPAImage,
				OPAPullPolicy:           corev1.PullPolicy(install.Injection.OPAPullPolicy),
				OPALogLevel:             install.Injection.OPALogLevel,
				OPAResources:            install.Injection.OPAResources,
			},
		},
	})
	mgr.GetWebhookServer().Register("/validate-pod", &webhook.Admission{
		Handler: &operator.PodValidator{
			Client:        mgr.GetClient(),
			Decoder:       decoder,
			IstioRevision: install.IstioRevision,
		},
	})
}

func initControllers(mgr ctrl.Manager, logger logging.Logger, checks *preflight.Runner) error {
	install := oc.Current.Install

	registry := operator.NewTenantRegistry(mgr.GetClient(), logger)
	aggregator := operator.NewClientSecretAggregator(mgr.GetClient(), logger)
	caAggregator := operator.NewTenantCAAggregator(mgr.GetClient(), logger)
	connAggregator := operator.NewConnectionAggregator(mgr.GetClient(), logger)
	mcpAggregator := operator.NewMCPServerAggregator(mgr.GetClient(), logger)
	oauthAggregator := operator.NewOAuthClientAggregator(mgr.GetClient(), logger)

	tenantReconciler := &operator.TenantReconciler{
		Client:                mgr.GetClient(),
		Scheme:                mgr.GetScheme(),
		Registry:              registry,
		Aggregator:            aggregator,
		CAAggregator:          caAggregator,
		ConnAggregator:        connAggregator,
		MCPAggregator:         mcpAggregator,
		OAuthAggregator:       oauthAggregator,
		Logger:                logger,
		BundlePollingMinDelay: install.Injection.BundlePollingMinDelay,
		BundlePollingMaxDelay: install.Injection.BundlePollingMaxDelay,
		IstioRevision:         install.IstioRevision,
		PodSecurity: operator.PodSecurityLabels{
			Enforce: install.PodSecurity.Enforce,
			Warn:    install.PodSecurity.Warn,
			Audit:   install.PodSecurity.Audit,
		},
		ImagePullSecrets: install.ImagePullSecrets,
		Preflight:        preflightSource{checks},
	}
	if err := tenantReconciler.SetupWithManager(mgr); err != nil {
		return err
	}
	// Identity reads the tenant registry at startup and fails without it: seed
	// it before the first tenant exists.
	if err := mgr.Add(manager.RunnableFunc(registry.Sync)); err != nil {
		return fmt.Errorf("add tenant registry seed: %w", err)
	}

	mcpServerReconciler := &operator.MCPServerReconciler{
		Client:     mgr.GetClient(),
		Scheme:     mgr.GetScheme(),
		PodReader:  mgr.GetAPIReader(),
		Aggregator: mcpAggregator,
		Logger:     logger,
	}
	return mcpServerReconciler.SetupWithManager(mgr)
}

// preflightSource adapts the runner to the reconciler's narrow interface.
type preflightSource struct{ runner *preflight.Runner }

func (p preflightSource) Latest() (operator.PreflightResult, bool) {
	res, ok := p.runner.Latest()
	if !ok {
		return operator.PreflightResult{}, false
	}
	return operator.PreflightResult{OK: res.OK(), Failures: res.Failures()}, true
}
