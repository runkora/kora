package agents

import (
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

var (
	recorderOnce sync.Once
	recorder     *tracetest.SpanRecorder
)

// spanRecorder installs one global tracer provider for the package's tests and clears its spans.
// The package tracer binds to the first provider set, so every test shares it.
func spanRecorder() *tracetest.SpanRecorder {
	recorderOnce.Do(func() {
		recorder = tracetest.NewSpanRecorder()
		otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	})
	recorder.Reset()
	return recorder
}

func endedSpan(t *testing.T, rec *tracetest.SpanRecorder, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	for _, s := range rec.Ended() {
		if s.Name() == name {
			return s
		}
	}
	t.Fatalf("no %s span", name)
	return nil
}

// A scheduler attempt continues the trace StartRun stored, though it runs on a context of its own.
func TestStartClaimed_ContinuesStartRunTrace(t *testing.T) {
	rec := spanRecorder()
	f, _, _, _ := schedulerService(t)
	reqCtx, req := otel.Tracer("test").Start(startCtx(), "POST /api/v1/runs")
	run, err := f.run.StartRun(reqCtx, StartRunRequest{AgentTypeID: "at-1"})
	req.End()
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if run.TraceID == nil || *run.TraceID != req.SpanContext().TraceID().String() {
		t.Fatalf("run TraceID = %v, want %s", run.TraceID, req.SpanContext().TraceID())
	}

	if _, err := startAndSchedule(startCtx(), f.run, StartRunRequest{AgentTypeID: "at-1"}); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	start := endedSpan(t, rec, "agents.StartRun")
	var attempt sdktrace.ReadOnlySpan
	for _, s := range rec.Ended() {
		if s.Name() == "agents.startClaimed" && s.SpanContext().TraceID() == req.SpanContext().TraceID() {
			attempt = s
		}
	}
	if attempt == nil || attempt.Parent().SpanID() != start.SpanContext().SpanID() {
		t.Fatal("no startClaimed span parented to the run's StartRun span")
	}
}
