package inferencegw

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type stubPricingSource struct{ err error }

func (s stubPricingSource) Fetch(context.Context) ([]ModelPriceEntry, error) { return nil, s.err }

func TestPricingSyncWorker_SyncRootsATrace(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	tests := []struct {
		name       string
		fetchErr   error
		upsertErr  error
		wantStatus codes.Code
	}{
		{"success", nil, nil, codes.Unset},
		{"fetch fails", errors.New("fetch"), nil, codes.Error},
		{"upsert fails", nil, errors.New("upsert"), codes.Error},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := len(recorder.Ended())
			w := NewPricingSyncWorker(stubPricingSource{err: tt.fetchErr}, &mockPricingCatalogRepository{upsertSyncedErr: tt.upsertErr}, DefaultSyncInterval, logging.NewLoggerWithOutput(io.Discard))
			w.sync(context.Background())

			spans := recorder.Ended()[before:]
			if len(spans) != 1 {
				t.Fatalf("spans = %d, want 1", len(spans))
			}
			if got := spans[0].Status(); got.Code != tt.wantStatus {
				t.Errorf("status = %v, want %v", got.Code, tt.wantStatus)
			}
		})
	}
}
