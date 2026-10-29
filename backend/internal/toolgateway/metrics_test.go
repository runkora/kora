package toolgateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/runkora/kora/backend/internal/platform/logging"
	mcpkit "github.com/runkora/kora/backend/internal/platform/mcp"
	platformtoken "github.com/runkora/kora/backend/internal/platform/token"
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
	m.recordInvocation(ctx, "kora", "allow", "acme", time.Millisecond)
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

	m.recordInvocation(ctx, "kora", "allow", "acme", 10*time.Millisecond)

	got := collect(t, reader)
	if _, exported := got["kora.tool_gateway.servers"]; exported {
		t.Error("servers-by-state gauge must not be registered without a snapshotReader")
	}
	if _, exported := got["kora.tool_gateway.invocations"]; !exported {
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

	m.recordInvocation(ctx, "kora", "allow", "acme", 5*time.Millisecond)
	m.recordInvocation(ctx, "kora", "allow", "acme", 5*time.Millisecond)
	m.recordInvocation(ctx, "kora", "deny", "acme", 5*time.Millisecond)
	m.recordInvocation(ctx, "github", "error", "globex", 5*time.Millisecond)

	metrics := collect(t, reader)

	sum, ok := metrics["kora.tool_gateway.invocations"].Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("invocations counter is %T, want a sum", metrics["kora.tool_gateway.invocations"].Data)
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
		if server.AsString() == "kora" && decision.AsString() == "allow" {
			if dp.Value != 2 {
				t.Errorf("kora/allow = %d, want 2", dp.Value)
			}
			if tenantID.AsString() != "acme" {
				t.Errorf("kora/allow tenant_id = %q, want acme", tenantID.AsString())
			}
		}
		if server.AsString() == "github" && tenantID.AsString() != "globex" {
			t.Errorf("github/error tenant_id = %q, want globex", tenantID.AsString())
		}
	}

	hist, ok := metrics["kora.tool_gateway.invocation_duration"].Data.(metricdata.Histogram[float64])
	if !ok || len(hist.DataPoints) != 3 {
		t.Fatalf("invocation duration = %+v, want one series per (server,decision,tenant_id)",
			metrics["kora.tool_gateway.invocation_duration"].Data)
	}
}

// TestHandleToolsCall_RecordsInvocationMetric drives the real HTTP handler
// (MCPController.handleMCP → handleToolsCall), not recordInvocation
// directly, so a future regression that removes the recording call from
// handleToolsCall itself would fail this test. It covers: an allowed call
// through a real upstream (the sleep proves the recorded duration now spans
// the upstream round trip, not just the policy decision), a policy-denied
// call, and the two early-return paths — malformed params and a missing
// tool name — that must record as "error" rather than being invisible to
// the metric.
func TestHandleToolsCall_RecordsInvocationMetric(t *testing.T) {
	const upstreamDelay = 20 * time.Millisecond

	upstream := mcpkit.NewServer("upstream", "v0")
	upstream.AddTool(&sdk.Tool{Name: "agents.list", InputSchema: json.RawMessage(`{"type":"object"}`)},
		func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			time.Sleep(upstreamDelay)
			return &sdk.CallToolResult{}, nil
		})
	ts := httptest.NewServer(mcpkit.NewController("/mcp", upstream).Handler())
	defer ts.Close()

	reg := &Registry{
		servers: []Server{{TenantID: "", Name: "kora", Endpoint: ts.URL + "/mcp"}},
		loaded:  true,
	}
	cat := &memCatalog{tools: []CatalogTool{{ServerSlug: "kora", ToolName: "agents.list", Scope: "mcp:kora:agents:list:use"}}}
	eval := &fakeEval{checkOK: true}

	m, reader := newTestMetrics(t, nil)
	c := NewMCPController(nil, eval, cat, reg, NewProxy(NewUpstreamCredentials(""), "test", logging.NewLogger()), nil, "/v1", m, logging.NewLogger())

	claims := &platformtoken.PlatformClaims{}
	claims.Kora.TenantID = "acme"
	claims.Subject = "agent-1"
	ctx := platformtoken.WithClaims(context.Background(), claims)

	call := func(body string) {
		req := httptest.NewRequest(http.MethodPost, "/v1/mcp/kora", strings.NewReader(body)).WithContext(ctx)
		req.SetPathValue("server", "kora")
		c.handleMCP(httptest.NewRecorder(), req)
	}

	call(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"agents.list","arguments":{}}}`) // allow
	eval.checkOK = false
	call(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"agents.list","arguments":{}}}`) // deny
	call(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"","arguments":{}}}`)            // missing tool name
	call(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":"bad"}`)                                 // malformed params

	metrics := collect(t, reader)
	sum, ok := metrics["kora.tool_gateway.invocations"].Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("invocations counter is %T, want a sum", metrics["kora.tool_gateway.invocations"].Data)
	}
	counts := map[string]int64{}
	for _, dp := range sum.DataPoints {
		decision, _ := dp.Attributes.Value("decision")
		counts[decision.AsString()] += dp.Value
	}
	if counts["allow"] != 1 {
		t.Errorf("allow invocations = %d, want 1", counts["allow"])
	}
	if counts["deny"] != 1 {
		t.Errorf("deny invocations = %d, want 1", counts["deny"])
	}
	if counts["error"] != 2 {
		t.Errorf("error invocations (missing tool name + malformed params) = %d, want 2", counts["error"])
	}

	hist, ok := metrics["kora.tool_gateway.invocation_duration"].Data.(metricdata.Histogram[float64])
	if !ok {
		t.Fatalf("invocation duration is %T, want a histogram", metrics["kora.tool_gateway.invocation_duration"].Data)
	}
	var allowSum float64
	var sawAllow bool
	for _, dp := range hist.DataPoints {
		decision, _ := dp.Attributes.Value("decision")
		if decision.AsString() == "allow" {
			allowSum, sawAllow = dp.Sum, true
		}
	}
	if !sawAllow {
		t.Fatalf("no allow data point recorded, got %+v", hist.DataPoints)
	}
	if allowSum < upstreamDelay.Seconds() {
		t.Errorf("allow duration = %v, want it to include the upstream call's %v sleep (timer must span the proxy call, not stop at the policy decision)", allowSum, upstreamDelay)
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

	gauge, ok := collect(t, reader)["kora.tool_gateway.servers"].Data.(metricdata.Gauge[int64])
	if !ok {
		t.Fatalf("servers gauge is %T, want a gauge", collect(t, reader)["kora.tool_gateway.servers"].Data)
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
