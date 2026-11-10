package inferencegw

import (
	"context"
	"errors"
	"io"
	"math"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

const (
	tokensMetric = "kora.inference_gateway.tokens"
	costMetric   = "kora.inference_gateway.cost_usd"
)

// metricsEnv is a systemOneEnv whose service exports usage counters to a manual reader.
type metricsEnv struct {
	systemOneEnv
	svc    *Service
	reader *sdkmetric.ManualReader
}

func newMetricsEnv(t *testing.T, stub *systemOneStub, prices []ModelPricing) metricsEnv {
	t.Helper()
	env := newSystemOneEnv(t, allow(), map[ProviderName]ProviderConnector{"typesafe": stub},
		decisionModel(VirtualModelRoute{Provider: "typesafe", ModelID: "jev-latest"}), prices)
	svc := NewService(allow(), env.router, NewStaticPricingCache(prices), env.usage, &mockSpendCounter{}, logging.NewLoggerWithOutput(io.Discard))
	reader := sdkmetric.NewManualReader()
	svc, err := svc.WithMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("WithMetrics: %v", err)
	}
	env.handler = (&Controller{svc: svc, logger: logging.NewLoggerWithOutput(io.Discard)}).handleSystemOne
	return metricsEnv{systemOneEnv: env, svc: svc, reader: reader}
}

// collect returns the tokens by (model,type) and cost by model, and fails on any other attribute.
func (e metricsEnv) collect(t *testing.T) (tokens map[[2]string]int64, cost map[string]float64) {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := e.reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	tokens, cost = map[[2]string]int64{}, map[string]float64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch d := m.Data.(type) {
			case metricdata.Sum[int64]:
				for _, dp := range d.DataPoints {
					if dp.Attributes.Len() != 2 {
						t.Errorf("%s attributes = %v, want model and type only", m.Name, dp.Attributes)
					}
					model, _ := dp.Attributes.Value(attribute.Key("model"))
					typ, _ := dp.Attributes.Value(attribute.Key("type"))
					tokens[[2]string{model.AsString(), typ.AsString()}] += dp.Value
				}
			case metricdata.Sum[float64]:
				for _, dp := range d.DataPoints {
					if dp.Attributes.Len() != 1 {
						t.Errorf("%s attributes = %v, want model only", m.Name, dp.Attributes)
					}
					model, _ := dp.Attributes.Value(attribute.Key("model"))
					cost[model.AsString()] += dp.Value
				}
			}
			if m.Name != tokensMetric && m.Name != costMetric {
				t.Errorf("unexpected metric %s", m.Name)
			}
		}
	}
	return tokens, cost
}

