package inferencegw

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/token"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/clear-route/orbit/internal/platform/storage"
)

var tracer = otel.Tracer("inferencegw")

// principal carries the extracted identity context for a single request.
// Populated from the verified PlatformClaims stored in context by AuthMiddleware.
// TokenID is only populated for PAT-authenticated requests (PrincipalType == "PAT");
// it holds the PAT record UUID from claims.Orbit.PatID. It is empty for all
// other principal types — those callers have no PAT record to reference.
type principal struct {
	TenantID       string
	PrincipalID    string
	PrincipalType  string
	IdentitySource string
	TokenID        string // PAT record UUID; empty for non-PAT principals
}

// Service is the inference gateway service layer.
// It orchestrates policy evaluation, routing, provider calls,
// and usage attribution for every inference request.
// Auth is handled upstream by AuthMiddleware — the service reads
// the verified principal from context, never from raw headers.
type Service struct {
	policy        policy.Evaluator
	router        *RoutingEngine
	pricing       PricingCache
	usage         InferenceUsageRepository
	budgetCounter SpendCounter
	roleCache     *roleCache
	logger        logging.Logger
}

// NewService constructs a Service with all required dependencies.
func NewService(
	pol policy.Evaluator,
	router *RoutingEngine,
	pricing PricingCache,
	usage InferenceUsageRepository,
	budgetCounter SpendCounter,
	logger logging.Logger,
) *Service {
	return &Service{
		policy:        pol,
		router:        router,
		pricing:       pricing,
		usage:         usage,
		budgetCounter: budgetCounter,
		roleCache:     newRoleCache(roleCacheTTL),
		logger:        logger,
	}
}

// ─── ListModels ───────────────────────────────────────────────────────────────

// ListModels returns the enabled virtual models from the catalog that the
// principal is granted access to. This is the consumer-facing model list —
// clients request models by virtual model ID (family/name).
func (s *Service) ListModels(ctx context.Context) ([]VirtualModel, error) {
	vms := s.router.VirtualModels()
	return s.filterVirtualModelsByPolicy(ctx, vms)
}

// ProviderModels returns the most recently discovered provider model list from
// the routing engine's cache. Used to enrich the consumer /v1/models response.
func (s *Service) ProviderModels() []ProviderModel {
	return s.router.ProviderModels()
}

// DiscoverProviderModels triggers a live discovery sweep and returns the results.
// Used as a lazy fallback when the cache is empty.
func (s *Service) DiscoverProviderModels(ctx context.Context) ([]ProviderModel, error) {
	return s.router.ListModels(ctx)
}

// PricingAll returns all model pricing entries from the in-memory cache.
// Used by handleListModels to embed effective pricing in the /v1/models response.
func (s *Service) PricingAll() []ModelPricing {
	return s.pricing.All()
}

// filterVirtualModelsByPolicy filters virtual models to only those the
// principal holds a policy grant for.
func (s *Service) filterVirtualModelsByPolicy(ctx context.Context, vms []VirtualModel) ([]VirtualModel, error) {
	if len(vms) == 0 {
		return nil, nil
	}

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		return nil, fmt.Errorf("%w: no verified claims in context", ErrTokenParseFailed)
	}

	candidates := make([]policy.Candidate, len(vms))
	for i, vm := range vms {
		candidates[i] = policy.Candidate{
			ID:           vm.Family + ":" + vm.Name,
			PathSegments: []string{vm.Family, vm.Name},
		}
	}

	allowedIDs, err := s.policy.Query(ctx, policy.QueryInput{
		Claims:       claims,
		Delegation:   policy.DelegationFromContext(ctx),
		ResourceType: "inference",
		Action:       "use",
		Candidates:   candidates,
	})
	if err != nil {
		return nil, fmt.Errorf("policy query: %w", err)
	}

	allowed := make(map[string]bool, len(allowedIDs))
	for _, id := range allowedIDs {
		allowed[id] = true
	}

	out := make([]VirtualModel, 0, len(vms))
	for _, vm := range vms {
		if allowed[vm.Family+":"+vm.Name] {
			out = append(out, vm)
		}
	}
	return out, nil
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
		usageStatus, usageCode, spanMsg := modelPolicyFailure(err)
		span.SetStatus(codes.Error, spanMsg)
		s.writeFailedUsage(ctx, p, routeReq, DialectOpenAI, nil, nil, nil, usageStatus, usageCode, nil, 0)
		return nil, err
	}

	// ── 2b. Budget check ─────────────────────────────────────────────────────
	if ctx, err = s.checkBudget(ctx, p); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, spanStatusBudgetExceeded)
		s.writeFailedUsage(ctx, p, routeReq, DialectOpenAI, nil, nil, nil, UsageStatusBudgetExceeded, "BUDGET_EXCEEDED", nil, 0)
		return nil, err
	}

	// ── 3. Route request ──────────────────────────────────────────────────────
	targets, strategy, err := s.router.RouteRequest(ctx, routeReq)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrRoutingFailed.Error())
		s.writeFailedUsage(ctx, p, routeReq, DialectOpenAI, nil, nil, nil, UsageStatusRoutingFailed, "", nil, 0)
		return nil, fmt.Errorf("%w: %w", ErrRoutingFailed, err)
	}

	// ── 3b. Policy-filter targets for wildcard routing ────────────────────────
	if s.router.IsWildcardModel(params.Model) {
		targets, err = s.filterTargetsByPolicy(ctx, targets)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, ErrPolicyDenied.Error())
			s.writeFailedUsage(ctx, p, routeReq, DialectOpenAI, nil, nthModelFamily(targets), &strategy, UsageStatusPolicyDenied, "POLICY_DENIED", nil, 0)
			return nil, err
		}
	}

	// ── 4. Call provider with retry / failover ────────────────────────────────
	result, usedTarget, retryCount, callErr := s.callWithFailover(ctx, targets, params)

	latencyMS := int(time.Since(start).Milliseconds())

	if callErr != nil || usedTarget == nil {
		span.RecordError(callErr)
		span.SetStatus(codes.Error, ErrProviderError.Error())
		s.logProviderError(ctx, params.Model, callErr)
		errCode := extractErrorCode(callErr)
		s.writeFailedUsage(ctx, p, routeReq, DialectOpenAI, nthProvider(targets), nthModelFamily(targets), &strategy, UsageStatusProviderError, errCode, extractFailureDetails(callErr), latencyMS)
		return nil, fmt.Errorf("%w: %w", ErrProviderError, callErr)
	}

	// ── 5. Write usage record ─────────────────────────────────────────────────
	resp := result.Completion
	promptTokens := int(resp.Usage.PromptTokens)
	completionTokens := int(resp.Usage.CompletionTokens)
	totalTokens := int(resp.Usage.TotalTokens)
	if totalTokens == 0 {
		totalTokens = promptTokens + completionTokens
	}
	cacheReadTokens := int(resp.Usage.PromptTokensDetails.CachedTokens)
	cacheCreationTokens := int(result.CacheCreationTokens)

	pricing := s.resolvePricing(usedTarget.Provider, usedTarget.ModelID)
	uncachedPromptTokens := disjointPromptTokens(promptTokens, cacheReadTokens, cacheCreationTokens)
	estimatedCost := pricing.estimatedCostUSD(uncachedPromptTokens, completionTokens, cacheReadTokens, cacheCreationTokens)

	rec := InferenceUsageRecord{
		ID:                           uuid.NewString(),
		TenantID:                     p.TenantID,
		PrincipalID:                  p.PrincipalID,
		PrincipalType:                p.PrincipalType,
		TokenID:                      storage.NullIfZero(p.TokenID),
		APIDialect:                   DialectOpenAI,
		Provider:                     &usedTarget.Provider,
		VirtualModelID:               storage.NullIfZero(usedTarget.VirtualModelID),
		ModelID:                      usedTarget.ModelID,
		ModelFamily:                  storage.NullIfZero(usedTarget.ModelFamily),
		RoutingStrategy:              &strategy,
		PromptTokens:                 uncachedPromptTokens,
		CompletionTokens:             completionTokens,
		TotalTokens:                  totalTokens,
		CacheReadInputTokens:         cacheReadTokens,
		CacheCreationInputTokens:     cacheCreationTokens,
		EstimatedCostUSD:             estimatedCost,
		PriceUnknown:                 pricing.priceUnknown,
		EffectiveInputCostPer1M:      pricing.inputCostPer1M,
		EffectiveOutputCostPer1M:     pricing.outputCostPer1M,
		EffectiveCacheReadCostPer1M:  pricing.cacheReadCostPer1M,
		EffectiveCacheWriteCostPer1M: pricing.cacheWriteCostPer1M,
		Status:                       UsageStatusSuccess,
		RetryCount:                   retryCount,
		LatencyMS:                    latencyMS,
		CreatedAt:                    start.UTC(),
	}

	if writeErr := s.writeUsage(ctx, rec); writeErr != nil {
		s.logger.Error(ctx, "failed to write inference usage record",
			logging.Field{Key: "error", Value: writeErr.Error()},
			logging.Field{Key: "usage_id", Value: rec.ID},
		)
	} else {
		s.incrementSpend(ctx, p.TenantID, p.PrincipalID, rec.EstimatedCostUSD)
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
		usageStatus, usageCode, spanMsg := modelPolicyFailure(err)
		span.SetStatus(codes.Error, spanMsg)
		span.End()
		s.writeFailedUsage(ctx, p, routeReq, DialectOpenAI, nil, nil, nil, usageStatus, usageCode, nil, 0)
		return nil, err
	}

	// ── 2b. Budget check ─────────────────────────────────────────────────────
	if ctx, err = s.checkBudget(ctx, p); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, spanStatusBudgetExceeded)
		span.End()
		s.writeFailedUsage(ctx, p, routeReq, DialectOpenAI, nil, nil, nil, UsageStatusBudgetExceeded, "BUDGET_EXCEEDED", nil, 0)
		return nil, err
	}

	// ── 3. Route ───────────────────────────────────────────────────────────────
	targets, strategy, err := s.router.RouteRequest(ctx, routeReq)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrRoutingFailed.Error())
		span.End()
		s.writeFailedUsage(ctx, p, routeReq, DialectOpenAI, nil, nil, nil, UsageStatusRoutingFailed, "", nil, 0)
		return nil, fmt.Errorf("%w: %w", ErrRoutingFailed, err)
	}

	// ── 3b. Policy-filter targets for wildcard routing ────────────────────────
	if s.router.IsWildcardModel(params.Model) {
		targets, err = s.filterTargetsByPolicy(ctx, targets)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, ErrPolicyDenied.Error())
			span.End()
			s.writeFailedUsage(ctx, p, routeReq, DialectOpenAI, nil, nthModelFamily(targets), &strategy, UsageStatusPolicyDenied, "POLICY_DENIED", nil, 0)
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
		s.logProviderError(ctx, params.Model, callErr)
		s.writeFailedUsage(ctx, p, routeReq, DialectOpenAI, nthProvider(targets), nthModelFamily(targets), &strategy, UsageStatusProviderError, extractErrorCode(callErr), extractFailureDetails(callErr), 0)
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

		promptTokens, completionTokens, totalTokens, cacheReadTokens, cacheCreationTokens, streamErr := drainOpenAIStream(ctx, upstream, out)

		latencyMS := int(time.Since(start).Milliseconds())

		if totalTokens == 0 {
			totalTokens = promptTokens + completionTokens
		}

		if streamErr != nil {
			if errors.Is(streamErr, errClientDisconnected) {
				span.SetStatus(codes.Ok, "client disconnected")
				s.writeFailedUsage(writeCtx, p, routeReq, DialectOpenAI, &capturedTarget.Provider, storage.NullIfZero(capturedTarget.ModelFamily), &capturedStrategy, UsageStatusCanceled, "CLIENT_DISCONNECTED", nil, latencyMS)
				return
			}
			span.RecordError(streamErr)
			span.SetStatus(codes.Error, ErrProviderError.Error())
			s.logProviderError(writeCtx, params.Model, streamErr)
			s.writeFailedUsage(writeCtx, p, routeReq, DialectOpenAI, &capturedTarget.Provider, storage.NullIfZero(capturedTarget.ModelFamily), &capturedStrategy, UsageStatusProviderError, extractErrorCode(streamErr), extractFailureDetails(streamErr), latencyMS)
			return
		}

		span.SetStatus(codes.Ok, "")

		pricing := s.resolvePricing(capturedTarget.Provider, capturedTarget.ModelID)
		uncachedPromptTokens := disjointPromptTokens(promptTokens, cacheReadTokens, cacheCreationTokens)
		estimatedCost := pricing.estimatedCostUSD(uncachedPromptTokens, completionTokens, cacheReadTokens, cacheCreationTokens)

		rec := InferenceUsageRecord{
			ID:                           uuid.NewString(),
			TenantID:                     p.TenantID,
			PrincipalID:                  p.PrincipalID,
			PrincipalType:                p.PrincipalType,
			TokenID:                      storage.NullIfZero(p.TokenID),
			APIDialect:                   DialectOpenAI,
			Provider:                     &capturedTarget.Provider,
			VirtualModelID:               storage.NullIfZero(capturedTarget.VirtualModelID),
			ModelID:                      capturedTarget.ModelID,
			ModelFamily:                  storage.NullIfZero(capturedTarget.ModelFamily),
			RoutingStrategy:              &capturedStrategy,
			PromptTokens:                 uncachedPromptTokens,
			CompletionTokens:             completionTokens,
			TotalTokens:                  totalTokens,
			CacheReadInputTokens:         cacheReadTokens,
			CacheCreationInputTokens:     cacheCreationTokens,
			EstimatedCostUSD:             estimatedCost,
			PriceUnknown:                 pricing.priceUnknown,
			EffectiveInputCostPer1M:      pricing.inputCostPer1M,
			EffectiveOutputCostPer1M:     pricing.outputCostPer1M,
			EffectiveCacheReadCostPer1M:  pricing.cacheReadCostPer1M,
			EffectiveCacheWriteCostPer1M: pricing.cacheWriteCostPer1M,
			Status:                       UsageStatusSuccess,
			RetryCount:                   streamRetryCount,
			LatencyMS:                    latencyMS,
			CreatedAt:                    start.UTC(),
		}

		if writeErr := s.writeUsage(writeCtx, rec); writeErr != nil {
			s.logger.Error(writeCtx, "failed to write streaming inference usage record",
				logging.Field{Key: "error", Value: writeErr.Error()},
				logging.Field{Key: "usage_id", Value: rec.ID},
			)
		} else {
			s.incrementSpend(writeCtx, p.TenantID, p.PrincipalID, rec.EstimatedCostUSD)
		}
	}()

	return out, nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// errClientDisconnected is returned by drain functions when the caller's context
