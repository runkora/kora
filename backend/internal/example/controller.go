package example

import (
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
	ctx := r.Context()
	c.logger.Info(ctx, "Health check requested")

	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]string{
		"status":  "healthy",
		"service": "orbit-example",
	})
}

// handleHello demonstrates path parameter handling with Go 1.22+ ServeMux
func (c *Controller) handleHello(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// Extract path parameter using Go 1.22+ ServeMux pattern
	name := r.PathValue("name")

	c.logger.Info(ctx, "Hello endpoint called",
		logging.Field{Key: "name", Value: name},
	)

	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]string{
		"message": "Hello, " + name + "!",
	})
}

// EchoRequest represents the request body for the echo endpoint
type EchoRequest struct {
	Message string `json:"message"`
}

// handleEcho demonstrates request body parsing and validation
func (c *Controller) handleEcho(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req EchoRequest

	// Parse JSON body using helper
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}

	// Validate required field
	if req.Message == "" {
		apiErr := api.ValidationError(api.ErrCodeMissingField, "message field is required", map[string]interface{}{
			"field": "message",
		})
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}

	c.logger.Info(ctx, "Echo request received",
		logging.Field{Key: "message", Value: req.Message},
	)

	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]interface{}{
		"echo":   req.Message,
		"length": len(req.Message),
	})
}

// handleGetUser retrieves a user from the database by ID
func (c *Controller) handleGetUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := r.PathValue("id")

	c.logger.Info(ctx, "Get user endpoint called",
		logging.Field{Key: "user_id", Value: userID},
	)

	user, err := c.repo.GetUserByID(ctx, userID)
	if err != nil {
		if err == ErrUserNotFound {
			apiErr := api.NotFoundError(api.ErrCodeResourceNotFound, "user not found", map[string]interface{}{
				"user_id": userID,
			})
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}

		apiErr := api.InternalError(api.ErrCodeDatabaseError, "internal server error", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	c.logger.Info(ctx, "User retrieved successfully",
		logging.Field{Key: "user_id", Value: userID},
	)

	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]interface{}{
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

	// Parse and validate JSON request body
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
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
			attribute.String("app.error.code", "missing_required_fields"),
			attribute.StringSlice("app.error.fields", missingFields),
		)

		apiErr := api.ValidationError(api.ErrCodeMissingField, "id and email fields are required", map[string]interface{}{
			"missing_fields": missingFields,
		})
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
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
				attribute.String("app.error.code", api.ErrCodeResourceExists),
			)

			apiErr := api.ConflictError(api.ErrCodeResourceExists, "user already exists", map[string]interface{}{
				"user_id": req.ID,
			})
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}

		// Other database errors return 500
		apiErr := api.InternalError(api.ErrCodeDatabaseError, "failed to create user", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	c.logger.Info(ctx, "User created successfully",
		logging.Field{Key: "user_id", Value: user.ID},
		logging.Field{Key: "email", Value: user.Email},
	)

	api.JSON(ctx, w, c.logger, http.StatusCreated, map[string]interface{}{
		"id":         user.ID,
		"email":      user.Email,
		"created_at": user.CreatedAt,
	})
}
