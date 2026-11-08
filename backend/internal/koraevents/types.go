package koraevents

import (
	"net/http"

	"github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/events"
)

// TypeInfo is one entry of the event type catalog.
type TypeInfo struct {
	Type        string `json:"type"`
	Category    string `json:"category"`
	Description string `json:"description"`
	SchemaURL   string `json:"schema_url"`
}

// TypeList is GET /v1/events/types.
type TypeList struct {
	Types []TypeInfo `json:"types"`
}

// SchemaURL is the public URL of type t's schema under base, or the API path when base is empty.
func SchemaURL(base string, t events.Type) string {
	if base == "" {
		base = "/v1/events/types/"
	}
	return base + string(t)
}

func (c *Controller) handleTypes(w http.ResponseWriter, r *http.Request) {
	list := TypeList{Types: []TypeInfo{}}
	for _, t := range events.Types() {
		list.Types = append(list.Types, TypeInfo{
			Type: string(t), Category: t.Category(), Description: t.Description(), SchemaURL: SchemaURL(c.schemaBase, t),
		})
	}
	api.JSON(r.Context(), w, c.logger, http.StatusOK, list)
}

func (c *Controller) handleTypeSchema(w http.ResponseWriter, r *http.Request) {
	t := events.Type(r.PathValue("type"))
	s, ok := events.TypeSchema(t)
	if !ok {
		api.ErrorResponse(r.Context(), w, c.logger, api.NotFoundError(api.ErrCodeResourceNotFound, "Unknown event type.", nil), nil)
		return
	}
	s["$id"] = SchemaURL(c.schemaBase, t)
	api.JSON(r.Context(), w, c.logger, http.StatusOK, s)
}