// is canceled before the upstream stream completes. It signals that the client
// disconnected rather than an upstream provider error, so usage can be recorded
// as canceled rather than as a provider failure.
var errClientDisconnected = errors.New("client disconnected")

// drainOpenAIStream forwards chunks from upstream to out, accumulating token
// usage counts. Both the outer receive and inner forward check ctx so a
// client disconnect is detected promptly even when upstream is idle.
func drainOpenAIStream(
	ctx context.Context,
	upstream <-chan OpenAIStreamResult,
	out chan<- OpenAIStreamResult,
) (promptTokens, completionTokens, totalTokens, cacheReadTokens, cacheCreationTokens int, streamErr error) {
	for {
		select {
		case result, ok := <-upstream:
			if !ok {
				return
			}
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
				cacheReadTokens += int(result.Chunk.Usage.PromptTokensDetails.CachedTokens)
			}
			cacheCreationTokens += int(result.CacheCreationTokens)
			select {
			case out <- result:
			case <-ctx.Done():
				go func() {
					for range upstream {
					}
				}()
				streamErr = errClientDisconnected
				return
			}
		case <-ctx.Done():
			go func() {
				for range upstream {
				}
			}()
			streamErr = errClientDisconnected
			return
		}
	}
}

// drainAnthropicStream forwards events from upstream to out, accumulating the
// full message via the SDK Accumulate helper. Both the outer receive and inner
// forward check ctx so a client disconnect is detected promptly even when
// upstream is idle.
func drainAnthropicStream(
	ctx context.Context,
	upstream <-chan AnthropicStreamEvent,
	out chan<- AnthropicStreamEvent,
) (accumulated anthropic.Message, streamErr error) {
	for {
		select {
		case ev, ok := <-upstream:
			if !ok {
				return
			}
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
				go func() {
					for range upstream {
					}
				}()
				streamErr = errClientDisconnected
				return
			}
		case <-ctx.Done():
			go func() {
				for range upstream {
				}
			}()
			streamErr = errClientDisconnected
			return
		}
	}
}

// checkModelPolicy performs the fine-grained Layer 2 policy check for a specific
// model request. It resolves the requested model to its canonical (family, alias)
// and checks inference:{family}:{alias}:use against the OPA bundle.
//
// This check runs once before routing, not per-target inside the failover loop.
// Failover targets are not independently re-checked — a principal allowed to
// invoke the requested model is allowed to reach any provider that serves it.
//
// NOTE: IsWildcardModel always returns false — the virtual model catalog requires
// consumers to specify an explicit virtual model ID. The wildcard branch below
// is intentionally unreachable with the current implementation. If wildcard
// routing is added in the future, filterTargetsByPolicy provides the per-target
// policy filter needed for that path.
func (s *Service) checkModelPolicy(ctx context.Context, model string) error {
	// Wildcards are handled post-routing by filterTargetsByPolicy.
	// TODO: implement wildcard model support when required.
	if s.router.IsWildcardModel(model) {
		return nil
	}

	info, ok := s.router.ResolveModelForPolicy(model)
	if !ok {
		// Model is not in the virtual-model catalog. Return a distinct not-found
		// error so callers can distinguish a typo/wrong model name from a real
		// authorization denial for a known model.
		return fmt.Errorf("%w: %q", ErrModelNotFound, model)
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
		// Carry the OPA deny reason verbatim: it distinguishes which leg of the
		// delegated intersection denied (deny:delegation_required,
		// deny:no_delegator_grant, deny:no_agent_grant, …) and flows through
		// the 403 body into the run's visible error
		// (docs/adr/2026-07-23-delegated-agent-identity.md, Phase 5).
		return fmt.Errorf("%w: %s", ErrPolicyDenied, result.Reason)
	}
	return nil
}