func metricNear(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestUsageMetrics_VirtualModelCallCountsResolvedModel(t *testing.T) {
	t.Parallel()
	stub := &systemOneStub{result: &SystemOneResult{Body: []byte(upstreamBody), InputTokens: 1_000_000, OutputTokens: 500_000}}
	env := newMetricsEnv(t, stub, []ModelPricing{{Provider: "typesafe", ModelID: "jev-latest", InputCostPerMillion: 0.042, OutputCostPerMillion: 2}})

	if rec := env.post(systemOneBody); rec.Code != 200 {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	tokens, cost := env.collect(t)
	const model = "typesafe/jev-latest"
	if len(tokens) != 2 || tokens[[2]string{model, "input"}] != 1_000_000 || tokens[[2]string{model, "output"}] != 500_000 {
		t.Errorf("tokens = %v", tokens)
	}
	if len(cost) != 1 || !metricNear(cost[model], 1.042) {
		t.Errorf("cost = %v, want %s: 1.042", cost, model)
	}
}

func TestUsageMetrics_RunCostMatchesPersistedRows(t *testing.T) {
	t.Parallel()
	env := newMetricsEnv(t, &systemOneStub{}, nil)
	prov := ProviderName("bedrock")
	virtual := "anthropic/claude"
	recs := []InferenceUsageRecord{
		{Provider: &prov, ModelID: "sonnet", VirtualModelID: &virtual, PromptTokens: 100, CompletionTokens: 50, CacheReadInputTokens: 30, CacheCreationInputTokens: 20, EstimatedCostUSD: 0.0123},
		{Provider: &prov, ModelID: "sonnet", VirtualModelID: &virtual, PromptTokens: 7, CompletionTokens: 3, EstimatedCostUSD: 0.0004},
		{ModelID: "bare", PromptTokens: 1, EstimatedCostUSD: 0.5},
	}
	var wantCost float64
	wantTokens := map[[2]string]int64{}
	for _, r := range recs {
		if err := env.svc.writeUsage(context.Background(), r); err != nil {
			t.Fatal(err)
		}
	}
	// What the runs API sums: the persisted rows.
	var rowsCost float64
	for _, r := range env.usage.records {
		rowsCost += r.EstimatedCostUSD
		wantTokens[[2]string{usageModelLabel(r), "input"}] += int64(r.PromptTokens)
		wantTokens[[2]string{usageModelLabel(r), "output"}] += int64(r.CompletionTokens)
		if r.CacheReadInputTokens > 0 {
			wantTokens[[2]string{usageModelLabel(r), "cache_read"}] += int64(r.CacheReadInputTokens)
		}
		if r.CacheCreationInputTokens > 0 {
			wantTokens[[2]string{usageModelLabel(r), "cache_write"}] += int64(r.CacheCreationInputTokens)
		}
	}
	for k, v := range wantTokens {
		if v == 0 {
			delete(wantTokens, k)
		}
	}
	tokens, cost := env.collect(t)
	for _, c := range cost {
		wantCost += c
	}
	if !metricNear(wantCost, rowsCost) {
		t.Errorf("cost counter = %v, rows = %v", wantCost, rowsCost)
	}
	if len(tokens) != len(wantTokens) {
		t.Errorf("tokens = %v, want %v", tokens, wantTokens)
	}
	for k, v := range wantTokens {
		if tokens[k] != v {
			t.Errorf("tokens[%v] = %d, want %d", k, tokens[k], v)
		}
	}
	if tokens[[2]string{"bedrock/sonnet", "cache_read"}] != 30 || tokens[[2]string{"bedrock/sonnet", "cache_write"}] != 20 {
		t.Errorf("cache tokens = %v", tokens)
	}
	if _, ok := cost[virtual]; ok {
		t.Error("virtual model name used as a label")
	}
}

func TestUsageMetrics_FailedWriteAndEmptyCallAddNothing(t *testing.T) {
	t.Parallel()
	env := newMetricsEnv(t, &systemOneStub{}, nil)
	prov := ProviderName("bedrock")

	env.usage.createErr = errors.New("db down")
	if err := env.svc.writeUsage(context.Background(), InferenceUsageRecord{Provider: &prov, ModelID: "m", PromptTokens: 5, EstimatedCostUSD: 1}); err == nil {
		t.Fatal("expected create error")
	}
	env.usage.createErr = nil
	if err := env.svc.writeUsage(context.Background(), InferenceUsageRecord{Provider: &prov, ModelID: "m", Status: UsageStatusProviderError}); err != nil {
		t.Fatal(err)
	}
	tokens, cost := env.collect(t)
	if len(tokens) != 0 || len(cost) != 0 {
		t.Errorf("tokens = %v, cost = %v, want none", tokens, cost)
	}
}

func TestUsageMetrics_NilIsNoop(t *testing.T) {
	t.Parallel()
	svc := NewService(allow(), NewRoutingEngine(nil, nil, nil), NewNopPricingCache(), newMockUsageRepo(), &mockSpendCounter{}, logging.NewLoggerWithOutput(io.Discard))
	if err := svc.writeUsage(context.Background(), InferenceUsageRecord{ModelID: "m", PromptTokens: 5, EstimatedCostUSD: 1}); err != nil {
		t.Fatal(err)
	}
	var m *usageMetrics
	m.record(context.Background(), InferenceUsageRecord{PromptTokens: 1})
}
