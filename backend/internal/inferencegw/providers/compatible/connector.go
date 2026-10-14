// Package compatible connects the gateway to an endpoint that speaks the
// OpenAI or Anthropic APIs: OpenAI, Anthropic, OpenRouter, vLLM. The
// endpoint's declared APIs pass through natively, except that a messages
// request for a non-Anthropic model has its trailing system messages folded.
// An undeclared API runs through an existing bridge when one of the declared
// APIs can carry it.
package compatible

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	anthropicopt "github.com/anthropics/anthropic-sdk-go/option"
	openai "github.com/openai/openai-go/v3"
	openaiopt "github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"

	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/clear-route/orbit/internal/inferencegw/providers/bridge"
	"github.com/clear-route/orbit/internal/inferencegw/providers/convert"
	"github.com/clear-route/orbit/internal/platform/clients/httpclient"
	ip "github.com/clear-route/orbit/internal/platform/inferenceproviders/v1alpha1"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// Config is one endpoint. BaseURL is the prefix of /chat/completions and
// /messages, e.g. https://openrouter.ai/api/v1. An empty APIKey sends none.
type Config struct {
	BaseURL string
	APIKey  string
	APIs    []ip.API

	// DecisionModels names models served on POST /systemone that the endpoint
	// does not mark as such in its model list. Only read when APIs has systemone.
	DecisionModels []string
}

// errMessagesBaseURL is returned when an endpoint serving Anthropic messages
// has a base URL that does not end in /v1; the Anthropic SDK appends v1/messages.
var errMessagesBaseURL = errors.New("an endpoint serving messages needs a base URL ending in /v1")

// Connector implements inferencegw.ProviderConnector for one endpoint.
type Connector struct {
	name      inferencegw.ProviderName
	serves    map[ip.API]bool
	openai    *openai.Client
	anthropic *anthropic.Client
	logger    logging.Logger

	// decisionOnly is set when the endpoint declares exactly systemone. It
	// has no OpenAI or Anthropic model list, so discovery reads /models itself.
	// A mixed endpoint (systemone with other APIs) reads it once more, for
	// its decision models.
	decisionOnly   bool
	decisionModels []string

	// baseURL, apiKey, http and decide serve the calls made without an SDK:
	// the model list (http) and systemone (decide), which have different
	// timeouts. They are set when the endpoint declares systemone.
	baseURL string
	apiKey  string
	http    *httpclient.Base
	decide  *httpclient.Base
}

// New builds a connector. The SDKs' environment defaults (OPENAI_API_KEY,
// ANTHROPIC_API_KEY, base URLs) are overridden; only cfg reaches the upstream.
func New(cfg Config, name inferencegw.ProviderName, logger logging.Logger) (*Connector, error) {
	if cfg.BaseURL == "" || len(cfg.APIs) == 0 {
		return nil, fmt.Errorf("compatible: a base URL and at least one API are required")
	}
	transport := httpclient.NewTracingTransport("orbit.compatible", nil)
	c := &Connector{name: name, serves: map[ip.API]bool{}, logger: logger}
	for _, api := range cfg.APIs {
		c.serves[api] = true
	}
	if c.serves[ip.APISystemOne] {
		c.decisionOnly = len(c.serves) == 1
		c.decisionModels = slices.Clone(cfg.DecisionModels)
		c.baseURL = strings.TrimSuffix(cfg.BaseURL, "/")
		c.apiKey = cfg.APIKey
		c.http = httpclient.New("orbit.compatible-list", logger, discoveryTimeout)
		c.decide = httpclient.New("orbit.compatible-systemone", logger, decisionTimeout)
	}

	openaiOpts := []openaiopt.RequestOption{
		openaiopt.WithBaseURL(cfg.BaseURL),
		openaiopt.WithAPIKey(cfg.APIKey),
		openaiopt.WithHTTPClient(httpclient.NewStreamingHTTPClient(transport)),
	}
	if cfg.APIKey == "" {
		openaiOpts = append(openaiOpts, openaiopt.WithHeaderDel("Authorization"))
	}
	oc := openai.NewClient(openaiOpts...)
	c.openai = &oc

	if c.serves[ip.APIMessages] {
		root, ok := strings.CutSuffix(strings.TrimSuffix(cfg.BaseURL, "/"), "/v1")
		if !ok {
			return nil, errMessagesBaseURL
		}
		anthropicOpts := []anthropicopt.RequestOption{
			anthropicopt.WithBaseURL(root),
			anthropicopt.WithAPIKey(cfg.APIKey),
			anthropicopt.WithHTTPClient(httpclient.NewStreamingHTTPClient(transport)),
		}
		if cfg.APIKey == "" {
			// The SDK refuses a request without a credential. A placeholder passes
			// its check and the middleware removes it before the request is sent.
			anthropicOpts = append(anthropicOpts, anthropicopt.WithAPIKey("none"), anthropicopt.WithMiddleware(dropAPIKey))
		}
		ac := anthropic.NewClient(anthropicOpts...)
		c.anthropic = &ac
	}
	return c, nil
}

