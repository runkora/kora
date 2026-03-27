package inferencegw

import (
	"context"
	"errors"
	"fmt"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/token"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("inferencegw")

// principal carries the extracted identity context for a single request.
// Populated from the verified PlatformClaims stored in context by AuthMiddleware.
type principal struct {
	TenantID       string
	PrincipalID    string
	PrincipalType  string
	IdentitySource string
}

// Service is the inference gateway service layer.
// It orchestrates policy evaluation, routing, provider calls,
// and usage attribution for every inference request.
// Auth is handled upstream by AuthMiddleware — the service reads
// the verified principal from context, never from raw headers.
type Service struct {
	policy  PolicyEvaluator
	router  *RoutingEngine
	pricing PricingCache
	usage   InferenceUsageRepository
	logger  logging.Logger
}

// NewService constructs a Service with all required dependencies.
func NewService(
	policy PolicyEvaluator,
	router *RoutingEngine,
	pricing PricingCache,
	usage InferenceUsageRepository,
	logger logging.Logger,
) *Service {
	return &Service{
		policy:  policy,
		router:  router,
		pricing: pricing,
		usage:   usage,
		logger:  logger,
	}
}

// ─── ListModels ───────────────────────────────────────────────────────────────

// ListModels queries all enabled provider connectors for their live model lists
// and returns the aggregated results.
func (s *Service) ListModels(ctx context.Context) ([]ProviderModel, error) {
	return s.router.ListModels(ctx)
}

// ─── ChatCompletion (non-streaming) ──────────────────────────────────────────

// ChatCompletion handles a non-streaming inference request end-to-end:
//  1. Read the verified principal from context (placed there by AuthMiddleware).
//  2. Evaluate policy.
//  3. Route to provider(s).
//  4. Call provider, retrying/failing-over on retryable errors.
//  5. Write usage record.
//  6. Return normalised response.
func (s *Service) ChatCompletion(
	ctx context.Context,
	dialect APIDialect,
	req ChatCompletionRequest,
) (ChatCompletionResponse, error) {
	ctx, span := tracer.Start(ctx, "inferencegw.ChatCompletion",
		trace.WithAttributes(
			attribute.String("model", req.Model),
			attribute.String("dialect", string(dialect)),
			attribute.Bool("stream", false),
		),
	)
	defer span.End()

	start := time.Now()

	// ── 1. Extract principal from context ─────────────────────────────────────
	p, err := principalFromClaims(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrTokenParseFailed.Error())
		return ChatCompletionResponse{}, fmt.Errorf("%w: %w", ErrTokenParseFailed, err)
	}

	span.SetAttributes(
		attribute.String("tenant_id", p.TenantID),
		attribute.String("principal_type", p.PrincipalType),
	)

	// ── 2. Route request ──────────────────────────────────────────────────────
	targets, strategy, err := s.router.RouteRequest(ctx, req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrRoutingFailed.Error())
		s.writeFailedUsage(ctx, p, req, dialect, nil, nil, UsageStatusRoutingFailed, "", 0)
		return ChatCompletionResponse{}, fmt.Errorf("%w: %w", ErrRoutingFailed, err)
	}

	// ── 3. Policy check ───────────────────────────────────────────────────────
	decision, err := s.policy.EvaluateInferenceRequest(ctx, &InferencePolicyRequest{
		TenantID:      p.TenantID,
		PrincipalID:   p.PrincipalID,
		PrincipalType: p.PrincipalType,
		Provider:      targets[0].Provider,
		ModelID:       targets[0].ModelID,
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrPolicyDenied.Error())
		s.writeFailedUsage(ctx, p, req, dialect, &targets[0].Provider, &strategy, UsageStatusPolicyDenied, "", 0)
		return ChatCompletionResponse{}, fmt.Errorf("%w: policy evaluation: %w", ErrPolicyDenied, err)
	}
	if !decision.Allowed {
		s.logger.Info(ctx, "inference request denied by policy",
			logging.Field{Key: "reason", Value: decision.Reason},
			logging.Field{Key: "principal_id", Value: p.PrincipalID},
		)
		span.SetStatus(codes.Error, ErrPolicyDenied.Error())
		s.writeFailedUsage(ctx, p, req, dialect, &targets[0].Provider, &strategy, UsageStatusPolicyDenied, "POLICY_DENIED", 0)
		return ChatCompletionResponse{}, ErrPolicyDenied
	}

	// ── 4. Call provider with retry / failover ────────────────────────────────
	resp, usedTarget, retryCount, callErr := s.callWithFailover(ctx, targets, dialect, req)

	latencyMS := int(time.Since(start).Milliseconds())

	if callErr != nil || usedTarget == nil {
		span.RecordError(callErr)
		span.SetStatus(codes.Error, ErrProviderError.Error())
		errCode := extractErrorCode(callErr)
		s.writeFailedUsage(ctx, p, req, dialect, nthProvider(targets), &strategy, UsageStatusProviderError, errCode, latencyMS)
		return ChatCompletionResponse{}, fmt.Errorf("%w: %w", ErrProviderError, callErr)
	}

	// ── 5. Write usage record ─────────────────────────────────────────────────
	pricing := usedTarget.Pricing
	estimatedCost := pricing.EstimatedCostUSD(resp.Usage.PromptTokens, resp.Usage.CompletionTokens)

	rec := InferenceUsageRecord{
		ID:               uuid.NewString(),
		TenantID:         p.TenantID,
		PrincipalID:      p.PrincipalID,
		PrincipalType:    p.PrincipalType,
		APIDialect:       dialect,
		Provider:         &usedTarget.Provider,
		ModelID:          usedTarget.ModelID,
		RoutingStrategy:  &strategy,
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		TotalTokens:      resp.Usage.TotalTokens,
		EstimatedCostUSD: estimatedCost,
		Status:           UsageStatusSuccess,
		RetryCount:       retryCount,
		LatencyMS:        latencyMS,
		CreatedAt:        time.Now().UTC(),
	}

	if writeErr := s.usage.Create(ctx, rec); writeErr != nil {
		s.logger.Error(ctx, "failed to write inference usage record",
			logging.Field{Key: "error", Value: writeErr.Error()},
			logging.Field{Key: "usage_id", Value: rec.ID},
		)
	}

	span.SetStatus(codes.Ok, "")
	return resp, nil
}