// modelPolicyFailure maps a checkModelPolicy error to the usage status, usage
// code, and span message to record. An unknown model (ErrModelNotFound) is a
// client 404, not an authorization failure, so it must not be recorded as a
// policy denial in telemetry or usage; every other failure keeps the existing
// policy-denied treatment.
func modelPolicyFailure(err error) (status UsageStatus, code, spanMsg string) {
	if errors.Is(err, ErrModelNotFound) {
		return UsageStatusModelNotFound, "MODEL_NOT_FOUND", err.Error()
	}
	return UsageStatusPolicyDenied, "POLICY_DENIED", ErrPolicyDenied.Error()
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

const spanStatusBudgetExceeded = "budget exceeded"

// writeUsage stamps the run identity onto the record and persists it. Role
// membership is NOT written here: it is single-sourced in the user_profiles
// snapshot (see
// docs/adr/2026-06-15-role-budget-attribution-for-non-interactive-credentials.md),
// and role-period spend is computed by joining usage to current membership at
// read time — so the principal_roles column is no longer populated and is
// slated for removal in a follow-up migration.
func (s *Service) writeUsage(ctx context.Context, rec InferenceUsageRecord) error {
	rec.RunID = runIDFromContext(ctx)
	rec.AgentSlug = agentSlugFromPrincipal(rec.PrincipalType, rec.PrincipalID)
	// Stamp the delegator from the VERIFIED delegation context — set by the
	// DelegationMiddleware after signature/blocklist checks, never from
	// anything the caller asserts. Spend then rolls up to the person as well
	// as the agent (delegated-agent-identity ADR, Decision 5).
	if d := policy.DelegationFromContext(ctx); d != nil && d.Sub != "" {
		delegatorID := d.Sub
		delegatorType := d.DelegatorPrincipalType
		rec.DelegatorID = &delegatorID
		rec.DelegatorPrincipalType = &delegatorType
	}
	return s.usage.Create(ctx, rec)
}

// budgetScopesFor derives the caller's applicable ceiling configuration: which
// canonical counters to read/write (windows, pools, tenant, run) and the
// attribution categories. Limits come from bundle data; role membership and
// run identity come from request context. Every lookup fails open.
func (s *Service) budgetScopesFor(ctx context.Context, p principal) (SpendScopes, []policy.BudgetCategory, []string) {
	// Live JWT roles (interactive browser sessions) win. When the credential
	// carries none — every PAT and agent token — fall back to the principal's
	// current role snapshot from user_profiles, so role budgets attribute and
	// enforce against non-interactive inference too. See
	// docs/adr/2026-06-15-role-budget-attribution-for-non-interactive-credentials.md
	roles := rolesFromContext(ctx)
	if len(roles) == 0 {
		roles = s.resolveRolesCached(ctx, p.TenantID, p.PrincipalID)
	}
	// De-duplicate before anything downstream consumes roles: the login flow
	// merges OIDC roles + groups, so the same role can appear twice and would
	// otherwise double-count a pooled role budget's per-category attribution.
	roles = dedupeRoles(roles)

	grantCats, err := s.policy.GetBudgetCategories(ctx, p.TenantID, p.PrincipalID)
	if err != nil {
		grantCats = nil
	}
	roleCats, err := s.policy.GetRoleBudgets(ctx, p.TenantID, roles)
	if err != nil {
		roleCats = nil
	}
	tenantBudget, err := s.policy.GetTenantBudget(ctx, p.TenantID)
	if err != nil {
		tenantBudget = nil
	}

	windowSet := map[string]struct{}{}
	var perRunLimit float64
	for _, c := range grantCats {
		if c.LimitUSD > 0 {
			windowSet[c.ResetCadence] = struct{}{}
		}
		if c.PerRunLimitUSD > perRunLimit {
			perRunLimit = c.PerRunLimitUSD
		}
	}
	pools := map[string]string{}
	for _, c := range roleCats {
		if c.LimitUSD <= 0 {
			continue
		}
		if c.Pooled {
			pools[c.ID] = c.ResetCadence
		} else {
			windowSet[c.ResetCadence] = struct{}{}
		}
	}
	windows := make([]string, 0, len(windowSet))
	for w := range windowSet {
		windows = append(windows, w)
	}
	sort.Strings(windows)

	scopes := SpendScopes{
		TenantID:         p.TenantID,
		PrincipalID:      p.PrincipalID,
		PrincipalWindows: windows,
		PoolWindows:      pools,
	}
	if tenantBudget != nil {
		scopes.TenantWindow = tenantBudget.ResetCadence
	}

	// Delegator ceilings (delegated-agent-identity ADR, Decision 5): when a
	// VERIFIED delegation is present, the delegator's per-window limits and
	// per-run limit apply as additional independent ceilings.
	var delegatorPerRunLimit float64
	scopes.DelegatorID, scopes.DelegatorWindows, delegatorPerRunLimit = s.delegatorBudgetScopes(ctx, p.TenantID)

	// Read the run counter only when a per-run ceiling exists (the agent's or
	// the delegator's); the write path sets RunID unconditionally so per-run
	// cost is always attributed.
	scopes.RunID = runCounterID(ctx, perRunLimit, delegatorPerRunLimit)

	return scopes, append(grantCats, roleCats...), roles
}

// runCounterID returns the run identity to READ the run counter under — only
// when a per-run ceiling exists (the agent's or, under delegation, the
// delegator's). Empty otherwise.
func runCounterID(ctx context.Context, perRunLimit, delegatorPerRunLimit float64) string {
	if perRunLimit <= 0 && delegatorPerRunLimit <= 0 {
		return ""
	}
	return runIDFromContext(ctx)
}

// delegatorBudgetScopes resolves the delegator's ceiling configuration from
// the VERIFIED delegation context: their id, the windows carrying configured
// limits, and their per-run limit. The delegator's limits come from the same
// bundle data the person's own enforcement uses, so the ceilings a delegated
// run adds are exactly the ceilings the person already lives under. Fails
// open on lookup errors, consistent with budgetScopesFor; returns zero values
// for non-delegated traffic.
func (s *Service) delegatorBudgetScopes(ctx context.Context, tenantID string) (string, []string, float64) {
	d := policy.DelegationFromContext(ctx)
	if d == nil || d.Sub == "" {
		return "", nil, 0
	}

	cats, err := s.policy.GetBudgetCategories(ctx, tenantID, d.Sub)
	if err != nil {
		cats = nil
	}

	windowSet := map[string]struct{}{}
	var perRunLimit float64
	for _, c := range cats {
		if c.LimitUSD > 0 {
			windowSet[c.ResetCadence] = struct{}{}
		}
		if c.PerRunLimitUSD > perRunLimit {
			perRunLimit = c.PerRunLimitUSD
		}
	}
	windows := make([]string, 0, len(windowSet))
	for w := range windowSet {
		windows = append(windows, w)
	}
	sort.Strings(windows)
	return d.Sub, windows, perRunLimit
}

// checkBudget evaluates the orbit.budget hierarchical ceilings via the policy
// evaluator. It reads every applicable canonical counter (principal windows,
// role pools, tenant cap, run), then delegates the decision to OPA, which
// denies if ANY ceiling is met. Fails open on any infrastructure error so
// that Redis or OPA unavailability never blocks inference traffic.
//
// Note: enforcement is soft — check and increment are not atomic. Concurrent
// requests near a limit may both pass and briefly overspend before the counter
// catches up. This is acceptable for cost-cap (not hard billing) use cases.
func (s *Service) checkBudget(ctx context.Context, p principal) (context.Context, error) {
	if s.budgetCounter == nil {
		return ctx, nil
	}

	ctx, span := tracer.Start(ctx, "budget.quota_check",
		trace.WithAttributes(
			attribute.String("tenant_id", p.TenantID),
			attribute.String("principal_id", p.PrincipalID),
		),
	)
	defer span.End()

	scopes, _, roles := s.budgetScopesFor(ctx, p)

	spends, err := s.budgetCounter.GetSpends(ctx, scopes)
	if err != nil {
		s.logger.Warn(ctx, "budget.fail_open",
			logging.Field{Key: "reason", Value: "spend counter get failed"},
			logging.Field{Key: "error", Value: err.Error()},
			logging.Field{Key: "principal_id", Value: p.PrincipalID},
		)
		span.RecordError(err)
		return ctx, nil
	}

	result, err := s.policy.CheckBudget(ctx, p.TenantID, policy.BudgetCheckRequest{
		PrincipalID:   p.PrincipalID,
		PrincipalType: p.PrincipalType,
		Roles:         roles,
		RunID:         scopes.RunID,
		// From the verified delegation context via budgetScopesFor — never
		// caller-asserted. Empty for non-delegated traffic.
		DelegatorID: scopes.DelegatorID,
		Spends:      spends,
	})
	if err != nil {
		s.logger.Warn(ctx, "budget.fail_open",
			logging.Field{Key: "reason", Value: "OPA budget check failed"},
			logging.Field{Key: "error", Value: err.Error()},
			logging.Field{Key: "principal_id", Value: p.PrincipalID},
		)
		span.RecordError(err)
		return ctx, nil
	}

	if !result.Allow {
		fields := []logging.Field{
			{Key: "principal_id", Value: p.PrincipalID},
			{Key: "tenant_id", Value: p.TenantID},
		}
		var breached []string
		for _, c := range result.Ceilings {
			if c.Reached() {
				fields = append(fields, logging.Field{
					Key:   "breached_" + c.Scope,
					Value: fmt.Sprintf("%s %s: %.4f/%.4f", c.ID, c.Window, c.SpendUSD, c.LimitUSD),
				})
				breached = append(breached, c.Scope)
			}
		}
		s.logger.Info(ctx, "budget.hard_block", fields...)
		span.SetStatus(codes.Error, spanStatusBudgetExceeded)
		if len(breached) > 0 {
			// Name the breached ceiling scope(s) in the error so the run's
			// visible refusal distinguishes e.g. the delegator's ceiling from
			// the agent's (delegated-agent-identity ADR, Phase 5: refusal
			// reasons distinguish which leg denied). Scopes only — never
			// amounts — so nothing about another principal's budget leaks.
			return ctx, fmt.Errorf("%w: the %s spending ceiling has been reached", ErrBudgetExceeded, strings.Join(breached, ", "))
		}
		return ctx, ErrBudgetExceeded
	}

	return ctx, nil
}

// incrementSpend best-effort increments every applicable canonical counter
// and the per-category attribution counters after a usage write. Logs on
// failure but never returns an error — a counter increment failure must not
// affect the caller's control flow.
func (s *Service) incrementSpend(ctx context.Context, tenantID, principalID string, amount float64) {
	if s.budgetCounter == nil {
		return
	}
	scopes, categories, _ := s.budgetScopesFor(ctx, principal{TenantID: tenantID, PrincipalID: principalID})
	// Write the run counter whenever run identity exists, even without a
	// configured per-run ceiling — it keeps per-run cost warm and auditable.
	scopes.RunID = runIDFromContext(ctx)
	if err := s.budgetCounter.IncrementSpends(ctx, scopes, categories, amount); err != nil {
		s.logger.Warn(ctx, "budget.counter_increment_failed",
			logging.Field{Key: "error", Value: err.Error()},
			logging.Field{Key: "principal_id", Value: principalID},
		)
	}
}

// GetPersonalBudget returns the caller's budget status: every applicable
// ceiling with its limit and current spend. This is the same gather + evaluate
// path enforcement takes, so the reported status always agrees with what the
// gateway will block. Fails open on Redis or OPA errors: returns an empty
// BudgetStatus rather than propagating infrastructure errors to the caller.
func (s *Service) GetPersonalBudget(ctx context.Context, tenantID, principalID string) (BudgetStatus, error) {
	if s.budgetCounter == nil {
		return BudgetStatus{}, nil
	}

	p := principal{TenantID: tenantID, PrincipalID: principalID}
	if claims, ok := token.ClaimsFromContext(ctx); ok && claims != nil {
		p.PrincipalType = string(claims.Orbit.PrincipalType)
	}
	scopes, _, roles := s.budgetScopesFor(ctx, p)

	spends, err := s.budgetCounter.GetSpends(ctx, scopes)
	if err != nil {
		s.logger.Warn(ctx, "budget.fail_open",
			logging.Field{Key: "reason", Value: "spend counter get failed"},
			logging.Field{Key: "error", Value: err.Error()},
			logging.Field{Key: "principal_id", Value: principalID},
		)
		return BudgetStatus{}, nil
	}

	result, err := s.policy.CheckBudget(ctx, tenantID, policy.BudgetCheckRequest{
		PrincipalID:   principalID,
		PrincipalType: p.PrincipalType,
		Roles:         roles,
		RunID:         scopes.RunID,
		Spends:        spends,
	})
	if err != nil {
		s.logger.Warn(ctx, "budget.fail_open",
			logging.Field{Key: "reason", Value: "OPA budget check failed"},
			logging.Field{Key: "error", Value: err.Error()},
			logging.Field{Key: "principal_id", Value: principalID},
		)
		return BudgetStatus{}, nil
	}

	status := BudgetStatus{
		HasLimit:          result.HasLimit,
		SpendLimitReached: !result.Allow,
	}
	for _, c := range result.Ceilings {
		status.Ceilings = append(status.Ceilings, BudgetCeilingStatus{
			Scope:    c.Scope,
			ID:       c.ID,
			Window:   c.Window,
			LimitUSD: c.LimitUSD,
			SpendUSD: c.SpendUSD,
			Reached:  c.Reached(),
		})
		// Mirror the principal-monthly ceiling into the legacy single-bar fields.
		if c.Scope == "principal" && c.Window == "monthly" {
			status.MonthlyBudgetUSD = c.LimitUSD
			status.CurrentSpendUSD = c.SpendUSD
		}
	}
	return status, nil
}

// GetRunUsage returns the spend and token total recorded against a run for the
// run chat usage meter, tenant-scoped. Display-only: it reads from the durable
// usage log (the authoritative per-run record) rather than the enforcement
// counter, so it stays correct after counter TTL expiry. Returns a zero-value
// status (not an error) when no usage repository is configured.
func (s *Service) GetRunUsage(ctx context.Context, tenantID, runID string) (RunUsage, error) {
	if s.usage == nil {
		return RunUsage{RunID: runID}, nil
	}
	spend, tokens, err := s.usage.GetRunUsage(ctx, tenantID, runID)
	if err != nil {
		return RunUsage{}, fmt.Errorf("get run usage: %w", err)
	}
	return RunUsage{RunID: runID, SpendUSD: spend, TotalTokens: tokens}, nil
}

// TenantCap returns the tenant rollup cap with its period spend, or nil when
// the tenant has no cap or its spend cannot be read.
func (s *Service) TenantCap(ctx context.Context, tenantID string) *TenantCapStatus {
	tb, err := s.policy.GetTenantBudget(ctx, tenantID)
	if err != nil || tb == nil {
		return nil
	}
	now := time.Now().UTC()
	capSpend, err := s.usage.GetTenantSpendSince(ctx, tenantID, periodStart(tb.ResetCadence, now))
	if err != nil {
		s.logger.Warn(ctx, "budget.tenant_cap_spend_unavailable",
			logging.Field{Key: "error", Value: err.Error()},
		)
		return nil
	}
	return &TenantCapStatus{
		LimitUSD:     tb.LimitUSD,
		ResetCadence: tb.ResetCadence,
		SpendUSD:     capSpend,
		Reached:      capSpend >= tb.LimitUSD,
		ResetsAt:     periodEnd(tb.ResetCadence, now),
	}
}

// ListBudgetPrincipals returns per-principal budget status for the tenant:
// every principal with a budget assignment (grant budgets from the compiled
// bundle, role budgets attributed via their active spend counters) plus every
// principal with spend this period (HasLimit=false when unbudgeted).
//
// Role-budget attribution is best-effort: a principal who holds a budgeted
// OIDC role but has made no requests this period has no attribution counter
// yet and appears without that category until their next request. Zero spend
// means zero budget risk, so this does not affect enforcement.
func (s *Service) ListBudgetPrincipals(ctx context.Context, tenantID string) (BudgetPrincipalsResponse, error) {
	now := time.Now().UTC()
	resp := BudgetPrincipalsResponse{
		Period: now.Format("2006-01"),
		Data:   []BudgetPrincipalStatus{},
	}

	// Grant budgets and role budget definitions from the compiled bundle.
	// ListBudgets fails open (empty maps) on bundle unavailability.
	principalBudgets, roleBudgets, err := s.policy.ListBudgets(ctx, tenantID)
	if err != nil {
		principalBudgets, roleBudgets = nil, nil
	}

	// Tenant rollup cap with its period spend (DB-authoritative).
	resp.TenantCap = s.TenantCap(ctx, tenantID)

	// Per-principal period spend — the DB is authoritative.
	spenders, err := s.usage.GetMonthlySpendByPrincipal(ctx, tenantID, now)
	if err != nil {
		return resp, fmt.Errorf("list budget principals: %w", err)
	}

	// True ISO-week spend per principal, so weekly categories report the same
	// window spend enforcement evaluates — not the attribution counter, which
	// starts at zero whenever an assignment (or its period key) is new and made
	// a fresh weekly budget look like the principal's usage had been reset.
	weeklySpendByPid, wkErr := s.usage.GetSpendByPrincipalSince(ctx, tenantID, periodStart("weekly", now))
	if wkErr != nil {
		s.logger.Warn(ctx, "budget.weekly_spend_unavailable",
			logging.Field{Key: "error", Value: wkErr.Error()},
		)
		weeklySpendByPid = nil
	}

	// Active role-budget holders: which principals exercised which OIDC roles
	// this period, read tenant-scoped from inference_usage.principal_roles.
	// (The policy service can't resolve IDP role membership; the usage log is
	// the authoritative record.) Lookback spans the longest live window so
	// weekly and monthly role budgets are both covered.
	roleHolders, err := s.usage.GetActiveRoleHolders(ctx, tenantID, periodStart("monthly", now))
	if err != nil {
		s.logger.Warn(ctx, "budget.role_holders_unavailable",
			logging.Field{Key: "error", Value: err.Error()},
		)
		roleHolders = nil
	}

	// Union of all principals that appear in any source.
	pids := make(map[string]struct{})
	spendByPid := make(map[string]PrincipalPeriodSpend, len(spenders))
	for _, sp := range spenders {
		pids[sp.PrincipalID] = struct{}{}
		spendByPid[sp.PrincipalID] = sp
	}
	for pid := range principalBudgets {
		pids[pid] = struct{}{}
	}
	for pid, held := range roleHolders {
		for _, role := range held {
			if _, ok := roleBudgets[role]; ok {
				pids[pid] = struct{}{}
				break
			}
		}
	}

	for pid := range pids {
		cats := budgetCategoriesFor(pid, principalBudgets, roleHolders, roleBudgets)
		resp.Data = append(resp.Data, s.buildBudgetPrincipalRow(ctx, pid, cats, spendByPid[pid], weeklySpendByPid[pid]))
	}

	sortBudgetPrincipals(resp.Data)
	return resp, nil
}

// budgetCategoriesFor collects every budget category applicable to the
// principal: grant categories from the bundle plus role categories for the
// budgeted roles the principal has exercised this period.
func budgetCategoriesFor(
	pid string,
	principalBudgets map[string]policy.PrincipalBudget,
	roleHolders map[string][]string,
	roleBudgets map[string]policy.RoleBudget,
) []policy.BudgetCategory {
	var cats []policy.BudgetCategory
	if pb, ok := principalBudgets[pid]; ok {
		cats = append(cats, pb.Categories...)
	}
	for _, role := range roleHolders[pid] {
		rb, ok := roleBudgets[role]
		if !ok {
			continue // counter for a role whose budget has since been removed
		}
		cats = append(cats, policy.BudgetCategory{
			Source:       "role",
			ID:           role,
			ResetCadence: rb.ResetCadence,
			LimitUSD:     rb.LimitUSD,
			Pooled:       rb.Pooled,
		})
	}
	return cats
}

// buildBudgetPrincipalRow assembles a single listing row. EffectiveLimitUSD is
// the MAX per-member window limit across all cadences (so a weekly-only
// principal still shows a non-zero limit). SpendLimitReached is true when the
// monthly canonical spend meets the monthly limit OR any non-monthly window
// category's attributed spend meets its own limit — so a weekly-blocked
// principal is reported blocked, not "OK". Pooled and per-run assignments are
// shown in the per-category breakdown with their own windows.
func (s *Service) buildBudgetPrincipalRow(
	ctx context.Context,
	pid string,
	cats []policy.BudgetCategory,
	sp PrincipalPeriodSpend,
	weeklySpend float64,
) BudgetPrincipalStatus {
	var catSpends []float64
	if s.budgetCounter != nil && len(cats) > 0 {
		if cs, csErr := s.budgetCounter.GetCategorySpends(ctx, pid, cats); csErr == nil {
			catSpends = cs
		}
	}

	catStatuses, effective, effectiveCadence, hasLimit, reached := summarizeBudgetCategories(cats, catSpends, sp.SpendUSD, weeklySpend)

	row := BudgetPrincipalStatus{
		PrincipalID:           pid,
		PrincipalType:         principalTypeFor(pid, sp.PrincipalType),
		Categories:            catStatuses,
		EffectiveLimitUSD:     effective,
		EffectiveResetCadence: effectiveCadence,
		EffectiveSpendUSD:     effectiveSpend(effectiveCadence, sp.SpendUSD, weeklySpend),
		CurrentSpendUSD:       sp.SpendUSD,
		HasLimit:              hasLimit,
		SpendLimitReached:     reached,
	}
	if !sp.LastActiveAt.IsZero() {
		row.LastActiveAt = sp.LastActiveAt.Format(time.RFC3339)
	}
	return row
}

// summarizeBudgetCategories builds the per-category status rows and the
// headline fields: effective (MAX per-member limit across all window cadences),
// effectiveCadence (cadence of the winning category), hasLimit, and reached
// (any per-member category's window spend ≥ its limit).
//
// Per-member category spend is the principal's canonical window spend
// (monthlySpend / weeklySpend) — the same number enforcement evaluates — so a
// freshly created or re-windowed assignment never reads as zero usage. Pooled
// categories keep the attribution counter: it records the member's own
// contribution to the shared pot (the pool's total lives on the role surface).
func summarizeBudgetCategories(cats []policy.BudgetCategory, catSpends []float64, monthlySpend, weeklySpend float64) (statuses []BudgetCategoryStatus, effective float64, effectiveCadence string, hasLimit, reached bool) {
	statuses = make([]BudgetCategoryStatus, len(cats))
	for i, cat := range cats {
		if cat.LimitUSD > 0 || cat.PerRunLimitUSD > 0 {
			hasLimit = true
		}
		st := BudgetCategoryStatus{
			Source:         cat.Source,
			ID:             cat.ID,
			LimitUSD:       cat.LimitUSD,
			ResetCadence:   cat.ResetCadence,
			PerRunLimitUSD: cat.PerRunLimitUSD,
			Pooled:         cat.Pooled,
		}
		if i < len(catSpends) {
			st.SpendUSD = catSpends[i]
		}
		if !cat.Pooled {
			if cat.ResetCadence == "weekly" {
				st.SpendUSD = weeklySpend
			} else {
				st.SpendUSD = monthlySpend
			}
		}
		statuses[i] = st

		if cat.Pooled {
			continue // pools are shared ceilings, never part of the per-member MAX
		}
		// Tie-break: prefer "weekly" when two categories share the same limit so
		// the UI always shows the more conservative (shorter) window.
		if cat.LimitUSD > effective || (cat.LimitUSD == effective && cat.ResetCadence == cadenceWeekly && effectiveCadence != cadenceWeekly) {
			effective = cat.LimitUSD
			effectiveCadence = cat.ResetCadence
		}
		if cat.LimitUSD > 0 && st.SpendUSD >= cat.LimitUSD {
			reached = true // this per-member window ceiling is exhausted
		}
	}
	return statuses, effective, effectiveCadence, hasLimit, reached
}

// principalTypeFor returns the recorded principal type, or — for synthesized
// rows (a budgeted principal with no spend this period, so no usage row to read
// the type from) — infers it from the principal ID: SPIFFE IDs are agents,
// everything else is treated as human. PAT credentials never appear as a
// principal here (PAT usage attributes to the human).
func principalTypeFor(pid, recorded string) string {
	if recorded != "" {
		return recorded
	}
	if strings.HasPrefix(pid, "spiffe://") {
		return "AGENT"
	}
	return "HUMAN"
}

// effectiveSpend returns the spend that belongs to the same window as the
// effective limit: ISO-week spend for weekly cadence, month-to-date otherwise.
func effectiveSpend(cadence string, monthlySpend, weeklySpend float64) float64 {
	if cadence == cadenceWeekly {
		return weeklySpend
	}
	return monthlySpend
}

// sortBudgetPrincipals orders budgeted principals first by utilization
// descending, then unbudgeted principals by spend descending.
func sortBudgetPrincipals(data []BudgetPrincipalStatus) {
	sort.Slice(data, func(i, j int) bool {
		a, b := data[i], data[j]
		if a.HasLimit != b.HasLimit {
			return a.HasLimit
		}
		if a.HasLimit {
			au, bu := utilization(a), utilization(b)
			if au != bu {
				return au > bu
			}
		}
		if a.CurrentSpendUSD != b.CurrentSpendUSD {
			return a.CurrentSpendUSD > b.CurrentSpendUSD
		}
		return a.PrincipalID < b.PrincipalID
	})
}

// utilization is the spend-to-limit ratio used for sorting the budgets listing.
// Uses EffectiveSpendUSD (window-matched to the effective limit) so that
// weekly-only principals are not inflated by their full month-to-date spend.
func utilization(s BudgetPrincipalStatus) float64 {
	if s.EffectiveLimitUSD <= 0 {
		return 0
	}
	return s.EffectiveSpendUSD / s.EffectiveLimitUSD
}

// principalFromClaims reads the verified PlatformClaims stored in context by
// AuthMiddleware and extracts the principal identity fields.
//
// TokenID is populated only for PAT-authenticated callers. Two distinct fields
// in the JWT carry related but non-equivalent identifiers:
//   - claims.RegisteredClaims.ID (JTI) — a per-issuance revocation key stored
//     in Redis; a new JTI is minted on every token issuance.
//   - claims.Orbit.PatID — the stable PAT record UUID; constant for the
//     lifetime of the PAT regardless of how many tokens have been issued.
//
// We use PatID (not JTI) so that all requests authenticated by the same
// PAT are attributable to a single token_id in inference_usage.
func principalFromClaims(ctx context.Context) (principal, error) {
	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		return principal{}, fmt.Errorf("no verified claims in context: AuthMiddleware may not have run")
	}

	var tokenID string
	if claims.Orbit.PrincipalType == token.PrincipalTypePAT {
		tokenID = claims.Orbit.PatID
	}

	return principal{
		TenantID:       claims.Orbit.TenantID,
		PrincipalID:    claims.Subject,
		PrincipalType:  string(claims.Orbit.PrincipalType),
		IdentitySource: string(claims.Orbit.IdentitySource),
		TokenID:        tokenID,
	}, nil
}

