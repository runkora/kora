package agents

import (
	"context"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/runkora/kora/backend/internal/platform/observability"
	"github.com/runkora/kora/backend/internal/platform/token"
)

var (
	recorderOnce sync.Once
	recorder     *tracetest.SpanRecorder
)

// spanRecorder returns the package's one recorder, cleared: the package tracer binds to the first provider set.
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

func TestSendMessage_LinksToRunTrace(t *testing.T) {
	rec := spanRecorder()
	svc, repo, _, _ := newTestService(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot", MaxConcurrentRuns: 5})
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

func TestStartRun_TriggerSpanCarriesTriggerName(t *testing.T) {
	rec := spanRecorder()
	f, repo, _, _ := newTestService(t)
	at := seedAutonomousAgentType(t, repo, "at-1", "t1", "reviewer")
	ctx := withPrincipalCtx(context.Background(), "t1", token.PrincipalTypeService, "trigger:trig-1")
	if _, err := f.run.StartRun(ctx, StartRunRequest{AgentTypeID: at.ID, TriggerID: "trig-1", TriggerName: "Nightly review"}); err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	for _, a := range endedSpan(t, rec, "agents.StartRun").Attributes() {
		if a.Key == observability.TriggerNameKey && a.Value.AsString() == "Nightly review" {
			return
		}
	}
	t.Errorf("agents.StartRun span has no %s attribute", observability.TriggerNameKey)
}

func TestStartRun_PersonSpanLacksTriggerName(t *testing.T) {
	rec := spanRecorder()
	f, repo, _, _ := newTestService(t)
	at := seedAutonomousAgentType(t, repo, "at-1", "t1", "reviewer")
	ctx := withPrincipalCtx(context.Background(), "t1", token.PrincipalTypeHuman, "user-1")
	if _, err := f.run.StartRun(ctx, StartRunRequest{AgentTypeID: at.ID}); err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	hasSlug := false
	for _, a := range endedSpan(t, rec, "agents.StartRun").Attributes() {
		switch a.Key {
		case observability.TriggerNameKey, observability.TriggerIDKey:
			t.Errorf("person-started agents.StartRun span carries %s", a.Key)
		case observability.AgentSlugKey:
			hasSlug = true
		}
	}
	if !hasSlug {
		t.Errorf("agents.StartRun span has no %s attribute", observability.AgentSlugKey)
	}
}
