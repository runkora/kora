package observability

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"google.golang.org/grpc/credentials"
)

// SetupMetrics initializes the OpenTelemetry metrics pipeline and returns the
// meter a binary's instruments are built on, plus a shutdown func that flushes
// pending exports.
//
// Opt-in: only binaries that call it get a metrics pipeline, so adding it here
// changes nothing for the services that only trace. It mirrors Setup's
// exporter selection, including its priority order — an explicit OTLP endpoint
// always wins, and everything else gets a provider that records nothing — so
// metrics and traces are configured by the same two settings and cannot end up
// pointing at different collectors. Unlike traces there is no local stdout
// exporter: periodic metric dumps in a dev terminal are noise, not signal.
//
// extraReaders attaches additional sdkmetric.Reader instances to the same
// MeterProvider — the Prometheus bridge in internal/platform/metrics is the
// one caller today. Readers are independent of the OTLP reader above and of
// each other: a service can have neither, either, or both, so an instrument
// built on the returned meter works whether or not a collector is configured.
// A nil reader is skipped, so passing (*metrics.Exporter)(nil).Reader() from a
// service with metrics disabled needs no caller-side nil check.
func SetupMetrics(ctx context.Context, cfg Config, extraReaders ...sdkmetric.Reader) (metric.Meter, func(context.Context) error, error) {
	res, err := resource.New(ctx, resource.WithAttributes(
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceVersion(cfg.ServiceVersion),
		semconv.DeploymentEnvironmentNameKey.String(cfg.Environment),
	))
	if err != nil {
		return nil, nil, fmt.Errorf("create metrics resource: %w", err)
	}

	mpOpts := []sdkmetric.Option{sdkmetric.WithResource(res)}

	endpoint := DefaultConfig.OTLPEndpoint
	if endpoint != "" {
		addr, insecure, err := otlpTarget(endpoint)
		if err != nil {
			return nil, nil, err
		}
		opts := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(addr)}
		if insecure {
			opts = append(opts, otlpmetricgrpc.WithInsecure())
		} else {
			opts = append(opts, otlpmetricgrpc.WithTLSCredentials(credentials.NewClientTLSFromCert(nil, "")))
		}
		exporter, err := otlpmetricgrpc.New(ctx, opts...)
		if err != nil {
			return nil, nil, fmt.Errorf("create OTLP metric exporter: %w", err)
		}
		mpOpts = append(mpOpts, sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter)))
	}

	for _, r := range extraReaders {
		if r != nil {
			mpOpts = append(mpOpts, sdkmetric.WithReader(r))
		}
	}

	// No reader at all (no OTLP endpoint, no extra readers): a provider with no
	// reader records nothing and exports nothing. The instruments still exist,
	// so no call site needs a nil check and nothing is written to pod logs.
	mp := sdkmetric.NewMeterProvider(mpOpts...)
	otel.SetMeterProvider(mp)
	return mp.Meter(cfg.ServiceName), mp.Shutdown, nil
}