// ─── ChatCompletionStream (streaming) ────────────────────────────────────────

// ChatCompletionStream handles a streaming inference request.
// The handler is responsible for writing SSE frames; this method returns a
// channel that emits chunks and closes when the stream is done.
// On mid-stream error a final chunk with Err set is sent before the channel closes.
//
// The OTel span started here lives until the goroutine finishes draining the
// upstream channel — it covers the full streaming lifecycle, not just setup.
func (s *Service) ChatCompletionStream(
	ctx context.Context,
	dialect APIDialect,
	req ChatCompletionRequest,
) (<-chan ChatCompletionChunk, error) {
	// Span must be ended manually (not via defer) because it outlives the
	// function — it is closed inside the goroutine once the stream drains.
	ctx, span := tracer.Start(ctx, "inferencegw.ChatCompletionStream",
		trace.WithAttributes(
			attribute.String("model", req.Model),
			attribute.String("dialect", string(dialect)),
			attribute.Bool("stream", true),
		),
	)
	start := time.Now()

	// ── 1. Extract principal from context ─────────────────────────────────────
	p, err := principalFromClaims(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrTokenParseFailed.Error())
		span.End()
		return nil, fmt.Errorf("%w: %w", ErrTokenParseFailed, err)
	}

	span.SetAttributes(
		attribute.String("tenant_id", p.TenantID),
		attribute.String("principal_type", p.PrincipalType),
	)

	// ── 2. Route request ──────────────────────────────────────────────────────
	targets, strategy, err := s.router.RouteRequest(ctx, req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrRoutingFailed.Error())
		span.End()
		s.writeFailedUsage(ctx, p, req, dialect, nil, nil, UsageStatusRoutingFailed, "", 0)
		return nil, fmt.Errorf("%w: %w", ErrRoutingFailed, err)
	}

	// ── 3. Policy check ───────────────────────────────────────────────────────
	decision, err := s.policy.EvaluateInferenceRequest(ctx, &InferencePolicyRequest{
		TenantID:      p.TenantID,
		PrincipalID:   p.PrincipalID,
		PrincipalType: p.PrincipalType,
		Provider:      targets[0].Provider,
		ModelID:       targets[0].ModelID,
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrPolicyDenied.Error())
		span.End()
		s.writeFailedUsage(ctx, p, req, dialect, &targets[0].Provider, &strategy, UsageStatusPolicyDenied, "", 0)
		return nil, fmt.Errorf("%w: policy evaluation: %w", ErrPolicyDenied, err)
	}
	if !decision.Allowed {
		s.logger.Info(ctx, "streaming inference request denied by policy",
			logging.Field{Key: "reason", Value: decision.Reason},
			logging.Field{Key: "principal_id", Value: p.PrincipalID},
		)
		span.SetStatus(codes.Error, ErrPolicyDenied.Error())
		span.End()
		s.writeFailedUsage(ctx, p, req, dialect, &targets[0].Provider, &strategy, UsageStatusPolicyDenied, "POLICY_DENIED", 0)
		return nil, ErrPolicyDenied
	}

	// ── 4. Open stream from provider ──────────────────────────────────────────
	// Try providers in order until one accepts the stream.
	var (
		upstream   <-chan ChatCompletionChunk
		callErr    error
		usedTarget *RouteTarget
	)

	for _, target := range targets {
		connector, ok := s.router.Connector(target.Provider)
		if !ok {
			continue
		}

		callReq := req
		callReq.Model = target.ModelID

		upstream, callErr = dialectStream(connector, dialect, ctx, callReq)
		if callErr == nil {
			t := target
			usedTarget = &t
			break
		}
	}

	if callErr != nil || usedTarget == nil {
		span.RecordError(callErr)
		span.SetStatus(codes.Error, ErrProviderError.Error())
		span.End()
		s.writeFailedUsage(ctx, p, req, dialect, nthProvider(targets), &strategy, UsageStatusProviderError, extractErrorCode(callErr), 0)
		return nil, fmt.Errorf("%w: %w", ErrProviderError, callErr)
	}

	// ── 5. Proxy stream + write usage ─────────────────────────────────────────
	// Proxy the upstream channel, accumulating token counts for usage attribution.
	out := make(chan ChatCompletionChunk, 16)
	capturedTarget := usedTarget
	capturedStrategy := strategy

	// writeCtx is detached from the request context so the goroutine survives
	// client disconnect, but preserves trace/baggage values via WithoutCancel.
	// The span itself is ended inside the goroutine — it must outlive the HTTP handler.
	writeCtx := context.WithoutCancel(ctx)

	go func() {
		defer span.End()
		defer close(out)

		var (
			promptTokens, completionTokens, totalTokens int
			streamErr                                   error
		)

		for chunk := range upstream {
			if chunk.Err != nil {
				streamErr = chunk.Err
			}
			promptTokens += chunk.Usage.PromptTokens
			completionTokens += chunk.Usage.CompletionTokens
			totalTokens += chunk.Usage.TotalTokens
			out <- chunk
		}

		latencyMS := int(time.Since(start).Milliseconds())

		// totalTokens may be zero when the provider (e.g. Anthropic) emits input
		// and output token counts in separate events rather than a combined total.
		// Compute it from the parts so the usage record is always populated.
		if totalTokens == 0 {
			totalTokens = promptTokens + completionTokens
		}

		if streamErr != nil {
			span.RecordError(streamErr)
			span.SetStatus(codes.Error, ErrProviderError.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}

		pricing := capturedTarget.Pricing
		estimatedCost := pricing.EstimatedCostUSD(promptTokens, completionTokens)

		rec := InferenceUsageRecord{
			ID:               uuid.NewString(),
			TenantID:         p.TenantID,
			PrincipalID:      p.PrincipalID,
			PrincipalType:    p.PrincipalType,
			APIDialect:       dialect,
			Provider:         &capturedTarget.Provider,
			ModelID:          capturedTarget.ModelID,
			RoutingStrategy:  &capturedStrategy,
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      totalTokens,
			EstimatedCostUSD: estimatedCost,
			Status:           UsageStatusSuccess,
			LatencyMS:        latencyMS,
			CreatedAt:        time.Now().UTC(),
		}

		if writeErr := s.usage.Create(writeCtx, rec); writeErr != nil {
			s.logger.Error(writeCtx, "failed to write streaming inference usage record",
				logging.Field{Key: "error", Value: writeErr.Error()},
				logging.Field{Key: "usage_id", Value: rec.ID},
			)
		}
	}()

	return out, nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// principalFromClaims reads the verified PlatformClaims stored in context by
// AuthMiddleware and extracts the principal identity fields.
// Returns an error if AuthMiddleware has not run (i.e. claims are absent).
func principalFromClaims(ctx context.Context) (principal, error) {
	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		return principal{}, fmt.Errorf("no verified claims in context: AuthMiddleware may not have run")
	}
	return principal{
		TenantID:       claims.Orbit.TenantID,
		PrincipalID:    claims.Subject,
		PrincipalType:  string(claims.Orbit.PrincipalType),
		IdentitySource: string(claims.Orbit.IdentitySource),
	}, nil
}