// rolesFromContext extracts the caller's OIDC roles from the verified
// PlatformClaims stored in context by AuthMiddleware. Returns nil when no
// claims are present (e.g. internal callers), which OPA treats as "no roles".
func rolesFromContext(ctx context.Context) []string {
	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		return nil
	}
	return extractRolesFromClaims(claims)
}

// dedupeRoles returns roles with duplicates removed, preserving first-seen
// order. Returns the input unchanged when there is nothing to dedupe, and never
// mutates the input slice (it may be a cached snapshot shared across requests).
func dedupeRoles(roles []string) []string {
	if len(roles) < 2 {
		return roles
	}
	seen := make(map[string]struct{}, len(roles))
	out := make([]string, 0, len(roles))
	for _, r := range roles {
		if _, dup := seen[r]; dup {
			continue
		}
		seen[r] = struct{}{}
		out = append(out, r)
	}
	return out
}

// extractRolesFromClaims reads the comma-separated roles claim from the platform JWT.
// Returns an empty slice when no roles claim is present.
func extractRolesFromClaims(claims *token.PlatformClaims) []string {
	if claims.Orbit.Claims == nil {
		return nil
	}
	raw, ok := claims.Orbit.Claims["roles"]
	if !ok || raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	roles := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			roles = append(roles, trimmed)
		}
	}
	return roles
}