// Name returns the provider name.
func (c *Connector) Name() inferencegw.ProviderName { return c.name }

// unsupported refuses an API the endpoint neither serves nor can bridge. It
// is retryable: the virtual model may route to a provider that serves it.
func (c *Connector) unsupported(api ip.API) error {
	return inferencegw.NewAPINotServedError(c.name, string(api))
}

// ListModels lists the endpoint's models through whichever API family it serves.
func (c *Connector) ListModels(ctx context.Context) ([]inferencegw.ProviderModel, error) {
	if c.decisionOnly {
		models, err := c.listDecisionModels(ctx)
		if err != nil {
			return nil, err
		}
		return c.withDecisionModels(models), nil
	}
	models, err := c.listChatModels(ctx)
	if err != nil || !c.serves[ip.APISystemOne] {
		return models, err
	}
	return c.withDecisionModels(c.mergeDecisionModels(ctx, models)), nil
}

// listChatModels lists the endpoint's models through the OpenAI or Anthropic API.
func (c *Connector) listChatModels(ctx context.Context) ([]inferencegw.ProviderModel, error) {
	var models []inferencegw.ProviderModel
	if c.serves[ip.APIChatCompletions] || c.serves[ip.APIResponses] || c.serves[ip.APIEmbeddings] {
		iter := c.openai.Models.ListAutoPaging(ctx)
		for iter.Next() {
			m := c.Describe(iter.Current().ID)
			m.Price = parsePrice(iter.Current().RawJSON())
			models = append(models, m)
		}
		if err := iter.Err(); err != nil {
			return nil, fmt.Errorf("%s: list models: %w", c.name, err)
		}
		return models, nil
	}
	iter := c.anthropic.Models.ListAutoPaging(ctx, anthropic.ModelListParams{})
	for iter.Next() {
		models = append(models, c.Describe(iter.Current().ID))
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("%s: list models: %w", c.name, err)
	}
	return models, nil
}

// Describe builds the catalog entry for a model id from the APIs this
// endpoint serves or bridges.
func (c *Connector) Describe(id string) inferencegw.ProviderModel {
	if c.decisionOnly || slices.Contains(c.decisionModels, id) {
		return c.describeDecision(id)
	}
	m := c.describeChat(id)
	if c.serves[ip.APIEmbeddings] && strings.Contains(strings.ToLower(id), "embed") {
		m.OutputModalities = []inferencegw.Modality{inferencegw.ModalityEmbedding}
		m.Capabilities = []inferencegw.ModelCapability{inferencegw.CapabilityEmbedding}
		return m
	}
	if c.serves[ip.APIChatCompletions] || c.serves[ip.APIMessages] {
		m.Dialects = append(m.Dialects, inferencegw.DialectOpenAI, inferencegw.DialectResponses)
	} else if c.serves[ip.APIResponses] {
		m.Dialects = append(m.Dialects, inferencegw.DialectResponses)
	}
	if c.serves[ip.APIMessages] {
		m.Dialects = append(m.Dialects, inferencegw.DialectAnthropic)
	}
	slices.Sort(m.Dialects)
	m.Dialects = slices.Compact(m.Dialects)
	return m
}

