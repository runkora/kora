package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/middleware"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestMux_MetricsMiddlewareReadsRoutePattern is the end-to-end regression
// guard for the bug where MetricsMiddleware sat in the global middleware
// chain (outside Mux's own dispatch) and so could never see the route
// RoutePattern sets — every request recorded route="unmatched" regardless of
// which route actually matched. It exercises the real Mux/HandleWithMiddleware
// wiring, not a hand-composed chain, so it would have caught that.
func TestMux_MetricsMiddlewareReadsRoutePattern(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := mp.Meter("test")

	metricsMW, err := middleware.MetricsMiddleware(meter, "testsvc")
	if err != nil {
		t.Fatalf("MetricsMiddleware() error = %v", err)
	}

	ctx := context.Background()
	logger := logging.NewLogger()
	mux := NewMux(ctx, logger)
	mux.SetMetricsMiddleware(middleware.MiddlewareChain(metricsMW))
	mux.SetBasePath("/widgets")
	mux.HandleWithMiddleware("GET /widgets/{id}", nil, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/widgets/123", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	route := findRouteAttribute(t, rm, "kora.testsvc.http.requests")
	if route == "" {
		t.Fatal("kora.testsvc.http.requests not recorded")
	}
	if route == "unmatched" {
		t.Fatal(`route recorded as "unmatched" — MetricsMiddleware ran before RoutePattern set the route on the request context`)
	}
	if route != "/widgets/{id}" {
		t.Errorf("route = %q, want %q", route, "/widgets/{id}")
	}
}

// findRouteAttribute returns the "route" attribute value of the first data
// point recorded for the named int64 sum instrument, or "" if not found.
func findRouteAttribute(t *testing.T, rm metricdata.ResourceMetrics, name string) string {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %q is not an int64 Sum: %T", name, m.Data)
			}
			for _, dp := range sum.DataPoints {
				if v, ok := dp.Attributes.Value("route"); ok {
					return v.AsString()
				}
			}
		}
	}
	return ""
}
