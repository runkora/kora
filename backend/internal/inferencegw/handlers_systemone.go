package inferencegw

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// ─── POST /systemone ──────────────────────────────────────────────────────────

// systemOneMaxBody is 10 MB, as for embeddings. It holds an image where an
// implementation accepts one.
const systemOneMaxBody = 10 << 20

// handleSystemOne passes a System One request through. Only the envelope is
// validated: a JSON object with a non-empty string model. questions and state
// are the upstream's to validate, because implementations differ in their
// limits.
func (c *Controller) handleSystemOne(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	r.Body = http.MaxBytesReader(w, r.Body, systemOneMaxBody)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.ClassifyReadError(err), err)
		return
	}

	var envelope struct {
		Model any `json:"model"`
	}
	if err = json.Unmarshal(body, &envelope); err != nil {
		api.ErrorResponse(ctx, w, c.logger,
			api.BadRequestError(api.ErrCodeInvalidJSON, "request body must be a JSON object", nil), err)
		return
	}
	model, _ := envelope.Model.(string)
	if model == "" {
		api.ErrorResponse(ctx, w, c.logger,
			api.BadRequestError("invalid_request", "model is required and must be a non-empty string", nil), nil)
		return
	}

	result, err := c.svc.SystemOne(ctx, model, body)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, systemOneErrorResponse(err), err)
		return
	}

	// Written as received: api.JSON would re-encode the body, compacting it
	// and escaping HTML characters. The status is the implicit 200.
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(result.Body); err != nil {
		c.logger.Error(ctx, "failed to write systemone response",
			logging.Field{Key: "error", Value: err.Error()},
		)
	}
}

// systemOneErrorResponse maps a SystemOne failure to the caller's response. A
// request the upstream rejected keeps its status and message (400, 422); a
// final 429 is rate_limited so SDKs back off; every other upstream failure,
// 401 and 403 included, is 503 provider_error. Retry-After is not forwarded.
func systemOneErrorResponse(err error) *api.APIError {
	var connErr *ConnectorError
	if !errors.Is(err, ErrProviderError) || !errors.As(err, &connErr) {
		return inferenceErrorResponse(err)
	}
	switch {
	case connErr.Code == "MODEL_NOT_SUPPORTED":
		// No route serves the API. The shared mapping's text is chat-specific.
		return api.BadRequestError("invalid_request", connErr.ProviderMessage, nil)
	case connErr.Code == "INVALID_REQUEST" && connErr.HTTPStatus == http.StatusUnprocessableEntity:
		return &api.APIError{Code: "invalid_request", Message: connErr.ProviderMessage, StatusCode: http.StatusUnprocessableEntity}
	case connErr.Code == "THROTTLED" && connErr.HTTPStatus == http.StatusTooManyRequests:
		return api.TooManyRequestsError("rate_limited", "the upstream provider is rate limiting requests; retry later", nil)
	}
	return inferenceErrorResponse(err)
}
