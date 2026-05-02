package bedrock

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	bedrockruntimeSDK "github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	openai "github.com/openai/openai-go"
	"github.com/openai/openai-go/shared/constant"

	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/clear-route/orbit/internal/inferencegw/providers/convert"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// Embeddings dispatches to the appropriate backend for the model:
//   - Titan models: native Bedrock InvokeModel API (Titan format)
//   - Cohere embed models: native Bedrock InvokeModel API (Cohere format)
//   - All others: OpenAI-compatible Bedrock endpoint
//
// Multi-input requests are handled correctly across all backends. Titan does
// not natively support batch input, so each text is invoked separately and
// results merged; Cohere supports batch input natively and is invoked once.
func (c *Connector) Embeddings(ctx context.Context, params openai.EmbeddingNewParams) (*openai.CreateEmbeddingResponse, error) {
	c.logger.Debug(ctx, "bedrock: embeddings start",
		logging.Field{Key: "model", Value: params.Model},
	)

	var (
		resp *openai.CreateEmbeddingResponse
		err  error
	)

	switch c.dispatchBackend(params.Model) {
	case backendTitanEmbed:
		resp, err = c.titanEmbeddings(ctx, params)
	case backendCohereEmbed:
		resp, err = c.cohereEmbeddings(ctx, params)
	default:
		resp, err = c.openaiClient.Embeddings.New(ctx, params)
		if err != nil {
			err = convert.MapOpenAIError(inferencegw.ProviderBedrock, err)
		}
	}

	if err != nil {
		c.logger.Error(ctx, "bedrock: embeddings error",
			logging.Field{Key: "error", Value: err.Error()},
		)
		return nil, err
	}

	c.logger.Debug(ctx, "bedrock: embeddings done",
		logging.Field{Key: "count", Value: len(resp.Data)},
	)
	return resp, nil
}

// --- Titan embeddings ---

type titanEmbedRequest struct {
	InputText string `json:"inputText"`
}

type titanEmbedResponse struct {
	Embedding           []float64 `json:"embedding"`
	InputTextTokenCount int64     `json:"inputTextTokenCount"`
}

// titanEmbeddings calls the Bedrock InvokeModel API using the Amazon Titan
// embedding request format. Titan accepts a single string per request, so
// multi-input requests are dispatched as N sequential InvokeModel calls and the
// results merged into a single OpenAI-shaped response with Index preserved.
func (c *Connector) titanEmbeddings(ctx context.Context, params openai.EmbeddingNewParams) (*openai.CreateEmbeddingResponse, error) {
	inputs, err := extractEmbeddingInputs(params)
	if err != nil {
		return nil, fmt.Errorf("bedrock titan embeddings: %w", err)
	}

	data := make([]openai.Embedding, 0, len(inputs))
	var totalTokens int64

	for i, text := range inputs {
		body, err := json.Marshal(titanEmbedRequest{InputText: text})
		if err != nil {
			return nil, fmt.Errorf("bedrock titan embeddings: marshal request: %w", err)
		}

		out, err := c.runtimeClient.InvokeModel(ctx, &bedrockruntimeSDK.InvokeModelInput{
			ModelId:     aws.String(params.Model),
			Body:        body,
			ContentType: aws.String("application/json"),
			Accept:      aws.String("application/json"),
		})
		if err != nil {
			return nil, fmt.Errorf("bedrock titan embeddings: invoke model: %w", err)
		}

		var native titanEmbedResponse
		if err := json.Unmarshal(out.Body, &native); err != nil {
			return nil, fmt.Errorf("bedrock titan embeddings: unmarshal response: %w", err)
		}

		data = append(data, openai.Embedding{
			Object:    constant.Embedding("embedding"),
			Index:     int64(i),
			Embedding: native.Embedding,
		})
		totalTokens += native.InputTextTokenCount
	}

	return &openai.CreateEmbeddingResponse{
		Object: constant.List("list"),
		Model:  params.Model,
		Data:   data,
		Usage: openai.CreateEmbeddingResponseUsage{
			PromptTokens: totalTokens,
			TotalTokens:  totalTokens,
		},
	}, nil
}

