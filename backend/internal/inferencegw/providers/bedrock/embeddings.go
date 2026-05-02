package bedrock

import (
	"context"
	"encoding/json"
	"fmt"

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
// embedding request format and converts the response to OpenAI format.
func (c *Connector) titanEmbeddings(ctx context.Context, params openai.EmbeddingNewParams) (*openai.CreateEmbeddingResponse, error) {
	inputText, err := extractEmbeddingText(params)
	if err != nil {
		return nil, fmt.Errorf("bedrock titan embeddings: %w", err)
	}

	body, err := json.Marshal(titanEmbedRequest{InputText: inputText})
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

	return &openai.CreateEmbeddingResponse{
		Object: constant.List("list"),
		Model:  params.Model,
		Data: []openai.Embedding{{
			Object:    constant.Embedding("embedding"),
			Index:     0,
			Embedding: native.Embedding,
		}},
		Usage: openai.CreateEmbeddingResponseUsage{
			PromptTokens: native.InputTextTokenCount,
			TotalTokens:  native.InputTextTokenCount,
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
}

// cohereEmbeddings calls the Bedrock InvokeModel API using the Cohere embed
// request format and converts the first returned embedding to OpenAI format.
func (c *Connector) cohereEmbeddings(ctx context.Context, params openai.EmbeddingNewParams) (*openai.CreateEmbeddingResponse, error) {
	inputText, err := extractEmbeddingText(params)
	if err != nil {
		return nil, fmt.Errorf("bedrock cohere embeddings: %w", err)
	}

	body, err := json.Marshal(cohereEmbedRequest{
		Texts:     []string{inputText},
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

	return &openai.CreateEmbeddingResponse{
		Object: constant.List("list"),
		Model:  params.Model,
		Data: []openai.Embedding{{
			Object:    constant.Embedding("embedding"),
			Index:     0,
			Embedding: native.Embeddings[0],
		}},
	}, nil
}

// --- shared helpers ---

// extractEmbeddingText returns the first text input from params.Input.
// Native Bedrock embedding models accept a single string per request.
func extractEmbeddingText(params openai.EmbeddingNewParams) (string, error) {
	if params.Input.OfString.Valid() {
		return params.Input.OfString.Value, nil
	}
	if len(params.Input.OfArrayOfStrings) > 0 {
		return params.Input.OfArrayOfStrings[0], nil
	}
	return "", fmt.Errorf("input must be a string or array of strings for native Bedrock models")
}
