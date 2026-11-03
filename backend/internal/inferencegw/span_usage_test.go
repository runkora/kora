package inferencegw

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/runkora/kora/backend/internal/platform/observability"
)

func TestRecordSpanUsage_SetsUsageAndResponseModel(t *testing.T) {
	t.Parallel()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	_, span := provider.Tracer("test").Start(context.Background(), "chat anthropic/claude-haiku-5-5",
		trace.WithAttributes(observability.GenAIRequestModelKey.String("anthropic/claude-haiku-5-5")))

	recordSpanUsage(span, "au.anthropic.claude-haiku-5-5", 100, 20, 7, 3)
	span.End()

	got := map[attribute.Key]attribute.Value{}
	for _, kv := range recorder.Ended()[0].Attributes() {
		got[kv.Key] = kv.Value
	}
	wantStr := map[attribute.Key]string{
		observability.GenAIRequestModelKey:  "anthropic/claude-haiku-5-5",
		observability.GenAIResponseModelKey: "au.anthropic.claude-haiku-5-5",
	}
	for k, want := range wantStr {
		if v := got[k].AsString(); v != want {
			t.Errorf("%s = %q, want %q", k, v, want)
		}
	}
	wantInt := map[attribute.Key]int64{
		observability.GenAIUsageInputTokensKey:         100,
		observability.GenAIUsageOutputTokensKey:        20,
		observability.GenAIUsageCacheReadTokensKey:     7,
		observability.GenAIUsageCacheCreationTokensKey: 3,
	}
	for k, want := range wantInt {
		if v := got[k].AsInt64(); v != want {
			t.Errorf("%s = %d, want %d", k, v, want)
		}
	}
}
