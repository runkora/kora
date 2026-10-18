package metrics

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/clear-route/orbit/internal/platform/logging"
)

func TestNew_Disabled(t *testing.T) {
	exp, err := New(Config{Enabled: false})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if exp != nil {
		t.Fatalf("New() with Enabled=false = %v, want nil", exp)
	}
}

func TestNilExporter_IsSafe(t *testing.T) {
	var exp *Exporter

	if r := exp.Reader(); r != nil {
		t.Errorf("nil Exporter.Reader() = %v, want nil", r)
	}
	if srv := exp.Server(logging.NewDevelopmentLogger()); srv != nil {
		t.Errorf("nil Exporter.Server() = %v, want nil", srv)
	}
}

func TestNew_EnabledServesMetrics(t *testing.T) {
	exp, err := New(Config{
		Enabled: true,
		Port:    0,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if exp == nil {
		t.Fatal("New() with Enabled=true returned nil Exporter")
	}

	reader := exp.Reader()
	if reader == nil {
		t.Fatal("Reader() returned nil")
	}

	// Feed the reader a MeterProvider and record one instrument, then scrape
	// the exporter's own handler and confirm the series shows up in Prometheus
	// text format — this is the actual bridge this package exists to provide.
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := mp.Meter("test")
	counter, err := meter.Int64Counter("orbit.test.widgets")
	if err != nil {
		t.Fatalf("create counter: %v", err)
	}
	counter.Add(context.Background(), 1)

	srv := exp.Server(logging.NewDevelopmentLogger())
	if srv == nil {
		t.Fatal("Server() returned nil for an enabled Exporter")
	}
	if srv.Handler == nil {
		t.Fatal("Server().Handler is nil")
	}

	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("scrape status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "orbit_test_widgets_total") {
		t.Errorf("scrape body missing orbit_test_widgets_total:\n%s", body)
	}
}

func TestNew_EnabledAlwaysServesAtMetricsPath(t *testing.T) {
	exp, err := New(Config{Enabled: true, Port: 0})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	srv := exp.Server(logging.NewDevelopmentLogger())
	if srv == nil {
		t.Fatal("Server() returned nil for an enabled Exporter")
	}

	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("scrape status = %d, want 200", rec.Code)
	}
}
