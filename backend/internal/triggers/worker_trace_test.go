package triggers

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/runkora/kora/backend/internal/platform/observability"
)

// spanRecorder is the package's one recording provider: the global provider delegates only to the first one set.
var spanRecorder = sync.OnceValue(func() *tracetest.SpanRecorder {
	rec := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	return rec
})

// endedSpan returns the recorded span with this name in the trace, or nil.
func endedSpan(rec *tracetest.SpanRecorder, name string, traceID trace.TraceID) sdktrace.ReadOnlySpan {
	for _, s := range rec.Ended() {
		if s.Name() == name && s.SpanContext().TraceID() == traceID {
			return s
		}
	}
	return nil
}

func hasStringAttr(s sdktrace.ReadOnlySpan, key attribute.Key, want string) bool {
	for _, a := range s.Attributes() {
		if a.Key == key && a.Value.AsString() == want {
			return true
		}
	}
	return false
}

func TestWorkerDispatchContinuesFireTrace(t *testing.T) {
	rec := spanRecorder()
	svc, deps := newTestService(t)
	trig := newAdmissionTrigger(t, deps, OverflowWait, 4, 32)
	fireCtx, fire := otel.Tracer("test").Start(context.Background(), "fire")
	tid := trig.ID
	if _, err := deps.repo.InsertDelivery(context.Background(), &Delivery{
		ID: "d-1", TenantID: testTenant, TriggerID: &tid, Kind: trig.Kind, Status: StatusVerified,
	}); err != nil {
		t.Fatalf("insert delivery: %v", err)
	}
	svc.admit(fireCtx, trig, "d-1", nil)
	fire.End()

	NewWorker(svc, deps.repo, silentLogger()).drain(context.Background())

	s := endedSpan(rec, "triggers.Worker.dispatch", fire.SpanContext().TraceID())
	if s == nil || s.Parent().SpanID() != fire.SpanContext().SpanID() {
		t.Fatal("no dispatch span recorded as a child of the fire span")
	}
	if !hasStringAttr(s, observability.TriggerNameKey, trig.Name) {
		t.Errorf("dispatch span lacks %s = %q", observability.TriggerNameKey, trig.Name)
	}
}

func TestTickScheduledRootsRunTrace(t *testing.T) {
	rec := spanRecorder()
	svc, deps := newTestService(t)
	trig := &Trigger{
		ID: "sched-trace", Name: "nightly", TenantID: testTenant, Kind: KindScheduled,
		PrincipalID: "trigger:sched-trace", AgentTypeID: "at-1", Enabled: true,
		Overflow: OverflowReplace, MaxParallel: 1, MaxQueueDepth: 8,
	}
	if err := deps.repo.CreateTrigger(context.Background(), trig); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	svc.TickScheduled(context.Background(), ScheduledTick{Trigger: *trig, ScheduledAt: time.Now()})
	queued := deps.repo.pendingFor(trig.ID)
	if len(queued) != 1 {
		t.Fatalf("queued %d rows, want 1", len(queued))
	}
	NewWorker(svc, deps.repo, silentLogger()).drain(context.Background())

	queuedSC := trace.SpanContextFromContext(observability.ContextWithTraceParent(context.Background(), queued[0].TraceParent))
	tick := endedSpan(rec, "triggers.Service.TickScheduled", queuedSC.TraceID())
	if tick == nil || tick.Parent().IsValid() || tick.SpanContext().SpanID() != queuedSC.SpanID() {
		t.Fatalf("queued traceparent %q does not name a root tick span", queued[0].TraceParent)
	}
	if !hasStringAttr(tick, observability.TriggerNameKey, trig.Name) {
		t.Errorf("tick span lacks %s = %q", observability.TriggerNameKey, trig.Name)
	}
	if endedSpan(rec, "triggers.Worker.dispatch", queuedSC.TraceID()) == nil {
		t.Error("no dispatch span recorded in the tick's trace")
	}
}
