package inferencegw

import (
	"context"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// DefaultSyncInterval is the default pricing sync frequency.
const DefaultSyncInterval = 24 * time.Hour

// PricingSyncWorker fetches model pricing from a PricingSource on a fixed
// schedule and writes the results to the PricingCatalogRepository. It never
// modifies override columns.
type PricingSyncWorker struct {
	source   PricingSource
	catalog  PricingCatalogRepository
	interval time.Duration
	logger   logging.Logger
}

// NewPricingSyncWorker constructs a worker with the given source and interval.
func NewPricingSyncWorker(source PricingSource, catalog PricingCatalogRepository, interval time.Duration, logger logging.Logger) *PricingSyncWorker {
	return &PricingSyncWorker{
		source:   source,
		catalog:  catalog,
		interval: interval,
		logger:   logger,
	}
}

// Start runs the worker. It performs an immediate sync on startup, then repeats
// on the configured interval. Runs until ctx is canceled.
func (w *PricingSyncWorker) Start(ctx context.Context) {
	w.sync(ctx)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w.sync(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (w *PricingSyncWorker) sync(ctx context.Context) {
	start := time.Now()

	entries, err := w.source.Fetch(ctx)
	if err != nil {
		w.logger.Error(ctx, "pricing.sync.failed",
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}

	if err := w.catalog.UpsertSynced(ctx, entries); err != nil {
		w.logger.Error(ctx, "pricing.sync.failed",
			logging.Field{Key: "error", Value: err.Error()},
			logging.Field{Key: "models_fetched", Value: len(entries)},
		)
		return
	}

	w.logger.Info(ctx, "pricing.sync.completed",
		logging.Field{Key: "models_upserted", Value: len(entries)},
		logging.Field{Key: "duration_ms", Value: time.Since(start).Milliseconds()},
	)
}
