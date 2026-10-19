package inferencegw

import (
	"net/http"

	openai "github.com/openai/openai-go/v3"

	"github.com/runkora/kora/backend/internal/platform/api"
)

// ─── POST /embeddings ─────────────────────────────────────────────────────────

func (c *Controller) handleEmbeddings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// 10 MB: embeddings requests contain short text input — 10 MB is a generous ceiling.
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)

	var params openai.EmbeddingNewParams
	if err := api.DecodeBody(r, &params); err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.ClassifyDecodeError(err), err)
		return
	}

	resp, err := c.svc.Embeddings(ctx, params)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, inferenceErrorResponse(err), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, resp)
}