func (s *Service) writeFailedUsage(
	ctx context.Context,
	p principal,
	req ChatCompletionRequest,
	dialect APIDialect,
	provider *ProviderName,
	strategy *RoutingStrategy,
	status UsageStatus,
	errorCode string,
	latencyMS int,
) {
	var errCodePtr *string
	if errorCode != "" {
		errCodePtr = &errorCode
	}

	rec := InferenceUsageRecord{
		ID:              uuid.NewString(),
		TenantID:        p.TenantID,
		PrincipalID:     p.PrincipalID,
		PrincipalType:   p.PrincipalType,
		APIDialect:      dialect,
		Provider:        provider,
		ModelID:         req.Model,
		RoutingStrategy: strategy,
		Status:          status,
		ErrorCode:       errCodePtr,
		LatencyMS:       latencyMS,
		CreatedAt:       time.Now().UTC(),
	}

	if err := s.usage.Create(ctx, rec); err != nil {
		s.logger.Error(ctx, "failed to write failed inference usage record",
			logging.Field{Key: "error", Value: err.Error()},
		)
	}
}

func extractErrorCode(err error) string {
	if err == nil {
		return ""
	}
	var connErr *ConnectorError
	if errors.As(err, &connErr) {
		return connErr.Code
	}
	return "UNKNOWN"
}

