package inferencegw

import (
	"context"
	"errors"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/clients/routescopes"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/token"
)

// Controller wires the inference gateway service to HTTP routes.
// Handler implementations are split by dialect:
//
//   - handlers_openai.go      — OpenAI-compatible dialect (POST /chat/completions, GET /models)
//   - handlers_anthropic.go   — Anthropic Messages dialect (POST /messages)
//   - handlers_embeddings.go  — OpenAI Embeddings API (POST /embeddings)
//   - handlers_responses.go   — OpenAI Responses API (POST /responses)
type Controller struct {
	svc      *Service
	verifier *token.Verifier
	eval     policy.Evaluator
	routes   []routescopes.RouteScope
	basePath string
	logger   logging.Logger
}

// NewController constructs a ready-to-boot inference gateway Controller.
// JWT verification config is loaded from the environment via token.LoadConfig —
// callers do not need to construct or inject a Verifier.
//
// routes and eval are passed to ScopeEnforcementMiddleware for in-process
// route → scope enforcement (docs/adr/2026-06-04-platform-scope-enforcement-middleware.md). basePath must match the server's
// BasePath so the middleware can reconstruct absolute paths after
// http.StripPrefix removes the prefix.
func NewController(ctx context.Context, svc *Service, eval policy.Evaluator, routes []routescopes.RouteScope, basePath string, logger logging.Logger) *Controller {
	verifier := token.NewVerifierFromConfig(ctx, token.LoadConfig())
	return &Controller{svc: svc, verifier: verifier, eval: eval, routes: routes, basePath: basePath, logger: logger}
}

// GroupedBy returns the route prefix. The inference gateway mounts directly
// under the server base path — routes like /chat/completions, /messages, /models
// are composed with the server's basePath (/v1) by the API layer.
func (c *Controller) GroupedBy() string { return "" }

// Middleware returns the controller-level middleware chain applied to every
// route registered in Boot. AuthMiddleware verifies the platform JWT and stores
// the claims in context — no handler needs to touch auth directly.
//
// Execution order:
//
//	xAPIKeyMiddleware       — promotes x-api-key to Authorization: Bearer
//	orbCredentialMiddleware — strips orb_ prefix from bearer value
//	AuthMiddleware          — verifies JWT, stores claims in context
//	DelegationMiddleware    — extracts X-Orbit-Delegation-Token, stores in context
func (c *Controller) Middleware() mw.MiddlewareHandler {
	return mw.MiddlewareChain(
		xAPIKeyMiddleware(),
		orbCredentialMiddleware(),
		mw.AuthMiddleware(c.verifier, c.logger),
		mw.DelegationMiddleware(c.logger, c.verifier),
		mw.ScopeEnforcementMiddleware(c.eval, c.routes, c.basePath, c.logger),
	)
}

// Boot registers all inference gateway routes with the Mux.
//
// Route map:
//
//	POST   /chat/completions      OpenAI-dialect completions (streaming and non-streaming)
//	POST   /messages              Anthropic Messages API dialect
//	POST   /embeddings            OpenAI Embeddings API
//	POST   /responses             OpenAI Responses API (streaming and non-streaming)
//	GET    /models                List available models
//	GET    /usage/stats           Per-PAT usage aggregates (request count, last used)
//	GET    /usage                 Inference usage log (authenticated principal, paginated)
//	GET    /analytics/me          Personal aggregated analytics (any authenticated caller)
//	GET    /analytics             Tenant-wide aggregated analytics (requires platform:analytics:read)
//	GET    /analytics/principals  Per-principal usage summary (requires platform:analytics:read)
func (c *Controller) Boot(mux *api.Mux) {
	mux.Handle("POST /chat/completions", c.handleChatCompletions)
	mux.Handle("POST /messages", c.handleMessages)
	mux.Handle("POST /embeddings", c.handleEmbeddings)
	mux.Handle("POST /responses", c.handleResponses)
	mux.Handle("GET /models", c.handleListModels)
	mux.Handle("GET /usage/stats", c.handleUsageStats)
	mux.Handle("GET /usage", c.handleListUsage)
	mux.Handle("GET /analytics/me", c.handleAnalyticsMe)
	mux.Handle("GET /analytics", c.handleAnalytics)
	mux.Handle("GET /analytics/principals", c.handleAnalyticsPrincipals)
}

// inferenceErrorResponse maps service-layer sentinel errors to API error responses.
// Shared by every dialect handler (chat completions, messages, embeddings, responses).
func inferenceErrorResponse(err error) *api.APIError {
	// ConnectorError codes that map to client errors are handled first so they
	// return the correct 4xx status rather than falling through to the generic
	// ErrProviderError → 503 case.
	var connErr *ConnectorError
	if errors.As(err, &connErr) {
		switch connErr.Code {
		case "MODEL_NOT_SUPPORTED":
			return api.BadRequestError("model_not_supported",
				"this model is not supported on this endpoint; use POST /v1/chat/completions for non-Anthropic models",
				nil,
			)
		case "INVALID_REQUEST":
			// Pass the provider's original message through so clients receive the
			// same actionable detail they would get from calling the provider directly.
			msg := connErr.ProviderMessage
			if msg == "" {
				msg = "the request was rejected as invalid by the upstream provider"
			}
			return api.BadRequestError("invalid_request", msg, nil)
		}
	}

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
