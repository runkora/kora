package inferencegw

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
)

// Controller wires the inference gateway service to HTTP routes.
// Handler files follow the dialect naming convention from day one:
//
//   - handlers_openai.go — OpenAI-compatible dialect (POST /chat/completions, GET /models)
//   - handlers_anthropic.go — Anthropic Messages dialect (POST /messages) — added at P1
type Controller struct {
	svc    *Service
	logger *logging.Logger
}

// NewController constructs a ready-to-boot inference gateway Controller.
func NewController(svc *Service, logger *logging.Logger) *Controller {
	return &Controller{svc: svc, logger: logger}
}

// GroupedBy returns the route prefix. The inference gateway is mounted at the
// API root ("/v1") matching the OpenAI API path structure.
func (c *Controller) GroupedBy() string { return "" }

// Middleware returns nil — JWT validation and principal context extraction are
// handled inside the service layer, not as HTTP middleware, to keep the
// middleware chain free of business logic.
func (c *Controller) Middleware() mw.MiddlewareHandler { return nil }

// Boot registers all inference gateway routes with the Mux.
//
// Route map:
//
//	POST   /chat/completions  Non-streaming and streaming chat completions (OpenAI dialect)
//	GET    /models            List available models (stub — returns configured providers)
func (c *Controller) Boot(mux *api.Mux) {
	mux.Handle("POST /chat/completions", c.handleChatCompletions)
	mux.Handle("GET /models", c.handleListModels)
}

// ─── POST /chat/completions ───────────────────────────────────────────────────

func (c *Controller) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req ChatCompletionRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}

	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		apiErr := api.UnauthorizedError(api.ErrCodeUnauthorized, "Authorization header required", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}

	// Streaming path.
	if req.Stream {
		c.handleChatCompletionsStream(w, r, authHeader, req)
		return
	}

	// Non-streaming path.
	resp, err := c.svc.ChatCompletion(ctx, authHeader, req)
	if err != nil {
		apiErr := inferenceErrorResponse(err)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, resp)
}

// handleChatCompletionsStream writes a Server-Sent Events stream.
// Each chunk is emitted as "data: <json>\n\n"; the stream is terminated
// with "data: [DONE]\n\n" on success.
// On mid-stream provider error a final "data: {\"error\":{...}}\n\n" frame
// is emitted before the connection is closed.
func (c *Controller) handleChatCompletionsStream(
	w http.ResponseWriter,
	r *http.Request,
	authHeader string,
	req ChatCompletionRequest,
) {
	ctx := r.Context()

	chunks, err := c.svc.ChatCompletionStream(ctx, authHeader, req)
	if err != nil {
		apiErr := inferenceErrorResponse(err)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, canFlush := w.(http.Flusher)

	writeSSE := func(payload any) error {
		data, jsonErr := json.Marshal(payload)
		if jsonErr != nil {
			return fmt.Errorf("marshal sse frame: %w", jsonErr)
		}
		if _, writeErr := fmt.Fprintf(w, "data: %s\n\n", data); writeErr != nil {
			return fmt.Errorf("write sse frame: %w", writeErr)
		}
		if canFlush {
			flusher.Flush()
		}
		return nil
	}

	for chunk := range chunks {
		if err := writeSSE(chunk); err != nil {
			c.logger.Error(ctx, "sse write error",
				logging.Field{Key: "error", Value: err.Error()},
			)
			return
		}
	}

	// Termination sentinel.
	if _, err := fmt.Fprint(w, "data: [DONE]\n\n"); err != nil {
		c.logger.Error(ctx, "sse done write error",
			logging.Field{Key: "error", Value: err.Error()},
		)
	}
	if canFlush {
		flusher.Flush()
	}
}

// ─── GET /models ──────────────────────────────────────────────────────────────

// modelsResponse is the OpenAI-compatible /v1/models response shape.
type modelsResponse struct {
	Object string        `json:"object"`
	Data   []modelObject `json:"data"`
}

type modelObject struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// handleListModels returns a stub list of available models drawn from
// the PricingCache. This is a best-effort inventory — policy may deny
// access to individual models for a given principal.
func (c *Controller) handleListModels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		apiErr := api.UnauthorizedError(api.ErrCodeUnauthorized, "Authorization header required", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}

	pricings := c.svc.pricing.All()
	now := time.Now().Unix()

	data := make([]modelObject, 0, len(pricings))
	for _, p := range pricings {
		data = append(data, modelObject{
			ID:      fmt.Sprintf("%s/%s", p.Provider, p.ModelID),
			Object:  "model",
			Created: now,
			OwnedBy: string(p.Provider),
		})
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, modelsResponse{
		Object: "list",
		Data:   data,
	})
}

// ─── error mapping ────────────────────────────────────────────────────────────

// inferenceErrorResponse maps service-layer sentinel errors to API error responses.
func inferenceErrorResponse(err error) *api.APIError {
	switch {
	case errors.Is(err, ErrTokenParseFailed):
		return api.UnauthorizedError(api.ErrCodeUnauthorized, "invalid or missing platform token", nil)
	case errors.Is(err, ErrPolicyDenied):
		return api.ForbiddenError("policy_denied", "access denied by policy", nil)
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
