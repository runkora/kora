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

	// register all controllers with their base paths
	controllerMux := NewMux(ctx, a.logger)
	for _, controller := range controllers {
		controllerMux.SetBasePath(controller.GroupedBy())
		controllerMux.SetMiddleware(controller.Middleware())
		controller.Boot(controllerMux)
	}

	// add a base path for every server route
	if basePath != "/" {
		a.logger.Info(ctx, "Setting base path for server",
			logging.Field{Key: "basePath", Value: basePath},
		)
		a.mux.Handle(
			fmt.Sprintf("%s/", basePath),
			http.StripPrefix(basePath, controllerMux),
		)
	} else {
		// if no base path is provided, use the controller mux directly
		a.logger.Info(ctx, "Setting base path for server",
			logging.Field{Key: "basePath", Value: "/"},
		)
		// this is the default case, no base path
		a.mux = controllerMux.ServeMux
	}

	// Add health check endpoint
	// Note: Registered directly to mux, so it gets server-level middleware (tracing, logging)
	// applied when the final handler chain is built below
	a.mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"status": "health ok"}`)); err != nil {
			// Use request context for runtime logging (OTEL trace extraction)
			a.logger.Error(r.Context(), "Failed to write health check response",
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
	})

	// default middleware runs before user provided middleware
	// provides a catch all middleware for general purpose server
	// Empty for now, but can be used for things like panic recovery, metrics, etc.
	defaultMiddleware := mw.MiddlewareChain()

	// Build final handler with middleware chain
	var finalHandler http.Handler = a.mux

	// Apply user-provided middleware first
	if middleware != nil {
		finalHandler = middleware(finalHandler)
	}

	// Apply default middleware last (runs first in execution)
	finalHandler = defaultMiddleware(finalHandler)

	return &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           finalHandler,
		ReadHeaderTimeout: 10 * time.Second,
	}
}
