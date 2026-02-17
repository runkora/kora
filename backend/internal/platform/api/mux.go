package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
)

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

// Prepares each route with middleware for processesing
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
	// split our route for adding groupedBy registration
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
	// Build middleware chain - check if m.middleware is nil
	var finalHandler http.Handler = http.HandlerFunc(handler)
	
	// Apply route-specific middleware first
	if middleware != nil {
		finalHandler = middleware(finalHandler)
	}
	
	// Apply mux-level middleware second (if it exists)
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