// --- Cohere embeddings ---

type cohereEmbedRequest struct {
	Texts     []string `json:"texts"`
	InputType string   `json:"input_type"` // "search_document", "search_query", etc.
}

type cohereEmbedResponse struct {
	Embeddings [][]float64 `json:"embeddings"`
	Meta       struct {
		BilledUnits struct {
			InputTokens int64 `json:"input_tokens"`
		} `json:"billed_units"`
	} `json:"meta"`
}

// cohereEmbeddings calls the Bedrock InvokeModel API using the Cohere embed
// request format. Cohere accepts a batch of texts natively, so all inputs are
// sent in a single call and the response array is preserved one-to-one.
func (c *Connector) cohereEmbeddings(ctx context.Context, params openai.EmbeddingNewParams) (*openai.CreateEmbeddingResponse, error) {
	inputs, err := extractEmbeddingInputs(params)
	if err != nil {
		return nil, fmt.Errorf("bedrock cohere embeddings: %w", err)
	}

	body, err := json.Marshal(cohereEmbedRequest{
		Texts:     inputs,
		InputType: "search_document",
	})
	if err != nil {
		return nil, fmt.Errorf("bedrock cohere embeddings: marshal request: %w", err)
	}

	out, err := c.runtimeClient.InvokeModel(ctx, &bedrockruntimeSDK.InvokeModelInput{
		ModelId:     aws.String(params.Model),
		Body:        body,
		ContentType: aws.String("application/json"),
		Accept:      aws.String("application/json"),
	})
	if err != nil {
		return nil, fmt.Errorf("bedrock cohere embeddings: invoke model: %w", err)
	}

	var native cohereEmbedResponse
	if err := json.Unmarshal(out.Body, &native); err != nil {
		return nil, fmt.Errorf("bedrock cohere embeddings: unmarshal response: %w", err)
	}

	if len(native.Embeddings) == 0 {
		return nil, fmt.Errorf("bedrock cohere embeddings: response contained no embeddings")
	}
	if len(native.Embeddings) != len(inputs) {
		return nil, fmt.Errorf("bedrock cohere embeddings: response contained %d embeddings, expected %d", len(native.Embeddings), len(inputs))
	}

	data := make([]openai.Embedding, 0, len(native.Embeddings))
	for i, emb := range native.Embeddings {
		data = append(data, openai.Embedding{
			Object:    constant.Embedding("embedding"),
			Index:     int64(i),
			Embedding: emb,
		})
	}

	// Use billed token count from Cohere response metadata when available;
	// fall back to a word-count estimate so usage records are never zero.
	totalTokens := native.Meta.BilledUnits.InputTokens
	if totalTokens == 0 {
		for _, text := range inputs {
			totalTokens += int64(len(strings.Fields(text)))
		}
	}

	return &openai.CreateEmbeddingResponse{
		Object: constant.List("list"),
		Model:  params.Model,
		Data:   data,
		Usage: openai.CreateEmbeddingResponseUsage{
			PromptTokens: totalTokens,
			TotalTokens:  totalTokens,
		},
	}, nil
}

// --- shared helpers ---

// extractEmbeddingInputs returns the list of text inputs from params.Input.
// Returns at least one element on success.
func extractEmbeddingInputs(params openai.EmbeddingNewParams) ([]string, error) {
	if params.Input.OfString.Valid() {
		return []string{params.Input.OfString.Value}, nil
	}
	if len(params.Input.OfArrayOfStrings) > 0 {
		return params.Input.OfArrayOfStrings, nil
	}
	return nil, fmt.Errorf("input must be a non-empty string or array of strings (token arrays are not supported for native Titan/Cohere Bedrock models)")
}
