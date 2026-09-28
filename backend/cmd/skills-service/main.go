package main

import (
    "context"
    "errors"
    "net/http"
    "os"
    "os/signal"
    "syscall"

    sc "github.com/clear-route/orbit/cmd/skills-service/config"
    "github.com/clear-route/orbit/internal/platform/api"
    "github.com/clear-route/orbit/internal/platform/config"
    "github.com/clear-route/orbit/internal/platform/logging"
    mw "github.com/clear-route/orbit/internal/platform/middleware"
    "github.com/clear-route/orbit/internal/platform/observability"
    "github.com/clear-route/orbit/internal/platform/storage"
    "github.com/clear-route/orbit/internal/skills"
)

func main() {
    ctx := context.Background()
    logger := initLogger()

    if err := sc.Current.ApplyEnvOverrides(); err != nil {
        logger.Error(ctx, "invalid config", logging.Field{Key: "error", Value: err.Error()})
        os.Exit(1)
    }

    shutdownObservability, err := initObservability(ctx, logger)
    if err != nil {
        logger.Error(ctx, "failed to setup observability", logging.Field{Key: "error", Value: err.Error()})
        os.Exit(1)
    }

    logger.Info(ctx, "skills-service starting", logging.Field{Key: "version", Value: sc.Current.App.Version})

    redis, err := initStorage(ctx, logger)
    if err != nil {
        logger.Error(ctx, "failed to initialize storage", logging.Field{Key: "error", Value: err.Error()})
        shutdownObservability()
        os.Exit(1)
    }

    registry, err := initRegistry(ctx, logger)
    if err != nil {
        logger.Error(ctx, "failed to initialize marketplace registry", logging.Field{Key: "error", Value: err.Error()})
        _ = redis.Close()
        shutdownObservability()
        os.Exit(1)
    }

    watchCtx, watchCancel := context.WithCancel(ctx)
    go registry.Watch(watchCtx)

    server := initServer(ctx, logger)

    exitCode := run(ctx, logger, server)
    watchCancel()

    if err := redis.Close(); err != nil {
        logger.Error(ctx, "failed to close redis", logging.Field{Key: "error", Value: err.Error()})
    }
    shutdownObservability()
    os.Exit(exitCode)
}

func initObservability(ctx context.Context, logger logging.Logger) (func(), error) {
    shutdown, err := observability.Setup(ctx, observability.Config{
        ServiceName:     sc.Current.Observability.ServiceName,
        ServiceVersion:  sc.Current.App.Version,
        Environment:     config.Environment(),
        ShutdownTimeout: sc.Current.ShutdownTimeout(),
    })
    if err != nil {
        return nil, err
    }

    return func() {
        shutdownCtx, cancel := context.WithTimeout(context.Background(), sc.Current.ShutdownTimeout())
        defer cancel()
        if err := shutdown(shutdownCtx); err != nil {
            logger.Error(shutdownCtx, "error shutting down observability", logging.Field{Key: "error", Value: err.Error()})
        }
    }, nil
}

func initLogger() logging.Logger {
    if observability.DefaultConfig.LogFormat == "console" {
        return logging.NewDevelopmentLogger()
    }
    return logging.NewLogger()
}

func initStorage(ctx context.Context, logger logging.Logger) (*storage.RedisClient, error) {
    cfg, err := storage.LoadRedisConfig()
    if err != nil {
        return nil, err
    }

    redis, err := storage.NewRedis(ctx, cfg, logger)
    if err != nil {
        return nil, err
    }

    logger.Info(ctx, "redis initialized successfully")
    return redis, nil
}

func initRegistry(_ context.Context, logger logging.Logger) (*skills.Registry, error) {
    return skills.NewRegistry(sc.Current.Marketplace.SourcesConfigPath, logger)
}

func initServer(ctx context.Context, logger logging.Logger) *http.Server {
    apiServer := api.NewAPI(ctx, logger)

    return apiServer.Server(
        ctx,
        sc.Current.Server.Port,
        sc.Current.Server.BasePath,
        mw.MiddlewareChain(
            mw.TracingMiddleware(sc.Current.Observability.ServiceName),
            mw.LoggingMiddleware(logger),
        ),
        // Feature controllers wired in later tickets.
    )
}

func run(ctx context.Context, logger logging.Logger, server *http.Server) int {
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

    serverErr := make(chan error, 1)
    go func() {
        logger.Info(ctx, "starting HTTP server", logging.Field{Key: "addr", Value: server.Addr})
        if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
            serverErr <- err
            return
        }
        serverErr <- nil
    }()

    select {
    case err := <-serverErr:
        if err != nil {
            logger.Error(ctx, "failed to start server", logging.Field{Key: "error", Value: err.Error()})
            return 1
        }
    case <-quit:
        logger.Info(ctx, "shutting down skills-service...")
    }

    shutdownCtx, cancel := context.WithTimeout(ctx, sc.Current.ShutdownTimeout())
    defer cancel()

    if err := server.Shutdown(shutdownCtx); err != nil {
        logger.Error(ctx, "server forced to shutdown", logging.Field{Key: "error", Value: err.Error()})
    }

    logger.Info(ctx, "skills-service shutdown complete")
    return 0
}
