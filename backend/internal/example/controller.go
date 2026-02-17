package example

import (
	"encoding/json"
	"net/http"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
)

// Controller implements the example feature controller
type Controller struct {
	logger logging.Logger
}

// NewController creates a new example controller
func NewController(logger logging.Logger) *Controller {
	return &Controller{
		logger: logger,
	}
}

// GroupedBy returns the route group prefix for this controller
func (c *Controller) GroupedBy() string {
	return "/example"
}

// Middleware returns any controller-specific middleware
func (c *Controller) Middleware() mw.MiddlewareHandler {
	// No additional middleware for this example controller
	return nil
}

// Boot registers routes with the mux
func (c *Controller) Boot(mux *api.Mux) {
	mux.Handle("GET /health", c.handleHealth)
	mux.Handle("GET /hello/{name}", c.handleHello)
	mux.Handle("POST /echo", c.handleEcho)
}

// handleHealth returns a simple health check response
func (c *Controller) handleHealth(w http.ResponseWriter, r *http.Request) {
	c.logger.Info(r.Context(), "Health check requested")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "healthy",
		"service": "orbit-example",
	})
}

// handleHello demonstrates path parameter handling with Go 1.22+ ServeMux
func (c *Controller) handleHello(w http.ResponseWriter, r *http.Request) {
	// Extract path parameter using Go 1.22+ ServeMux pattern
	name := r.PathValue("name")

	c.logger.Info(r.Context(), "Hello endpoint called",
		logging.Field{Key: "name", Value: name},
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Hello, " + name + "!",
	})
}

// EchoRequest represents the request body for the echo endpoint
type EchoRequest struct {
	Message string `json:"message"`
}

// handleEcho demonstrates request body parsing and validation
func (c *Controller) handleEcho(w http.ResponseWriter, r *http.Request) {
	var req EchoRequest

	// Parse JSON body
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		c.logger.Warn(r.Context(), "Invalid echo request",
			logging.Field{Key: "error", Value: err.Error()},
		)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error":   "Invalid request body",
			"details": err.Error(),
		})
		return
	}

	// Validate required field
	if req.Message == "" {
		c.logger.Warn(r.Context(), "Echo request missing message field")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "message field is required",
		})
		return
	}

	c.logger.Info(r.Context(), "Echo request received",
		logging.Field{Key: "message", Value: req.Message},
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"echo":   req.Message,
		"length": len(req.Message),
	})
}
