// Command identity-proxy is the Orbit identity-proxy sidecar binary.
//
// It runs as an Envoy External Processing (ext_proc) gRPC server on the Unix
// socket defined in config (default: /run/orbit/identity-proxy.sock). Envoy
// calls it for every outbound request from the agent pod; the proxy injects a
// platform Bearer token into the Authorization header so downstream Orbit
// services can verify the caller without knowing about SPIRE.
//
// Secrets and deployment-specific values are supplied via environment variables:
//
//	ORBIT_IDENTITY_SERVICE_URL  Full base URL of the identity service
//	                            e.g. http://orbit-identity.orbit-system.svc.cluster.local
//	                            Plain HTTP — Istio handles mTLS transparently.
//	SPIFFE_ENDPOINT_SOCKET      Path to SPIRE Agent workload API socket (optional;
//	                            defaults to the value in config/base.yaml)
//
// All other tunables (socket path, timeouts, backoff values, service name) are
// configured in cmd/identity-proxy/config/{base,local,development,production}.yaml.
//
// See docs/adr/2026-03-21-envoy-ext-proc-sidecar-token-injection.md for the full architecture.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/spiffe/go-spiffe/v2/workloadapi"

	identityProxyConfig "github.com/clear-route/orbit/cmd/identity-proxy/config"
	"github.com/clear-route/orbit/internal/identityproxy"
	identityclient "github.com/clear-route/orbit/internal/platform/clients/identity"
	"github.com/clear-route/orbit/internal/platform/config"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/observability"
)

func main() {
	// os.Exit is called here — outside any defers — so that deferred cleanup in
	// mainRun (observability shutdown, context cancellation) always executes.
	os.Exit(mainRun())
}

// mainRun contains the full startup/shutdown sequence and returns an exit code.
// Separating it from main() ensures deferred functions run even on fatal errors.
func mainRun() int {
	cfg := identityProxyConfig.Current

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Observability (tracing + metrics). Failure is non-fatal for a sidecar —
	// we log the error and continue without telemetry rather than refusing to start.
	shutdownObs, err := observability.Setup(ctx, observability.Config{
		ServiceName:     cfg.Observability.ServiceName,
		ServiceVersion:  cfg.App.Version,
		Environment:     config.Environment(),
		ShutdownTimeout: cfg.ShutdownTimeout(),
	})
	if err != nil {
		log.Printf("observability setup failed (continuing without telemetry): %v", err)
		shutdownObs = func(_ context.Context) error { return nil }
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout())
		defer cancel()
		if err := shutdownObs(shutdownCtx); err != nil {
			log.Printf("observability shutdown error: %v", err)
		}
	}()

	logger := initLogger()
	logger.Info(ctx, "identity-proxy starting",
		logging.Field{Key: "service", Value: cfg.Observability.ServiceName},
		logging.Field{Key: "version", Value: cfg.App.Version},
	)

	if err := run(ctx, cfg, logger); err != nil {
		logger.Error(ctx, "identity-proxy fatal error", logging.Field{Key: "error", Value: err.Error()})
		return 1
	}

	logger.Info(ctx, "identity-proxy shutdown complete")
	return 0
}

// run is the actual entrypoint so main() stays clean and errors can be returned.
func run(ctx context.Context, cfg *identityProxyConfig.Config, logger logging.Logger) error {
	// ORBIT_IDENTITY_SERVICE_URL must be provided at runtime — it is not in YAML
	// because it is a deployment-specific endpoint, not a tuneable default.
	identityURL := os.Getenv("ORBIT_IDENTITY_SERVICE_URL")
	if identityURL == "" {
		return fmt.Errorf("ORBIT_IDENTITY_SERVICE_URL must be set")
	}

	// SPIFFE_ENDPOINT_SOCKET may be overridden via env; the YAML base.yaml
	// provides a sensible default for the standard SPIRE Agent mount path.
	spiffeSocket := os.Getenv("SPIFFE_ENDPOINT_SOCKET")
	if spiffeSocket == "" {
		spiffeSocket = cfg.Proxy.SpiffeSocketDefault
	}

	logger.Info(ctx, "connecting to SPIRE workload API",
		logging.Field{Key: "socket", Value: spiffeSocket},
	)

	// Translate YAML config into the identityproxy package's Config type.
	proxyCfg := identityproxy.Config{
		SocketPath:        cfg.Server.SocketPath,
		ReadyTimeout:      cfg.ReadyTimeout(),
		BackoffInitial:    cfg.BackoffInitial(),
		BackoffMax:        cfg.BackoffMax(),
		PollInterval:      cfg.PollInterval(),
		RefreshThreshold:  cfg.RefreshThreshold(),
		HealthAddr:        cfg.Server.HealthAddr,
		EnvoyReadyTimeout: cfg.EnvoyReadyTimeout(),
	}

	// Wire up the identity-proxy components.
	cache := identityproxy.NewTokenCache()
	// The platform identity client uses plain HTTP — Istio's Envoy sidecar
	// upgrades the outbound connection to mTLS using the pod's SPIRE SVID
	// automatically. Doing our own mTLS via go-spiffe would produce a
	// double-TLS situation where Envoy sees an opaque stream and never injects
	// the XFCC header. See docs/adr/2026-03-19-multi-service-backend-deployment-structure.md Service-to-Service Communication.
	exchanger := identityclient.NewClient(identityURL, logger)
	refresher := identityproxy.NewRefresher(cache, exchanger, logger, proxyCfg)
	server := identityproxy.NewServer(cache, exchanger, refresher, logger, proxyCfg)

	// Watch SVID rotations in the background. workloadapi.WatchX509Context
	// blocks until ctx is canceled; the watcher invalidates the token cache
	// and triggers a re-exchange whenever the SPIRE Agent delivers a new SVID.
	go func() {
		watcher := refresher.OnSVIDRotation(ctx)
		if err := workloadapi.WatchX509Context(ctx, watcher,
			workloadapi.WithAddr(spiffeSocket),
		); err != nil && ctx.Err() == nil {
			// Log unexpected watch terminations (not clean cancellations).
			logger.Warn(ctx, "SVID watch terminated unexpectedly",
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
	}()

	// Start blocks until ctx is canceled (SIGINT/SIGTERM) or a fatal error.
	return server.Start(ctx)
}

// initLogger picks console (human-readable) or JSON logging depending on the
// log format configured in the observability package, mirroring the pattern
// used by all other Orbit service binaries.
func initLogger() logging.Logger {
	if observability.DefaultConfig.LogFormat == "console" {
		return logging.NewDevelopmentLogger()
	}
	return logging.NewLogger()
}
