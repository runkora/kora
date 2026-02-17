package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
)

// Mux wraps http.ServeMux with controller-level routing and middleware support.
//
// Thread Safety: Mux embeds *http.ServeMux which is safe for concurrent use after
// registration is complete. The Mux wrapper fields (basePath, middleware, logger, ctx)
// are only modified during server setup (NewServer) before the server starts accepting
// requests. Once the server is running, these fields are read-only, making the entire
// Mux structure safe for concurrent request handling.
//
// Single Instance Pattern: Each controller gets its own Mux instance during NewServer,
// but all route registrations happen before http.Server.ListenAndServe is called.
// After that point, only the ServeMux.ServeHTTP method is called concurrently by
// the http.Server, which is explicitly documented as safe in net/http.
type Mux struct {
	*http.ServeMux
	basePath   string
	middleware mw.MiddlewareHandler
	logger     logging.Logger
	ctx        context.Context
}

func NewMux(ctx context.Context, logger logging.Logger) *Mux {
	return &Mux{
		ServeMux: http.NewServeMux(),
		logger:   logger,
		ctx:      ctx,
	}
}

func (m *Mux) SetBasePath(basePath string) {
	m.basePath = basePath
}

func (m *Mux) SetMiddleware(middleware mw.MiddlewareHandler) {
	m.middleware = middleware
}

// Handles routes for each controller and adds them to the server.
// Each controller is responsible for creation and delegation of its routes.
// Routes are grouped by the controller's GroupedBy method, this serves as a collection
// group within the controller. A new Mux is created from a controller using its GroupedBy
// as the basePath.
func (m *Mux) Handle(route string, handler func(http.ResponseWriter, *http.Request)) {
	m.HandleWithMiddleware(
		route,
		// controller level middleware is handled by HandleWithMiddleware mux
		// during creation, so an empty middleware chain is passed here
		mw.MiddlewareChain(),
		handler,
	)
}

// Prepares each route with middleware for processing
//
// A controller with a GroupedBy of "/planets", and the routes /create, /delete, /update
// would be registered as:
//
//	GroupedBy: /planets
//	Routes:
//	 - /create
//	 - /delete
//	 - /update
//	Booted:
//	 - /planets/create
//	 - /planets/delete
//	 - /planets/update
func (m *Mux) HandleWithMiddleware(
	route string,
	middleware mw.MiddlewareHandler,
	handler func(http.ResponseWriter, *http.Request),
) {
	// Split route into method and path
	// Routes must follow "METHOD /path" format (enforced by ServeMux)
	// This split is safe because invalid formats will fail at ServeMux.HandleFunc
	method := strings.Split(route, " ")[0]
	path := strings.Split(route, " ")[1]

	// create our grouped route
	trimmedPath := strings.TrimPrefix(path, m.basePath)
	if m.basePath == "/" {
		// if basepath is only a slash, we dont need to trim it
		trimmedPath = strings.TrimPrefix(path, "/")
	}
	joinedRoute := fmt.Sprintf("%s %s%s", method, m.basePath, trimmedPath)
	m.logger.Info(m.ctx, "Registering sub-route",
		logging.Field{Key: "route", Value: joinedRoute},
	)
	// Build middleware chain
	// Middleware wrapping order (inside-out): handler → route-specific → controller-level
	// Execution order (outside-in): controller-level → route-specific → handler
	var finalHandler http.Handler = http.HandlerFunc(handler)

	// Apply route-specific middleware first (wraps handler, executes last)
	if middleware != nil {
		finalHandler = middleware(finalHandler)
	}

	// Apply controller-level middleware last (wraps everything, executes first)
	if m.middleware != nil {
		finalHandler = m.middleware(finalHandler)
	}

	m.ServeMux.Handle(joinedRoute, finalHandler)
}

func (m *Mux) HandleFileServer(
	route string,
	handler http.Handler,
) {
	m.ServeMux.Handle(
		route,
		handler,
	)
}
