package compatible

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/runkora/kora/backend/internal/inferencegw"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// Timeouts of the connector's own HTTP clients. Each covers one request,
// body included, and applies afresh to every attempt of the retry policy.
const (
	// discoveryTimeout bounds GET <baseURL>/models.
	discoveryTimeout = 30 * time.Second
	// decisionTimeout bounds POST <baseURL>/systemone. Decisions should be quick.
	decisionTimeout = 15 * time.Second
)

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

// decisionsModality is the output modality OpenRouter declares for a decision model.
const decisionsModality = "decisions"

// decisionQuery asks an endpoint that supports it for its decision models only.
// OpenRouter's default listing leaves them out.
const decisionQuery = "?output_modalities=" + decisionsModality

// getModels reads GET <baseURL>/models<query> in either list shape and returns its raw entries.
func (c *Connector) getModels(ctx context.Context, query string) ([]json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/models"+query, http.NoBody)
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
	return append(list.Data, list.Models...), nil
}

// listDecisionModels lists a systemone-only endpoint's models. The OpenAI and
// Anthropic SDKs are not involved: the endpoint declares neither API.
func (c *Connector) listDecisionModels(ctx context.Context) ([]inferencegw.ProviderModel, error) {
	entries, err := c.getModels(ctx, "")
	if err != nil {
		return nil, err
	}
	var models []inferencegw.ProviderModel
	for _, raw := range entries {
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

// allModalitiesQuery asks an endpoint that supports it for every model,
// whatever its output modality. OpenRouter's default listing is text only.
const allModalitiesQuery = "?output_modalities=all"

// Output modalities OpenRouter declares for the models classified here.
const (
	embeddingsModality = "embeddings"
	rerankModality     = "rerank"
)

// outputModalities reads architecture.output_modalities from a /models entry.
// An endpoint without that metadata yields nil.
func outputModalities(raw json.RawMessage) []string {
	var e struct {
		Architecture struct {
			OutputModalities []string `json:"output_modalities"`
		} `json:"architecture"`
	}
	if json.Unmarshal(raw, &e) != nil {
		return nil
	}
	return e.Architecture.OutputModalities
}

// mergeNonTextModels adds the embedding and rerank models the default listing
// leaves out, and corrects the capability of a listed model that declares one.
// Only an entry that publishes architecture.output_modalities counts, so an
// endpoint that ignores the query adds nothing. Image, speech, video and
// transcription models are skipped. A failed request keeps the models and logs
// a warning.
func (c *Connector) mergeNonTextModels(ctx context.Context, models []inferencegw.ProviderModel) []inferencegw.ProviderModel {
	entries, err := c.getModels(ctx, allModalitiesQuery)
	if err != nil {
		c.logger.Warn(ctx, "non-text model listing failed; keeping the default models", logging.Field{Key: "provider", Value: string(c.name)}, logging.Field{Key: "error", Value: err.Error()})
		return models
	}
	for _, raw := range entries {
		var l listedModel
		if json.Unmarshal(raw, &l) != nil || l.ID == "" {
			continue
		}
		out := outputModalities(raw)
		var m inferencegw.ProviderModel
		switch {
		case slices.Contains(out, embeddingsModality):
			m = c.describeEmbedding(l.ID)
		case slices.Contains(out, rerankModality):
			m = c.describeRerank(l.ID)
		default:
			continue
		}
		m.Price = parsePrice(string(raw))
		models = upsertModel(models, m)
	}
	return models
}

// declaresDecisions reports whether a /models entry lists decisions among its
// architecture.output_modalities. An endpoint that ignores the query returns
// entries without that metadata, and none of them counts.
func declaresDecisions(raw json.RawMessage) bool {
	return slices.Contains(outputModalities(raw), decisionsModality)
}

// mergeDecisionModels adds the models a mixed endpoint declares as decision
// models to its chat listing. The decision description replaces a chat one with
// the same id. A failed request keeps the chat models and logs a warning.
func (c *Connector) mergeDecisionModels(ctx context.Context, models []inferencegw.ProviderModel) []inferencegw.ProviderModel {
	entries, err := c.getModels(ctx, decisionQuery)
	if err != nil {
		c.logger.Warn(ctx, "decision model listing failed; keeping the chat models", logging.Field{Key: "provider", Value: string(c.name)}, logging.Field{Key: "error", Value: err.Error()})
		return models
	}
	for _, raw := range entries {
		var l listedModel
		if json.Unmarshal(raw, &l) != nil || l.ID == "" || !declaresDecisions(raw) {
			continue
		}
		m := c.describeDecision(l.ID)
		m.Price = parsePrice(string(raw))
		models = upsertModel(models, m)
	}
	return models
}

// withDecisionModels adds the operator's decisionModels that the listing did not return.
func (c *Connector) withDecisionModels(models []inferencegw.ProviderModel) []inferencegw.ProviderModel {
	for _, id := range c.decisionModels {
		if !slices.ContainsFunc(models, func(m inferencegw.ProviderModel) bool { return m.ModelID == id }) {
			models = append(models, c.describeDecision(id))
		}
	}
	return models
}

// upsertModel replaces the entry with m's id, or appends m.
func upsertModel(models []inferencegw.ProviderModel, m inferencegw.ProviderModel) []inferencegw.ProviderModel {
	for i := range models {
		if models[i].ModelID == m.ModelID {
			models[i] = m
			return models
		}
	}
	return append(models, m)
}
