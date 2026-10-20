package toolgateway

import (
	"context"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// fakeSnapshots is a snapshotReader stub returning a fixed set of server
// snapshots, so the gauge callback can be tested without an Introspector.
type fakeSnapshots struct{ snaps []ServerSnapshot }

func (f *fakeSnapshots) Snapshot() []ServerSnapshot { return f.snaps }

// newTestMetrics builds the real instrument set on an SDK meter with a manual
// reader, so tests assert on what a collector would actually receive rather
// than on a mock of the OTel API.
func newTestMetrics(t *testing.T, snapshots snapshotReader) (*Metrics, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	m, err := NewMetrics(provider.Meter("test"), snapshots)
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
		_ = provider.Shutdown(context.Background())
	})
	return m, reader
}

// collect gathers one collection cycle, keyed by instrument name.
func collect(t *testing.T, reader *sdkmetric.ManualReader) map[string]metricdata.Metrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	out := map[string]metricdata.Metrics{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			out[m.Name] = m
		}
	}
	return out
}

// A nil *Metrics is a supported configuration: instrumentation is injected,
// and a caller that wired no meter must behave exactly as before.
func TestMetrics_NilIsSafe(t *testing.T) {
	var m *Metrics
	ctx := context.Background()
	m.recordInvocation(ctx, "orbit", "allow", "acme", time.Millisecond)
	if err := m.Close(); err != nil {
		t.Errorf("Close on nil = %v", err)
	}
}

// NewMetrics with a nil snapshotReader (the proxy binary's configuration)
// skips the servers-by-state gauge but still builds a usable invocation
// counter/histogram.
func TestMetrics_NilSnapshotReaderSkipsServerGauge(t *testing.T) {
	m, reader := newTestMetrics(t, nil)
	ctx := context.Background()

	m.recordInvocation(ctx, "orbit", "allow", "acme", 10*time.Millisecond)

	got := collect(t, reader)
	if _, exported := got["orbit.tool_gateway.servers"]; exported {
		t.Error("servers-by-state gauge must not be registered without a snapshotReader")
	}
	if _, exported := got["orbit.tool_gateway.invocations"]; !exported {
		t.Error("invocations counter must still be usable without a snapshotReader")
	}
}

// The invocation counter and duration histogram both carry server, decision,
// and tenant_id — the dimensions an operator needs to tell "this server
// keeps getting denied" from "the gateway's policy check is slow for
// everyone", filterable per tenant per the ADR's 2026-10-20 amendment — and
// deliberately no raw tool label (unbounded cardinality; see
// recordInvocation).
func TestMetrics_InvocationCarriesServerAndDecision(t *testing.T) {
	m, reader := newTestMetrics(t, nil)
	ctx := context.Background()

	m.recordInvocation(ctx, "orbit", "allow", "acme", 5*time.Millisecond)
	m.recordInvocation(ctx, "orbit", "allow", "acme", 5*time.Millisecond)
	m.recordInvocation(ctx, "orbit", "deny", "acme", 5*time.Millisecond)
	m.recordInvocation(ctx, "github", "error", "globex", 5*time.Millisecond)

	metrics := collect(t, reader)

	sum, ok := metrics["orbit.tool_gateway.invocations"].Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("invocations counter is %T, want a sum", metrics["orbit.tool_gateway.invocations"].Data)
	}
	if len(sum.DataPoints) != 3 {
		t.Fatalf("data points = %d, want one per (server,decision,tenant_id)", len(sum.DataPoints))
	}
	for _, dp := range sum.DataPoints {
		if _, hasTool := dp.Attributes.Value("tool"); hasTool {
			t.Errorf("invocations counter must not carry a raw tool label, got attrs %v", dp.Attributes)
		}
		server, _ := dp.Attributes.Value("server")
		decision, _ := dp.Attributes.Value("decision")
		tenantID, hasTenant := dp.Attributes.Value("tenant_id")
		if !hasTenant {
			t.Errorf("invocations counter must carry tenant_id, got attrs %v", dp.Attributes)
		}
		if server.AsString() == "orbit" && decision.AsString() == "allow" {
			if dp.Value != 2 {
				t.Errorf("orbit/allow = %d, want 2", dp.Value)
			}
			if tenantID.AsString() != "acme" {
				t.Errorf("orbit/allow tenant_id = %q, want acme", tenantID.AsString())
			}
		}
		if server.AsString() == "github" && tenantID.AsString() != "globex" {
			t.Errorf("github/error tenant_id = %q, want globex", tenantID.AsString())
		}
	}

	hist, ok := metrics["orbit.tool_gateway.invocation_duration"].Data.(metricdata.Histogram[float64])
	if !ok || len(hist.DataPoints) != 3 {
		t.Fatalf("invocation duration = %+v, want one series per (server,decision,tenant_id)",
			metrics["orbit.tool_gateway.invocation_duration"].Data)
	}
}

// The servers-by-state gauge reports every known state on every collection,
// including zero-count states: an absent series can't be told apart from "not
// scraped yet", and a dashboard needs the zero.
func TestMetrics_ServersByStateReportsEveryStateIncludingZero(t *testing.T) {
	snapshots := &fakeSnapshots{snaps: []ServerSnapshot{
		{Server: Server{Name: "a"}, State: StateOK},
		{Server: Server{Name: "b"}, State: StateOK},
		{Server: Server{Name: "c"}, State: StateDegraded},
	}}
	_, reader := newTestMetrics(t, snapshots)

	gauge, ok := collect(t, reader)["orbit.tool_gateway.servers"].Data.(metricdata.Gauge[int64])
	if !ok {
		t.Fatalf("servers gauge is %T, want a gauge", collect(t, reader)["orbit.tool_gateway.servers"].Data)
	}
	byState := map[string]int64{}
	for _, dp := range gauge.DataPoints {
		state, _ := dp.Attributes.Value("state")
		byState[state.AsString()] = dp.Value
	}
	want := map[string]int64{"pending": 0, "ok": 2, "degraded": 1}
	for state, count := range want {
		if byState[state] != count {
			t.Errorf("state %s = %d, want %d (full: %v)", state, byState[state], count, byState)
		}
	}
}
