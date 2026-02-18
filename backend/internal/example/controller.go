package example

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Controller implements the example feature controller
type Controller struct {
	logger logging.Logger
	repo   Repository
}

// NewController creates a new example controller
func NewController(logger logging.Logger, repo Repository) *Controller {
	return &Controller{
		logger: logger,
		repo:   repo,
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
	mux.Handle("GET /users/{id}", c.handleGetUser)
	mux.Handle("POST /users", c.handleCreateUser)
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

// handleGetUser retrieves a user from the database by ID
func (c *Controller) handleGetUser(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")

	c.logger.Info(r.Context(), "Get user endpoint called",
		logging.Field{Key: "user_id", Value: userID},
	)

	user, err := c.repo.GetUserByID(r.Context(), userID)
	if err != nil {
		if err == ErrUserNotFound {
			c.logger.Warn(r.Context(), "User not found",
				logging.Field{Key: "user_id", Value: userID},
			)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "user not found",
			})
			return
		}

		c.logger.Error(r.Context(), "Failed to get user",
			logging.Field{Key: "user_id", Value: userID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "internal server error",
		})
		return
	}

	c.logger.Info(r.Context(), "User retrieved successfully",
		logging.Field{Key: "user_id", Value: userID},
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":         user.ID,
		"email":      user.Email,
		"created_at": user.CreatedAt,
	})
}

// CreateUserRequest represents the request body for creating a user
type CreateUserRequest struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

// handleCreateUser creates a new user in the database
func (c *Controller) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req CreateUserRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Add error context to the parent HTTP span created by middleware
		span := trace.SpanFromContext(ctx)
		span.SetAttributes(
			attribute.String("app.error.kind", "validation"),
			attribute.String("app.error.code", "invalid_json"),
		)

		c.logger.Warn(ctx, "Invalid create user request",
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

	// Validate required fields
	if req.ID == "" || req.Email == "" {
		// Track validation errors in the parent span
		missingFields := []string{}
		if req.ID == "" {
			missingFields = append(missingFields, "id")
		}
		if req.Email == "" {
			missingFields = append(missingFields, "email")
		}

		span := trace.SpanFromContext(ctx)
		span.SetAttributes(
			attribute.String("app.error.kind", "validation"),
			attribute.String("app.error.code", "missing_required_fields"),
			attribute.StringSlice("app.error.fields", missingFields),
		)

		c.logger.Warn(ctx, "Create user request missing required fields")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "id and email fields are required",
		})
		return
	}

	// Create user object
	user := &User{
		ID:        req.ID,
		Email:     req.Email,
		CreatedAt: time.Now(),
	}

	// Save to database - pgx tracer will automatically create db.query child span
	if err := c.repo.CreateUser(ctx, user); err != nil {
		// Handle duplicate key violations with 409 Conflict
		if errors.Is(err, ErrDuplicateKey) {
			span := trace.SpanFromContext(ctx)
			span.SetAttributes(
				attribute.String("app.error.kind", "conflict"),
				attribute.String("app.error.code", "duplicate_user"),
			)

			c.logger.Warn(ctx, "User already exists",
				logging.Field{Key: "user_id", Value: req.ID},
			)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "user already exists",
			})
			return
		}

		// Other database errors return 500
		c.logger.Error(ctx, "Failed to create user",
			logging.Field{Key: "user_id", Value: req.ID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "failed to create user",
		})
		return
	}

	c.logger.Info(ctx, "User created successfully",
		logging.Field{Key: "user_id", Value: user.ID},
		logging.Field{Key: "email", Value: user.Email},
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":         user.ID,
		"email":      user.Email,
		"created_at": user.CreatedAt,
	})
}
