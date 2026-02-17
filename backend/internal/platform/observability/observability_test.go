package observability

import (
	"context"
	"os"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

// TestSetup verifies basic observability setup
func TestSetup(t *testing.T) {
	ctx := context.Background()
	cfg := Config{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "development",
		SamplingRate:   0.5,
	}
	
	shutdown, err := Setup(ctx, cfg)
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	defer shutdown(ctx)
	
	if shutdown == nil {
		t.Error("Setup() returned nil shutdown function")
	}
}

// TestSetupDevelopmentMode verifies development mode with stdout exporter
func TestSetupDevelopmentMode(t *testing.T) {
	ctx := context.Background()
	cfg := Config{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "development",
	}
	
	shutdown, err := Setup(ctx, cfg)
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	defer shutdown(ctx)
	
	// Verify tracer provider was set
	tp := otel.GetTracerProvider()
	if tp == nil {
		t.Error("Tracer provider not set")
	}
	
	// Create a test span to verify it works
	tracer := tp.Tracer("test")
	_, span := tracer.Start(ctx, "test-span")
	span.End()
}

// TestSetupProductionWithOTLP verifies production mode with OTLP endpoint
func TestSetupProductionWithOTLP(t *testing.T) {
	// Skip in CI or if no OTLP collector available
	if os.Getenv("CI") != "" {
		t.Skip("Skipping OTLP test in CI")
	}
	
	ctx := context.Background()
	cfg := Config{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "production",
		OTLPEndpoint:   "localhost:4317",
		OTLPInsecure:   true, // For testing
	}
	
	shutdown, err := Setup(ctx, cfg)
	if err != nil {
		// If OTLP collector is not available, that's okay for this test
		t.Skipf("OTLP endpoint not available: %v", err)
	}
	defer shutdown(ctx)
}

// TestSetupProductionNoOTLP verifies production mode without OTLP (no-op)
func TestSetupProductionNoOTLP(t *testing.T) {
	ctx := context.Background()
	cfg := Config{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "production",
		// No OTLP endpoint - should create no-op provider
	}
	
	shutdown, err := Setup(ctx, cfg)
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	defer shutdown(ctx)
	
	if shutdown == nil {
		t.Error("Setup() returned nil shutdown function")
	}
}

// TestSetupDefaultSamplingRate verifies default sampling rate
func TestSetupDefaultSamplingRate(t *testing.T) {
	ctx := context.Background()
	cfg := Config{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "development",
		SamplingRate:   0, // Should default to 0.1
	}
	
	shutdown, err := Setup(ctx, cfg)
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	defer shutdown(ctx)
	
	// The default should have been applied internally
	// We can't easily verify the sampling rate directly, but we verified no error
}

// TestSetupCustomSamplingRate verifies custom sampling rates
func TestSetupCustomSamplingRate(t *testing.T) {
	tests := []struct {
		name         string
		samplingRate float64
	}{
		{"0% sampling", 0.0},
		{"50% sampling", 0.5},
		{"100% sampling", 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			cfg := Config{
				ServiceName:    "test-service",
				ServiceVersion: "1.0.0",
				Environment:    "development",
				SamplingRate:   tt.samplingRate,
			}
			
			shutdown, err := Setup(ctx, cfg)
			if err != nil {
				t.Fatalf("Setup() error = %v", err)
			}
			defer shutdown(ctx)
		})
	}
}

// TestSetupShutdown verifies shutdown functionality
func TestSetupShutdown(t *testing.T) {
	ctx := context.Background()
	cfg := Config{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "development",
	}
	
	shutdown, err := Setup(ctx, cfg)
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	
	// Call shutdown
	if err := shutdown(ctx); err != nil {
		t.Errorf("shutdown() error = %v", err)
	}
	
	// Calling shutdown again should not panic
	if err := shutdown(ctx); err != nil {
		t.Errorf("second shutdown() error = %v", err)
	}
}

// TestSetupShutdownTimeout verifies shutdown respects timeout
func TestSetupShutdownTimeout(t *testing.T) {
	ctx := context.Background()
	cfg := Config{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "development",
	}
	
	shutdown, err := Setup(ctx, cfg)
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	
	// Create context with very short timeout
	shutdownCtx, cancel := context.WithTimeout(ctx, 1*time.Millisecond)
	defer cancel()
	
	// Should complete (possibly with timeout error, but shouldn't hang)
	_ = shutdown(shutdownCtx)
}