// resolvedPricing holds the effective costs and price_unknown flag for a single
// inference request. It is the output of resolvePricing and written directly
// onto the InferenceUsageRecord.
type resolvedPricing struct {
	inputCostPer1M      float64
	outputCostPer1M     float64
	cacheReadCostPer1M  float64
	cacheWriteCostPer1M float64
	priceUnknown        bool
}

// resolvePricing looks up the effective price for a provider+model pair from
// the PricingCache. When the model is not in the catalog it returns
// priceUnknown=true and all-zero costs.
func (s *Service) resolvePricing(provider ProviderName, modelID string) resolvedPricing {
	p, ok := s.pricing.Price(provider, modelID)
	if !ok {
		return resolvedPricing{priceUnknown: true}
	}
	return resolvedPricing{
		inputCostPer1M:      p.InputCostPerMillion,
		outputCostPer1M:     p.OutputCostPerMillion,
		cacheReadCostPer1M:  p.CacheReadCostPerMillion,
		cacheWriteCostPer1M: p.CacheWriteCostPerMillion,
	}
}

// disjointPromptTokens subtracts the cache buckets an OpenAI-dialect provider
// counts inside prompt_tokens, leaving the buckets estimatedCostUSD expects.
// Pass zero for a bucket the dialect cannot report separately; those tokens
// then stay in the prompt and bill at the plain input rate. Clamped at zero: a
// provider reporting more cached tokens than prompt tokens is nonsense, and
// must not produce a negative charge.
func disjointPromptTokens(promptTokens, cacheReadTokens, cacheWriteTokens int) int {
	if n := promptTokens - cacheReadTokens - cacheWriteTokens; n > 0 {
		return n
	}
	return 0
}

// estimatedCostUSD computes the USD cost from token counts and resolved prices,
// applying industry-standard cache fallbacks when explicit cache prices are zero.
//
// The four buckets must be disjoint: promptTokens is the prompt MINUS anything
// counted in cacheReadTokens or cacheWriteTokens, since each is priced
// separately. Providers disagree on this. Anthropic reports input_tokens already
// disjoint; OpenAI reports prompt_tokens inclusive of
// prompt_tokens_details.cached_tokens. Callers on the OpenAI side pass their
// counts through disjointPromptTokens first.
func (rp resolvedPricing) estimatedCostUSD(promptTokens, completionTokens, cacheReadTokens, cacheWriteTokens int) float64 {
	if rp.priceUnknown {
		return 0
	}
	m := ModelPricing{
		InputCostPerMillion:      rp.inputCostPer1M,
		OutputCostPerMillion:     rp.outputCostPer1M,
		CacheReadCostPerMillion:  rp.cacheReadCostPer1M,
		CacheWriteCostPerMillion: rp.cacheWriteCostPer1M,
	}
	return m.EstimatedCostUSD(promptTokens, completionTokens, cacheReadTokens, cacheWriteTokens)
}

func (s *Service) writeFailedUsage(
	ctx context.Context,
	p principal,
	req ChatCompletionRequest,
	dialect APIDialect,
	provider *ProviderName,
	modelFamily *string,
	strategy *RoutingStrategy,
	status UsageStatus,
	errorCode string,
	failureDetails *FailureDetails,
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
		TokenID:         storage.NullIfZero(p.TokenID),
		APIDialect:      dialect,
		Provider:        provider,
		VirtualModelID:  storage.NullIfZero(req.Model),
		ModelID:         req.Model,
		ModelFamily:     modelFamily,
		RoutingStrategy: strategy,
		Status:          status,
		ErrorCode:       errCodePtr,
		FailureDetails:  failureDetails,
		LatencyMS:       latencyMS,
		CreatedAt:       time.Now().UTC(),
	}

	if err := s.writeUsage(ctx, rec); err != nil {
		s.logger.Error(ctx, "failed to write failed inference usage record",
			logging.Field{Key: "error", Value: err.Error()},
		)
	}
}

const maxProviderMessageLen = 500

func extractFailureDetails(err error) *FailureDetails {
	if err == nil {
		return nil
	}
	var connErr *ConnectorError
	if !errors.As(err, &connErr) {
		return nil
	}
	fd := &FailureDetails{}
	if connErr.HTTPStatus != 0 {
		status := connErr.HTTPStatus
		fd.ProviderHTTPStatus = &status
	}
	if connErr.Code != "" {
		code := connErr.Code
		fd.ProviderErrorType = &code
	}
	if connErr.ProviderMessage != "" {
		msg := connErr.ProviderMessage
		if len(msg) > maxProviderMessageLen {
			// Truncate before storage. Providers (e.g. OpenAI on context-length
			// violations) can echo request fragments in error messages; the cap
			// bounds both storage growth and incidental content exposure.
			msg = msg[:maxProviderMessageLen]
		}
		fd.ProviderErrorMessage = &msg
	}
	if fd.ProviderHTTPStatus == nil && fd.ProviderErrorType == nil && fd.ProviderErrorMessage == nil {
		return nil
	}
	return fd
}