// callWithFailover iterates targets, retrying retryable errors up to the engine limits.
func (s *Service) callWithFailover(
	ctx context.Context,
	targets []RouteTarget,
	dialect APIDialect,
	req ChatCompletionRequest,
) (resp ChatCompletionResponse, usedTarget *RouteTarget, retryCount int, callErr error) {
	totalAttempts := 0
	for _, target := range targets {
		if totalAttempts >= s.router.MaxTotalAttempts() {
			break
		}
		connector, ok := s.router.Connector(target.Provider)
		if !ok {
			continue
		}
		for attempt := 0; attempt < s.router.MaxAttemptsPerTarget(); attempt++ {
			totalAttempts++
			if totalAttempts > s.router.MaxTotalAttempts() {
				break
			}
			callReq := req
			callReq.Model = target.ModelID
			resp, callErr = dialectCall(connector, dialect, ctx, callReq)
			if callErr == nil {
				t := target
				usedTarget = &t
				return
			}
			retryCount++
			var connErr *ConnectorError
			if !errors.As(callErr, &connErr) || !connErr.Retryable {
				break
			}
			s.logger.Info(ctx, "retrying after retryable provider error",
				logging.Field{Key: "provider", Value: string(target.Provider)},
				logging.Field{Key: "attempt", Value: attempt + 1},
				logging.Field{Key: "error_code", Value: connErr.Code},
			)
		}
		if callErr == nil {
			break
		}
	}
	return
}

