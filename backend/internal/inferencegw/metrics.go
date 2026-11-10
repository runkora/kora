package inferencegw

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/metric"

	"github.com/runkora/kora/backend/internal/platform/observability"
)

// usageMetrics exports the tokens and estimated cost of every persisted usage
// record as Prometheus counters. Attributes are the resolved provider model and
// the token type only: tenant, principal, agent, run and trigger identifiers
// stay off metrics because the endpoint is unauthenticated; see
// docs/adr/2026-10-06-prometheus-metrics-endpoints.md. A nil *usageMetrics
// records nothing.
type usageMetrics struct {
	tokens metric.Int64Counter
	cost   metric.Float64Counter
}

// newUsageMetrics builds the counters on meter. The Prometheus exporter exposes
// them as kora_inference_gateway_tokens_total and
// kora_inference_gateway_cost_usd_total.
func newUsageMetrics(meter metric.Meter) (*usageMetrics, error) {
	tokens, err := meter.Int64Counter(
		"kora.inference_gateway.tokens",
		metric.WithDescription("Tokens of persisted inference usage, by resolved provider model and token type"),
	)
	if err != nil {
		return nil, fmt.Errorf("create tokens counter: %w", err)
	}
	cost, err := meter.Float64Counter(
		"kora.inference_gateway.cost_usd",
		metric.WithDescription("Estimated cost in USD of persisted inference usage, by resolved provider model"),
	)
	if err != nil {
		return nil, fmt.Errorf("create cost counter: %w", err)
	}
	return &usageMetrics{tokens: tokens, cost: cost}, nil
}

// usageModelLabel is the resolved provider model of rec, as provider/model. It
// is never the virtual model name.
func usageModelLabel(rec InferenceUsageRecord) string {
	if rec.Provider != nil && *rec.Provider != "" {
		return string(*rec.Provider) + "/" + rec.ModelID
	}
	return rec.ModelID
}

// record adds rec's tokens and cost to the counters. Call it only after rec is
// persisted, so the counters equal the stored rows.
func (m *usageMetrics) record(ctx context.Context, rec InferenceUsageRecord) {
	if m == nil {
		return
	}
	model := observability.UsageModelKey.String(usageModelLabel(rec))
	for _, t := range []struct {
		typ   string
		count int
	}{
		{"input", rec.PromptTokens},
		{"output", rec.CompletionTokens},
		{"cache_read", rec.CacheReadInputTokens},
		{"cache_write", rec.CacheCreationInputTokens},
	} {
		if t.count > 0 {
			m.tokens.Add(ctx, int64(t.count), metric.WithAttributes(model, observability.UsageTokenTypeKey.String(t.typ)))
		}
	}
	if rec.EstimatedCostUSD > 0 {
		m.cost.Add(ctx, rec.EstimatedCostUSD, metric.WithAttributes(model))
	}
}

// WithMetrics exports usage counters on meter.
func (s *Service) WithMetrics(meter metric.Meter) (*Service, error) {
	m, err := newUsageMetrics(meter)
	if err != nil {
		return nil, err
	}
	s.metrics = m
	return s, nil
}
