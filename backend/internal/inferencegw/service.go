package inferencegw

import (
	"context"
	"errors"
	"fmt"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	openai "github.com/openai/openai-go"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/policy"
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
	policy  policy.Evaluator
	router  *RoutingEngine
	pricing PricingCache
	usage   InferenceUsageRepository
	logger  logging.Logger
}

// NewService constructs a Service with all required dependencies.
func NewService(
	pol policy.Evaluator,
	router *RoutingEngine,
	pricing PricingCache,
	usage InferenceUsageRepository,
	logger logging.Logger,
) *Service {
	return &Service{
		policy:  pol,
		router:  router,
		pricing: pricing,
		usage:   usage,
		logger:  logger,
	}
}

// ─── ListModels ───────────────────────────────────────────────────────────────

// ListModels queries all enabled provider connectors for their live model lists,
// then filters the results to only models the principal is granted access to.
func (s *Service) ListModels(ctx context.Context) ([]ProviderModel, error) {
	models, err := s.router.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	return s.filterModelsByPolicy(ctx, models)
}

// ─── ChatCompletion (non-streaming) ──────────────────────────────────────────

// ChatCompletion handles a non-streaming inference request end-to-end.
// The full openai.ChatCompletionNewParams wire type is passed through to the
// connector unchanged — tool definitions, response format, logprobs, and all
// other fields are preserved with zero translation loss.
func (s *Service) ChatCompletion(
	ctx context.Context,
	params openai.ChatCompletionNewParams,
) (*openai.ChatCompletion, error) {
	ctx, span := tracer.Start(ctx, "inferencegw.ChatCompletion",
		trace.WithAttributes(
			attribute.String("model", params.Model),
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
		return nil, fmt.Errorf("%w: %w", ErrTokenParseFailed, err)
	}

	span.SetAttributes(
		attribute.String("tenant_id", p.TenantID),
		attribute.String("principal_type", p.PrincipalType),
	)

	routeReq := ChatCompletionRequest{Model: params.Model}

	// ── 2. Policy check ───────────────────────────────────────────────────────
	// Fine-grained check: inference:{family}:{alias}:use.
	// Wildcard routing and unknown models fall through to the coarse
	// inference:invoke scope enforced at Layer 1 by Envoy.
	if err = s.checkModelPolicy(ctx, params.Model); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrPolicyDenied.Error())
		s.writeFailedUsage(ctx, p, routeReq, DialectOpenAI, nil, nil, UsageStatusPolicyDenied, "POLICY_DENIED", 0)
		return nil, err
	}

	// ── 3. Route request ──────────────────────────────────────────────────────
	targets, strategy, err := s.router.RouteRequest(ctx, routeReq)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrRoutingFailed.Error())
		s.writeFailedUsage(ctx, p, routeReq, DialectOpenAI, nil, nil, UsageStatusRoutingFailed, "", 0)
		return nil, fmt.Errorf("%w: %w", ErrRoutingFailed, err)
	}

	// ── 3b. Policy-filter targets for wildcard routing ────────────────────────
	if s.router.IsWildcardModel(params.Model) {
		targets, err = s.filterTargetsByPolicy(ctx, targets)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, ErrPolicyDenied.Error())
			s.writeFailedUsage(ctx, p, routeReq, DialectOpenAI, nil, &strategy, UsageStatusPolicyDenied, "POLICY_DENIED", 0)
			return nil, err
		}
	}

	// ── 4. Call provider with retry / failover ────────────────────────────────
	resp, usedTarget, retryCount, callErr := s.callWithFailover(ctx, targets, params)

	latencyMS := int(time.Since(start).Milliseconds())

	if callErr != nil || usedTarget == nil {
		span.RecordError(callErr)
		span.SetStatus(codes.Error, ErrProviderError.Error())
		errCode := extractErrorCode(callErr)
		s.writeFailedUsage(ctx, p, routeReq, DialectOpenAI, nthProvider(targets), &strategy, UsageStatusProviderError, errCode, latencyMS)
		return nil, fmt.Errorf("%w: %w", ErrProviderError, callErr)
	}

	// ── 5. Write usage record ─────────────────────────────────────────────────
	promptTokens := int(resp.Usage.PromptTokens)
	completionTokens := int(resp.Usage.CompletionTokens)
	totalTokens := int(resp.Usage.TotalTokens)
	if totalTokens == 0 {
		totalTokens = promptTokens + completionTokens
	}

	pricing := usedTarget.Pricing
	estimatedCost := pricing.EstimatedCostUSD(promptTokens, completionTokens)

	rec := InferenceUsageRecord{
		ID:               uuid.NewString(),
		TenantID:         p.TenantID,
		PrincipalID:      p.PrincipalID,
		PrincipalType:    p.PrincipalType,
		APIDialect:       DialectOpenAI,
		Provider:         &usedTarget.Provider,
		ModelID:          usedTarget.ModelID,
		RoutingStrategy:  &strategy,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      totalTokens,
		EstimatedCostUSD: estimatedCost,
		Status:           UsageStatusSuccess,
		RetryCount:       retryCount,
		LatencyMS:        latencyMS,
		CreatedAt:        start.UTC(),
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
// channel that emits OpenAIStreamResult values and closes when the stream ends.
// On mid-stream error a final result with Err set is sent before the channel closes.
//
// The OTel span started here lives until the goroutine finishes draining the
// upstream channel — it covers the full streaming lifecycle, not just setup.
func (s *Service) ChatCompletionStream(
	ctx context.Context,
	params openai.ChatCompletionNewParams,
) (<-chan OpenAIStreamResult, error) {
	// Span must be ended manually (not via defer) because it outlives the
	// function — it is closed inside the goroutine once the stream drains.
	ctx, span := tracer.Start(ctx, "inferencegw.ChatCompletionStream",
		trace.WithAttributes(
			attribute.String("model", params.Model),
			attribute.Bool("stream", true),
		),
	)
	start := time.Now()

	// ── 1. Extract principal ───────────────────────────────────────────────────
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

	// ── 2. Policy check ───────────────────────────────────────────────────────
	if err = s.checkModelPolicy(ctx, params.Model); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrPolicyDenied.Error())
		span.End()
		s.writeFailedUsage(ctx, p, routeReq, DialectOpenAI, nil, nil, UsageStatusPolicyDenied, "POLICY_DENIED", 0)
		return nil, err
	}

	// ── 3. Route ───────────────────────────────────────────────────────────────
	targets, strategy, err := s.router.RouteRequest(ctx, routeReq)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrRoutingFailed.Error())
		span.End()
		s.writeFailedUsage(ctx, p, routeReq, DialectOpenAI, nil, nil, UsageStatusRoutingFailed, "", 0)
		return nil, fmt.Errorf("%w: %w", ErrRoutingFailed, err)
	}

	// ── 3b. Policy-filter targets for wildcard routing ────────────────────────
	if s.router.IsWildcardModel(params.Model) {
		targets, err = s.filterTargetsByPolicy(ctx, targets)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, ErrPolicyDenied.Error())
			span.End()
			s.writeFailedUsage(ctx, p, routeReq, DialectOpenAI, nil, &strategy, UsageStatusPolicyDenied, "POLICY_DENIED", 0)
			return nil, err
		}
	}

	// ── 4. Open stream from provider ──────────────────────────────────────────
	// Inject stream_options so providers that support it return usage data.
	streamParams := params
	streamParams.StreamOptions = openai.ChatCompletionStreamOptionsParam{
		IncludeUsage: openai.Bool(true),
	}

	upstream, usedTarget, streamRetryCount, callErr := retryTargets(ctx, s, targets, "retrying after retryable provider error",
		func(c ProviderConnector, target RouteTarget) (<-chan OpenAIStreamResult, error) {
			q := streamParams
			q.Model = target.ModelID
			return c.ChatCompletionStream(ctx, q)
		})

	if callErr != nil || usedTarget == nil {
		span.RecordError(callErr)
		span.SetStatus(codes.Error, ErrProviderError.Error())
		span.End()
		s.writeFailedUsage(ctx, p, routeReq, DialectOpenAI, nthProvider(targets), &strategy, UsageStatusProviderError, extractErrorCode(callErr), 0)
		return nil, fmt.Errorf("%w: %w", ErrProviderError, callErr)
	}

	// ── 5. Proxy stream + write usage ─────────────────────────────────────────
	out := make(chan OpenAIStreamResult, 16)
	capturedTarget := usedTarget
	capturedStrategy := strategy

	// writeCtx is detached from the request context so the goroutine survives
	// client disconnect, but preserves trace/baggage values via WithoutCancel.
	writeCtx := context.WithoutCancel(ctx)

	go func() {
		defer span.End()
		defer close(out)

		promptTokens, completionTokens, totalTokens, streamErr := drainOpenAIStream(ctx, upstream, out)

		latencyMS := int(time.Since(start).Milliseconds())

		if totalTokens == 0 {
			totalTokens = promptTokens + completionTokens
		}

		if streamErr != nil {
			span.RecordError(streamErr)
			span.SetStatus(codes.Error, ErrProviderError.Error())
			s.writeFailedUsage(writeCtx, p, routeReq, DialectOpenAI, &capturedTarget.Provider, &capturedStrategy, UsageStatusProviderError, extractErrorCode(streamErr), latencyMS)
			return
		}

		span.SetStatus(codes.Ok, "")

		pricing := capturedTarget.Pricing
		estimatedCost := pricing.EstimatedCostUSD(promptTokens, completionTokens)

		rec := InferenceUsageRecord{
			ID:               uuid.NewString(),
			TenantID:         p.TenantID,
			PrincipalID:      p.PrincipalID,
			PrincipalType:    p.PrincipalType,
			APIDialect:       DialectOpenAI,
			Provider:         &capturedTarget.Provider,
			ModelID:          capturedTarget.ModelID,
			RoutingStrategy:  &capturedStrategy,
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      totalTokens,
			EstimatedCostUSD: estimatedCost,
			Status:           UsageStatusSuccess,
			RetryCount:       streamRetryCount,
			LatencyMS:        latencyMS,
			CreatedAt:        start.UTC(),
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

// drainOpenAIStream forwards chunks from upstream to out, accumulating token
// usage counts. If ctx is canceled before the stream ends, the remaining
// upstream items are drained asynchronously so the connector goroutine can exit.
func drainOpenAIStream(
	ctx context.Context,
	upstream <-chan OpenAIStreamResult,
	out chan<- OpenAIStreamResult,
) (promptTokens, completionTokens, totalTokens int, streamErr error) {
	for result := range upstream {
		if result.Err != nil {
			streamErr = result.Err
			select {
			case out <- result:
			case <-ctx.Done():
			}
			return
		}
		// Accumulate usage — some providers send it only in the final chunk.
		// Additive accumulation handles providers that split across chunks.
		if result.Chunk != nil {
			promptTokens += int(result.Chunk.Usage.PromptTokens)
			completionTokens += int(result.Chunk.Usage.CompletionTokens)
			totalTokens += int(result.Chunk.Usage.TotalTokens)
		}
		select {
		case out <- result:
		case <-ctx.Done():
			// Client disconnected; drain upstream so the connector goroutine can exit.
			go func() {
				for range upstream {
				}
			}()
			return
		}
	}
	return
}

// drainAnthropicStream forwards events from upstream to out, accumulating the
// full message via the SDK Accumulate helper. If ctx is canceled before the
// stream ends, the remaining upstream items are drained asynchronously.
func drainAnthropicStream(
	ctx context.Context,
	upstream <-chan AnthropicStreamEvent,
	out chan<- AnthropicStreamEvent,
) (accumulated anthropic.Message, streamErr error) {
	for ev := range upstream {
		if ev.Err != nil {
			streamErr = ev.Err
			select {
			case out <- ev:
			case <-ctx.Done():
			}
			return
		}
		_ = accumulated.Accumulate(ev.Event)
		select {
		case out <- ev:
		case <-ctx.Done():
			// Client disconnected; drain upstream so the connector goroutine can exit.
			go func() {
				for range upstream {
				}
			}()
			return
		}
	}
	return
}

// checkModelPolicy performs the fine-grained Layer 2 policy check for a specific
// model request. It resolves the requested model to its canonical (family, alias)
// and checks inference:{family}:{alias}:use against the OPA bundle.
//
// This check runs once before routing, not per-target inside the failover loop.
// Failover targets are not independently re-checked — a principal allowed to
// invoke the requested model is allowed to reach any provider that serves it.
//
// Wildcard routing ("", "*", "failover") returns nil here — those paths are
// policy-filtered post-routing via filterTargetsByPolicy.
//
// For all other models, if the model cannot be resolved to a catalog entry,
// the request is denied (fail closed).
func (s *Service) checkModelPolicy(ctx context.Context, model string) error {
	// Wildcards are handled post-routing by filterTargetsByPolicy.
	if s.router.IsWildcardModel(model) {
		return nil
	}

	info, ok := s.router.ResolveModelForPolicy(model)
	if !ok {
		// Model not in catalog — fail closed.
		return ErrPolicyDenied
	}
	claims, _ := token.ClaimsFromContext(ctx)
	result, err := s.policy.Check(ctx, policy.CheckInput{
		Claims:       claims,
		Delegation:   policy.DelegationFromContext(ctx),
		ResourceType: "inference",
		PathSegments: []string{info.Family, info.Alias},
		Action:       "use",
	})
	if err != nil {
		return fmt.Errorf("policy check: %w", err)
	}
	if !result.Allow {
		return ErrPolicyDenied
	}
	return nil
}

// filterTargetsByPolicy batch-filters routing targets through the OPA policy
// engine. Used for wildcard routing strategies where multiple targets are
// resolved and each must be individually checked against the principal's grants.
//
// Targets whose model cannot be resolved in the catalog are excluded (fail closed).
// Returns ErrPolicyDenied if no targets survive the filter.
func (s *Service) filterTargetsByPolicy(ctx context.Context, targets []RouteTarget) ([]RouteTarget, error) {
	claims, _ := token.ClaimsFromContext(ctx)

	// Deduplicate candidates by (family, alias) — multiple providers may serve
	// the same logical model and share the same policy scope.
	type policyKey struct{ family, alias string }
	seen := make(map[policyKey]string) // policyKey → candidate ID
	var candidates []policy.Candidate

	for _, t := range targets {
		info, ok := s.router.ResolveTargetForPolicy(t.Provider, t.ModelID)
		if !ok {
			continue // not in catalog — excluded
		}
		pk := policyKey{info.Family, info.Alias}
		if _, exists := seen[pk]; exists {
			continue
		}
		id := info.Family + ":" + info.Alias
		seen[pk] = id
		candidates = append(candidates, policy.Candidate{
			ID:           id,
			PathSegments: []string{info.Family, info.Alias},
		})
	}

	if len(candidates) == 0 {
		return nil, ErrPolicyDenied
	}

	allowedIDs, err := s.policy.Query(ctx, policy.QueryInput{
		Claims:       claims,
		Delegation:   policy.DelegationFromContext(ctx),
		ResourceType: "inference",
		Action:       "use",
		Candidates:   candidates,
	})
	if err != nil {
		return nil, fmt.Errorf("policy filter: %w", err)
	}

	allowedSet := make(map[string]bool, len(allowedIDs))
	for _, id := range allowedIDs {
		allowedSet[id] = true
	}

	filtered := make([]RouteTarget, 0, len(targets))
	for _, t := range targets {
		info, ok := s.router.ResolveTargetForPolicy(t.Provider, t.ModelID)
		if !ok {
			continue
		}
		if allowedSet[info.Family+":"+info.Alias] {
			filtered = append(filtered, t)
		}
	}

	if len(filtered) == 0 {
		return nil, ErrPolicyDenied
	}
	return filtered, nil
}

// filterModelsByPolicy filters the model catalog to only models the principal
// is granted access to. Models are deduplicated by (family, PolicyAlias) before
// querying OPA — if a logical model is allowed, all provider variants are returned.
func (s *Service) filterModelsByPolicy(ctx context.Context, models []ProviderModel) ([]ProviderModel, error) {
	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		return nil, fmt.Errorf("%w: no verified claims in context", ErrTokenParseFailed)
	}

	type policyKey struct{ family, alias string }
	seen := make(map[policyKey]string)
	var candidates []policy.Candidate

	for _, m := range models {
		if m.Family == "" || m.Family == "unknown" {
			continue
		}
		alias := m.PolicyAlias()
		pk := policyKey{m.Family, alias}
		if _, exists := seen[pk]; exists {
			continue
		}
		id := m.Family + ":" + alias
		seen[pk] = id
		candidates = append(candidates, policy.Candidate{
			ID:           id,
			PathSegments: []string{m.Family, alias},
		})
	}

	if len(candidates) == 0 {
		return nil, nil
	}

	allowedIDs, err := s.policy.Query(ctx, policy.QueryInput{
		Claims:       claims,
		Delegation:   policy.DelegationFromContext(ctx),
		ResourceType: "inference",
		Action:       "use",
		Candidates:   candidates,
	})
	if err != nil {
		return nil, fmt.Errorf("policy filter: %w", err)
	}

	allowedSet := make(map[string]bool, len(allowedIDs))
	for _, id := range allowedIDs {
		allowedSet[id] = true
	}

	filtered := make([]ProviderModel, 0, len(models))
	for _, m := range models {
		if m.Family == "" || m.Family == "unknown" {
			continue
		}
		if allowedSet[m.Family+":"+m.PolicyAlias()] {
			filtered = append(filtered, m)
		}
	}

	return filtered, nil
}

// principalFromClaims reads the verified PlatformClaims stored in context by
// AuthMiddleware and extracts the principal identity fields.
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

// retryTargets is the shared retry scaffolding used by all call paths.
// It iterates targets, calling fn for each, and retries retryable errors within
// each target up to the engine's per-target and total attempt limits.
func retryTargets[T any](
	ctx context.Context,
	s *Service,
	targets []RouteTarget,
	logMsg string,
	fn func(ProviderConnector, RouteTarget) (T, error),
) (result T, usedTarget *RouteTarget, retryCount int, callErr error) {
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
			if totalAttempts >= s.router.MaxTotalAttempts() {
				break
			}
			totalAttempts++
			result, callErr = fn(connector, target)
			if callErr == nil {
				t := target
				usedTarget = &t
				return
			}
			var connErr *ConnectorError
			if !errors.As(callErr, &connErr) || !connErr.Retryable {
				break
			}
			retryCount++
			// Brief backoff before retrying to avoid hammering throttled providers.
			// THROTTLED (429) errors almost always fail again if retried immediately.
			time.Sleep(500 * time.Millisecond)
			s.logger.Info(ctx, logMsg,
				logging.Field{Key: "provider", Value: string(target.Provider)},
				logging.Field{Key: "attempt", Value: attempt + 1},
				logging.Field{Key: "error_code", Value: connErr.Code},
			)
		}
	}
	return
}

