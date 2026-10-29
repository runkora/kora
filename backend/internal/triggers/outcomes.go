package triggers

import (
	"context"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// outcomeStore is the two writes recording an outcome takes.
//
// DeriveDeliveryStatus takes excludePendingID so a caller finishing its own
// pending-queue row can tell the finality check apart from a sibling trigger's
// fire still in flight for the same delivery: see derive's doc.
type outcomeStore interface {
	InsertDeliveryRun(ctx context.Context, r *DeliveryRun) error
	DeriveDeliveryStatus(ctx context.Context, deliveryID, excludePendingID string) (status string, final bool, err error)
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
	repo    outcomeStore
	logger  logging.Logger
	metrics *Metrics // nil records nothing
}

// record writes one outcome and re-derives the delivery. This is the default;
// use insert plus derive only when writing a whole fan-out, where one derive at
// the end is both cheaper and more accurate than one per row.
func (o outcomeRecorder) record(ctx context.Context, dr *DeliveryRun) {
	o.recordSettling(ctx, dr, "")
}

// recordSettling is record for an outcome whose own pending-queue row has not
// yet been released. DispatchPending runs before the worker calls
// ReleasePending (worker.go's drain loop releases only after DispatchPending
// returns), so the claim for the trigger run finishing right now is still a
// live row in trigger_pending_deliveries when this fires. ownPendingID names
// it so the finality check below can tell "my own not-yet-released claim" apart
// from "a sibling trigger's fire still genuinely outstanding" — see derive.
func (o outcomeRecorder) recordSettling(ctx context.Context, dr *DeliveryRun, ownPendingID string) {
	if o.insert(ctx, dr) {
		o.deriveSettling(ctx, dr.DeliveryID, dr.TenantID, ownPendingID)
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
// tenantID is carried by the caller rather than read back from the row: every
// caller already has the delivery's or trigger's tenant in scope, and a second
// read here would only duplicate it.
func (o outcomeRecorder) derive(ctx context.Context, deliveryID, tenantID string) {
	o.deriveSettling(ctx, deliveryID, tenantID, "")
}

// deriveSettling is derive for the same not-yet-released situation
// recordSettling handles: the caller's own pending-queue row, named by
// ownPendingID, is excluded from the finality check because the caller IS that
// row settling, not an outstanding sibling. Every other caller passes "".
//
// A single delivery can legitimately pass through several DISTINCT computed
// statuses as its fan-out resolves over time, and the dispatch paths pre-write
// the delivery's status before deriving, so a status change is not a reliable
// signal that this is the delivery's outcome. The metric is recorded only when
// DeriveDeliveryStatus reports final, which it does once per delivery: the
// first call that finds no trigger match still outstanding. "Outstanding" is
// read off trigger_pending_deliveries — every matched trigger that has not yet
// produced a terminal/dispatched DeliveryRun still owns a row there (waiting
// or claimed), and every one that has does not (ReleasePending deletes it, and
// the DB-side eviction paths — TrimPending, Drain, SupersedeAndEnqueue,
// EnqueueCoalescing's replace — all delete the row in the same statement that
// reports it, before the outcome is // even recorded). ownPendingID is the one exception that check cannot resolve
// on its own: DispatchPending calls this before the worker releases its own
// claim, so without naming it, a delivery would never read as final until
// whichever sibling trigger happens to be released last.
func (o outcomeRecorder) deriveSettling(ctx context.Context, deliveryID, tenantID, ownPendingID string) {
	status, final, err := o.repo.DeriveDeliveryStatus(ctx, deliveryID, ownPendingID)
	if err != nil {
		o.logger.Error(ctx, "failed to derive delivery status",
			logging.Field{Key: "delivery_id", Value: deliveryID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}
	if status != "" && final {
		o.metrics.recordDeliveryOutcome(ctx, status, tenantID)
	}
}
