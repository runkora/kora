package compatible

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/clear-route/orbit/internal/inferencegw"
)

// listModelsTimeout bounds one GET <baseURL>/models for a systemone endpoint.
const listModelsTimeout = 30 * time.Second

// errListModelsStatus is returned when the model list answers with a non-2xx status.
var errListModelsStatus = errors.New("list models: unexpected status")

// modelList is the body of GET /models in either shape seen in the wild:
// OpenAI and OpenRouter's {"data":[{"id":…}]} and TypeSafe's
// {"models":[{"name":…}]}. Entries stay raw so a published price can be read.
type modelList struct {
	Data   []json.RawMessage `json:"data"`
	Models []json.RawMessage `json:"models"`
}

// listedModel is the identifying field of one entry of either shape.
type listedModel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// listDecisionModels lists a systemone-only endpoint's models. The OpenAI and
// Anthropic SDKs are not involved: the endpoint declares neither API.
func (c *Connector) listDecisionModels(ctx context.Context) ([]inferencegw.ProviderModel, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/models", http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("%s: list models: %w", c.name, err)
	}
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	_, span, resp, err := c.http.Do(ctx, "compatible.Connector.ListModels", req)
	defer span.End()
	if err != nil {
		return nil, fmt.Errorf("%s: list models: %w", c.name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("%s: %w", c.name, c.http.HandleErrorResponse(span, resp, errListModelsStatus))
	}
	var list modelList
	if err := c.http.DecodeJSON(span, resp, &list); err != nil {
		return nil, fmt.Errorf("%s: list models: %w", c.name, err)
	}

	var models []inferencegw.ProviderModel
	for _, raw := range append(list.Data, list.Models...) {
		var l listedModel
		if json.Unmarshal(raw, &l) != nil {
			continue
		}
		id := l.ID
		if id == "" {
			id = l.Name
		}
		if id == "" {
			continue
		}
		m := c.Describe(id)
		m.Price = parsePrice(string(raw))
		models = append(models, m)
	}
	return models, nil
}
