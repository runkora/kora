package metrics

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// Exporter bridges a service's OTel instruments to a pull-based Prometheus
// /metrics endpoint. It owns a dedicated prometheus.Registry rather than the
// global DefaultRegisterer: a transitive dependency elsewhere in the module
// graph (OPA, controller-runtime) auto-registering onto the default registry
// in an init() func must not leak into, or collide with, this service's own
// scrape output.
type Exporter struct {
	reader *otelprom.Exporter // implements sdkmetric.Reader
	reg    *prometheus.Registry
	cfg    Config
}

// promErrorLogger adapts logging.Logger to promhttp.HandlerOpts.ErrorLog, so
// a scrape-time collector/registration error is actually recorded instead of
// silently dropped (promhttp's zero-value ErrorLog is nil). promhttp gives no
// per-request context to log against, so this logs against
// context.Background(), same as any other background/shutdown-path log call
// in this package.
type promErrorLogger struct {
	logger logging.Logger
}

func (l promErrorLogger) Println(v ...any) {
	l.logger.Error(context.Background(), "prometheus scrape error",
		logging.Field{Key: "error", Value: fmt.Sprint(v...)},
	)
}

// New builds the Prometheus bridge when cfg.Enabled; returns (nil, nil)
// otherwise, so every call site can do `if exp != nil` once and skip the rest.
func New(cfg Config) (*Exporter, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	reg := prometheus.NewRegistry()
	if cfg.CollectGoRuntimeMetrics {
		reg.MustRegister(collectors.NewGoCollector())
	}
	if cfg.CollectProcessMetrics {
		reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	}

	opts := []otelprom.Option{otelprom.WithRegisterer(reg)}
	if cfg.Namespace != "" {
		opts = append(opts, otelprom.WithNamespace(cfg.Namespace))
	}
	reader, err := otelprom.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("create prometheus exporter: %w", err)
	}

	return &Exporter{
		reader: reader,
		reg:    reg,
		cfg:    cfg,
	}, nil
}

// Reader returns the sdkmetric.Reader to hand to observability.SetupMetrics.
// Nil-safe: a nil *Exporter yields a nil reader, which SetupMetrics's
// extraReaders parameter already treats as "nothing to add".
func (e *Exporter) Reader() sdkmetric.Reader {
	if e == nil {
		return nil
	}
	return e.reader
}

// Server returns this service's dedicated metrics *http.Server: one bare
// route on its own ServeMux, with no TracingMiddleware/LoggingMiddleware
// chain — modeled directly on api.Server's /health bypass (api/api.go:63-92):
// scrape traffic fires every few seconds and must not flood traces/logs, and
// here it also must not share the authenticated app's mux or port at all.
// Nil-safe: returns nil when metrics are disabled, so callers can start it
// with a single `if metricsSrv != nil` guard.
func (e *Exporter) Server(logger logging.Logger) *http.Server {
	if e == nil {
		return nil
	}
	handler := promhttp.HandlerFor(e.reg, promhttp.HandlerOpts{
		ErrorLog: promErrorLogger{logger: logger},
	})
	mux := http.NewServeMux()
	mux.Handle(e.cfg.path(), handler)
	return &http.Server{
		Addr:              fmt.Sprintf(":%d", e.cfg.Port),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
}
