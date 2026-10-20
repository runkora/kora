package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/runkora/kora/backend/internal/platform/metrics/buckets"
)

// collectCounter returns the sum of int64 counter data points for the given
// instrument name, and true if the instrument was found at all.
func collectCounter(t *testing.T, rm metricdata.ResourceMetrics, name string) (int64, bool) {
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
			var total int64
			for _, dp := range sum.DataPoints {
				total += dp.Value
			}
			return total, true
		}
	}
	return 0, false
}

func TestMetricsMiddleware_RecordsRequest(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := mp.Meter("test")

	mw, err := MetricsMiddleware(meter, "testsvc")
	if err != nil {
		t.Fatalf("MetricsMiddleware() error = %v", err)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})
	// RoutePattern outermost, MetricsMiddleware inside it — matching
	// api.Mux.HandleWithMiddleware's real wiring order, where RoutePattern
	// sets the route on the request context before MetricsMiddleware runs.
	// Reversing this order is exactly the bug this test guards against; see
	// TestMux_MetricsMiddlewareReadsRoutePattern in the api package for the
	// end-to-end version that exercises the real Mux.
	wrapped := RoutePattern("/widgets")(mw.next(handler))

	req := httptest.NewRequest("POST", "/widgets", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	count, ok := collectCounter(t, rm, "orbit.testsvc.http.requests")
	if !ok {
		t.Fatal("orbit.testsvc.http.requests not recorded")
	}
	if count != 1 {
		t.Errorf("request count = %d, want 1", count)
	}

	var histogram *metricdata.Histogram[float64]
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "orbit.testsvc.http.request.duration" {
				h, ok := m.Data.(metricdata.Histogram[float64])
				if !ok {
					t.Fatalf("orbit.testsvc.http.request.duration is not a Histogram[float64]: %T", m.Data)
				}
				histogram = &h
			}
		}
	}
	if histogram == nil {
		t.Fatal("orbit.testsvc.http.request.duration not recorded")
	}
	if len(histogram.DataPoints) != 1 {
		t.Fatalf("duration data points = %d, want 1", len(histogram.DataPoints))
	}
	gotBounds := histogram.DataPoints[0].Bounds
	if len(gotBounds) != len(buckets.Default) {
		t.Fatalf("bucket boundaries = %v, want seconds-scale buckets %v (not the OTel SDK's millisecond-tuned default)", gotBounds, buckets.Default)
	}
	for i, b := range buckets.Default {
		if gotBounds[i] != b {
			t.Fatalf("bucket boundaries = %v, want %v", gotBounds, buckets.Default)
		}
	}
}

func TestMetricsMiddleware_DefaultsRouteWhenUnmatched(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := mp.Meter("test")

	mw, err := MetricsMiddleware(meter, "testsvc")
	if err != nil {
		t.Fatalf("MetricsMiddleware() error = %v", err)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	// No RoutePattern middleware applied — simulates a request served outside
	// the Mux (e.g. /health), which must not panic and must still record.
	wrapped := mw.next(handler)

	req := httptest.NewRequest("GET", "/unmatched", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if _, ok := collectCounter(t, rm, "orbit.testsvc.http.requests"); !ok {
		t.Fatal("orbit.testsvc.http.requests not recorded for unmatched route")
	}
}
