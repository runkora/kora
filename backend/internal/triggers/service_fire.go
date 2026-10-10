package triggers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// Delivery listing bounds. An out-of-range ask is shaped rather than refused:
// the limit shapes a read, it does not change what exists. Absent or
// non-positive gets the default; above the ceiling clamps DOWN to the ceiling,
// which is the whole point of having one. It used to reset to the default, so
// asking for 501 returned five times fewer rows than a legal request.
const (
	defaultDeliveryPageSize = 100
	maxDeliveryPageSize     = 500
)

// buildManualDelivery is the delivery row a manual fire produces, whether it
// came from a person pressing fire or from a replay of one that already did.
//
// redeliveryOf is nil for an original fire and names the lineage ROOT when this
// is a replay (Redeliver pins every replay to the root, so lineage stays flat):
// that field is the whole difference between the two rows, and the lineage is
// what keeps a replay from rewriting the history of the fire it came from and
// what the replay budget counts against.
func buildManualDelivery(tenantID string, t *Trigger, callerPrincipalID string, captured []byte, redeliveryOf *string) *Delivery {
	triggerID := t.ID
	return &Delivery{
		ID:           uuid.NewString(),
		TenantID:     tenantID,
		TriggerID:    &triggerID,
		TriggerName:  t.Name,
		Kind:         KindManual,
		Status:       StatusVerified,
		FiredBy:      callerPrincipalID,
		Payload:      captured,
		PayloadBytes: len(captured),
		RedeliveryOf: redeliveryOf,
	}
}

// admitOrDerive puts a recorded fire through admission, and settles the
// delivery's own status when admission refuses it.
//
// The derive is the part that must not be forgotten: a refused fire has its
// outcome row written by admission, and without this the delivery it belongs to
// stays 'verified' — reading as still in progress for something that will never
// run.
//
// Reports whether the fire was queued. What to tell the caller about a refusal
// differs by path and stays with the caller.
func (s *Service) admitOrDerive(ctx context.Context, t Trigger, deliveryID string, fields map[string]string) (bool, error) {
	admitted, err := s.admit(ctx, t, deliveryID, fields)
	if admitted {
		return true, nil
	}
	s.outcomes.derive(ctx, deliveryID)
	return false, err
}