// describeChat is the base entry for a text model.
func (c *Connector) describeChat(id string) inferencegw.ProviderModel {
	text := []inferencegw.Modality{inferencegw.ModalityText}
	return inferencegw.ProviderModel{
		Provider:         c.name,
		ModelID:          id,
		Family:           inferencegw.InferFamily(id),
		DisplayName:      id,
		InputModalities:  text,
		OutputModalities: text,
		Capabilities:     []inferencegw.ModelCapability{inferencegw.CapabilityChat},
	}
}

// describeDecision is the entry for a model served on POST /systemone: the
// decision capability only, text input and no chat dialects.
func (c *Connector) describeDecision(id string) inferencegw.ProviderModel {
	m := c.describeChat(id)
	m.Capabilities = []inferencegw.ModelCapability{inferencegw.CapabilityDecision}
	return m
}

// ChatCompletion passes through, or converts to Anthropic messages.
func (c *Connector) ChatCompletion(ctx context.Context, params openai.ChatCompletionNewParams) (*inferencegw.OpenAIResult, error) {
	switch {
	case c.serves[ip.APIChatCompletions]:
		resp, err := c.openai.Chat.Completions.New(ctx, params)
		if err != nil {
			return nil, convert.MapOpenAIError(c.name, err)
		}
		return &inferencegw.OpenAIResult{Completion: resp}, nil
	case c.serves[ip.APIMessages]:
		startedAt := time.Now().Unix()
		msg, err := c.message(ctx, convert.OpenAIParamsToAnthropicParams(params))
		if err != nil {
			return nil, err
		}
		return &inferencegw.OpenAIResult{
			Completion:          convert.AnthropicMessageToOpenAI(msg, startedAt),
			CacheCreationTokens: msg.Usage.CacheCreationInputTokens,
		}, nil
	}
	return nil, c.unsupported(ip.APIChatCompletions)
}

// ChatCompletionStream passes through, or streams converted Anthropic events.
func (c *Connector) ChatCompletionStream(ctx context.Context, params openai.ChatCompletionNewParams) (<-chan inferencegw.OpenAIStreamResult, error) {
	switch {
	case c.serves[ip.APIChatCompletions]:
		return forward(c.openai.Chat.Completions.NewStreaming(ctx, params),
			func(chunk openai.ChatCompletionChunk) inferencegw.OpenAIStreamResult {
				return inferencegw.OpenAIStreamResult{Chunk: &chunk}
			},
			func(err error) inferencegw.OpenAIStreamResult {
				return inferencegw.OpenAIStreamResult{Err: convert.MapOpenAIError(c.name, err)}
			}), nil
	case c.serves[ip.APIMessages]:
		stream := c.anthropic.Messages.NewStreaming(ctx, convert.OpenAIParamsToAnthropicParams(params))
		return convert.AnthropicStreamToOpenAIChunks(stream, c.name, nil), nil
	}
	return nil, c.unsupported(ip.APIChatCompletions)
}

// Embeddings passes through when the endpoint serves embeddings.
func (c *Connector) Embeddings(ctx context.Context, params openai.EmbeddingNewParams) (*openai.CreateEmbeddingResponse, error) {
	if !c.serves[ip.APIEmbeddings] {
		return nil, c.unsupported(ip.APIEmbeddings)
	}
	resp, err := c.openai.Embeddings.New(ctx, params)
	if err != nil {
		return nil, convert.MapOpenAIError(c.name, err)
	}
	return resp, nil
}

// Responses passes through, or bridges to chat completions or messages.
func (c *Connector) Responses(ctx context.Context, params responses.ResponseNewParams) (*inferencegw.ResponsesResult, error) {
	switch {
	case c.serves[ip.APIResponses]:
		resp, err := c.openai.Responses.New(ctx, params)
		if err != nil {
			return nil, convert.MapOpenAIError(c.name, err)
		}
		return &inferencegw.ResponsesResult{Response: resp}, nil
	case c.serves[ip.APIChatCompletions]:
		return bridge.NewChatCompletionsResponsesBridge(c.openai, c.name, c.logger).Execute(ctx, params)
	case c.serves[ip.APIMessages]:
		return bridge.NewAnthropicResponsesBridge(c.anthropic, c.name, c.logger).Execute(ctx, params)
	}
	return nil, c.unsupported(ip.APIResponses)
}