// callWithFailover wraps retryTargets for non-streaming OpenAI requests.
// Policy is evaluated per target so that failover targets are also checked.
func (s *Service) callWithFailover(
	ctx context.Context,
	targets []RouteTarget,
	params openai.ChatCompletionNewParams,
) (*openai.ChatCompletion, *RouteTarget, int, error) {
	return retryTargets(ctx, s, targets, "retrying after retryable provider error",
		func(c ProviderConnector, target RouteTarget) (*openai.ChatCompletion, error) {
			q := params
			q.Model = target.ModelID
			return c.ChatCompletion(ctx, q)
		})
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

	routeReq := ChatCompletionRequest{Model: params.Model}

	// ── Policy check ──────────────────────────────────────────────────────────
	if err = s.checkModelPolicy(ctx, params.Model); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrPolicyDenied.Error())
		s.writeFailedUsage(ctx, p, routeReq, DialectAnthropic, nil, nil, UsageStatusPolicyDenied, "POLICY_DENIED", 0)
		return nil, err
	}

	targets, strategy, err := s.router.RouteRequest(ctx, routeReq)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrRoutingFailed.Error())
		s.writeFailedUsage(ctx, p, routeReq, DialectAnthropic, nil, nil, UsageStatusRoutingFailed, "", 0)
		return nil, fmt.Errorf("%w: %w", ErrRoutingFailed, err)
	}

	// Policy-filter targets for wildcard routing.
	if s.router.IsWildcardModel(params.Model) {
		targets, err = s.filterTargetsByPolicy(ctx, targets)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, ErrPolicyDenied.Error())
			s.writeFailedUsage(ctx, p, routeReq, DialectAnthropic, nil, &strategy, UsageStatusPolicyDenied, "POLICY_DENIED", 0)
			return nil, err
		}
	}

	msg, usedTarget, retryCount, callErr := retryTargets(ctx, s, targets, "retrying after retryable anthropic provider error",
		func(c ProviderConnector, target RouteTarget) (*anthropic.Message, error) {
			q := params
			q.Model = target.ModelID
			return c.AnthropicMessages(ctx, q)
		})

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
		RetryCount:       retryCount,
		LatencyMS:        latencyMS,
		CreatedAt:        start.UTC(),
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

	// ── Policy check ──────────────────────────────────────────────────────────
	if err = s.checkModelPolicy(ctx, params.Model); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrPolicyDenied.Error())
		span.End()
		s.writeFailedUsage(ctx, p, routeReq, DialectAnthropic, nil, nil, UsageStatusPolicyDenied, "POLICY_DENIED", 0)
		return nil, err
	}

	targets, strategy, err := s.router.RouteRequest(ctx, routeReq)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrRoutingFailed.Error())
		span.End()
		s.writeFailedUsage(ctx, p, routeReq, DialectAnthropic, nil, nil, UsageStatusRoutingFailed, "", 0)
		return nil, fmt.Errorf("%w: %w", ErrRoutingFailed, err)
	}

	// Policy-filter targets for wildcard routing.
	if s.router.IsWildcardModel(params.Model) {
		targets, err = s.filterTargetsByPolicy(ctx, targets)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, ErrPolicyDenied.Error())
			span.End()
			s.writeFailedUsage(ctx, p, routeReq, DialectAnthropic, nil, &strategy, UsageStatusPolicyDenied, "POLICY_DENIED", 0)
			return nil, err
		}
	}

	upstream, usedTarget, streamRetryCount, callErr := retryTargets(ctx, s, targets, "retrying after retryable anthropic stream setup error",
		func(c ProviderConnector, target RouteTarget) (<-chan AnthropicStreamEvent, error) {
			q := params
			q.Model = target.ModelID
			return c.AnthropicMessagesStream(ctx, q)
		})

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

		accumulated, streamErr := drainAnthropicStream(ctx, upstream, out)

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
			RetryCount:       streamRetryCount,
			LatencyMS:        latencyMS,
			CreatedAt:        start.UTC(),
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
