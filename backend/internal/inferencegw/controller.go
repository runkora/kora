package inferencegw

import (
	"context"
	"errors"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/token"
)

// Controller wires the inference gateway service to HTTP routes.
// Handler implementations are split by dialect:
//
//   - handlers_openai.go    — OpenAI-compatible dialect (POST /chat/completions, GET /models)
//   - handlers_anthropic.go — Anthropic Messages dialect (POST /messages)
type Controller struct {
	svc      *Service
	verifier *token.Verifier
	logger   logging.Logger
}

// NewController constructs a ready-to-boot inference gateway Controller.
// JWT verification config is loaded from the environment via token.LoadConfig —
// callers do not need to construct or inject a Verifier.
func NewController(ctx context.Context, svc *Service, logger logging.Logger) *Controller {
	verifier := token.NewVerifierFromConfig(ctx, token.LoadConfig())
	return &Controller{svc: svc, verifier: verifier, logger: logger}
}

// GroupedBy returns the route prefix. The inference gateway mounts directly
// under the server base path — routes like /chat/completions, /messages, /models
// are composed with the server's basePath (/v1) by the API layer.
func (c *Controller) GroupedBy() string { return "" }

// Middleware returns the controller-level middleware chain applied to every
// route registered in Boot. AuthMiddleware verifies the platform JWT and stores
// the claims in context — no handler needs to touch auth directly.
func (c *Controller) Middleware() mw.MiddlewareHandler {
	return mw.MiddlewareChain(
		xAPIKeyMiddleware(),
		mw.AuthMiddleware(c.verifier, c.logger),
	)
}

// Boot registers all inference gateway routes with the Mux.
//
// Route map:
//
//	POST   /chat/completions  OpenAI-dialect completions (streaming and non-streaming)
//	POST   /messages          Anthropic Messages API dialect
//	GET    /models            List available models
func (c *Controller) Boot(mux *api.Mux) {
	mux.Handle("POST /chat/completions", c.handleChatCompletions)
	mux.Handle("POST /messages", c.handleMessages)
	mux.Handle("GET /models", c.handleListModels)
}

// inferenceErrorResponse maps service-layer sentinel errors to API error responses.
// Shared by both the OpenAI and Anthropic dialect handlers.
func inferenceErrorResponse(err error) *api.APIError {
	switch {
	case errors.Is(err, ErrTokenParseFailed):
		return api.UnauthorizedError(api.ErrCodeUnauthorized, "invalid or missing platform token", nil)
	case errors.Is(err, ErrPolicyDenied):
		return api.ForbiddenError("policy_denied", "access denied by policy", nil)
	case errors.Is(err, ErrProviderNotConfigured):
		return api.BadRequestError("provider_not_configured", "the requested provider is not enabled", nil)
	case errors.Is(err, ErrNoProvidersEnabled):
		return api.ServiceUnavailableError("no_providers", "no inference providers are currently available", nil)
	case errors.Is(err, ErrRoutingFailed):
		return api.ServiceUnavailableError("routing_failed", "unable to route request to any provider", nil)
	case errors.Is(err, ErrProviderError):
		return api.ServiceUnavailableError("provider_error", "upstream provider returned an error", nil)
	case errors.Is(err, ErrInvalidRequest):
		return api.BadRequestError("invalid_request", "request validation failed", nil)
	default:
		return api.InternalError(api.ErrCodeInternalError, "internal inference gateway error", nil)
	}
}
