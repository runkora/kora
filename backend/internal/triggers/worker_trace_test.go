package triggers

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// A queued fire dispatches in the trace of the request that fired it.
func TestWorkerDispatchContinuesFireTrace(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	svc, deps := newTestService(t)
	trig := newAdmissionTrigger(t, deps, OverflowWait, 4, 32)
	fireCtx, fire := tp.Tracer("test").Start(context.Background(), "fire")
	tid := trig.ID
	if _, err := deps.repo.InsertDelivery(context.Background(), &Delivery{
		ID: "d-1", TenantID: testTenant, TriggerID: &tid, Kind: trig.Kind, Status: StatusVerified,
	}); err != nil {
		t.Fatalf("insert delivery: %v", err)
	}
	svc.admit(fireCtx, trig, "d-1", nil)
	fire.End()

	NewWorker(svc, deps.repo, silentLogger()).drain(context.Background())

	for _, s := range rec.Ended() {
		if s.Name() == "triggers.Worker.dispatch" {
			if s.SpanContext().TraceID() != fire.SpanContext().TraceID() || s.Parent().SpanID() != fire.SpanContext().SpanID() {
				t.Fatalf("dispatch span trace %s parent %s, want a child of the fire span", s.SpanContext().TraceID(), s.Parent().SpanID())
			}
			return
		}
	}
	t.Fatal("no dispatch span recorded")
}
