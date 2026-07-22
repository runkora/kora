package operator

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RegisterDevEnvironmentsGauge registers an observable gauge reporting the
// current count of OrbitDevEnvironment objects, one series per phase
// (attribute "phase") — orbit_devenv_environments in the ADR §10 metrics
// table. Called once at startup (cmd/operator/main.go) once the manager's
// client is available; the callback re-lists on every collection pass, so
// it always reflects live state rather than a value staged inline by the
// reconciler.
func RegisterDevEnvironmentsGauge(meter otelmetric.Meter, c client.Client) error {
	gauge, err := meter.Int64ObservableGauge(
		"orbit_devenv_environments",
		otelmetric.WithDescription("Current count of OrbitDevEnvironment objects by phase"),
	)
	if err != nil {
		return fmt.Errorf("create orbit_devenv_environments gauge: %w", err)
	}

	_, err = meter.RegisterCallback(func(ctx context.Context, o otelmetric.Observer) error {
		list := &OrbitDevEnvironmentList{}
		if listErr := c.List(ctx, list); listErr != nil {
			return fmt.Errorf("list OrbitDevEnvironment for gauge callback: %w", listErr)
		}
		counts := make(map[string]int64, len(list.Items))
		for i := range list.Items {
			phase := list.Items[i].Status.Phase
			if phase == "" {
				phase = DevEnvPhasePending
			}
			counts[phase]++
		}
		for phase, count := range counts {
			o.ObserveInt64(gauge, count, otelmetric.WithAttributes(attribute.String("phase", phase)))
		}
		return nil
	}, gauge)
	if err != nil {
		return fmt.Errorf("register orbit_devenv_environments callback: %w", err)
	}
	return nil
}
