package koraevents

import (
	"context"
	"fmt"
	"time"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// sweepStore is the store surface the retention sweep uses.
type sweepStore interface {
	SweepBatch(ctx context.Context, scope SweepScope, cutoff time.Time) (int64, bool, error)
}

// retentionSource returns each tenant's declared retention in days.
type retentionSource interface {
	EventRetentionDays() map[string]int
	Loaded() bool
	Err() error
}

// Retention deletes events older than their tenant's retention.
type Retention struct {
	store       sweepStore
	tenants     retentionSource
	defaultDays int
	logger      logging.Logger
	now         func() time.Time
}

// NewRetention returns the sweep. defaultDays applies to tenants that declare none.
func NewRetention(store sweepStore, tenants retentionSource, defaultDays int, logger logging.Logger) *Retention {
	return &Retention{store: store, tenants: tenants, defaultDays: defaultDays, logger: logger, now: time.Now}
}

// Run sweeps now and every hour until ctx ends.
func (r *Retention) Run(ctx context.Context) {
	every(ctx, nil, retentionInterval, func() {
		if err := r.Sweep(ctx); err != nil && ctx.Err() == nil {
			r.logger.Error(ctx, "event retention sweep failed", logging.Field{Key: "error", Value: err.Error()})
		}
	})
}

// Sweep deletes every expired event, a batch at a time. It stops when another
// replica holds the retention lock.
func (r *Retention) Sweep(ctx context.Context) error {
	// An unreadable registry would apply the default to tenants that keep longer.
	if err := r.tenants.Err(); err != nil && !r.tenants.Loaded() {
		return fmt.Errorf("tenant registry unreadable, sweep skipped: %w", err)
	}
	overrides := r.tenants.EventRetentionDays()
	except := make([]string, 0, len(overrides))
	for tenant, days := range overrides {
		except = append(except, tenant)
		if done, err := r.sweep(ctx, SweepScope{Tenant: tenant}, days); err != nil || !done {
			return err
		}
	}
	_, err := r.sweep(ctx, SweepScope{Except: except}, r.defaultDays)
	return err
}

// sweep deletes one scope's expired events and reports false when it lost the lock.
func (r *Retention) sweep(ctx context.Context, scope SweepScope, days int) (bool, error) {
	cutoff := r.now().Add(-time.Duration(days) * 24 * time.Hour)
	for {
		deleted, locked, err := r.store.SweepBatch(ctx, scope, cutoff)
		if err != nil || !locked {
			return false, err
		}
		if deleted < sweepBatchSize {
			return true, nil
		}
	}
}