// logProviderError emits a structured error log for upstream provider failures.
// Called on every code path where a ConnectorError reaches the service layer,
// including streaming goroutines that otherwise only record an OTel span.
func (s *Service) logProviderError(ctx context.Context, model string, err error) {
	fields := []logging.Field{
		{Key: "model", Value: model},
	}
	var connErr *ConnectorError
	if errors.As(err, &connErr) {
		fields = append(fields,
			logging.Field{Key: "provider", Value: string(connErr.Provider)},
			logging.Field{Key: "error_code", Value: connErr.Code},
		)
		if connErr.HTTPStatus != 0 {
			fields = append(fields, logging.Field{Key: "provider_http_status", Value: connErr.HTTPStatus})
		}
		if connErr.ProviderMessage != "" {
			fields = append(fields, logging.Field{Key: "provider_message", Value: connErr.ProviderMessage})
		}
	}
	s.logger.Error(ctx, "provider error", fields...)
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
) (*OpenAIResult, *RouteTarget, int, error) {
	return retryTargets(ctx, s, targets, "retrying after retryable provider error",
		func(c ProviderConnector, target RouteTarget) (*OpenAIResult, error) {
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

func nthModelFamily(targets []RouteTarget) *string {
	if len(targets) > 0 && targets[0].ModelFamily != "" {
		f := targets[0].ModelFamily
		return &f
	}
	return nil
}

// ─── Embeddings (non-streaming) ───────────────────────────────────────────────

// Embeddings handles a POST /v1/embeddings request end-to-end.
// The full openai.EmbeddingNewParams wire type is passed through to the
// connector unchanged. Usage is attributed with prompt tokens only;
// completion tokens are always zero for embedding requests.
func (s *Service) Embeddings(
	ctx context.Context,
	params openai.EmbeddingNewParams,
) (*openai.CreateEmbeddingResponse, error) {
	ctx, span := tracer.Start(ctx, "inferencegw.Embeddings",
		trace.WithAttributes(attribute.String("model", params.Model)),
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

	if err = s.checkModelPolicy(ctx, params.Model); err != nil {
		span.RecordError(err)
		usageStatus, usageCode, spanMsg := modelPolicyFailure(err)
		span.SetStatus(codes.Error, spanMsg)
		s.writeFailedUsage(ctx, p, routeReq, DialectOpenAI, nil, nil, nil, usageStatus, usageCode, nil, 0)
		return nil, err
	}

	if ctx, err = s.checkBudget(ctx, p); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, spanStatusBudgetExceeded)
		s.writeFailedUsage(ctx, p, routeReq, DialectOpenAI, nil, nil, nil, UsageStatusBudgetExceeded, "BUDGET_EXCEEDED", nil, 0)
		return nil, err
	}

	targets, strategy, err := s.router.RouteRequest(ctx, routeReq)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrRoutingFailed.Error())
		s.writeFailedUsage(ctx, p, routeReq, DialectOpenAI, nil, nil, nil, UsageStatusRoutingFailed, "", nil, 0)
		return nil, fmt.Errorf("%w: %w", ErrRoutingFailed, err)
	}

	resp, usedTarget, retryCount, callErr := retryTargets(ctx, s, targets, "retrying after retryable provider error",
		func(c ProviderConnector, target RouteTarget) (*openai.CreateEmbeddingResponse, error) {
			q := params
			q.Model = target.ModelID
			return c.Embeddings(ctx, q)
		})

	latencyMS := int(time.Since(start).Milliseconds())

	if callErr != nil || usedTarget == nil {
		span.RecordError(callErr)
		span.SetStatus(codes.Error, ErrProviderError.Error())
		s.logProviderError(ctx, params.Model, callErr)
		errCode := extractErrorCode(callErr)
		s.writeFailedUsage(ctx, p, routeReq, DialectOpenAI, nthProvider(targets), nthModelFamily(targets), &strategy, UsageStatusProviderError, errCode, extractFailureDetails(callErr), latencyMS)
		return nil, fmt.Errorf("%w: %w", ErrProviderError, callErr)
	}

	promptTokens := int(resp.Usage.PromptTokens)
	pricing := s.resolvePricing(usedTarget.Provider, usedTarget.ModelID)
	estimatedCost := pricing.estimatedCostUSD(promptTokens, 0, 0, 0)

	rec := InferenceUsageRecord{
		ID:                           uuid.NewString(),
		TenantID:                     p.TenantID,
		PrincipalID:                  p.PrincipalID,
		PrincipalType:                p.PrincipalType,
		TokenID:                      storage.NullIfZero(p.TokenID),
		APIDialect:                   DialectOpenAI,
		Provider:                     &usedTarget.Provider,
		VirtualModelID:               storage.NullIfZero(usedTarget.VirtualModelID),
		ModelID:                      usedTarget.ModelID,
		ModelFamily:                  storage.NullIfZero(usedTarget.ModelFamily),
		RoutingStrategy:              &strategy,
		PromptTokens:                 promptTokens,
		CompletionTokens:             0,
		TotalTokens:                  promptTokens,
		EstimatedCostUSD:             estimatedCost,
		PriceUnknown:                 pricing.priceUnknown,
		EffectiveInputCostPer1M:      pricing.inputCostPer1M,
		EffectiveOutputCostPer1M:     pricing.outputCostPer1M,
		EffectiveCacheReadCostPer1M:  pricing.cacheReadCostPer1M,
		EffectiveCacheWriteCostPer1M: pricing.cacheWriteCostPer1M,
		Status:                       UsageStatusSuccess,
		RetryCount:                   retryCount,
		LatencyMS:                    latencyMS,
		CreatedAt:                    start.UTC(),
	}

	if writeErr := s.writeUsage(ctx, rec); writeErr != nil {
		s.logger.Error(ctx, "failed to write inference usage record",
			logging.Field{Key: "error", Value: writeErr.Error()},
			logging.Field{Key: "usage_id", Value: rec.ID},
		)
	} else {
		s.incrementSpend(ctx, p.TenantID, p.PrincipalID, rec.EstimatedCostUSD)
	}

	span.SetStatus(codes.Ok, "")
	return resp, nil
}

// ─── Responses (non-streaming) ────────────────────────────────────────────────

// Responses handles a non-streaming POST /v1/responses request end-to-end.
func (s *Service) Responses(
	ctx context.Context,
	params responses.ResponseNewParams,
) (*responses.Response, error) {
	ctx, span := tracer.Start(ctx, "inferencegw.Responses",
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

	if err = s.checkModelPolicy(ctx, params.Model); err != nil {
		span.RecordError(err)
		usageStatus, usageCode, spanMsg := modelPolicyFailure(err)
		span.SetStatus(codes.Error, spanMsg)
		s.writeFailedUsage(ctx, p, routeReq, DialectResponses, nil, nil, nil, usageStatus, usageCode, nil, 0)
		return nil, err
	}

	if ctx, err = s.checkBudget(ctx, p); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, spanStatusBudgetExceeded)
		s.writeFailedUsage(ctx, p, routeReq, DialectResponses, nil, nil, nil, UsageStatusBudgetExceeded, "BUDGET_EXCEEDED", nil, 0)
		return nil, err
	}

	targets, strategy, err := s.router.RouteRequest(ctx, routeReq)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrRoutingFailed.Error())
		s.writeFailedUsage(ctx, p, routeReq, DialectResponses, nil, nil, nil, UsageStatusRoutingFailed, "", nil, 0)
		return nil, fmt.Errorf("%w: %w", ErrRoutingFailed, err)
	}

	result, usedTarget, retryCount, callErr := retryTargets(ctx, s, targets, "retrying after retryable provider error",
		func(c ProviderConnector, target RouteTarget) (*ResponsesResult, error) {
			q := params
			q.Model = target.ModelID
			return c.Responses(ctx, q)
		})

	latencyMS := int(time.Since(start).Milliseconds())

	if callErr != nil || usedTarget == nil {
		span.RecordError(callErr)
		span.SetStatus(codes.Error, ErrProviderError.Error())
		s.logProviderError(ctx, params.Model, callErr)
		errCode := extractErrorCode(callErr)
		s.writeFailedUsage(ctx, p, routeReq, DialectResponses, nthProvider(targets), nthModelFamily(targets), &strategy, UsageStatusProviderError, errCode, extractFailureDetails(callErr), latencyMS)
		return nil, fmt.Errorf("%w: %w", ErrProviderError, callErr)
	}

	resp := result.Response
	promptTokens := int(resp.Usage.InputTokens)
	completionTokens := int(resp.Usage.OutputTokens)
	totalTokens := int(resp.Usage.TotalTokens)
	if totalTokens == 0 {
		totalTokens = promptTokens + completionTokens
	}
	cacheReadTokens := int(resp.Usage.InputTokensDetails.CachedTokens)
	cacheCreationTokens := int(result.CacheCreationTokens)
	pricing := s.resolvePricing(usedTarget.Provider, usedTarget.ModelID)
	uncachedPromptTokens := disjointPromptTokens(promptTokens, cacheReadTokens, cacheCreationTokens)
	estimatedCost := pricing.estimatedCostUSD(uncachedPromptTokens, completionTokens, cacheReadTokens, cacheCreationTokens)

	rec := InferenceUsageRecord{
		ID:                           uuid.NewString(),
		TenantID:                     p.TenantID,
		PrincipalID:                  p.PrincipalID,
		PrincipalType:                p.PrincipalType,
		TokenID:                      storage.NullIfZero(p.TokenID),
		APIDialect:                   DialectResponses,
		Provider:                     &usedTarget.Provider,
		VirtualModelID:               storage.NullIfZero(usedTarget.VirtualModelID),
		ModelID:                      usedTarget.ModelID,
		ModelFamily:                  storage.NullIfZero(usedTarget.ModelFamily),
		RoutingStrategy:              &strategy,
		PromptTokens:                 uncachedPromptTokens,
		CompletionTokens:             completionTokens,
		TotalTokens:                  totalTokens,
		CacheReadInputTokens:         cacheReadTokens,
		CacheCreationInputTokens:     cacheCreationTokens,
		EstimatedCostUSD:             estimatedCost,
		PriceUnknown:                 pricing.priceUnknown,
		EffectiveInputCostPer1M:      pricing.inputCostPer1M,
		EffectiveOutputCostPer1M:     pricing.outputCostPer1M,
		EffectiveCacheReadCostPer1M:  pricing.cacheReadCostPer1M,
		EffectiveCacheWriteCostPer1M: pricing.cacheWriteCostPer1M,
		Status:                       UsageStatusSuccess,
		RetryCount:                   retryCount,
		LatencyMS:                    latencyMS,
		CreatedAt:                    start.UTC(),
	}

	if writeErr := s.writeUsage(ctx, rec); writeErr != nil {
		s.logger.Error(ctx, "failed to write inference usage record",
			logging.Field{Key: "error", Value: writeErr.Error()},
			logging.Field{Key: "usage_id", Value: rec.ID},
		)
	} else {
		s.incrementSpend(ctx, p.TenantID, p.PrincipalID, rec.EstimatedCostUSD)
	}

	span.SetStatus(codes.Ok, "")
	return resp, nil
}

// ─── ResponsesStream (streaming) ─────────────────────────────────────────────

