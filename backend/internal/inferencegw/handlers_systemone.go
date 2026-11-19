package inferencegw

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// ─── POST /systemone ──────────────────────────────────────────────────────────

// systemOneMaxBody is 10 MB, as for embeddings. It holds an image where an
// implementation accepts one.
const systemOneMaxBody = 10 << 20

// Response headers that tie a System One answer to its usage row. They are
// headers because the body is the upstream's, passed through unchanged. The
// cost header is absent when the model has no price, so absence means unknown.
const (
	headerUsageID = "X-Kora-Usage-Id"
	headerCostUSD = "X-Kora-Cost-USD"
)

// headerDecisionTimeout lets a caller raise the per-attempt timeout, in whole
// seconds. The connector caps it at the gateway's configured maximum. A caller
// that omits it keeps the default.
const headerDecisionTimeout = "X-Kora-Decision-Timeout"

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

	var timeout time.Duration
	if v := r.Header.Get(headerDecisionTimeout); v != "" {
		secs, convErr := strconv.Atoi(v)
		// The upper bound keeps the Duration from overflowing. The connector caps it lower.
		if convErr != nil || secs <= 0 || int64(secs) > math.MaxInt64/int64(time.Second) {
			api.ErrorResponse(ctx, w, c.logger,
				api.BadRequestError("invalid_request", headerDecisionTimeout+" must be a positive whole number of seconds", nil), convErr)
			return
		}
		timeout = time.Duration(secs) * time.Second
	}

	result, err := c.svc.SystemOne(ctx, model, body, timeout)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, systemOneErrorResponse(err), err)
		return
	}

	// Written as received: api.JSON would re-encode the body, compacting it
	// and escaping HTML characters. The status is the implicit 200.
	w.Header().Set("Content-Type", "application/json")
	if result.UsageID != "" {
		w.Header().Set(headerUsageID, result.UsageID)
	}
	if result.CostUSD != nil {
		// Ten decimals, the scale of inference_usage.estimated_cost_usd.
		w.Header().Set(headerCostUSD, strconv.FormatFloat(*result.CostUSD, 'f', 10, 64))
	}
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
