package example

import (
	"errors"
	"net/http"

	"github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/logging"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
)

// Controller is the HTTP transport for the example feature. It decodes
// requests, delegates to the Service, and maps domain results and sentinel
// errors onto api responses. It contains no business logic itself.
type Controller struct {
	logger logging.Logger
	svc    *Service
}

// NewController wires the example controller. It builds the Service around the
// repository, so callers supply only storage — the shape cmd/example/main.go
// uses. A feature with more dependencies would inject a pre-built Service here.
func NewController(logger logging.Logger, repo Repository) *Controller {
	return &Controller{logger: logger, svc: NewService(logger, repo)}
}

// GroupedBy returns the route-group prefix for this controller.
func (c *Controller) GroupedBy() string { return "/example" }

// Middleware returns controller-specific middleware (none for the example).
func (c *Controller) Middleware() mw.MiddlewareHandler { return nil }

// Boot registers the controller's routes on the mux.
func (c *Controller) Boot(mux *api.Mux) {
	mux.Handle("GET /health", c.handleHealth)
	mux.Handle("GET /hello/{name}", c.handleHello)
	mux.Handle("POST /echo", c.handleEcho)
	mux.Handle("GET /users/{id}", c.handleGetUser)
	mux.Handle("POST /users", c.handleCreateUser)
}

func (c *Controller) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]string{
		"status":  "healthy",
		"service": "orbit-example",
	})
}

// handleHello demonstrates reading a path parameter (Go 1.22+ ServeMux).
func (c *Controller) handleHello(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := r.PathValue("name")
	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]string{
		"message": "Hello, " + name + "!",
	})
}

func (c *Controller) handleEcho(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req EchoRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}

	resp, err := c.svc.Echo(ctx, req.Message)
	if err != nil {
		if errors.Is(err, ErrEmptyMessage) {
			apiErr := api.ValidationError(api.ErrCodeMissingField, err.Error(), map[string]any{"field": "message"})
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		apiErr := api.InternalError(api.ErrCodeInternalError, "internal server error", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, resp)
}

func (c *Controller) handleGetUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := r.PathValue("id")

	user, err := c.svc.GetUser(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			apiErr := api.NotFoundError(api.ErrCodeResourceNotFound, "user not found", map[string]any{"user_id": userID})
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		apiErr := api.InternalError(api.ErrCodeDatabaseError, "internal server error", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, newUserResponse(user))
}

func (c *Controller) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req CreateUserRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}

	user, err := c.svc.CreateUser(ctx, req)
	if err != nil {
		switch {
		case errors.Is(err, ErrMissingUserFields):
			apiErr := api.ValidationError(api.ErrCodeMissingField, err.Error(), map[string]any{"fields": []string{"id", "email"}})
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		case errors.Is(err, ErrUserExists):
			apiErr := api.ConflictError(api.ErrCodeResourceExists, "user already exists", map[string]any{"user_id": req.ID})
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		default:
			apiErr := api.InternalError(api.ErrCodeDatabaseError, "failed to create user", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		}
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusCreated, newUserResponse(user))
}