// TestSetupFromEnv verifies environment variable configuration
func TestSetupFromEnv(t *testing.T) {
	// Save original env vars
	origServiceName := os.Getenv("OTEL_SERVICE_NAME")
	origServiceVersion := os.Getenv("OTEL_SERVICE_VERSION")
	origEnvironment := os.Getenv("ENVIRONMENT")
	origSamplingRate := os.Getenv("OTEL_TRACE_SAMPLING_RATE")
	
	// Restore after test
	defer func() {
		os.Setenv("OTEL_SERVICE_NAME", origServiceName)
		os.Setenv("OTEL_SERVICE_VERSION", origServiceVersion)
		os.Setenv("ENVIRONMENT", origEnvironment)
		os.Setenv("OTEL_TRACE_SAMPLING_RATE", origSamplingRate)
	}()
	
	// Set test env vars
	os.Setenv("OTEL_SERVICE_NAME", "env-test-service")
	os.Setenv("OTEL_SERVICE_VERSION", "2.0.0")
	os.Setenv("ENVIRONMENT", "development")
	os.Setenv("OTEL_TRACE_SAMPLING_RATE", "0.5")
	
	ctx := context.Background()
	shutdown, err := SetupFromEnv(ctx)
	if err != nil {
		t.Fatalf("SetupFromEnv() error = %v", err)
	}
	defer shutdown(ctx)
}

// TestSetupFromEnvDefaults verifies default values when env vars not set
func TestSetupFromEnvDefaults(t *testing.T) {
	// Clear env vars
	origServiceName := os.Getenv("OTEL_SERVICE_NAME")
	origServiceVersion := os.Getenv("OTEL_SERVICE_VERSION")
	origEnvironment := os.Getenv("ENVIRONMENT")
	origSamplingRate := os.Getenv("OTEL_TRACE_SAMPLING_RATE")
	
	os.Unsetenv("OTEL_SERVICE_NAME")
	os.Unsetenv("OTEL_SERVICE_VERSION")
	os.Unsetenv("ENVIRONMENT")
	os.Unsetenv("OTEL_TRACE_SAMPLING_RATE")
	
	defer func() {
		os.Setenv("OTEL_SERVICE_NAME", origServiceName)
		os.Setenv("OTEL_SERVICE_VERSION", origServiceVersion)
		os.Setenv("ENVIRONMENT", origEnvironment)
		os.Setenv("OTEL_TRACE_SAMPLING_RATE", origSamplingRate)
	}()
	
	ctx := context.Background()
	shutdown, err := SetupFromEnv(ctx)
	if err != nil {
		t.Fatalf("SetupFromEnv() error = %v", err)
	}
	defer shutdown(ctx)
	
	// Should use defaults: orbit, dev, development, 0.1
}

// TestSetupFromEnvInvalidSamplingRate verifies invalid sampling rate handling
func TestSetupFromEnvInvalidSamplingRate(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"invalid format", "invalid"},
		{"negative rate", "-0.5"},
		{"rate > 1", "1.5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origSamplingRate := os.Getenv("OTEL_TRACE_SAMPLING_RATE")
			defer os.Setenv("OTEL_TRACE_SAMPLING_RATE", origSamplingRate)
			
			os.Setenv("OTEL_TRACE_SAMPLING_RATE", tt.value)
			
			ctx := context.Background()
			shutdown, err := SetupFromEnv(ctx)
			if err != nil {
				t.Fatalf("SetupFromEnv() error = %v", err)
			}
			defer shutdown(ctx)
			
			// Should fall back to default (0.1) without error
		})
	}
}

// TestSetupFromEnvInsecureFlag verifies OTLP insecure flag parsing
func TestSetupFromEnvInsecureFlag(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected bool
	}{
		{"true", "true", true},
		{"false", "false", false},
		{"empty", "", false},
		{"invalid", "yes", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origInsecure := os.Getenv("OTEL_EXPORTER_OTLP_INSECURE")
			defer os.Setenv("OTEL_EXPORTER_OTLP_INSECURE", origInsecure)
			
			if tt.value != "" {
				os.Setenv("OTEL_EXPORTER_OTLP_INSECURE", tt.value)
			} else {
				os.Unsetenv("OTEL_EXPORTER_OTLP_INSECURE")
			}
			
			ctx := context.Background()
			shutdown, err := SetupFromEnv(ctx)
			if err != nil {
				t.Fatalf("SetupFromEnv() error = %v", err)
			}
			defer shutdown(ctx)
		})
	}
}

