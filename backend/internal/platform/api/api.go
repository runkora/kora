package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
)

type api struct {
	logger logging.Logger
	mux    *http.ServeMux
}

func NewAPI(ctx context.Context, logger logging.Logger) *api {
	logger.Info(ctx, "Creating new API")
	return &api{
		logger: logger,
		mux:    http.NewServeMux(),
	}
}

func (a *api) Server(
	ctx context.Context,
	port int,
	basePath string,
	middleware mw.MiddlewareHandler,
	controllers ...Controller,
) *http.Server {
	a.logger.Info(ctx, "Creating new server",
		logging.Field{Key: "port", Value: port},
	)

	// Register all controllers with their base paths.
	controllerMux := NewMux(ctx, a.logger)
	for _, controller := range controllers {
		controllerMux.SetBasePath(controller.GroupedBy())
		controllerMux.SetMiddleware(controller.Middleware())
		controller.Boot(controllerMux)
	}

	// Build the application handler (controller routes under basePath).
	var appHandler http.Handler
	if basePath != "/" {
		a.logger.Info(ctx, "Setting base path for server",
			logging.Field{Key: "basePath", Value: basePath},
		)
		a.mux.Handle(
			fmt.Sprintf("%s/", basePath),
			http.StripPrefix(basePath, controllerMux),
		)
		appHandler = a.mux
	} else {
		a.logger.Info(ctx, "Setting base path for server",
			logging.Field{Key: "basePath", Value: "/"},
		)
		appHandler = controllerMux.ServeMux
	}

	// Apply middleware ONLY to application routes.
	//
	// The health endpoint is wired on the outer mux below, intentionally
	// bypassing tracing, logging, and any future metrics middleware.
	// Kubernetes liveness/readiness probes fire every few seconds and would
	// otherwise flood logs, traces, and metrics with zero-signal noise.
	//
	// defaultMiddleware runs before user-provided middleware and provides a
	// catch-all for general-purpose concerns (e.g. panic recovery in future).
	defaultMiddleware := mw.MiddlewareChain()
	if middleware != nil {
		appHandler = middleware(appHandler)
	}
	appHandler = defaultMiddleware(appHandler)

	// Build the outer mux: health check (no middleware) + all other routes.
	// http.ServeMux matches more specific patterns first, so "GET /health"
	// always wins over the "/" catch-all regardless of registration order.
	outerMux := http.NewServeMux()
	outerMux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"status": "health ok"}`)); err != nil {
			// r.Context() has no active span here by design — health is
			// intentionally outside the tracing middleware.
			a.logger.Error(r.Context(), "Failed to write health check response",
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
	})
	outerMux.Handle("/", appHandler)

	return &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           outerMux,
		ReadHeaderTimeout: 10 * time.Second,
	}
}