// FireTrigger fires a manual trigger on behalf of callerPrincipalID.
// The resulting run executes under the trigger's own SERVICE principal, not
// the caller's identity.
//
// Firing is authorized per trigger by the controller, against
// trigger:<id>:fire, before this method is reached (ADR Decision 6). The run
// still executes under the trigger's own principal, so the caller's own
// permissions neither widen nor narrow what it can do.
func (s *Service) FireTrigger(ctx context.Context, tenantID, id, callerPrincipalID string, req FireTriggerRequest) (*FireTriggerResponse, error) {
	t, err := s.repo.GetTrigger(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if t.Kind != KindManual {
		return nil, fmt.Errorf("%w: only manual triggers can be fired directly", ErrWrongKind)
	}
	// A person's OFF is refused with nothing recorded; a platform pause is
	// carried through to admission. See Trigger.DisabledByPerson for why.
	if t.DisabledByPerson() {
		return nil, ErrTriggerDisabled
	}

	// Validated against the trigger's own input declaration, which the console
	// also renders its form from, so what it shows and what the API accepts
	// cannot drift.
	values := t.ManualConfig.ApplyDefaults(req.Variables)
	problems := t.ManualConfig.ValidateInputs(values)

	fields := varFields(values)

	// The values a person supplied are the manual kind's payload. Storing them is
	// what makes a manual fire answerable after the fact ("what was it run
	// with?") and what makes it replayable at all. A rejected fire is recorded
	// with the values that were rejected, which is the pair that makes it
	// diagnosable.
	captured := s.captureVariables(ctx, values)

	delivery := buildManualDelivery(tenantID, t, callerPrincipalID, captured, nil)
	deliveryID := delivery.ID
	// A rejected fire is still a fire: someone tried to start this trigger and
	// the platform refused. Recorded before validation is judged, so the attempt
	// leaves a row either way — an invalid fire that vanished was indistinguishable
	// from one that was never made, which is the gap that made "why did nothing
	// happen?" unanswerable.
	if problems != nil {
		delivery.Status = StatusFailed
		delivery.Reason = new(inputProblemSummary(problems))
	}
	inserted, err := s.repo.InsertDelivery(ctx, delivery)
	if err != nil {
		return nil, fmt.Errorf("record manual delivery: %w", err)
	}
	if !inserted {
		// Should not happen for manual (no dedup key), but handle defensively.
		return nil, fmt.Errorf("failed to record delivery")
	}
	if problems != nil {
		// Recorded, never admitted: a fire the platform refused must not reach
		// the queue, or it would start the run the refusal was about. The caller
		// gets the per-field detail, which is what they can act on.
		s.logger.Info(ctx, "manual fire rejected: invalid input",
			logging.Field{Key: "trigger_id", Value: t.ID},
			logging.Field{Key: "delivery_id", Value: deliveryID},
			logging.Field{Key: "fired_by", Value: callerPrincipalID},
		)
		return nil, &InputValidationError{Problems: problems}
	}

	// Manual fires go through the queue like every other kind, so an accepted
	// fire survives a restart. The caller gets the delivery id immediately and
	// can follow it; the run id is not known until the worker starts it.
	//
	// Manual triggers default to refusing rather than waiting: a person is
	// standing there, and an immediate "not now" is more use than silent
	// queueing behind work they cannot see.
	admitted, admitErr := s.admitOrDerive(ctx, *t, deliveryID, fields)
	if admitErr != nil {
		// Admission failed for an infrastructure reason. This is a 5xx, not the
		// caller's fault, and must not read as "busy, retry" — retrying a fire
		// that failed because the database was down just fails again.
		return nil, fmt.Errorf("admit manual fire: %w", admitErr)
	}
	if !admitted {
		// A genuine refusal. The error says which, because the fixes differ:
		// wait a moment, versus deal with the failures that paused the trigger.
		if t.AutoPausedAt != nil {
			return nil, ErrTriggerAutoPaused
		}
		return nil, ErrTriggerAtCapacity
	}

	s.logger.Info(ctx, "manual trigger queued for dispatch",
		logging.Field{Key: "trigger_id", Value: t.ID},
		logging.Field{Key: "delivery_id", Value: deliveryID},
		logging.Field{Key: "fired_by", Value: callerPrincipalID},
	)

	return &FireTriggerResponse{DeliveryID: deliveryID}, nil
}

// TickScheduled is called by the Scheduler for each enabled scheduled trigger at
// its cron tick. It creates a delivery record (with dedup) and starts the run.
func (s *Service) TickScheduled(ctx context.Context, tick ScheduledTick) {
	t := tick.Trigger
	scheduledAt := scheduledAtMinute(tick.ScheduledAt)

	deliveryID := uuid.NewString()
	triggerID := t.ID
	delivery := &Delivery{
		ID:          deliveryID,
		TenantID:    t.TenantID,
		TriggerID:   &triggerID,
		TriggerName: t.Name,
		Kind:        KindScheduled,
		Status:      StatusVerified,
		ScheduledAt: &scheduledAt,
	}

	// Record the delivery before dispatching. The unique (trigger_id,
	// scheduled_at) key makes this the scheduler's concurrency control: whoever
	// wins the insert dispatches, and the rest stop. That holds across replicas
	// and across a restart within the same minute (ADR Decision 2).
	inserted, err := s.repo.InsertScheduledDelivery(ctx, delivery)
	if err != nil {
		s.logger.Error(ctx, "failed to record scheduled delivery",
			logging.Field{Key: "trigger_id", Value: t.ID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}
	if !inserted {
		// Normal whenever more than one replica is running: another one claimed
		// this tick. Debug, not info — it is not an incident.
		s.logger.Debug(ctx, "scheduled tick already claimed by another replica",
			logging.Field{Key: "trigger_id", Value: t.ID},
			logging.Field{Key: "scheduled_at", Value: scheduledAt},
		)
		return
	}

	// Inject scheduler context as var fields (e.g. {{ var.date }}).
	fields := map[string]string{
		"var.date":       scheduledAt.Format("2006-01-02"),
		"var.datetime":   scheduledAt.Format(time.RFC3339),
		"var.trigger_id": t.ID,
		"var.tenant_id":  t.TenantID,
	}

	// Scheduled triggers default to replacing what is waiting: if the previous
	// tick has not run yet, the newest one is the state worth acting on.
	if admitted, _ := s.admitOrDerive(ctx, t, deliveryID, fields); !admitted {
		return
	}

	s.logger.Info(ctx, "scheduled trigger queued for dispatch",
		logging.Field{Key: "trigger_id", Value: t.ID},
		logging.Field{Key: "scheduled_at", Value: scheduledAt},
	)
}

// ListDeliveries returns one page of a tenant's deliveries, newest first, each
// with its per-trigger run fan-out and coalescing keys attached (one batched
// query each, no N+1). nextCursor is empty on the last page.
func (s *Service) ListDeliveries(ctx context.Context, tenantID string, filter DeliveryFilter) ([]Delivery, string, error) {
	// Page-size bounds are business policy, not storage. An over-ceiling limit
	// clamps DOWN to the ceiling rather than collapsing to the default, so asking
	// for more than the maximum returns the maximum.
	switch {
	case filter.Limit <= 0:
		filter.Limit = defaultDeliveryPageSize
	case filter.Limit > maxDeliveryPageSize:
		filter.Limit = maxDeliveryPageSize
	}
	if err := validateDeliveryFilter(filter); err != nil {
		return nil, "", err
	}
	deliveries, hasNext, err := s.repo.ListDeliveries(ctx, tenantID, filter)
	if err != nil {
		return nil, "", err
	}
	if len(deliveries) == 0 {
		return deliveries, "", nil
	}
	ids := make([]string, len(deliveries))
	for i := range deliveries {
		ids[i] = deliveries[i].ID
	}
	runs, err := s.repo.ListDeliveryRunsForDeliveries(ctx, tenantID, ids)
	if err != nil {
		return nil, "", fmt.Errorf("attach delivery runs: %w", err)
	}
	keys, err := s.repo.ListDeliveryKeysForDeliveries(ctx, tenantID, ids)
	if err != nil {
		return nil, "", fmt.Errorf("attach delivery keys: %w", err)
	}
	for i := range deliveries {
		deliveries[i].Runs = runs[deliveries[i].ID]
		deliveries[i].CoalesceKeys = keys[deliveries[i].ID]
	}
	var nextCursor string
	if hasNext {
		last := deliveries[len(deliveries)-1]
		nextCursor = deliveryCursor{ReceivedAt: last.ReceivedAt, ID: last.ID}.encode()
	}
	return deliveries, nextCursor, nil
}

// validateDeliveryFilter rejects a status or outcome the pipeline can never
// write. Ignoring an unknown value would silently return an unfiltered page,
// which reads as a bug rather than as the typo it is.
func validateDeliveryFilter(filter DeliveryFilter) error {
	for _, v := range filter.Status {
		if !deliveryStatusSet[v] {
			return fmt.Errorf("%w: unknown delivery status %q", ErrValidation, v)
		}
	}
	for _, v := range filter.Outcome {
		if !runStatusSet[v] {
			return fmt.Errorf("%w: unknown delivery outcome %q", ErrValidation, v)
		}
	}
	return nil
}

// Redeliver replays a delivery: it records a NEW delivery naming the original
// and routes it through the normal fan-out.
//
// A replay is never an edit of the original. The original's outcome is the
// record of what happened the first time, and an operator re-running an event
// after fixing its cause must not overwrite the evidence of the failure they
// fixed.
//
// The response is 202 whenever the new delivery was recorded, including when
// admission then refuses it. The recording is the guarantee; whether a trigger
// ran is the new delivery's own fan-out, exactly as for a fresh arrival.
func (s *Service) Redeliver(ctx context.Context, tenantID, deliveryID, callerPrincipalID string) (*RedeliverResponse, error) {
	original, err := s.repo.GetDelivery(ctx, tenantID, deliveryID)
	if err != nil {
		return nil, err
	}
	// Every replay pins its lineage to the root, not to the delivery being
	// replayed, so the budget counts replays of the one original however deep
	// the caller reaches. Without this, replaying a replay starts a fresh chain
	// whose child count is zero, and the endpoint becomes the unbounded run
	// generator the budget exists to prevent. Lineage is kept flat (depth 1) by
	// this pinning, so the root is the delivery's own id, or its parent when it
	// is itself a replay.
	rootID := original.ID
	if original.RedeliveryOf != nil {
		rootID = *original.RedeliveryOf
	}
	if err := s.checkRedeliveryBudget(ctx, rootID); err != nil {
		return nil, err
	}

	switch original.Kind {
	case KindWebhook:
		return s.redeliverWebhook(ctx, tenantID, original, rootID, callerPrincipalID)
	case KindManual:
		return s.redeliverManual(ctx, tenantID, original, rootID, callerPrincipalID)
	default:
		// A scheduled tick is defined by the minute it was due. Replaying one
		// would either collide with that minute's row or invent a fire that was
		// never due, and neither is a thing an operator can reason about. Fire
		// the schedule's trigger manually if the work needs redoing.
		return nil, fmt.Errorf("%w: a scheduled tick cannot be redelivered; it is defined by the minute it was due", ErrNotRedeliverable)
	}
}

// redeliverWebhook replays an inbound webhook against the source's triggers as
// they are configured NOW, not as they were when the original arrived.
//
// That is the point of the endpoint: the usual reason to replay is that the
// cause was fixed, and replaying against the old configuration would replay the
// failure with it.
func (s *Service) redeliverWebhook(ctx context.Context, tenantID string, original *Delivery, rootID, callerPrincipalID string) (*RedeliverResponse, error) {
	if original.PayloadPurged || len(original.Payload) == 0 {
		return nil, fmt.Errorf("%w: this delivery's payload is no longer stored, so there is nothing to replay", ErrNotRedeliverable)
	}
	src, err := s.repo.GetSource(ctx, tenantID, original.SourceID)
	if err != nil {
		if errors.Is(err, ErrSourceNotFound) {
			return nil, fmt.Errorf("%w: the source this delivery arrived on no longer exists", ErrNotRedeliverable)
		}
		return nil, err
	}
	if !src.Enabled {
		return nil, fmt.Errorf("%w: the source this delivery arrived on is disabled", ErrNotRedeliverable)
	}
	// A payload that arrived before the current signing secret was issued is
	// not replayable. Replay is the one path that re-runs a stored body without
	// re-verifying it, and rotating a secret is what an operator does when the
	// old one may be in someone else's hands — so replaying a payload from
	// before the rotation would re-run, under the platform's own authority,
	// something the rotation was meant to render unusable.
	if src.SecretRotatedAt != nil && original.ReceivedAt.Before(*src.SecretRotatedAt) {
		return nil, fmt.Errorf("%w: this payload arrived before the source's current signing secret was issued", ErrNotRedeliverable)
	}
	adapter, ok := s.registry.Get(src.Provider)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownProvider, src.Provider)
	}

	// No signature check. The body was verified when it first arrived and has
	// been stored under the platform's control since; re-verifying would fail
	// for any source whose secret has been rotated, which is exactly the kind of
	// fix that precedes a replay.
	dup := &Delivery{
		ID:                 uuid.NewString(),
		TenantID:           tenantID,
		SourceID:           src.ID,
		Kind:               KindWebhook,
		ProviderDeliveryID: original.ProviderDeliveryID,
		EventType:          original.EventType,
		Status:             StatusVerified,
		PayloadExcerpt:     original.PayloadExcerpt,
		Payload:            original.Payload,
		PayloadBytes:       original.PayloadBytes,
		FiredBy:            callerPrincipalID,
		RedeliveryOf:       &rootID,
	}
	if _, err := s.repo.InsertDelivery(ctx, dup); err != nil {
		return nil, fmt.Errorf("record redelivery: %w", err)
	}

	s.logger.Info(ctx, "webhook delivery replayed",
		logging.Field{Key: "delivery_id", Value: dup.ID},
		logging.Field{Key: "redelivery_of", Value: rootID},
		logging.Field{Key: "requested_by", Value: callerPrincipalID},
	)

	// Inline, before the replay is acknowledged: the same durability rule as
	// ingest. A crash before the rows exist means no 2xx and the caller can
	// retry, rather than a replay the platform reported accepted and then lost.
	s.dispatch(context.WithoutCancel(ctx), *src, adapter, arrival{
		deliveryID: dup.ID, providerDeliveryID: dup.ProviderDeliveryID, eventType: dup.EventType,
		body: dup.Payload, truncated: dup.PayloadBytes > maxPayloadBytes,
	})

	return &RedeliverResponse{DeliveryID: dup.ID, RedeliveryOf: rootID}, nil
}

// redeliverManual replays a manual fire with the values it was given, validated
// against the trigger's CURRENT input declaration.
//
// Revalidating is deliberate: a declaration that changed since the original fire
// is the current contract, and quietly starting a run with values it no longer
// accepts would be the same drift the declaration exists to prevent.
func (s *Service) redeliverManual(ctx context.Context, tenantID string, original *Delivery, rootID, callerPrincipalID string) (*RedeliverResponse, error) {
	if original.TriggerID == nil {
		return nil, fmt.Errorf("%w: this delivery records no trigger to re-run", ErrNotRedeliverable)
	}
	t, err := s.repo.GetTrigger(ctx, tenantID, *original.TriggerID)
	if err != nil {
		if errors.Is(err, ErrTriggerNotFound) {
			return nil, fmt.Errorf("%w: the trigger this delivery fired no longer exists", ErrNotRedeliverable)
		}
		return nil, err
	}
	// Auto-paused is allowed through: replaying after fixing the cause is the
	// common case, and admission records whatever it then decides. A person's
	// OFF is not worked around with a replay (Trigger.DisabledByPerson).
	if t.DisabledByPerson() {
		return nil, fmt.Errorf("%w: the trigger this delivery fired is disabled", ErrNotRedeliverable)
	}

	values := t.ManualConfig.ApplyDefaults(storedVariables(ctx, s.logger, original.Payload))
	if problems := t.ManualConfig.ValidateInputs(values); problems != nil {
		return nil, &InputValidationError{Problems: problems}
	}

	captured := s.captureVariables(ctx, values)
	dup := buildManualDelivery(tenantID, t, callerPrincipalID, captured, &rootID)
	if _, err := s.repo.InsertDelivery(ctx, dup); err != nil {
		return nil, fmt.Errorf("record redelivery: %w", err)
	}

	// Unlike FireTrigger, a refusal here is not returned as an error: the replay
	// was accepted and recorded, and what admission then decided is in the
	// delivery history the caller is already looking at.
	_, _ = s.admitOrDerive(ctx, *t, dup.ID, varFields(values))

	s.logger.Info(ctx, "manual fire replayed",
		logging.Field{Key: "trigger_id", Value: t.ID},
		logging.Field{Key: "delivery_id", Value: dup.ID},
		logging.Field{Key: "redelivery_of", Value: rootID},
		logging.Field{Key: "requested_by", Value: callerPrincipalID},
	)

	return &RedeliverResponse{DeliveryID: dup.ID, RedeliveryOf: rootID}, nil
}

// maxRedeliveriesPerOriginal bounds how many times one delivery may be replayed.
//
// Replaying starts real work under the trigger's own authority, from a body the
// platform is holding and does not re-verify. Ten is generous for the reason the
// endpoint exists (re-run after fixing the cause) and small enough that the
// endpoint cannot be turned into an unbounded run generator by anyone who can
// reach it. The bound is per original, so it is not a rate limit an operator has
// to wait out; it is a limit on how many times one event can happen.
const maxRedeliveriesPerOriginal = 10

// checkRedeliveryBudget refuses a replay once an original has been replayed
// enough times.
func (s *Service) checkRedeliveryBudget(ctx context.Context, originalID string) error {
	n, err := s.repo.CountRedeliveries(ctx, originalID)
	if err != nil {
		return fmt.Errorf("count existing redeliveries: %w", err)
	}
	if n >= maxRedeliveriesPerOriginal {
		return fmt.Errorf("%w: this delivery has already been replayed %d times, which is the limit",
			ErrNotRedeliverable, maxRedeliveriesPerOriginal)
	}
	return nil
}

// varFields maps a manual trigger's input values into the {{ var.key }}
// namespace the prompt template renders against.
func varFields(values map[string]string) map[string]string {
	fields := make(map[string]string, len(values))
	for k, v := range values {
		fields["var."+k] = v
	}
	return fields
}

// captureVariables serializes a manual fire's inputs for storage, bounded by the
// same maxPayloadBytes ceiling every other stored payload obeys.
//
// The bound matters because a fire is recorded BEFORE its inputs are judged: a
// rejected fire is stored with the values that were rejected, which is the pair
// that makes it diagnosable, and which without a ceiling lets a caller write a
// row of their choosing on a request the platform is refusing. An oversized
// capture is dropped rather than truncated: truncated JSON is not readable back,
// so storing it would only trade a large row for an unparseable one.
//
// Best-effort otherwise: a fire must not be refused because its own audit record
// could not be encoded, and every value is a string, so there is no realistic
// encoding failure here beyond a bug worth logging.
func (s *Service) captureVariables(ctx context.Context, values map[string]string) []byte {
	if len(values) == 0 {
		return nil
	}
	b, err := json.Marshal(values)
	if err != nil {
		s.logger.Error(ctx, "failed to capture manual fire inputs",
			logging.Field{Key: "error", Value: err.Error()})
		return nil
	}
	if len(b) > maxPayloadBytes {
		s.logger.Warn(ctx, "manual fire inputs exceed the payload ceiling; recording the fire without them",
			logging.Field{Key: "bytes", Value: len(b)},
			logging.Field{Key: "limit", Value: maxPayloadBytes})
		return nil
	}
	return b
}

// storedVariables reads back what captureVariables wrote. A delivery recorded
// before inputs were stored simply replays with none, which the declaration's
// defaults and validation then judge on their current terms.
func storedVariables(ctx context.Context, logger logging.Logger, payload []byte) map[string]string {
	if len(payload) == 0 {
		return nil
	}
	var values map[string]string
	if err := json.Unmarshal(payload, &values); err != nil {
		logger.Warn(ctx, "stored manual fire inputs could not be read; replaying with none",
			logging.Field{Key: "error", Value: err.Error()})
		return nil
	}
	return values
}
