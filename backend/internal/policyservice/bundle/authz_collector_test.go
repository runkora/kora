package bundle_test

import "testing"

const testCollector = "otel-collector.observability.svc:4317"

// Export calls to the configured collector are allowed without a token; any
// other path to it, or an export call to any other authority, is not. See
// docs/adr/2026-09-18-telemetry-collector-platform-destination.md.
func TestAuthz_TelemetryCollector(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{telemetryCollector: testCollector})

	cases := []struct {
		name      string
		authority string
		path      string
		allow     bool
	}{
		{"trace export", testCollector, "/opentelemetry.proto.collector.trace.v1.TraceService/Export", true},
		{"metrics export", testCollector, "/opentelemetry.proto.collector.metrics.v1.MetricsService/Export", true},
		{"logs export", testCollector, "/opentelemetry.proto.collector.logs.v1.LogsService/Export", true},
		{"authority case differs", "OTEL-Collector.observability.svc:4317", "/opentelemetry.proto.collector.trace.v1.TraceService/Export", true},
		{"other path to the collector", testCollector, "/api/v1/anything", false},
		{"export to another authority", "attacker.example.com:4317", "/opentelemetry.proto.collector.trace.v1.TraceService/Export", false},
		{"collector host on another port", "otel-collector.observability.svc:4318", "/opentelemetry.proto.collector.trace.v1.TraceService/Export", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			allow, reason := evalLayer1(t, pq, "POST", tc.path, map[string]string{":authority": tc.authority})
			if allow != tc.allow {
				t.Errorf("allow = %v (reason %q), want %v", allow, reason, tc.allow)
			}
		})
	}
}

// With no collector configured, export calls get no special treatment.
func TestAuthz_TelemetryCollector_Unconfigured(t *testing.T) {
	pq := compileAndPrepare(t, bundleOpts{})

	allow, reason := evalLayer1(t, pq, "POST", "/opentelemetry.proto.collector.trace.v1.TraceService/Export",
		map[string]string{":authority": testCollector})
	if allow {
		t.Errorf("allow = true (reason %q), want false without a configured collector", reason)
	}
}