func nthProvider(targets []RouteTarget) *ProviderName {
	if len(targets) > 0 {
		p := targets[0].Provider
		return &p
	}
	return nil
}

// ─── AnthropicMessages (zero-translation, non-streaming) ─────────────────────

// AnthropicMessages handles a non-streaming POST /v1/messages request using the
// zero-translation path: params and response are native Anthropic SDK types.
// Policy evaluation and usage attribution still apply.
func (s *Service) AnthropicMessages(
	ctx context.Context,
	params anthropic.MessageNewParams,
) (*anthropic.Message, error) {
	ctx, span := tracer.Start(ctx, "inferencegw.AnthropicMessages",
		trace.WithAttributes(
			attribute.String("model", params.Model),
			attribute.Bool("stream", false),
		),
	)
	defer span.End()

	start := time.Now()

	p, err := principalFromClaims(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrTokenParseFailed.Error())
		return nil, fmt.Errorf("%w: %w", ErrTokenParseFailed, err)
	}

	span.SetAttributes(
		attribute.String("tenant_id", p.TenantID),
		attribute.String("principal_type", p.PrincipalType),
	)

	// Route using a minimal request carrying just the model name.
	routeReq := ChatCompletionRequest{Model: params.Model}
	targets, strategy, err := s.router.RouteRequest(ctx, routeReq)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrRoutingFailed.Error())
		s.writeFailedUsage(ctx, p, routeReq, DialectAnthropic, nil, nil, UsageStatusRoutingFailed, "", 0)
		return nil, fmt.Errorf("%w: %w", ErrRoutingFailed, err)
	}

	decision, err := s.policy.EvaluateInferenceRequest(ctx, &InferencePolicyRequest{
		TenantID:      p.TenantID,
		PrincipalID:   p.PrincipalID,
		PrincipalType: p.PrincipalType,
		Provider:      targets[0].Provider,
		ModelID:       targets[0].ModelID,
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrPolicyDenied.Error())
		s.writeFailedUsage(ctx, p, routeReq, DialectAnthropic, &targets[0].Provider, &strategy, UsageStatusPolicyDenied, "", 0)
		return nil, fmt.Errorf("%w: policy evaluation: %w", ErrPolicyDenied, err)
	}
	if !decision.Allowed {
		s.logger.Info(ctx, "inference request denied by policy",
			logging.Field{Key: "reason", Value: decision.Reason},
			logging.Field{Key: "principal_id", Value: p.PrincipalID},
		)
		span.SetStatus(codes.Error, ErrPolicyDenied.Error())
		s.writeFailedUsage(ctx, p, routeReq, DialectAnthropic, &targets[0].Provider, &strategy, UsageStatusPolicyDenied, "POLICY_DENIED", 0)
		return nil, ErrPolicyDenied
	}

	// Try targets in order; no retry loop needed for non-streaming (connector handles
	// Bedrock's internal streaming transparently).
	var (
		msg        *anthropic.Message
		callErr    error
		usedTarget *RouteTarget
	)
	for _, target := range targets {
		connector, ok := s.router.Connector(target.Provider)
		if !ok {
			continue
		}
		callParams := params
		callParams.Model = target.ModelID
		msg, callErr = connector.AnthropicMessages(ctx, callParams)
		if callErr == nil {
			t := target
			usedTarget = &t
			break
		}
		var connErr *ConnectorError
		if !errors.As(callErr, &connErr) || !connErr.Retryable {
			break
		}
	}

	latencyMS := int(time.Since(start).Milliseconds())

	if callErr != nil || usedTarget == nil {
		span.RecordError(callErr)
		span.SetStatus(codes.Error, ErrProviderError.Error())
		s.writeFailedUsage(ctx, p, routeReq, DialectAnthropic, nthProvider(targets), &strategy, UsageStatusProviderError, extractErrorCode(callErr), latencyMS)
		return nil, fmt.Errorf("%w: %w", ErrProviderError, callErr)
	}

	promptTokens := int(msg.Usage.InputTokens)
	completionTokens := int(msg.Usage.OutputTokens)
	pricing := usedTarget.Pricing
	estimatedCost := pricing.EstimatedCostUSD(promptTokens, completionTokens)

	rec := InferenceUsageRecord{
		ID:               uuid.NewString(),
		TenantID:         p.TenantID,
		PrincipalID:      p.PrincipalID,
		PrincipalType:    p.PrincipalType,
		APIDialect:       DialectAnthropic,
		Provider:         &usedTarget.Provider,
		ModelID:          usedTarget.ModelID,
		RoutingStrategy:  &strategy,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
		EstimatedCostUSD: estimatedCost,
		Status:           UsageStatusSuccess,
		LatencyMS:        latencyMS,
		CreatedAt:        time.Now().UTC(),
	}

	if writeErr := s.usage.Create(ctx, rec); writeErr != nil {
		s.logger.Error(ctx, "failed to write inference usage record",
			logging.Field{Key: "error", Value: writeErr.Error()},
			logging.Field{Key: "usage_id", Value: rec.ID},
		)
	}

	span.SetStatus(codes.Ok, "")
	return msg, nil
}

