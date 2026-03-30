package vertexai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// publishers are the Model Garden publishers whose models the gateway exposes.
// Each publisher has a distinct set of models on Vertex AI.
var publishers = []string{"google", "anthropic", "meta"}

// modelGardenBaseURL is the Vertex AI Model Garden v1beta1 REST API base.
// The list endpoint does not require a project ID — it lists globally available publisher models.
const modelGardenBaseURL = "https://aiplatform.googleapis.com/v1beta1"

// publisherListResponse is the JSON response from publishers.models.listPublisherModels.
// See: https://cloud.google.com/vertex-ai/docs/reference/rest/v1beta1/publishers.models/list
type publisherListResponse struct {
	PublisherModels []struct {
		// Name is the full resource name: publishers/{publisher}/models/{modelId}
		Name string `json:"name"`
	} `json:"publisherModels"`
	NextPageToken string `json:"nextPageToken"`
}

// ListModels fetches the live model catalog from the Vertex AI Model Garden for each
// registered publisher (google, anthropic, meta) and returns them as ProviderModel entries.
//
// This calls the v1beta1 publishers.models.listPublisherModels endpoint with pagination
// so the returned list always reflects what the Model Garden currently advertises —
// no hardcoded model IDs anywhere.
//
// Note: a model appearing in the Model Garden list does not guarantee it is enabled for
// your project. Models must be enabled in the GCP Console before inference calls succeed.
func (c *Connector) ListModels(ctx context.Context) ([]inferencegw.ProviderModel, error) {
	var models []inferencegw.ProviderModel

	for _, publisher := range publishers {
		pm, err := c.listModelsForPublisher(ctx, publisher)
		if err != nil {
			c.logger.Warn(ctx, "vertexai: ListModels: failed to list publisher models — skipping",
				logging.Field{Key: "publisher", Value: publisher},
				logging.Field{Key: "error", Value: err.Error()},
			)
			continue
		}
		models = append(models, pm...)
	}

	if len(models) == 0 {
		return nil, fmt.Errorf("vertexai: no models returned from Model Garden for publishers %v", publishers)
	}

	return models, nil
}

// listModelsForPublisher calls the Model Garden list API for a single publisher,
// following pagination until all pages are consumed.
func (c *Connector) listModelsForPublisher(ctx context.Context, publisher string) ([]inferencegw.ProviderModel, error) {
	var models []inferencegw.ProviderModel
	pageToken := ""

	for {
		url := fmt.Sprintf("%s/publishers/%s/models?pageSize=100", modelGardenBaseURL, publisher)
		if pageToken != "" {
			url += "&pageToken=" + pageToken
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		// Required when using local ADC (user credentials) — tells the API which
		// project to bill quota against. On GKE Workload Identity this is a no-op
		// since the service account is already scoped to the project.
		req.Header.Set("x-goog-user-project", c.cfg.ProjectID)

		resp, err := c.listHTTPClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("GET %s: %w", url, err)
		}
		defer resp.Body.Close() //nolint:errcheck

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("read response body: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GET %s: status %d: %s", url, resp.StatusCode, body)
		}

		var page publisherListResponse
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}

		for _, pm := range page.PublisherModels {
			model := publisherModelToProviderModel(publisher, pm.Name)
			if model != nil {
				models = append(models, *model)
			}
		}

		if page.NextPageToken == "" {
			break
		}
		pageToken = page.NextPageToken
	}

	return models, nil
}

// publisherModelToProviderModel converts a raw Model Garden resource name into a
// ProviderModel suitable for the inference gateway catalog.
//
// Resource name format: publishers/{publisher}/models/{modelId}
//
// Model ID conventions on Vertex AI:
//   - Anthropic: bare model ID only, e.g. "claude-3-5-haiku@20241022"
//   - Google/Meta: publisher-prefixed, e.g. "google/gemini-2.5-pro"
//
// This mirrors how models must be referenced when calling the inference endpoints.
func publisherModelToProviderModel(publisher, resourceName string) *inferencegw.ProviderModel {
	// Extract model ID from "publishers/{publisher}/models/{modelId}"
	const sep = "/models/"
	idx := strings.Index(resourceName, sep)
	if idx < 0 {
		return nil
	}
	rawModelID := resourceName[idx+len(sep):]
	if rawModelID == "" {
		return nil
	}

	// Build the canonical model ID as the endpoint expects it.
	var modelID string
	if publisher == "anthropic" {
		// Claude-on-Vertex uses the bare model ID (no publisher prefix).
		modelID = rawModelID
	} else {
		// Gemini, Llama, etc. require the "publisher/model" form.
		modelID = publisher + "/" + rawModelID
	}

	// Derive a short alias by stripping the version suffix (@date or -NNN).
	alias := stripVersionSuffix(rawModelID)

	return &inferencegw.ProviderModel{
		Provider:    inferencegw.ProviderVertexAI,
		ModelID:     modelID,
		DisplayName: modelIDToDisplayName(rawModelID),
		Aliases:     []string{alias},
	}
}

// stripVersionSuffix removes version-pinning suffixes so callers can use
// a stable short alias regardless of the pinned version date.
//
// Handles two formats:
//   - @YYYYMMDD suffix  (Anthropic on Vertex)  → claude-3-5-haiku
//   - trailing -NNN version tag (Gemini, Llama) → gemini-2.0-flash
func stripVersionSuffix(modelID string) string {
	// Anthropic @date suffix
	if at := strings.LastIndex(modelID, "@"); at >= 0 {
		return modelID[:at]
	}
	// Google/Meta numeric trailing segment, e.g. gemini-2.0-flash-001
	// Only strip if the last dash-separated segment is entirely digits.
	parts := strings.Split(modelID, "-")
	if len(parts) > 1 {
		last := parts[len(parts)-1]
		if isDigits(last) {
			return strings.Join(parts[:len(parts)-1], "-")
		}
	}
	return modelID
}

// modelIDToDisplayName produces a human-readable display name from a raw model ID.
// This is best-effort — e.g. "gemini-2.5-pro" → "Gemini 2.5 Pro".
func modelIDToDisplayName(modelID string) string {
	// Strip version suffix first
	base := stripVersionSuffix(modelID)
	// Split on dash and title-case each segment that isn't purely numeric
	parts := strings.Split(base, "-")
	for i, p := range parts {
		if !isDigits(p) && len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}

// isDigits reports whether s consists only of ASCII digits.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
