package storage

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/runkora/kora/backend/internal/platform/observability"
)

func TestOtelQueryTracer_ErrorStatusIsStatic(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	tracer := &otelQueryTracer{tracer: provider.Tracer("test")}
	parent, _ := provider.Tracer("test").Start(context.Background(), "parent")
	ctx := tracer.TraceQueryStart(parent, nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: errors.New(`relation "runs" does not exist`)})

	ended := recorder.Ended()
	if len(ended) != 1 {
		t.Fatalf("expected 1 span, got %d", len(ended))
	}
	status := ended[0].Status()
	if status.Code != codes.Error || status.Description != errQueryFailed.Error() {
		t.Errorf("status = %v %q, want Error %q", status.Code, status.Description, errQueryFailed.Error())
	}
	if len(ended[0].Events()) != 1 {
		t.Errorf("expected the driver error recorded as one event, got %d", len(ended[0].Events()))
	}
}

func TestOtelQueryTracer_SpansOnlyUnderAParent(t *testing.T) {
	tests := []struct {
		name      string
		hasParent bool
		wantSpans int
	}{
		{name: "no parent records no span", hasParent: false, wantSpans: 0},
		{name: "parent records a child span", hasParent: true, wantSpans: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

			ctx := context.Background()
			var parent trace.Span
			if tt.hasParent {
				ctx, parent = provider.Tracer("test").Start(ctx, "parent")
			}
			tracer := &otelQueryTracer{tracer: provider.Tracer("test")}
			qctx := tracer.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
			tracer.TraceQueryEnd(qctx, nil, pgx.TraceQueryEndData{})

			if got := len(recorder.Ended()); got != tt.wantSpans {
				t.Fatalf("ended spans = %d, want %d", got, tt.wantSpans)
			}
			if parent != nil && !parent.IsRecording() {
				t.Error("query end ended the parent span")
			}
		})
	}
}

func TestOtelQueryTracer_SpanVocabulary(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	cfg := &PostgresConfig{Host: "db.internal", Port: 5433, Database: "kora", User: "u", SSLMode: "disable"}
	poolConfig, err := cfg.BuildPoolConfig()
	if err != nil {
		t.Fatalf("BuildPoolConfig: %v", err)
	}
	tracer := poolConfig.ConnConfig.Tracer.(*otelQueryTracer)
	tracer.tracer = provider.Tracer("test")
	parent, _ := provider.Tracer("test").Start(context.Background(), "parent")

	ctx := tracer.TraceQueryStart(parent, nil, pgx.TraceQueryStartData{SQL: "UPDATE runs SET state = 'done'"})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{CommandTag: pgconn.NewCommandTag("UPDATE 3")})
	ctx = tracer.TraceQueryStart(parent, nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: errors.New("boom")})

	conn := map[attribute.Key]attribute.Value{
		semconv.DBSystemNameKey:  attribute.StringValue("postgresql"),
		semconv.DBNamespaceKey:   attribute.StringValue("kora"),
		semconv.ServerAddressKey: attribute.StringValue("db.internal"),
		semconv.ServerPortKey:    attribute.IntValue(5433),
	}
	wantExtra := []map[attribute.Key]attribute.Value{
		{observability.DBRowsAffectedKey: attribute.Int64Value(3)},
		{semconv.ErrorTypeKey: attribute.StringValue(errQueryFailed.Error())},
	}
	ended := recorder.Ended()
	if len(ended) != 2 {
		t.Fatalf("expected 2 spans, got %d", len(ended))
	}
	for i, span := range ended {
		if span.Name() != "postgresql" {
			t.Errorf("span name = %q, want postgresql", span.Name())
		}
		got := map[attribute.Key]attribute.Value{}
		for _, kv := range span.Attributes() {
			got[kv.Key] = kv.Value
		}
		if _, ok := got[semconv.DBQueryTextKey]; ok {
			t.Error("query text captured with capture off")
		}
		for _, want := range []map[attribute.Key]attribute.Value{conn, wantExtra[i]} {
			for k, v := range want {
				if got[k] != v {
					t.Errorf("span %d: %s = %v, want %v", i, k, got[k].Emit(), v.Emit())
				}
			}
		}
	}
}
