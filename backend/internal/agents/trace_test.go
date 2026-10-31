package agents

import (
	"context"
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

// A message to a running run is its own trace with a link to the run's.
func TestSendMessage_LinksToRunTrace(t *testing.T) {
	rec := spanRecorder()
	svc, repo, _, _ := newTestService(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot", MaxConcurrentRunsPerPod: 5})
	const runTrace = "4bf92f3577b34da6a3ce929d0e0e4736"
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateActive,
		TraceParent: "00-" + runTrace + "-00f067aa0ba902b7-01"})

	if err := svc.SendMessage(context.Background(), "run-1", "hello"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	span := endedSpan(t, rec, "agents.SendMessage")
	links := span.Links()
	if span.SpanContext().TraceID().String() == runTrace || len(links) != 1 || links[0].SpanContext.TraceID().String() != runTrace {
		t.Fatalf("trace %s links %v, want its own trace linked to %s", span.SpanContext().TraceID(), links, runTrace)
	}
}