// ResponsesStream passes through, or bridges to chat completions or messages.
func (c *Connector) ResponsesStream(ctx context.Context, params responses.ResponseNewParams) (<-chan inferencegw.ResponsesStreamResult, error) {
	switch {
	case c.serves[ip.APIResponses]:
		return forward(c.openai.Responses.NewStreaming(ctx, params),
			func(ev responses.ResponseStreamEventUnion) inferencegw.ResponsesStreamResult {
				return inferencegw.ResponsesStreamResult{Event: ev}
			},
			func(err error) inferencegw.ResponsesStreamResult {
				return inferencegw.ResponsesStreamResult{Err: convert.MapOpenAIError(c.name, err)}
			}), nil
	case c.serves[ip.APIChatCompletions]:
		return bridge.NewChatCompletionsResponsesBridge(c.openai, c.name, c.logger).ExecuteStream(ctx, params)
	case c.serves[ip.APIMessages]:
		return bridge.NewAnthropicResponsesBridge(c.anthropic, c.name, c.logger).ExecuteStream(ctx, params)
	}
	return nil, c.unsupported(ip.APIResponses)
}

// AnthropicMessages passes through when the endpoint serves messages, with
// trailing system messages folded for a non-Anthropic model. There is no
// bridge from messages to the OpenAI APIs.
func (c *Connector) AnthropicMessages(ctx context.Context, params anthropic.MessageNewParams) (*anthropic.Message, error) {
	if !c.serves[ip.APIMessages] {
		return nil, c.unsupported(ip.APIMessages)
	}
	return c.message(ctx, foldTrailingSystem(params))
}

// AnthropicMessagesStream passes a streaming messages request through, with
// trailing system messages folded for a non-Anthropic model.
func (c *Connector) AnthropicMessagesStream(ctx context.Context, params anthropic.MessageNewParams) (<-chan inferencegw.AnthropicStreamEvent, error) {
	if !c.serves[ip.APIMessages] {
		return nil, c.unsupported(ip.APIMessages)
	}
	return forward(c.anthropic.Messages.NewStreaming(ctx, foldTrailingSystem(params)),
		func(ev anthropic.MessageStreamEventUnion) inferencegw.AnthropicStreamEvent {
			return inferencegw.AnthropicStreamEvent{Event: ev}
		},
		func(err error) inferencegw.AnthropicStreamEvent {
			return inferencegw.AnthropicStreamEvent{Err: convert.MapAnthropicError(c.name, err)}
		}), nil
}

// message streams and accumulates one messages call. The SDK refuses a
// non-streaming call whose max_tokens could run past ten minutes.
func (c *Connector) message(ctx context.Context, params anthropic.MessageNewParams) (*anthropic.Message, error) {
	msg, _, err := convert.AccumulateAnthropicStream(c.anthropic.Messages.NewStreaming(ctx, params), c.name)
	return msg, err
}

// stream is the SSE stream shape both SDKs return.
type stream[E any] interface {
	Next() bool
	Current() E
	Err() error
	Close() error
}

// forward relays a stream's events on a channel, then its error, and closes
// the channel when the stream ends.
func forward[E, R any](s stream[E], event func(E) R, fail func(error) R) <-chan R {
	ch := make(chan R, 32)
	go func() {
		defer close(ch)
		defer func() { _ = s.Close() }()
		for s.Next() {
			ch <- event(s.Current())
		}
		if err := s.Err(); err != nil && !errors.Is(err, io.EOF) {
			ch <- fail(err)
		}
	}()
	return ch
}

// dropAPIKey sends a messages request with no key, for an endpoint that needs none.
func dropAPIKey(req *http.Request, next anthropicopt.MiddlewareNext) (*http.Response, error) {
	req.Header.Del("X-Api-Key")
	req.Header.Del("Authorization")
	return next(req)
}
