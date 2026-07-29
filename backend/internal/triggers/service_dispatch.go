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
func (s *Service) admit(ctx context.Context, t Trigger, deliveryID string, fields map[string]string) bool {
	if s.admission == nil {
		return false
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
		return false
	}
	if result == AdmitEnqueued {
		return true
	}
	s.outcomes.insert(ctx, &DeliveryRun{
		TenantID: t.TenantID, DeliveryID: deliveryID,
		TriggerID: t.ID, TriggerName: t.Name,
		Status: result.RunStatus(), Error: new(admissionReason(result)),
	})
	return false
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

	// The timeout bounds starting the run, not running it: a long-running agent
	// is not a failure, but an agent that never accepts the start is.
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
		s.noteFailure(ctx, *t)
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
func (s *Service) noteFailure(ctx context.Context, t Trigger) {
	if !t.AutopauseEnabled {
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
	s.metrics.recordAutoPause(ctx, t.ID)
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

// afterEnabledChange runs the side effects of a toggle, AFTER the new state is
// stored.
//
// Order is the whole point. Draining first and persisting second leaves a window
// where the trigger is still enabled in the database with its backlog already
// gone: a fire admitted in that window is enqueued against a trigger that is
// about to be disabled, and nothing will ever claim it (claiming requires an
// enabled trigger) or reap it (the stale-claim reaper only touches claimed
// rows). Persisting first closes admission before the drain runs. The worker's
// orphan sweep is the backstop for anything that still lands in between.
func (s *Service) afterEnabledChange(ctx context.Context, t Trigger, wasEnabled, enabled bool) {
	if enabled {
		if wasEnabled {
			return
		}
		// Leaving the counter at the threshold would pause the trigger again on
		// its very next failure, which is not what "turn it back on" means.
		if err := s.repo.ResetConsecutiveFailures(ctx, t.ID); err != nil {
			s.logger.Error(ctx, "failed to clear failure count on re-enable",
				logging.Field{Key: "trigger_id", Value: t.ID},
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
		return
	}
	// Anything still waiting would drain into a trigger the admin just turned
	// off. Drop it visibly rather than silently.
	if wasEnabled && s.admission != nil {
		_, _ = s.admission.Drain(ctx, t, "the trigger was disabled while this fire was waiting")
	}
}
