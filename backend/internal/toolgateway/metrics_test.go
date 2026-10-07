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
	m, err := NewMetrics(provider.Meter("test"), snapshots, testLogger())
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
	m.recordInvocation(ctx, "orbit", "read_file", "allow", time.Millisecond)
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

	m.recordInvocation(ctx, "orbit", "read_file", "allow", 10*time.Millisecond)

	got := collect(t, reader)
	if _, exported := got["orbit.tool_gateway.servers"]; exported {
		t.Error("servers-by-state gauge must not be registered without a snapshotReader")
	}
	if _, exported := got["orbit.tool_gateway.invocations"]; !exported {
		t.Error("invocations counter must still be usable without a snapshotReader")
	}
}

// The invocation counter and duration histogram both carry server, tool and
// decision — the three dimensions an operator needs to tell "this tool keeps
// getting denied" from "the gateway's policy check is slow for everyone".
func TestMetrics_InvocationCarriesServerToolDecision(t *testing.T) {
	m, reader := newTestMetrics(t, nil)
	ctx := context.Background()

	m.recordInvocation(ctx, "orbit", "read_file", "allow", 5*time.Millisecond)
	m.recordInvocation(ctx, "orbit", "read_file", "allow", 5*time.Millisecond)
	m.recordInvocation(ctx, "orbit", "write_file", "deny", 5*time.Millisecond)
	m.recordInvocation(ctx, "github", "list_prs", "error", 5*time.Millisecond)

	metrics := collect(t, reader)

	sum, ok := metrics["orbit.tool_gateway.invocations"].Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("invocations counter is %T, want a sum", metrics["orbit.tool_gateway.invocations"].Data)
	}
	if len(sum.DataPoints) != 3 {
		t.Fatalf("data points = %d, want one per (server,tool,decision)", len(sum.DataPoints))
	}
	for _, dp := range sum.DataPoints {
		server, _ := dp.Attributes.Value("server")
		tool, _ := dp.Attributes.Value("tool")
		decision, _ := dp.Attributes.Value("decision")
		if server.AsString() == "orbit" && tool.AsString() == "read_file" {
			if decision.AsString() != "allow" || dp.Value != 2 {
				t.Errorf("orbit/read_file = (%s, %d), want (allow, 2)", decision.AsString(), dp.Value)
			}
		}
	}

	hist, ok := metrics["orbit.tool_gateway.invocation_duration"].Data.(metricdata.Histogram[float64])
	if !ok || len(hist.DataPoints) != 3 {
		t.Fatalf("invocation duration = %+v, want one series per (server,tool,decision)",
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