// ─── AnthropicMessagesStream (zero-translation, streaming) ───────────────────

// AnthropicMessagesStream handles a streaming POST /v1/messages request using
// the zero-translation path. Raw SDK stream events are forwarded to the caller;
// usage is attributed after the stream closes by accumulating the full message.
func (s *Service) AnthropicMessagesStream(
	ctx context.Context,
	params anthropic.MessageNewParams,
) (<-chan AnthropicStreamEvent, error) {
	ctx, span := tracer.Start(ctx, "inferencegw.AnthropicMessagesStream",
		trace.WithAttributes(
			attribute.String("model", params.Model),
			attribute.Bool("stream", true),
		),
	)
	start := time.Now()

	p, err := principalFromClaims(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrTokenParseFailed.Error())
		span.End()
		return nil, fmt.Errorf("%w: %w", ErrTokenParseFailed, err)
	}

	span.SetAttributes(
		attribute.String("tenant_id", p.TenantID),
		attribute.String("principal_type", p.PrincipalType),
	)

	routeReq := ChatCompletionRequest{Model: params.Model}
	targets, strategy, err := s.router.RouteRequest(ctx, routeReq)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrRoutingFailed.Error())
		span.End()
		s.writeFailedUsage(ctx, p, routeReq, DialectAnthropic, nil, nil, UsageStatusRoutingFailed, "", 0)
		return nil, fmt.Errorf("%w: %w", ErrRoutingFailed, err)
	}

	decision, err := s.policy.EvaluateInferenceRequest(ctx, &InferencePolicyRequest{
		TenantID:      p.TenantID,
		PrincipalID:   p.PrincipalID,
		PrincipalType: p.PrincipalType,
		Provider:      targets[0].Provider,
		ModelID:       targets[0].ModelID,
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrPolicyDenied.Error())
		span.End()
		s.writeFailedUsage(ctx, p, routeReq, DialectAnthropic, &targets[0].Provider, &strategy, UsageStatusPolicyDenied, "", 0)
		return nil, fmt.Errorf("%w: policy evaluation: %w", ErrPolicyDenied, err)
	}
	if !decision.Allowed {
		s.logger.Info(ctx, "streaming anthropic request denied by policy",
			logging.Field{Key: "reason", Value: decision.Reason},
			logging.Field{Key: "principal_id", Value: p.PrincipalID},
		)
		span.SetStatus(codes.Error, ErrPolicyDenied.Error())
		span.End()
		s.writeFailedUsage(ctx, p, routeReq, DialectAnthropic, &targets[0].Provider, &strategy, UsageStatusPolicyDenied, "POLICY_DENIED", 0)
		return nil, ErrPolicyDenied
	}

	var (
		upstream   <-chan AnthropicStreamEvent
		callErr    error
		usedTarget *RouteTarget
	)
	for _, target := range targets {
		connector, ok := s.router.Connector(target.Provider)
		if !ok {
			continue
		}
		callParams := params
		callParams.Model = target.ModelID
		upstream, callErr = connector.AnthropicMessagesStream(ctx, callParams)
		if callErr == nil {
			t := target
			usedTarget = &t
			break
		}
	}

	if callErr != nil || usedTarget == nil {
		span.RecordError(callErr)
		span.SetStatus(codes.Error, ErrProviderError.Error())
		span.End()
		s.writeFailedUsage(ctx, p, routeReq, DialectAnthropic, nthProvider(targets), &strategy, UsageStatusProviderError, extractErrorCode(callErr), 0)
		return nil, fmt.Errorf("%w: %w", ErrProviderError, callErr)
	}

	out := make(chan AnthropicStreamEvent, 16)
	capturedTarget := usedTarget
	capturedStrategy := strategy
	writeCtx := context.WithoutCancel(ctx)

	go func() {
		defer span.End()
		defer close(out)

		var (
			accumulated anthropic.Message
			streamErr   error
		)

		for ev := range upstream {
			if ev.Err != nil {
				streamErr = ev.Err
				out <- ev
				break
			}
			// Accumulate for usage tracking; ignore accumulation errors (non-fatal).
			_ = accumulated.Accumulate(ev.Event)
			out <- ev
		}

		latencyMS := int(time.Since(start).Milliseconds())

		if streamErr != nil {
			span.RecordError(streamErr)
			span.SetStatus(codes.Error, ErrProviderError.Error())
			s.writeFailedUsage(writeCtx, p, routeReq, DialectAnthropic, &capturedTarget.Provider, &capturedStrategy, UsageStatusProviderError, extractErrorCode(streamErr), latencyMS)
			return
		}

		span.SetStatus(codes.Ok, "")

		promptTokens := int(accumulated.Usage.InputTokens)
		completionTokens := int(accumulated.Usage.OutputTokens)
		pricing := capturedTarget.Pricing
		estimatedCost := pricing.EstimatedCostUSD(promptTokens, completionTokens)

		rec := InferenceUsageRecord{
			ID:               uuid.NewString(),
			TenantID:         p.TenantID,
			PrincipalID:      p.PrincipalID,
			PrincipalType:    p.PrincipalType,
			APIDialect:       DialectAnthropic,
			Provider:         &capturedTarget.Provider,
			ModelID:          capturedTarget.ModelID,
			RoutingStrategy:  &capturedStrategy,
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
			EstimatedCostUSD: estimatedCost,
			Status:           UsageStatusSuccess,
			LatencyMS:        latencyMS,
			CreatedAt:        time.Now().UTC(),
		}

		if writeErr := s.usage.Create(writeCtx, rec); writeErr != nil {
			s.logger.Error(writeCtx, "failed to write streaming anthropic usage record",
				logging.Field{Key: "error", Value: writeErr.Error()},
				logging.Field{Key: "usage_id", Value: rec.ID},
			)
		}
	}()

	return out, nil
}

// ─── dialect dispatch helpers ─────────────────────────────────────────────────

// dialectCall invokes the correct non-streaming connector method for the given
// dialect. The connector is never responsible for making this decision.
func dialectCall(
	c ProviderConnector,
	dialect APIDialect,
	ctx context.Context,
	req ChatCompletionRequest,
) (ChatCompletionResponse, error) {
	if dialect == DialectAnthropic {
		return c.AnthropicChatCompletion(ctx, req)
	}
	return c.ChatCompletion(ctx, req)
}

// dialectStream invokes the correct streaming connector method for the given
// dialect. The connector is never responsible for making this decision.
func dialectStream(
	c ProviderConnector,
	dialect APIDialect,
	ctx context.Context,
	req ChatCompletionRequest,
) (<-chan ChatCompletionChunk, error) {
	if dialect == DialectAnthropic {
		return c.AnthropicChatCompletionStream(ctx, req)
	}
	return c.ChatCompletionStream(ctx, req)
}