// ResponsesStream handles a streaming POST /v1/responses request.
// Returns a channel of ResponsesStreamResult values; the channel is closed
// when the stream ends (successfully or on error).
func (s *Service) ResponsesStream(
	ctx context.Context,
	params responses.ResponseNewParams,
) (<-chan ResponsesStreamResult, error) {
	ctx, span := tracer.Start(ctx, "inferencegw.ResponsesStream",
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

	if err = s.checkModelPolicy(ctx, params.Model); err != nil {
		span.RecordError(err)
		usageStatus, usageCode, spanMsg := modelPolicyFailure(err)
		span.SetStatus(codes.Error, spanMsg)
		span.End()
		s.writeFailedUsage(ctx, p, routeReq, DialectResponses, nil, nil, nil, usageStatus, usageCode, nil, 0)
		return nil, err
	}

	if ctx, err = s.checkBudget(ctx, p); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, spanStatusBudgetExceeded)
		span.End()
		s.writeFailedUsage(ctx, p, routeReq, DialectResponses, nil, nil, nil, UsageStatusBudgetExceeded, "BUDGET_EXCEEDED", nil, 0)
		return nil, err
	}

	targets, strategy, err := s.router.RouteRequest(ctx, routeReq)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrRoutingFailed.Error())
		span.End()
		s.writeFailedUsage(ctx, p, routeReq, DialectResponses, nil, nil, nil, UsageStatusRoutingFailed, "", nil, 0)
		return nil, fmt.Errorf("%w: %w", ErrRoutingFailed, err)
	}

	upstream, usedTarget, streamRetryCount, callErr := retryTargets(ctx, s, targets, "retrying after retryable provider error",
		func(c ProviderConnector, target RouteTarget) (<-chan ResponsesStreamResult, error) {
			q := params
			q.Model = target.ModelID
			return c.ResponsesStream(ctx, q)
		})

	if callErr != nil || usedTarget == nil {
		span.RecordError(callErr)
		span.SetStatus(codes.Error, ErrProviderError.Error())
		span.End()
		s.logProviderError(ctx, params.Model, callErr)
		s.writeFailedUsage(ctx, p, routeReq, DialectResponses, nthProvider(targets), nthModelFamily(targets), &strategy, UsageStatusProviderError, extractErrorCode(callErr), extractFailureDetails(callErr), 0)
		return nil, fmt.Errorf("%w: %w", ErrProviderError, callErr)
	}

	out := make(chan ResponsesStreamResult, 16)
	capturedTarget := usedTarget
	capturedStrategy := strategy

	writeCtx := context.WithoutCancel(ctx)

	go func() {
		defer span.End()
		defer close(out)

		inputTokens, outputTokens, cacheReadTokens, cacheCreationTokens, streamErr := drainResponsesStream(ctx, upstream, out)
		latencyMS := int(time.Since(start).Milliseconds())

		if streamErr != nil {
			if errors.Is(streamErr, errClientDisconnected) {
				span.SetStatus(codes.Ok, "client disconnected")
				s.writeFailedUsage(writeCtx, p, routeReq, DialectResponses, &capturedTarget.Provider, storage.NullIfZero(capturedTarget.ModelFamily), &capturedStrategy, UsageStatusCanceled, "CLIENT_DISCONNECTED", nil, latencyMS)
				return
			}
			span.RecordError(streamErr)
			span.SetStatus(codes.Error, ErrProviderError.Error())
			s.logProviderError(writeCtx, params.Model, streamErr)
			s.writeFailedUsage(writeCtx, p, routeReq, DialectResponses, &capturedTarget.Provider, storage.NullIfZero(capturedTarget.ModelFamily), &capturedStrategy, UsageStatusProviderError, extractErrorCode(streamErr), extractFailureDetails(streamErr), latencyMS)
			return
		}

		span.SetStatus(codes.Ok, "")

		totalTokens := inputTokens + outputTokens
		pricing := s.resolvePricing(capturedTarget.Provider, capturedTarget.ModelID)
		uncachedInputTokens := disjointPromptTokens(inputTokens, cacheReadTokens, cacheCreationTokens)
		estimatedCost := pricing.estimatedCostUSD(uncachedInputTokens, outputTokens, cacheReadTokens, cacheCreationTokens)

		rec := InferenceUsageRecord{
			ID:                           uuid.NewString(),
			TenantID:                     p.TenantID,
			PrincipalID:                  p.PrincipalID,
			PrincipalType:                p.PrincipalType,
			TokenID:                      storage.NullIfZero(p.TokenID),
			APIDialect:                   DialectResponses,
			Provider:                     &capturedTarget.Provider,
			VirtualModelID:               storage.NullIfZero(capturedTarget.VirtualModelID),
			ModelID:                      capturedTarget.ModelID,
			ModelFamily:                  storage.NullIfZero(capturedTarget.ModelFamily),
			RoutingStrategy:              &capturedStrategy,
			PromptTokens:                 uncachedInputTokens,
			CompletionTokens:             outputTokens,
			TotalTokens:                  totalTokens,
			CacheReadInputTokens:         cacheReadTokens,
			CacheCreationInputTokens:     cacheCreationTokens,
			EstimatedCostUSD:             estimatedCost,
			PriceUnknown:                 pricing.priceUnknown,
			EffectiveInputCostPer1M:      pricing.inputCostPer1M,
			EffectiveOutputCostPer1M:     pricing.outputCostPer1M,
			EffectiveCacheReadCostPer1M:  pricing.cacheReadCostPer1M,
			EffectiveCacheWriteCostPer1M: pricing.cacheWriteCostPer1M,
			Status:                       UsageStatusSuccess,
			RetryCount:                   streamRetryCount,
			LatencyMS:                    latencyMS,
			CreatedAt:                    start.UTC(),
		}

		if writeErr := s.writeUsage(writeCtx, rec); writeErr != nil {
			s.logger.Error(writeCtx, "failed to write streaming inference usage record",
				logging.Field{Key: "error", Value: writeErr.Error()},
				logging.Field{Key: "usage_id", Value: rec.ID},
			)
		} else {
			s.incrementSpend(writeCtx, p.TenantID, p.PrincipalID, rec.EstimatedCostUSD)
		}
	}()

	return out, nil
}

// drainResponsesStream forwards events from upstream to out and accumulates
// token counts from the terminal response.completed event.
// Returns (inputTokens, outputTokens, cacheReadTokens, streamErr).
func drainResponsesStream(
	ctx context.Context,
	upstream <-chan ResponsesStreamResult,
	out chan<- ResponsesStreamResult,
) (inputTokens, outputTokens, cacheReadTokens, cacheCreationTokens int, streamErr error) {
	for {
		select {
		case result, ok := <-upstream:
			if !ok {
				return
			}
			if result.Err != nil {
				streamErr = result.Err
				responsesStreamTrySend(ctx, out, result)
				return
			}
			// Extract token counts from the terminal event. Both response.completed
			// and response.incomplete carry a fully-populated Response.Usage field.
			if result.Event.Type == "response.completed" || result.Event.Type == "response.incomplete" {
				inputTokens = int(result.Event.Response.Usage.InputTokens)
				outputTokens = int(result.Event.Response.Usage.OutputTokens)
				cacheReadTokens = int(result.Event.Response.Usage.InputTokensDetails.CachedTokens)
				cacheCreationTokens = int(result.CacheCreationTokens)
			}
			// response.incomplete means the model hit max_output_tokens — it is a
			// successful, billable completion. Forward it and let the caller decide
			// how to handle the truncation. Only response.failed is a true error.
			if result.Event.Type == "response.failed" {
				streamErr = ErrProviderError
				responsesStreamTrySend(ctx, out, result)
				return
			}
			if !responsesStreamForward(ctx, out, result, upstream) {
				streamErr = errClientDisconnected
				return
			}
		case <-ctx.Done():
			go func() {
				for range upstream {
				}
			}()
			streamErr = errClientDisconnected
			return
		}
	}
}

// responsesStreamTrySend forwards a terminal event to out, best-effort.
func responsesStreamTrySend(ctx context.Context, out chan<- ResponsesStreamResult, result ResponsesStreamResult) {
	select {
	case out <- result:
	case <-ctx.Done():
	}
}