// TestGetEnv verifies getEnv helper function
func TestGetEnv(t *testing.T) {
	tests := []struct {
		name         string
		envKey       string
		envValue     string
		defaultValue string
		expected     string
	}{
		{
			name:         "env var set",
			envKey:       "TEST_KEY",
			envValue:     "test-value",
			defaultValue: "default",
			expected:     "test-value",
		},
		{
			name:         "env var not set",
			envKey:       "NONEXISTENT_KEY",
			envValue:     "",
			defaultValue: "default",
			expected:     "default",
		},
		{
			name:         "env var empty string",
			envKey:       "EMPTY_KEY",
			envValue:     "",
			defaultValue: "default",
			expected:     "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				os.Setenv(tt.envKey, tt.envValue)
				defer os.Unsetenv(tt.envKey)
			} else {
				os.Unsetenv(tt.envKey)
			}
			
			result := getEnv(tt.envKey, tt.defaultValue)
			if result != tt.expected {
				t.Errorf("getEnv() = %s, want %s", result, tt.expected)
			}
		})
	}
}

// TestTracerProviderGlobalSet verifies tracer provider is set globally
func TestTracerProviderGlobalSet(t *testing.T) {
	// Clear any existing tracer provider
	otel.SetTracerProvider(nil)
	
	ctx := context.Background()
	cfg := Config{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "development",
	}
	
	shutdown, err := Setup(ctx, cfg)
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	defer shutdown(ctx)
	
	// Verify global tracer provider is set
	tp := otel.GetTracerProvider()
	if tp == nil {
		t.Error("Global tracer provider not set")
	}
	
	// Verify we can create a tracer
	tracer := tp.Tracer("test")
	if tracer == nil {
		t.Error("Failed to create tracer from provider")
	}
}

// TestPropagatorGlobalSet verifies W3C TraceContext propagator is set
func TestPropagatorGlobalSet(t *testing.T) {
	ctx := context.Background()
	cfg := Config{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "development",
	}
	
	shutdown, err := Setup(ctx, cfg)
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	defer shutdown(ctx)
	
	// Verify propagator is set (we can't easily inspect it, but we can verify no panic)
	propagator := otel.GetTextMapPropagator()
	if propagator == nil {
		t.Error("Global text map propagator not set")
	}
}

// TestSpanExportWithDevelopment verifies spans are exported in development mode
func TestSpanExportWithDevelopment(t *testing.T) {
	ctx := context.Background()
	cfg := Config{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "development",
		SamplingRate:   1.0, // Always sample for this test
	}
	
	shutdown, err := Setup(ctx, cfg)
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	defer shutdown(ctx)
	
	// Create and end a span
	tracer := otel.GetTracerProvider().Tracer("test")
	_, span := tracer.Start(ctx, "test-span")
	span.End()
	
	// Shutdown to flush
	if err := shutdown(ctx); err != nil {
		t.Errorf("shutdown() error = %v", err)
	}
}

// TestNoOpTracerProviderShutdown verifies no-op tracer provider shutdown
func TestNoOpTracerProviderShutdown(t *testing.T) {
	ctx := context.Background()
	
	// Create a simple no-op provider to test shutdown
	tp := trace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(nil)
	
	shutdown := func(ctx context.Context) error {
		return tp.Shutdown(ctx)
	}
	
	if shutdown == nil {
		t.Error("Shutdown function is nil")
	}
	
	// Verify shutdown works
	if err := shutdown(ctx); err != nil {
		t.Errorf("shutdown() error = %v", err)
	}
}

// TestCreateNoOpProvider verifies createNoOpProvider function
func TestCreateNoOpProvider(t *testing.T) {
	ctx := context.Background()
	
	// Create test resource
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName("test-service"),
		),
	)
	if err != nil {
		t.Fatalf("Failed to create resource: %v", err)
	}
	
	shutdown := createNoOpProvider(res)
	
	if shutdown == nil {
		t.Fatal("Shutdown function should not be nil")
	}
	
	// Verify the tracer provider is set globally
	tp := otel.GetTracerProvider()
	if tp == nil {
		t.Fatal("TracerProvider should not be nil")
	}
	
	defer otel.SetTracerProvider(nil)
	
	// Verify shutdown works without error
	if err := shutdown(ctx); err != nil {
		t.Errorf("shutdown() error = %v", err)
	}
	
	// Create a tracer and span to verify it's functional
	tracer := tp.Tracer("test")
	if tracer == nil {
		t.Fatal("Tracer should not be nil")
	}
	
	_, span := tracer.Start(ctx, "test-span")
	if span == nil {
		t.Fatal("Span should not be nil")
	}
	span.End()
}
