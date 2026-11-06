// Package policyservice wires the policy-service feature's cross-cutting
// instrumentation. The feature logic itself lives in its subpackages (api,
// grants, roles, bundle, ...); this file only builds the OTel instrument set
// on top of them.
package policyservice

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/policyservice/api"
)

// metricsSnapshotQueryTimeout bounds the grants/roles/bundle-staleness
// gauges' DB read. The Prometheus exporter invokes this callback at scrape
// time with context.TODO() — no deadline of its own — so without a timeout a
// slow database hangs the whole scrape indefinitely. Mirrors the short
// DB-query budgets in internal/platform/storage/db.go (ping/health checks).
const metricsSnapshotQueryTimeout = 5 * time.Second

// snapshotReader is the one read the grants/roles/bundle-staleness gauges
// take. *api.Service satisfies it (api.Service.MetricsSnapshot).
//
// Every instrument built here is a tenant-aggregate — summed across all
// tenants, no tenant_id label, no per-tenant breakdown — because
// policyservice.Stats() (policyservice/api/service.go:334-399) is
// deliberately scoped to one tenant per call to avoid leaking cross-tenant
// counts (service.go:360-362), and that isolation must not be weakened to
// make a gauge convenient. See
// docs/adr/2026-10-07-domain-metrics-instrumentation.md.
type snapshotReader interface {
	MetricsSnapshot(ctx context.Context) (api.MetricsSnapshot, error)
}

// Metrics is the policy-service feature's observable-gauge instrument set.
//
// It is injected, never global, mirroring internal/triggers/metrics.go: a nil
// *Metrics is fully usable and records nothing, so tests and any binary that
// has not wired a meter behave exactly as they did before instrumentation
// existed.
type Metrics struct {
	registration metric.Registration
}

// NewMetrics builds the instrument set on the given meter. reader is polled
// once per collection cycle (at Prometheus scrape time), not on every write,
// since these are tenant-aggregate counts rather than per-request events.
func NewMetrics(meter metric.Meter, reader snapshotReader, logger logging.Logger) (*Metrics, error) {
	m := &Metrics{}

	grantsGauge, err := meter.Int64ObservableGauge(
		"kora.policy_service.grants",
		metric.WithDescription("Grants across all tenants, by principal type"),
		metric.WithUnit("{grant}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create grants gauge: %w", err)
	}

	rolesGauge, err := meter.Int64ObservableGauge(
		"kora.policy_service.roles",
		metric.WithDescription("Roles across all tenants, by kind (reserved or custom)"),
		metric.WithUnit("{role}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create roles gauge: %w", err)
	}

	staleBundles, err := meter.Int64ObservableGauge(
		"kora.policy_service.bundles_stale",
		metric.WithDescription("Count of known tenants whose compiled OPA bundle is stale"),
		metric.WithUnit("{tenant}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create bundle-staleness gauge: %w", err)
	}

	m.registration, err = meter.RegisterCallback(
		func(ctx context.Context, o metric.Observer) error {
			ctx, cancel := context.WithTimeout(ctx, metricsSnapshotQueryTimeout)
			defer cancel()

			snapshot, snapErr := reader.MetricsSnapshot(ctx)
			if snapErr != nil {
				// Reported, not returned: a failed scrape of these gauges must
				// not abort the whole collection cycle and take every other
				// instrument on this meter down with it.
				logger.Error(ctx, "failed to read policy-service metrics snapshot",
					logging.Field{Key: "error", Value: snapErr.Error()})
				return nil
			}
			for principalType, count := range snapshot.GrantsByPrincipalType {
				o.ObserveInt64(grantsGauge, int64(count),
					metric.WithAttributes(attribute.String("principal_type", string(principalType))))
			}
			for kind, count := range snapshot.RolesByKind {
				o.ObserveInt64(rolesGauge, int64(count),
					metric.WithAttributes(attribute.String("kind", kind)))
			}
			o.ObserveInt64(staleBundles, int64(snapshot.StaleBundles))
			return nil
		},
		grantsGauge, rolesGauge, staleBundles,
	)
	if err != nil {
		return nil, fmt.Errorf("register policy-service metrics callback: %w", err)
	}

	return m, nil
}

// Close unregisters the observable callback. Safe on a nil *Metrics.
func (m *Metrics) Close() error {
	if m == nil || m.registration == nil {
		return nil
	}
	if err := m.registration.Unregister(); err != nil {
		return fmt.Errorf("unregister policy-service metrics callback: %w", err)
	}
	return nil
}
