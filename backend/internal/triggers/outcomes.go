package triggers

import (
	"context"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// outcomeStore is the two writes recording an outcome takes.
type outcomeStore interface {
	InsertDeliveryRun(ctx context.Context, r *DeliveryRun) error
	DeriveDeliveryStatus(ctx context.Context, deliveryID string) error
}

// outcomeRecorder writes the per-trigger outcome rows that make up a delivery's
// fan-out, and keeps the delivery's own status derived from them.
//
// It exists so that "record the outcome, then re-derive the delivery" is written
// once. That pairing is the audit spine (docs/adr/2026-06-20-extended-trigger-types.md,
// Decision 3): a delivery's status is never set directly, it is always a
// function of the outcomes recorded against it, so an outcome written without a
// derive leaves a delivery reading as still in progress forever. The service and
// admission both record outcomes, and one type keeps that invariant for both.
//
// Failures are logged rather than returned throughout: the outcome row is a
// record of something that already happened, and there is no caller who could
// act on the error.
type outcomeRecorder struct {
	repo   outcomeStore
	logger logging.Logger
}

// record writes one outcome and re-derives the delivery. This is the default;
// use insert plus derive only when writing a whole fan-out, where one derive at
// the end is both cheaper and more accurate than one per row.
func (o outcomeRecorder) record(ctx context.Context, dr *DeliveryRun) {
	if o.insert(ctx, dr) {
		o.derive(ctx, dr.DeliveryID)
	}
}

// insert writes one per-trigger outcome, reporting whether it landed. The caller
// owes the delivery a derive.
//
// TriggerName is captured onto the row rather than joined at read time: a
// delivery outlives the trigger it names, and the console showing a bare id for
// a deleted trigger is exactly the gap US-004 closes. It is empty only where the
// name is genuinely unknown — an outcome recorded because the trigger could not
// be read at all.
func (o outcomeRecorder) insert(ctx context.Context, dr *DeliveryRun) bool {
	if dr.ID == "" {
		dr.ID = newID()
	}
	if err := o.repo.InsertDeliveryRun(ctx, dr); err != nil {
		o.logger.Error(ctx, "failed to record delivery run",
			logging.Field{Key: "delivery_id", Value: dr.DeliveryID},
			logging.Field{Key: "trigger_id", Value: dr.TriggerID},
			logging.Field{Key: "status", Value: dr.Status},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return false
	}
	return true
}

// derive recomputes a delivery's summary status from its per-trigger outcomes.
func (o outcomeRecorder) derive(ctx context.Context, deliveryID string) {
	if err := o.repo.DeriveDeliveryStatus(ctx, deliveryID); err != nil {
		o.logger.Error(ctx, "failed to derive delivery status",
			logging.Field{Key: "delivery_id", Value: deliveryID},
			logging.Field{Key: "error", Value: err.Error()},
		)
	}
}
