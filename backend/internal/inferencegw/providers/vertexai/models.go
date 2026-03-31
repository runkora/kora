package vertexai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// publishers are the Model Garden publishers whose models the gateway exposes.
// Each publisher has a distinct set of models on Vertex AI.
var publishers = []string{"google", "anthropic", "meta"}

// modelGardenBaseURL is the Vertex AI Model Garden v1beta1 REST API base.
const modelGardenBaseURL = "https://aiplatform.googleapis.com/v1beta1"

// publisherListResponse is the JSON response from publishers.models.listPublisherModels.
type publisherListResponse struct {
	PublisherModels []struct {
		Name string `json:"name"`
	} `json:"publisherModels"`
	NextPageToken string `json:"nextPageToken"`
}

// ListModels fetches the live model catalog from the Vertex AI Model Garden for each
// registered publisher (google, anthropic, meta) and returns them as ProviderModel entries.
func (c *Connector) ListModels(ctx context.Context) ([]inferencegw.ProviderModel, error) {
	var models []inferencegw.ProviderModel

	for _, publisher := range publishers {
		pm, err := c.listModelsForPublisher(ctx, publisher)
		if err != nil {
			c.logger.Warn(ctx, "vertexai: ListModels: failed to list publisher models -- skipping",
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
func publisherModelToProviderModel(publisher, resourceName string) *inferencegw.ProviderModel {
	const sep = "/models/"
	_, after, ok := strings.Cut(resourceName, sep)
	if !ok {
		return nil
	}
	rawModelID := after
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

	// Derive a short alias by stripping the @YYYYMMDD version suffix.
	// Only Anthropic models use this pinning format; Google/Meta use numeric
	// revision suffixes ("-001") and also expose unversioned IDs directly, so
	// generating an alias from those would shadow the real unversioned model ID.
	var aliases []string
	if publisher == "anthropic" {
		if stripped := stripVersionSuffix(rawModelID); stripped != rawModelID {
			aliases = []string{stripped}
		}
	}

	// Derive modalities, dialects, and capabilities from publisher and model ID.
	inputMods, outputMods := inferVertexModalities(publisher, modelID)
	dialects := inferVertexDialects(publisher, modelID, outputMods)
	caps := inferVertexCapabilities(modelID, outputMods)

	return &inferencegw.ProviderModel{
		Provider:         inferencegw.ProviderVertexAI,
		ModelID:          modelID,
		DisplayName:      modelIDToDisplayName(rawModelID),
		Aliases:          aliases,
		InputModalities:  inputMods,
		OutputModalities: outputMods,
		Dialects:         dialects,
		Capabilities:     caps,
	}
}

// inferVertexModalities returns input and output modalities for a Vertex AI model.
// The Model Garden list API does not return modality metadata, so we infer from
// publisher and model ID patterns.
func inferVertexModalities(publisher, modelID string) (input, output []inferencegw.Modality) {
	lower := strings.ToLower(modelID)

	// Check for specialised Google model types first.
	if publisher == "google" {
		switch {
		case strings.Contains(lower, "embed"):
			return []inferencegw.Modality{inferencegw.ModalityText},
				[]inferencegw.Modality{inferencegw.ModalityEmbedding}
		case strings.Contains(lower, "imagen") || strings.Contains(lower, "imagegeneration"):
			return []inferencegw.Modality{inferencegw.ModalityText, inferencegw.ModalityImage},
				[]inferencegw.Modality{inferencegw.ModalityImage}
		case strings.Contains(lower, "image-preview") || strings.HasSuffix(lower, "-image-preview"):
			// e.g. gemini-3.1-flash-image-preview — generates images from text.
			return []inferencegw.Modality{inferencegw.ModalityText, inferencegw.ModalityImage},
				[]inferencegw.Modality{inferencegw.ModalityImage}
		case strings.Contains(lower, "tts"):
			// e.g. gemini-2.5-pro-tts — text-to-speech output only.
			return []inferencegw.Modality{inferencegw.ModalityText},
				[]inferencegw.Modality{inferencegw.ModalityAudio}
		case strings.Contains(lower, "audio"):
			// e.g. gemini-live-2.5-flash-native-audio — live audio I/O.
			return []inferencegw.Modality{inferencegw.ModalityText, inferencegw.ModalityAudio},
				[]inferencegw.Modality{inferencegw.ModalityAudio}
		}
	}

	// Default by publisher.
	switch publisher {
	case "anthropic":
		// All current Claude models support vision input.
		return []inferencegw.Modality{inferencegw.ModalityText, inferencegw.ModalityImage},
			[]inferencegw.Modality{inferencegw.ModalityText}
	case "google":
		// Gemini models are natively multimodal.
		return []inferencegw.Modality{inferencegw.ModalityText, inferencegw.ModalityImage},
			[]inferencegw.Modality{inferencegw.ModalityText}
	default:
		// Meta Llama and others: text-only.
		return []inferencegw.Modality{inferencegw.ModalityText},
			[]inferencegw.Modality{inferencegw.ModalityText}
	}
}

// inferVertexCapabilities returns the endpoint families supported for a Vertex AI model.
// Rules follow ADR-0026: output modality drives capability, with text → chat,
// embedding → embedding, image → image_generation.
func inferVertexCapabilities(modelID string, outputMods []inferencegw.Modality) []inferencegw.ModelCapability {
	lower := strings.ToLower(modelID)
	if strings.Contains(lower, "rerank") {
		return []inferencegw.ModelCapability{inferencegw.CapabilityRerank}
	}
	var caps []inferencegw.ModelCapability
	for _, m := range outputMods {
		switch m {
		case inferencegw.ModalityText:
			caps = append(caps, inferencegw.CapabilityChat)
		case inferencegw.ModalityEmbedding:
			caps = append(caps, inferencegw.CapabilityEmbedding)
		case inferencegw.ModalityImage:
			caps = append(caps, inferencegw.CapabilityImageGeneration)
		case inferencegw.ModalityVideo:
			caps = append(caps, inferencegw.CapabilityVideoGeneration)
		case inferencegw.ModalityAudio:
			caps = append(caps, inferencegw.CapabilityAudioGeneration)
		}
	}
	return caps
}

// inferVertexDialects returns the gateway wire protocols supported for a Vertex AI model.
// Embedding and image-gen models get no dialect (they don't use chat APIs).
// Claude models get both openai and anthropic dialects.
// All other text-output models get openai dialect only.
func inferVertexDialects(publisher, modelID string, outputMods []inferencegw.Modality) []inferencegw.APIDialect {
	hasTextOutput := slices.Contains(outputMods, inferencegw.ModalityText)
	if !hasTextOutput {
		return nil
	}
	if publisher == "anthropic" || isClaudeModel(modelID) {
		return []inferencegw.APIDialect{inferencegw.DialectOpenAI, inferencegw.DialectAnthropic}
	}
	return []inferencegw.APIDialect{inferencegw.DialectOpenAI}
}

// stripVersionSuffix removes the Anthropic @YYYYMMDD version-pinning suffix
// from a model ID, returning the stable base name.
//
// Example: "claude-3-5-haiku@20241022" -> "claude-3-5-haiku"
func stripVersionSuffix(modelID string) string {
	if at := strings.LastIndex(modelID, "@"); at >= 0 {
		return modelID[:at]
	}
	return modelID
}

// modelIDToDisplayName produces a human-readable display name from a raw model ID.
// e.g. "gemini-2.5-pro" -> "Gemini 2.5 Pro"
func modelIDToDisplayName(modelID string) string {
	base := stripVersionSuffix(modelID)
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
