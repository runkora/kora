package inferencegw

import (
	"context"
	"errors"
	"fmt"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	openai "github.com/openai/openai-go"
	"github.com/openai/openai-go/responses"

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
		span.SetStatus(codes.Error, ErrPolicyDenied.Error())
		s.writeFailedUsage(ctx, p, routeReq, DialectOpenAI, nil, nil, nil, UsageStatusPolicyDenied, "POLICY_DENIED", nil, 0)
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
	resp, usedTarget, retryCount, callErr := s.callWithFailover(ctx, targets, params)

	latencyMS := int(time.Since(start).Milliseconds())

	if callErr != nil || usedTarget == nil {
		span.RecordError(callErr)
		span.SetStatus(codes.Error, ErrProviderError.Error())
		errCode := extractErrorCode(callErr)
		s.writeFailedUsage(ctx, p, routeReq, DialectOpenAI, nthProvider(targets), nthModelFamily(targets), &strategy, UsageStatusProviderError, errCode, extractFailureDetails(callErr), latencyMS)
		return nil, fmt.Errorf("%w: %w", ErrProviderError, callErr)
	}

	// ── 5. Write usage record ─────────────────────────────────────────────────
	promptTokens := int(resp.Usage.PromptTokens)
	completionTokens := int(resp.Usage.CompletionTokens)
	totalTokens := int(resp.Usage.TotalTokens)
	if totalTokens == 0 {
		totalTokens = promptTokens + completionTokens
	}
	cacheReadTokens := int(resp.Usage.PromptTokensDetails.CachedTokens)

	pricing := usedTarget.Pricing
	estimatedCost := pricing.EstimatedCostUSD(promptTokens, completionTokens, cacheReadTokens, 0)

	rec := InferenceUsageRecord{
		ID:                   uuid.NewString(),
		TenantID:             p.TenantID,
		PrincipalID:          p.PrincipalID,
		PrincipalType:        p.PrincipalType,
		TokenID:              nullableText(p.TokenID),
		APIDialect:           DialectOpenAI,
		Provider:             &usedTarget.Provider,
		VirtualModelID:       nullableText(usedTarget.VirtualModelID),
		ModelID:              usedTarget.ModelID,
		ModelFamily:          nullableText(usedTarget.ModelFamily),
		RoutingStrategy:      &strategy,
		PromptTokens:         promptTokens,
		CompletionTokens:     completionTokens,
		TotalTokens:          totalTokens,
		CacheReadInputTokens: cacheReadTokens,
		EstimatedCostUSD:     estimatedCost,
		Status:               UsageStatusSuccess,
		RetryCount:           retryCount,
		LatencyMS:            latencyMS,
		CreatedAt:            start.UTC(),
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
		s.writeFailedUsage(ctx, p, routeReq, DialectOpenAI, nil, nil, nil, UsageStatusPolicyDenied, "POLICY_DENIED", nil, 0)
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

		promptTokens, completionTokens, totalTokens, cacheReadTokens, streamErr := drainOpenAIStream(ctx, upstream, out)

		latencyMS := int(time.Since(start).Milliseconds())

		if totalTokens == 0 {
			totalTokens = promptTokens + completionTokens
		}

		if streamErr != nil {
			if errors.Is(streamErr, errClientDisconnected) {
				span.SetStatus(codes.Ok, "client disconnected")
				s.writeFailedUsage(writeCtx, p, routeReq, DialectOpenAI, &capturedTarget.Provider, nullableText(capturedTarget.ModelFamily), &capturedStrategy, UsageStatusCanceled, "CLIENT_DISCONNECTED", nil, latencyMS)
				return
			}
			span.RecordError(streamErr)
			span.SetStatus(codes.Error, ErrProviderError.Error())
			s.writeFailedUsage(writeCtx, p, routeReq, DialectOpenAI, &capturedTarget.Provider, nullableText(capturedTarget.ModelFamily), &capturedStrategy, UsageStatusProviderError, extractErrorCode(streamErr), extractFailureDetails(streamErr), latencyMS)
			return
		}

		span.SetStatus(codes.Ok, "")

		pricing := capturedTarget.Pricing
		estimatedCost := pricing.EstimatedCostUSD(promptTokens, completionTokens, cacheReadTokens, 0)

		rec := InferenceUsageRecord{
			ID:                   uuid.NewString(),
			TenantID:             p.TenantID,
			PrincipalID:          p.PrincipalID,
			PrincipalType:        p.PrincipalType,
			TokenID:              nullableText(p.TokenID),
			APIDialect:           DialectOpenAI,
			Provider:             &capturedTarget.Provider,
			VirtualModelID:       nullableText(capturedTarget.VirtualModelID),
			ModelID:              capturedTarget.ModelID,
			ModelFamily:          nullableText(capturedTarget.ModelFamily),
			RoutingStrategy:      &capturedStrategy,
			PromptTokens:         promptTokens,
			CompletionTokens:     completionTokens,
			TotalTokens:          totalTokens,
			CacheReadInputTokens: cacheReadTokens,
			EstimatedCostUSD:     estimatedCost,
			Status:               UsageStatusSuccess,
			RetryCount:           streamRetryCount,
			LatencyMS:            latencyMS,
			CreatedAt:            start.UTC(),
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
) (promptTokens, completionTokens, totalTokens, cacheReadTokens int, streamErr error) {
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
		TokenID:         nullableText(p.TokenID),
		APIDialect:      dialect,
		Provider:        provider,
		VirtualModelID:  nullableText(req.Model),
		ModelID:         req.Model,
		ModelFamily:     modelFamily,
		RoutingStrategy: strategy,
		Status:          status,
		ErrorCode:       errCodePtr,
		FailureDetails:  failureDetails,
		LatencyMS:       latencyMS,
		CreatedAt:       time.Now().UTC(),
	}

	if err := s.usage.Create(ctx, rec); err != nil {
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
			msg = msg[:maxProviderMessageLen]
		}
		fd.ProviderErrorMessage = &msg
	}
	if fd.ProviderHTTPStatus == nil && fd.ProviderErrorType == nil && fd.ProviderErrorMessage == nil {
		return nil
	}
	return fd
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
		span.SetStatus(codes.Error, ErrPolicyDenied.Error())
		s.writeFailedUsage(ctx, p, routeReq, DialectOpenAI, nil, nil, nil, UsageStatusPolicyDenied, "POLICY_DENIED", nil, 0)
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
		errCode := extractErrorCode(callErr)
		s.writeFailedUsage(ctx, p, routeReq, DialectOpenAI, nthProvider(targets), nthModelFamily(targets), &strategy, UsageStatusProviderError, errCode, extractFailureDetails(callErr), latencyMS)
		return nil, fmt.Errorf("%w: %w", ErrProviderError, callErr)
	}

	promptTokens := int(resp.Usage.PromptTokens)
	pricing := usedTarget.Pricing
	estimatedCost := pricing.EstimatedCostUSD(promptTokens, 0, 0, 0)

	rec := InferenceUsageRecord{
		ID:               uuid.NewString(),
		TenantID:         p.TenantID,
		PrincipalID:      p.PrincipalID,
		PrincipalType:    p.PrincipalType,
		TokenID:          nullableText(p.TokenID),
		APIDialect:       DialectOpenAI,
		Provider:         &usedTarget.Provider,
		VirtualModelID:   nullableText(usedTarget.VirtualModelID),
		ModelID:          usedTarget.ModelID,
		ModelFamily:      nullableText(usedTarget.ModelFamily),
		RoutingStrategy:  &strategy,
		PromptTokens:     promptTokens,
		CompletionTokens: 0,
		TotalTokens:      promptTokens,
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
		span.SetStatus(codes.Error, ErrPolicyDenied.Error())
		s.writeFailedUsage(ctx, p, routeReq, DialectResponses, nil, nil, nil, UsageStatusPolicyDenied, "POLICY_DENIED", nil, 0)
		return nil, err
	}

	targets, strategy, err := s.router.RouteRequest(ctx, routeReq)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrRoutingFailed.Error())
		s.writeFailedUsage(ctx, p, routeReq, DialectResponses, nil, nil, nil, UsageStatusRoutingFailed, "", nil, 0)
		return nil, fmt.Errorf("%w: %w", ErrRoutingFailed, err)
	}

	resp, usedTarget, retryCount, callErr := retryTargets(ctx, s, targets, "retrying after retryable provider error",
		func(c ProviderConnector, target RouteTarget) (*responses.Response, error) {
			q := params
			q.Model = target.ModelID
			return c.Responses(ctx, q)
		})

	latencyMS := int(time.Since(start).Milliseconds())

	if callErr != nil || usedTarget == nil {
		span.RecordError(callErr)
		span.SetStatus(codes.Error, ErrProviderError.Error())
		errCode := extractErrorCode(callErr)
		s.writeFailedUsage(ctx, p, routeReq, DialectResponses, nthProvider(targets), nthModelFamily(targets), &strategy, UsageStatusProviderError, errCode, extractFailureDetails(callErr), latencyMS)
		return nil, fmt.Errorf("%w: %w", ErrProviderError, callErr)
	}

	promptTokens := int(resp.Usage.InputTokens)
	completionTokens := int(resp.Usage.OutputTokens)
	totalTokens := int(resp.Usage.TotalTokens)
	if totalTokens == 0 {
		totalTokens = promptTokens + completionTokens
	}
	cacheReadTokens := int(resp.Usage.InputTokensDetails.CachedTokens)
	pricing := usedTarget.Pricing
	estimatedCost := pricing.EstimatedCostUSD(promptTokens, completionTokens, cacheReadTokens, 0)

	rec := InferenceUsageRecord{
		ID:                   uuid.NewString(),
		TenantID:             p.TenantID,
		PrincipalID:          p.PrincipalID,
		PrincipalType:        p.PrincipalType,
		TokenID:              nullableText(p.TokenID),
		APIDialect:           DialectResponses,
		Provider:             &usedTarget.Provider,
		VirtualModelID:       nullableText(usedTarget.VirtualModelID),
		ModelID:              usedTarget.ModelID,
		ModelFamily:          nullableText(usedTarget.ModelFamily),
		RoutingStrategy:      &strategy,
		PromptTokens:         promptTokens,
		CompletionTokens:     completionTokens,
		TotalTokens:          totalTokens,
		CacheReadInputTokens: cacheReadTokens,
		EstimatedCostUSD:     estimatedCost,
		Status:               UsageStatusSuccess,
		RetryCount:           retryCount,
		LatencyMS:            latencyMS,
		CreatedAt:            start.UTC(),
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
		span.SetStatus(codes.Error, ErrPolicyDenied.Error())
		span.End()
		s.writeFailedUsage(ctx, p, routeReq, DialectResponses, nil, nil, nil, UsageStatusPolicyDenied, "POLICY_DENIED", nil, 0)
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

		inputTokens, outputTokens, cacheReadTokens, streamErr := drainResponsesStream(ctx, upstream, out)
		latencyMS := int(time.Since(start).Milliseconds())

		if streamErr != nil {
			if errors.Is(streamErr, errClientDisconnected) {
				span.SetStatus(codes.Ok, "client disconnected")
				s.writeFailedUsage(writeCtx, p, routeReq, DialectResponses, &capturedTarget.Provider, nullableText(capturedTarget.ModelFamily), &capturedStrategy, UsageStatusCanceled, "CLIENT_DISCONNECTED", nil, latencyMS)
				return
			}
			span.RecordError(streamErr)
			span.SetStatus(codes.Error, ErrProviderError.Error())
			s.writeFailedUsage(writeCtx, p, routeReq, DialectResponses, &capturedTarget.Provider, nullableText(capturedTarget.ModelFamily), &capturedStrategy, UsageStatusProviderError, extractErrorCode(streamErr), extractFailureDetails(streamErr), latencyMS)
			return
		}

		span.SetStatus(codes.Ok, "")

		totalTokens := inputTokens + outputTokens
		pricing := capturedTarget.Pricing
		estimatedCost := pricing.EstimatedCostUSD(inputTokens, outputTokens, cacheReadTokens, 0)

		rec := InferenceUsageRecord{
			ID:                   uuid.NewString(),
			TenantID:             p.TenantID,
			PrincipalID:          p.PrincipalID,
			PrincipalType:        p.PrincipalType,
			TokenID:              nullableText(p.TokenID),
			APIDialect:           DialectResponses,
			Provider:             &capturedTarget.Provider,
			VirtualModelID:       nullableText(capturedTarget.VirtualModelID),
			ModelID:              capturedTarget.ModelID,
			ModelFamily:          nullableText(capturedTarget.ModelFamily),
			RoutingStrategy:      &capturedStrategy,
			PromptTokens:         inputTokens,
			CompletionTokens:     outputTokens,
			TotalTokens:          totalTokens,
			CacheReadInputTokens: cacheReadTokens,
			EstimatedCostUSD:     estimatedCost,
			Status:               UsageStatusSuccess,
			RetryCount:           streamRetryCount,
			LatencyMS:            latencyMS,
			CreatedAt:            start.UTC(),
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

// drainResponsesStream forwards events from upstream to out and accumulates
// token counts from the terminal response.completed event.
// Returns (inputTokens, outputTokens, cacheReadTokens, streamErr).
func drainResponsesStream(
	ctx context.Context,
	upstream <-chan ResponsesStreamResult,
	out chan<- ResponsesStreamResult,
) (inputTokens, outputTokens, cacheReadTokens int, streamErr error) {
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
		span.SetStatus(codes.Error, ErrPolicyDenied.Error())
		s.writeFailedUsage(ctx, p, routeReq, DialectAnthropic, nil, nil, nil, UsageStatusPolicyDenied, "POLICY_DENIED", nil, 0)
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
		s.writeFailedUsage(ctx, p, routeReq, DialectAnthropic, nthProvider(targets), nthModelFamily(targets), &strategy, UsageStatusProviderError, extractErrorCode(callErr), extractFailureDetails(callErr), latencyMS)
		return nil, fmt.Errorf("%w: %w", ErrProviderError, callErr)
	}

	promptTokens := int(msg.Usage.InputTokens)
	completionTokens := int(msg.Usage.OutputTokens)
	cacheReadTokens := int(msg.Usage.CacheReadInputTokens)
	cacheCreationTokens := int(msg.Usage.CacheCreationInputTokens)
	pricing := usedTarget.Pricing
	estimatedCost := pricing.EstimatedCostUSD(promptTokens, completionTokens, cacheReadTokens, cacheCreationTokens)

	rec := InferenceUsageRecord{
		ID:                       uuid.NewString(),
		TenantID:                 p.TenantID,
		PrincipalID:              p.PrincipalID,
		PrincipalType:            p.PrincipalType,
		TokenID:                  nullableText(p.TokenID),
		APIDialect:               DialectAnthropic,
		Provider:                 &usedTarget.Provider,
		VirtualModelID:           nullableText(usedTarget.VirtualModelID),
		ModelID:                  usedTarget.ModelID,
		ModelFamily:              nullableText(usedTarget.ModelFamily),
		RoutingStrategy:          &strategy,
		PromptTokens:             promptTokens,
		CompletionTokens:         completionTokens,
		TotalTokens:              promptTokens + completionTokens + cacheReadTokens + cacheCreationTokens,
		CacheReadInputTokens:     cacheReadTokens,
		CacheCreationInputTokens: cacheCreationTokens,
		EstimatedCostUSD:         estimatedCost,
		Status:                   UsageStatusSuccess,
		RetryCount:               retryCount,
		LatencyMS:                latencyMS,
		CreatedAt:                start.UTC(),
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
		s.writeFailedUsage(ctx, p, routeReq, DialectAnthropic, nil, nil, nil, UsageStatusPolicyDenied, "POLICY_DENIED", nil, 0)
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
				s.writeFailedUsage(writeCtx, p, routeReq, DialectAnthropic, &capturedTarget.Provider, nullableText(capturedTarget.ModelFamily), &capturedStrategy, UsageStatusCanceled, "CLIENT_DISCONNECTED", nil, latencyMS)
				return
			}
			span.RecordError(streamErr)
			span.SetStatus(codes.Error, ErrProviderError.Error())
			s.writeFailedUsage(writeCtx, p, routeReq, DialectAnthropic, &capturedTarget.Provider, nullableText(capturedTarget.ModelFamily), &capturedStrategy, UsageStatusProviderError, extractErrorCode(streamErr), extractFailureDetails(streamErr), latencyMS)
			return
		}

		span.SetStatus(codes.Ok, "")

		promptTokens := int(accumulated.Usage.InputTokens)
		completionTokens := int(accumulated.Usage.OutputTokens)
		cacheReadTokens := int(accumulated.Usage.CacheReadInputTokens)
		cacheCreationTokens := int(accumulated.Usage.CacheCreationInputTokens)
		pricing := capturedTarget.Pricing
		estimatedCost := pricing.EstimatedCostUSD(promptTokens, completionTokens, cacheReadTokens, cacheCreationTokens)

		rec := InferenceUsageRecord{
			ID:                       uuid.NewString(),
			TenantID:                 p.TenantID,
			PrincipalID:              p.PrincipalID,
			PrincipalType:            p.PrincipalType,
			TokenID:                  nullableText(p.TokenID),
			APIDialect:               DialectAnthropic,
			Provider:                 &capturedTarget.Provider,
			VirtualModelID:           nullableText(capturedTarget.VirtualModelID),
			ModelID:                  capturedTarget.ModelID,
			ModelFamily:              nullableText(capturedTarget.ModelFamily),
			RoutingStrategy:          &capturedStrategy,
			PromptTokens:             promptTokens,
			CompletionTokens:         completionTokens,
			TotalTokens:              promptTokens + completionTokens + cacheReadTokens + cacheCreationTokens,
			CacheReadInputTokens:     cacheReadTokens,
			CacheCreationInputTokens: cacheCreationTokens,
			EstimatedCostUSD:         estimatedCost,
			Status:                   UsageStatusSuccess,
			RetryCount:               streamRetryCount,
			LatencyMS:                latencyMS,
			CreatedAt:                start.UTC(),
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
