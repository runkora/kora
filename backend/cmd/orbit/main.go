package main

import (
	"context"
	"log"
	"time"

	"github.com/clear-route/orbit/internal/logging"
	"github.com/clear-route/orbit/internal/observability"
	"go.opentelemetry.io/otel"
)

func main() {
	// Initialize observability (OTEL tracing)
	ctx := context.Background()
	shutdown, err := observability.SetupFromEnv(ctx)
	if err != nil {
		log.Fatalf("Failed to setup observability: %v", err)
	}
	defer func() {
		if err := shutdown(ctx); err != nil {
			log.Printf("Error shutting down observability: %v", err)
		}
	}()

	// Create logger instance - in production, inject this via dependency injection
	logger := logging.NewDevelopmentLogger()

	// Example 1: Basic logging without trace context
	logger.Info(ctx, "Orbit control plane starting", logging.Field{Key: "version", Value: "0.1.0"})

	// Example 2: Logging with OpenTelemetry trace context
	// In production, traces would come from incoming requests via OTEL middleware
	tracer := otel.Tracer("orbit-example")
	
	// Start a traced operation
	tracedCtx, span := tracer.Start(ctx, "agent-execution")
	defer span.End()

	// These logs will automatically include trace_id and span_id
	logger.Info(tracedCtx, "Agent execution started", 
		logging.Field{Key: "agent_id", Value: "agent-123"},
		logging.Field{Key: "model", Value: "gpt-4"},
	)

	// Simulate some work
	time.Sleep(100 * time.Millisecond)

	// Nested span example - shows parent-child relationship in traces
	nestedCtx, nestedSpan := tracer.Start(tracedCtx, "policy-evaluation")
	logger.Info(nestedCtx, "Evaluating agent policies",
		logging.Field{Key: "policy_count", Value: 3},
	)
	nestedSpan.End()

	// Error logging with trace context
	logger.Error(tracedCtx, "Simulated error for demonstration",
		logging.Field{Key: "error_code", Value: "ERR_DEMO"},
		logging.Field{Key: "retry_count", Value: 0},
	)

	// Example 3: Logging back in non-traced context
	logger.Info(ctx, "Orbit control plane shutdown complete")

	// Example 4: Debug level (typically filtered in production)
	logger.Debug(ctx, "This is a debug message",
		logging.Field{Key: "detail", Value: "only shown in development"},
	)

	// Example 5: Warning level
	logger.Warn(ctx, "High memory usage detected",
		logging.Field{Key: "memory_percent", Value: 85.5},
		logging.Field{Key: "threshold", Value: 80.0},
	)
}
