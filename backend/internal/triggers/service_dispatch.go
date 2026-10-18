package triggers

import (
	"context"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// ─── Admission-controlled dispatch ───────────────────────────────────────────

// admit routes a matched trigger's fire through admission rather than starting
// it directly, recording the outcome when it is not admitted.
//
// Returns true when the fire was queued and will dispatch. Everything goes
// through the queue, so a fire the platform acknowledged survives a restart.
// admit runs one matched trigger through admission and records the outcome when
// it is not queued. It returns whether the fire was queued, and — separately —
// a non-nil error ONLY when admission failed for an infrastructure reason (a
// database or transaction fault). A genuine ceiling refusal or a paused trigger
// is (false, nil): the fire was handled, just not queued. Keeping the two apart
// is what lets a synchronous caller answer a manual fire with 429 "busy, retry"
// for a real refusal and 500 for an infrastructure fault, rather than telling a
// caller to retry a fire that failed because Postgres was down.
func (s *Service) admit(ctx context.Context, t Trigger, deliveryID string, fields map[string]string) (bool, error) {
	if s.admission == nil {
		return false, nil
	}
	result, err := s.admission.Admit(ctx, t, deliveryID, fields)
	if err != nil {
		s.logger.Error(ctx, "admission failed",
			logging.Field{Key: "trigger_id", Value: t.ID},
			logging.Field{Key: "delivery_id", Value: deliveryID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		// Insert without deriving: the caller derives once it knows whether
		// anything else was admitted for this delivery.
		s.outcomes.insert(ctx, &DeliveryRun{
			TenantID: t.TenantID, DeliveryID: deliveryID,
			TriggerID: t.ID, TriggerName: t.Name,
			Status: RunStatusDropped, Error: new(err.Error()),
		})
		return false, err
	}
	if result == AdmitEnqueued {
		return true, nil
	}
	s.outcomes.insert(ctx, &DeliveryRun{
		TenantID: t.TenantID, DeliveryID: deliveryID,
		TriggerID: t.ID, TriggerName: t.Name,
		Status: result.RunStatus(), Error: new(admissionReason(result)),
	})
	return false, nil
}

// admissionReason explains a non-admitted outcome in the terms an admin needs.
func admissionReason(r AdmissionResult) string {
	switch r {
	case AdmitSuperseded:
		return "replaced by a newer fire before this one ran"
	case AdmitPaused:
		return "the trigger was auto-paused after repeated failures"
	case AdmitDropped:
		return "the trigger is at its concurrency limit"
	default:
		return ""
	}
}

// DispatchPending starts the run for a fire the worker has claimed.
//
// The trigger is re-read rather than carried on the queue row: it may have been
// disabled, retargeted or auto-paused while this fire waited, and acting on
// stale configuration is exactly what an admin turning something off is trying
// to prevent.
func (s *Service) DispatchPending(ctx context.Context, p PendingDelivery) {
	t, err := s.repo.GetTrigger(ctx, p.TenantID, p.TriggerID)
	if err != nil {
		s.logger.Error(ctx, "pending dispatch: trigger no longer readable",
			logging.Field{Key: "trigger_id", Value: p.TriggerID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		// No name to record: the trigger could not be read at all, which is the
		// one case where the id is genuinely all there is.
		s.outcomes.record(ctx, &DeliveryRun{
			TenantID: p.TenantID, DeliveryID: p.DeliveryID, TriggerID: p.TriggerID,
			Status: RunStatusDropped, Error: new("trigger no longer available"),
		})
		return
	}
	if !t.Enabled {
		s.outcomes.record(ctx, &DeliveryRun{
			TenantID: p.TenantID, DeliveryID: p.DeliveryID,
			TriggerID: p.TriggerID, TriggerName: t.Name,
			Status: RunStatusDropped,
			Error:  new("trigger was disabled while this fire was waiting"),
		})
		return
	}

	// The timeout bounds the start request, which returns once orbit-agents has
	// queued the run. A run that later fails to start does not reach this outcome.
	dispatchCtx, cancel := context.WithTimeout(ctx, t.dispatchTimeout())
	defer cancel()

	runID, err := s.startRunForTriggerWithFields(dispatchCtx, *t, p.DeliveryID, p.Fields)
	if err != nil {
		s.logger.Error(ctx, "pending dispatch failed",
			logging.Field{Key: "trigger_id", Value: t.ID},
			logging.Field{Key: "delivery_id", Value: p.DeliveryID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		s.outcomes.insert(ctx, &DeliveryRun{
			TenantID: t.TenantID, DeliveryID: p.DeliveryID,
			TriggerID: t.ID, TriggerName: t.Name,
			Status: runStatusForDispatchError(err), Error: new(err.Error()),
		})
		// Carry one reason up to the delivery so the feed can say why in a
		// single line; the per-trigger detail is the fan-out.
		s.recordFailure(ctx, p.DeliveryID, err.Error())
		s.noteFailure(ctx, *t, err)
		s.outcomes.derive(ctx, p.DeliveryID)
		return
	}

	rID := runID
	s.outcomes.insert(ctx, &DeliveryRun{
		TenantID: t.TenantID, DeliveryID: p.DeliveryID,
		TriggerID: t.ID, TriggerName: t.Name,
		Status: RunStatusDispatched, RunID: &rID,
	})
	_ = s.repo.UpdateDeliveryOutcome(ctx, p.DeliveryID, StatusDispatched, &t.ID, &rID, nil)
	s.noteSuccess(ctx, *t)
	s.outcomes.derive(ctx, p.DeliveryID)

	s.logger.Info(ctx, "trigger started run",
		logging.Field{Key: "trigger_id", Value: t.ID},
		logging.Field{Key: "run_id", Value: runID},
		logging.Field{Key: "delivery_id", Value: p.DeliveryID},
	)
}

// RecordOrphanedPending records a waiting fire the worker collected because its
// trigger can no longer run it.
//
// The row is written for the same reason every other refusal is: work the
// platform accepted and then did not do has to be visible in the delivery
// history. Silently deleting the queue row would leave a delivery marked
// 'verified' forever, which reads as still-in-progress.
func (s *Service) RecordOrphanedPending(ctx context.Context, o OrphanedPending) {
	reason := "the trigger was deleted while this fire was waiting"
	if o.TriggerExists {
		reason = "the trigger was disabled while this fire was waiting"
	}
	s.outcomes.record(ctx, &DeliveryRun{
		TenantID: o.TenantID, DeliveryID: o.DeliveryID,
		TriggerID: o.TriggerID, TriggerName: o.TriggerName,
		Status: RunStatusDropped, Error: &reason,
	})
}

// noteFailure counts a failure toward auto-pause and disables the trigger once
// it has failed enough times in a row.
//
// A policy denial counts as a failure: a trigger whose grant was revoked would
// otherwise be denied forever and never pause, which is precisely the case
// auto-pause exists for. Admission rejections do not reach here — dropping a
// fire at the ceiling is the configuration working, not the trigger breaking.
//
// A platform fault does NOT count. Auto-pause exists to stop a trigger that is
// itself broken, and it costs a human a manual re-enable to undo. When
// orbit-agents is the thing failing, every trigger in the tenant is failing at
// once through no fault of its own: counting that would pause the entire estate
// during a capacity blip and leave someone re-enabling each one by hand.
func (s *Service) noteFailure(ctx context.Context, t Trigger, cause error) {
	if !t.AutopauseEnabled || !countsTowardAutoPause(cause) {
		return
	}
	n, err := s.repo.BumpConsecutiveFailures(ctx, t.ID)
	if err != nil {
		s.logger.Error(ctx, "failed to count trigger failure",
			logging.Field{Key: "trigger_id", Value: t.ID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}
	if n < autoPauseThreshold {
		return
	}
	if err := s.repo.AutoPause(ctx, t.ID); err != nil {
		s.logger.Error(ctx, "failed to auto-pause trigger",
			logging.Field{Key: "trigger_id", Value: t.ID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}
	s.metrics.recordAutoPause(ctx)
	s.logger.Warn(ctx, "auto-paused trigger after consecutive failures",
		logging.Field{Key: "trigger_id", Value: t.ID},
		logging.Field{Key: "consecutive_failures", Value: n},
	)
	// Anything still waiting would fail the same way. Drop it visibly rather
	// than leaving it to drain against a trigger the platform just disabled.
	if s.admission != nil {
		_, _ = s.admission.Drain(ctx, t, "the trigger was auto-paused after repeated failures")
	}
}

// noteSuccess clears the failure counter. Consecutive means consecutive: one
// success is enough to say the trigger is working again.
func (s *Service) noteSuccess(ctx context.Context, t Trigger) {
	if err := s.repo.ResetConsecutiveFailures(ctx, t.ID); err != nil {
		s.logger.Error(ctx, "failed to reset trigger failure counter",
			logging.Field{Key: "trigger_id", Value: t.ID},
			logging.Field{Key: "error", Value: err.Error()},
		)
	}
}

// applyEnabledFields sets the in-memory state a toggle implies, so the write
// that follows carries it. Pure: no collaborators, nothing persisted.
//
// Both directions clear the auto-pause marker. Re-enabling clears it because an
// admin turning a trigger back on has decided the cause is fixed. Disabling
// clears it because an explicit "off" is a person's decision and must not keep
// presenting as the platform's — which is the difference between recording a
// refused fire and recording nothing at all.
func applyEnabledFields(t *Trigger, enabled bool) {
	t.Enabled = enabled
	t.AutoPausedAt = nil
}
