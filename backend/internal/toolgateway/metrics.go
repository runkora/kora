// Tool-gateway domain metrics (ADR 2026-10-07: Domain Metrics Instrumentation
// per Backend Service).
package toolgateway

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// Metrics is the tool-gateway's instrument set.
//
// It is injected, never global: a package-level meter would make the feature
// untestable and would tie every consumer of this package to whatever
// provider happened to be installed first. A nil *Metrics is fully usable and
// records nothing, so tests and any binary that has not wired a meter behave
// exactly as they did before instrumentation existed.
//
// The gateway runs as two separate deployments (publisher, proxy — see
// cmd/tool-gateway/main.go), and the two instrument groups below belong to
// different ones: the invocation counter/histogram are only ever recorded
// from the proxy's request path (handleToolsCall), and the per-server-state
// gauge is only ever observable in the publisher, which is the only mode that
// holds an Introspector. One Metrics value supports both — snapshots is nil
// in proxy mode and the gauge is simply not registered.
type Metrics struct {
	invocations        metric.Int64Counter
	invocationDuration metric.Float64Histogram

	registration metric.Registration
}

// snapshotReader is the one read the per-server-state gauge callback takes.
type snapshotReader interface {
	Snapshot() []ServerSnapshot
}

// NewMetrics builds the instrument set on the given meter.
//
// snapshots is read by the servers-by-state gauge callback at each collection;
// pass nil (the proxy binary has no Introspector) to skip registering that
// gauge while still getting a usable invocation counter/histogram.
func NewMetrics(meter metric.Meter, snapshots snapshotReader, logger logging.Logger) (*Metrics, error) {
	m := &Metrics{}

	var err error
	if m.invocations, err = meter.Int64Counter(
		"orbit.tool_gateway.invocations",
		metric.WithDescription("MCP tools/call requests, by server, tool and policy decision"),
		metric.WithUnit("{invocation}"),
	); err != nil {
		return nil, fmt.Errorf("create invocations counter: %w", err)
	}

	if m.invocationDuration, err = meter.Float64Histogram(
		"orbit.tool_gateway.invocation_duration",
		metric.WithDescription("Wall time of a tools/call request, from receipt to policy decision"),
		metric.WithUnit("s"),
	); err != nil {
		return nil, fmt.Errorf("create invocation-duration histogram: %w", err)
	}

	if snapshots == nil {
		return m, nil
	}

	serversGauge, err := meter.Int64ObservableGauge(
		"orbit.tool_gateway.servers",
		metric.WithDescription("Mounted MCP servers by introspection state"),
		metric.WithUnit("{server}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create servers-by-state gauge: %w", err)
	}

	m.registration, err = meter.RegisterCallback(
		func(_ context.Context, o metric.Observer) error {
			// Every known state is reported on every collection, including the
			// states with zero servers — an absent series is indistinguishable
			// from "not scraped yet", and a dashboard panel needs the zero.
			counts := map[ServerState]int64{StatePending: 0, StateOK: 0, StateDegraded: 0}
			for _, snap := range snapshots.Snapshot() {
				counts[snap.State]++
			}
			for state, count := range counts {
				o.ObserveInt64(serversGauge, count, metric.WithAttributes(attribute.String("state", string(state))))
			}
			return nil
		},
		serversGauge,
	)
	if err != nil {
		return nil, fmt.Errorf("register servers-by-state callback: %w", err)
	}

	return m, nil
}

// Close unregisters the observable callback, when one was registered. Safe on
// a nil *Metrics.
func (m *Metrics) Close() error {
	if m == nil || m.registration == nil {
		return nil
	}
	if err := m.registration.Unregister(); err != nil {
		return fmt.Errorf("unregister tool-gateway metrics callback: %w", err)
	}
	return nil
}

// recordInvocation records one tools/call outcome: whether the policy check
// errored, denied, or allowed the call, how long reaching that decision took,
// and which server/tool it targeted. decision is one of "error", "deny",
// "allow" — the same three values already set on the request span's
// mcp.decision attribute, so the metric and the trace agree on vocabulary.
func (m *Metrics) recordInvocation(ctx context.Context, server, tool, decision string, took time.Duration) {
	if m == nil {
		return
	}
	attrs := metric.WithAttributes(
		attribute.String("server", server),
		attribute.String("tool", tool),
		attribute.String("decision", decision),
	)
	m.invocations.Add(ctx, 1, attrs)
	m.invocationDuration.Record(ctx, took.Seconds(), attrs)
}