// responsesStreamForward forwards a non-terminal event to out.
// Returns false and drains upstream asynchronously if ctx is canceled while sending.
func responsesStreamForward(ctx context.Context, out chan<- ResponsesStreamResult, result ResponsesStreamResult, upstream <-chan ResponsesStreamResult) bool {
	select {
	case out <- result:
		return true
	case <-ctx.Done():
		go func() {
			for range upstream {
			}
		}()
		return false
	}
}

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
		usageStatus, usageCode, spanMsg := modelPolicyFailure(err)
		span.SetStatus(codes.Error, spanMsg)
		s.writeFailedUsage(ctx, p, routeReq, DialectAnthropic, nil, nil, nil, usageStatus, usageCode, nil, 0)
		return nil, err
	}

	// ── Budget check ─────────────────────────────────────────────────────────
	if ctx, err = s.checkBudget(ctx, p); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, spanStatusBudgetExceeded)
		s.writeFailedUsage(ctx, p, routeReq, DialectAnthropic, nil, nil, nil, UsageStatusBudgetExceeded, "BUDGET_EXCEEDED", nil, 0)
		return nil, err
	}

	targets, strategy, err := s.router.RouteRequest(ctx, routeReq)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrRoutingFailed.Error())
		s.writeFailedUsage(ctx, p, routeReq, DialectAnthropic, nil, nil, nil, UsageStatusRoutingFailed, "", nil, 0)
		return nil, fmt.Errorf("%w: %w", ErrRoutingFailed, err)
	}

	// Policy-filter targets for wildcard routing.
	if s.router.IsWildcardModel(params.Model) {
		targets, err = s.filterTargetsByPolicy(ctx, targets)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, ErrPolicyDenied.Error())
			s.writeFailedUsage(ctx, p, routeReq, DialectAnthropic, nil, nthModelFamily(targets), &strategy, UsageStatusPolicyDenied, "POLICY_DENIED", nil, 0)
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
		s.logProviderError(ctx, params.Model, callErr)
		s.writeFailedUsage(ctx, p, routeReq, DialectAnthropic, nthProvider(targets), nthModelFamily(targets), &strategy, UsageStatusProviderError, extractErrorCode(callErr), extractFailureDetails(callErr), latencyMS)
		return nil, fmt.Errorf("%w: %w", ErrProviderError, callErr)
	}

	promptTokens := int(msg.Usage.InputTokens)
	completionTokens := int(msg.Usage.OutputTokens)
	cacheReadTokens := int(msg.Usage.CacheReadInputTokens)
	cacheCreationTokens := int(msg.Usage.CacheCreationInputTokens)
	pricing := s.resolvePricing(usedTarget.Provider, usedTarget.ModelID)
	estimatedCost := pricing.estimatedCostUSD(promptTokens, completionTokens, cacheReadTokens, cacheCreationTokens)

	rec := InferenceUsageRecord{
		ID:                           uuid.NewString(),
		TenantID:                     p.TenantID,
		PrincipalID:                  p.PrincipalID,
		PrincipalType:                p.PrincipalType,
		TokenID:                      storage.NullIfZero(p.TokenID),
		APIDialect:                   DialectAnthropic,
		Provider:                     &usedTarget.Provider,
		VirtualModelID:               storage.NullIfZero(usedTarget.VirtualModelID),
		ModelID:                      usedTarget.ModelID,
		ModelFamily:                  storage.NullIfZero(usedTarget.ModelFamily),
		RoutingStrategy:              &strategy,
		PromptTokens:                 promptTokens,
		CompletionTokens:             completionTokens,
		TotalTokens:                  promptTokens + completionTokens + cacheReadTokens + cacheCreationTokens,
		CacheReadInputTokens:         cacheReadTokens,
		CacheCreationInputTokens:     cacheCreationTokens,
		EstimatedCostUSD:             estimatedCost,
		PriceUnknown:                 pricing.priceUnknown,
		EffectiveInputCostPer1M:      pricing.inputCostPer1M,
		EffectiveOutputCostPer1M:     pricing.outputCostPer1M,
		EffectiveCacheReadCostPer1M:  pricing.cacheReadCostPer1M,
		EffectiveCacheWriteCostPer1M: pricing.cacheWriteCostPer1M,
		Status:                       UsageStatusSuccess,
		RetryCount:                   retryCount,
		LatencyMS:                    latencyMS,
		CreatedAt:                    start.UTC(),
	}

	if writeErr := s.writeUsage(ctx, rec); writeErr != nil {
		s.logger.Error(ctx, "failed to write inference usage record",
			logging.Field{Key: "error", Value: writeErr.Error()},
			logging.Field{Key: "usage_id", Value: rec.ID},
		)
	} else {
		s.incrementSpend(ctx, p.TenantID, p.PrincipalID, rec.EstimatedCostUSD)
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
		usageStatus, usageCode, spanMsg := modelPolicyFailure(err)
		span.SetStatus(codes.Error, spanMsg)
		span.End()
		s.writeFailedUsage(ctx, p, routeReq, DialectAnthropic, nil, nil, nil, usageStatus, usageCode, nil, 0)
		return nil, err
	}

	// ── Budget check ─────────────────────────────────────────────────────────
	if ctx, err = s.checkBudget(ctx, p); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, spanStatusBudgetExceeded)
		span.End()
		s.writeFailedUsage(ctx, p, routeReq, DialectAnthropic, nil, nil, nil, UsageStatusBudgetExceeded, "BUDGET_EXCEEDED", nil, 0)
		return nil, err
	}

	targets, strategy, err := s.router.RouteRequest(ctx, routeReq)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrRoutingFailed.Error())
		span.End()
		s.writeFailedUsage(ctx, p, routeReq, DialectAnthropic, nil, nil, nil, UsageStatusRoutingFailed, "", nil, 0)
		return nil, fmt.Errorf("%w: %w", ErrRoutingFailed, err)
	}

	// Policy-filter targets for wildcard routing.
	if s.router.IsWildcardModel(params.Model) {
		targets, err = s.filterTargetsByPolicy(ctx, targets)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, ErrPolicyDenied.Error())
			span.End()
			s.writeFailedUsage(ctx, p, routeReq, DialectAnthropic, nil, nthModelFamily(targets), &strategy, UsageStatusPolicyDenied, "POLICY_DENIED", nil, 0)
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
		s.logProviderError(ctx, params.Model, callErr)
		s.writeFailedUsage(ctx, p, routeReq, DialectAnthropic, nthProvider(targets), nthModelFamily(targets), &strategy, UsageStatusProviderError, extractErrorCode(callErr), extractFailureDetails(callErr), 0)
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
			if errors.Is(streamErr, errClientDisconnected) {
				span.SetStatus(codes.Ok, "client disconnected")
				s.writeFailedUsage(writeCtx, p, routeReq, DialectAnthropic, &capturedTarget.Provider, storage.NullIfZero(capturedTarget.ModelFamily), &capturedStrategy, UsageStatusCanceled, "CLIENT_DISCONNECTED", nil, latencyMS)
				return
			}
			span.RecordError(streamErr)
			span.SetStatus(codes.Error, ErrProviderError.Error())
			s.logProviderError(writeCtx, params.Model, streamErr)
			s.writeFailedUsage(writeCtx, p, routeReq, DialectAnthropic, &capturedTarget.Provider, storage.NullIfZero(capturedTarget.ModelFamily), &capturedStrategy, UsageStatusProviderError, extractErrorCode(streamErr), extractFailureDetails(streamErr), latencyMS)
			return
		}

		span.SetStatus(codes.Ok, "")

		promptTokens := int(accumulated.Usage.InputTokens)
		completionTokens := int(accumulated.Usage.OutputTokens)
		cacheReadTokens := int(accumulated.Usage.CacheReadInputTokens)
		cacheCreationTokens := int(accumulated.Usage.CacheCreationInputTokens)
		pricing := s.resolvePricing(capturedTarget.Provider, capturedTarget.ModelID)
		estimatedCost := pricing.estimatedCostUSD(promptTokens, completionTokens, cacheReadTokens, cacheCreationTokens)

		rec := InferenceUsageRecord{
			ID:                           uuid.NewString(),
			TenantID:                     p.TenantID,
			PrincipalID:                  p.PrincipalID,
			PrincipalType:                p.PrincipalType,
			TokenID:                      storage.NullIfZero(p.TokenID),
			APIDialect:                   DialectAnthropic,
			Provider:                     &capturedTarget.Provider,
			VirtualModelID:               storage.NullIfZero(capturedTarget.VirtualModelID),
			ModelID:                      capturedTarget.ModelID,
			ModelFamily:                  storage.NullIfZero(capturedTarget.ModelFamily),
			RoutingStrategy:              &capturedStrategy,
			PromptTokens:                 promptTokens,
			CompletionTokens:             completionTokens,
			TotalTokens:                  promptTokens + completionTokens + cacheReadTokens + cacheCreationTokens,
			CacheReadInputTokens:         cacheReadTokens,
			CacheCreationInputTokens:     cacheCreationTokens,
			EstimatedCostUSD:             estimatedCost,
			PriceUnknown:                 pricing.priceUnknown,
			EffectiveInputCostPer1M:      pricing.inputCostPer1M,
			EffectiveOutputCostPer1M:     pricing.outputCostPer1M,
			EffectiveCacheReadCostPer1M:  pricing.cacheReadCostPer1M,
			EffectiveCacheWriteCostPer1M: pricing.cacheWriteCostPer1M,
			Status:                       UsageStatusSuccess,
			RetryCount:                   streamRetryCount,
			LatencyMS:                    latencyMS,
			CreatedAt:                    start.UTC(),
		}

		if writeErr := s.writeUsage(writeCtx, rec); writeErr != nil {
			s.logger.Error(writeCtx, "failed to write streaming anthropic usage record",
				logging.Field{Key: "error", Value: writeErr.Error()},
				logging.Field{Key: "usage_id", Value: rec.ID},
			)
		} else {
			s.incrementSpend(writeCtx, p.TenantID, p.PrincipalID, rec.EstimatedCostUSD)
		}
	}()

	return out, nil
}

// ─── ListUsage ───────────────────────────────────────────────────────────────

// ListUsage returns a page of inference usage records for the authenticated
// principal. TenantID and PrincipalID are always sourced from the verified JWT
// claims — callers cannot supply their own values. TokenID, when provided,
// must already belong to the authenticated principal; the compound
// (tenant_id, principal_id, token_id) predicate prevents cross-user reads
// without any additional pre-validation.
func (s *Service) ListUsage(ctx context.Context, filter InferenceUsageFilter) ([]InferenceUsageRecord, bool, error) {
	p, err := principalFromClaims(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %w", ErrTokenParseFailed, err)
	}

	// Always enforce tenant and principal scoping from verified claims.
	filter.TenantID = p.TenantID
	filter.PrincipalID = &p.PrincipalID
	// The personal view includes rows a delegated agent run attributed to the
	// caller as delegator; each such row stays distinguishable (delegator_id
	// set, principal_id = the agent). Delegated-agent-identity ADR, Decision 5.
	filter.IncludeDelegated = true

	records, hasNextPage, err := s.usage.List(ctx, filter)
	if err != nil {
		return nil, false, fmt.Errorf("list usage: %w", err)
	}
	return records, hasNextPage, nil
}

// UsageStats returns per-PAT aggregates (request count, last used) for the
// authenticated principal. Scoping is always enforced from the verified JWT.
func (s *Service) UsageStats(ctx context.Context) ([]TokenUsageStat, error) {
	p, err := principalFromClaims(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrTokenParseFailed, err)
	}
	stats, err := s.usage.UsageStats(ctx, p.TenantID, p.PrincipalID)
	if err != nil {
		return nil, fmt.Errorf("usage stats: %w", err)
	}
	return stats, nil
}

// ─── Analytics ────────────────────────────────────────────────────────────────

// AnalyticsMe returns aggregated analytics for the authenticated principal only.
// TenantID and PrincipalID are always sourced from the verified JWT — callers
// cannot widen or redirect the scope via query parameters.
func (s *Service) AnalyticsMe(ctx context.Context, filter AnalyticsFilter) (AnalyticsResponse, error) {
	p, err := principalFromClaims(ctx)
	if err != nil {
		return AnalyticsResponse{}, fmt.Errorf("%w: %w", ErrTokenParseFailed, err)
	}
	filter.TenantID = p.TenantID
	filter.PrincipalID = &p.PrincipalID
	// A person's view includes their delegated agent runs (rows where
	// delegator_id = their principal id), reported additively via the
	// delegated_cost_usd KPI. Delegated-agent-identity ADR, Decision 5.
	filter.IncludeDelegated = true
	resp, err := s.usage.GetAnalytics(ctx, filter)
	if err != nil {
		return AnalyticsResponse{}, fmt.Errorf("analytics me: %w", err)
	}
	return resp, nil
}

// Analytics returns tenant-wide aggregated analytics. TenantID is always
// sourced from the verified JWT; PrincipalID is never applied. The caller
// is responsible for ensuring the request carries the platform:analytics:read
// scope — enforcement happens at the route registration layer.
func (s *Service) Analytics(ctx context.Context, filter AnalyticsFilter) (AnalyticsResponse, error) {
	p, err := principalFromClaims(ctx)
	if err != nil {
		return AnalyticsResponse{}, fmt.Errorf("%w: %w", ErrTokenParseFailed, err)
	}
	filter.TenantID = p.TenantID
	filter.PrincipalID = nil // always tenant-wide; must not be set by the caller
	resp, err := s.usage.GetAnalytics(ctx, filter)
	if err != nil {
		return AnalyticsResponse{}, fmt.Errorf("analytics: %w", err)
	}
	return resp, nil
}

// AnalyticsPrincipals returns per-principal usage aggregates for the tenant.
// TenantID is sourced from the verified JWT; callers cannot override it.
// Requires the platform:analytics:read scope.
func (s *Service) AnalyticsPrincipals(ctx context.Context, filter AnalyticsPrincipalsFilter) (AnalyticsPrincipalsResponse, error) {
	p, err := principalFromClaims(ctx)
	if err != nil {
		return AnalyticsPrincipalsResponse{}, fmt.Errorf("%w: %w", ErrTokenParseFailed, err)
	}
	filter.TenantID = p.TenantID
	resp, err := s.usage.ListPrincipalUsage(ctx, filter)
	if err != nil {
		return AnalyticsPrincipalsResponse{}, fmt.Errorf("analytics principals: %w", err)
	}
	return resp, nil
}

// AnalyticsPrincipalByID returns the same aggregated breakdown as AnalyticsMe
// (time-series buckets, KPIs, cost-by-model) but for the specified principal
// rather than the caller's own identity. TenantID is always sourced from the
// verified JWT — callers cannot cross tenants regardless of the requested
// principalID. The caller is responsible for ensuring the request carries the
// platform:analytics:read scope — enforcement happens at the route
// registration layer, the same gate already covering /v1/analytics and
// /v1/analytics/principals.
func (s *Service) AnalyticsPrincipalByID(ctx context.Context, principalID string, filter AnalyticsFilter) (AnalyticsResponse, error) {
	p, err := principalFromClaims(ctx)
	if err != nil {
		return AnalyticsResponse{}, fmt.Errorf("%w: %w", ErrTokenParseFailed, err)
	}
	filter.TenantID = p.TenantID
	filter.PrincipalID = &principalID
	resp, err := s.usage.GetAnalytics(ctx, filter)
	if err != nil {
		return AnalyticsResponse{}, fmt.Errorf("analytics principal by id: %w", err)
	}
	return resp, nil
}
