package inferencegw

import (
	"encoding/json"
	"errors"
	"net/http"

	openai "github.com/openai/openai-go/v3"

	"github.com/clear-route/orbit/internal/platform/api"
)

// ─── POST /embeddings ─────────────────────────────────────────────────────────

func (c *Controller) handleEmbeddings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// 10 MB: embeddings requests contain short text input — 10 MB is a generous ceiling.
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)

	var params openai.EmbeddingNewParams
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_request", "request body too large", nil), err)
		} else {
			api.ErrorResponse(ctx, w, c.logger, api.BadRequestError(api.ErrCodeInvalidJSON, "invalid request body", nil), err)
		}
		return
	}

	resp, err := c.svc.Embeddings(ctx, params)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, inferenceErrorResponse(err), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, resp)
}
