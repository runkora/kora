package triggers

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	agentsclient "github.com/clear-route/orbit/internal/platform/clients/agents"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// ─── Ingest (webhook) ─────────────────────────────────────────────────────────

// Ingest verifies an inbound delivery, records it, and queues asynchronous
// dispatch. It returns IngestAccepted for a new, verified delivery and
// IngestDuplicate for a redelivery. Signature/visibility failures are returned as
// sentinel errors and produce no delivery row (so unauthenticated callers cannot
// flood the audit log).
func (s *Service) Ingest(ctx context.Context, sourceID string, headers http.Header, body []byte) (IngestOutcome, error) {
	src, err := s.repo.GetSourceByID(ctx, sourceID)
	if err != nil {
		return 0, err
	}
	if !src.Enabled {
		s.recordSourceRejection(ctx, src.ID, "source is disabled")
		return 0, ErrSourceDisabled
	}
	adapter, ok := s.registry.Get(src.Provider)
	if !ok {
		return 0, fmt.Errorf("%w: %q", ErrUnknownProvider, src.Provider)
	}

	secret, err := s.cipher.decrypt(src.SecretCiphertext)
	if err != nil {
		return 0, fmt.Errorf("decrypt signing secret: %w", err)
	}

	if verifyErr := adapter.Verify(secret, headers, body); verifyErr != nil {
		// The most common real cause is a mismatched signing secret, and until
		// now it presented as silence. Counted on the source rather than
		// recorded as a delivery: this request is unauthenticated, so a row per
		// request would be an attacker-controlled write (ADR Decision 4).
		s.recordSourceRejection(ctx, src.ID, "invalid signature")
		return 0, ErrSignatureInvalid
	}

	deliveryID := adapter.DeliveryID(headers, body)
	if deliveryID == "" {
		// Replay protection keys entirely on the provider delivery id via the
		// partial unique index on trigger_deliveries. The id is not part of the
		// authenticated payload for every provider (GitHub carries it in an
		// unsigned header; Alertmanager derives it from body content), so a
		// captured-and-replayed delivery whose id was stripped or unparseable
		// would otherwise re-dispatch unbounded. Reject fail-closed.
		s.recordSourceRejection(ctx, src.ID, "missing provider delivery id")
		return 0, ErrMissingDeliveryID
	}
	// Replay defense that does not depend on the delivery id being honest. The id
	// is not part of what a provider signs (GitHub puts it in a plain header), so
	// a captured body with a rewritten id verifies perfectly and dedups against
	// nothing — the same event starts a run as many times as it is re-sent. The
	// body is the one thing whoever holds a captured delivery cannot change
	// without invalidating the signature, so the window keys on that, and on the
	// id the body first arrived under so an ordinary provider retry still reads
	// as the duplicate it is.
	//
	// Failing OPEN on a storage error is deliberate: this is a second line behind
	// signature verification, and losing every genuine delivery because one dedup
	// table is unavailable is the larger outage.
	if fresh, replayErr := s.repo.ClaimReplayKey(ctx, src.ID, bodyFingerprint(body), deliveryID, replayWindow); replayErr != nil {
		s.logger.Error(ctx, "replay check failed; accepting the delivery",
			logging.Field{Key: "source_id", Value: src.ID},
			logging.Field{Key: "error", Value: replayErr.Error()},
		)
	} else if !fresh {
		s.recordSourceRejection(ctx, src.ID, "payload already delivered under a different delivery id")
		return 0, ErrReplayedPayload
	}

	eventType := adapter.EventType(headers, body)

	delivery := &Delivery{
		ID:                 uuid.NewString(),
		TenantID:           src.TenantID,
		SourceID:           src.ID,
		Kind:               KindWebhook,
		ProviderDeliveryID: deliveryID,
		EventType:          eventType,
		Status:             StatusVerified,
		PayloadExcerpt:     truncate(body, payloadExcerptBytes),
		Payload:            capturePayload(body),
		PayloadBytes:       len(body),
	}
	inserted, err := s.repo.InsertDelivery(ctx, delivery)
	if err != nil {
		return 0, err
	}
	if !inserted {
		// The sender retried something already handled. The retry is recorded
		// rather than dropped: "GitHub sent this five times" is a question the
		// delivery history has to be able to answer, and until now the repeats
		// left nothing behind at all.
		s.recordDuplicate(ctx, *src, adapter, delivery, body)
		return IngestDuplicate, nil
	}

	s.recordAccepted(ctx, src.ID)

	go s.dispatch(context.WithoutCancel(ctx), *src, adapter, delivery.ID, eventType, body)

	return IngestAccepted, nil
}

// replayWindow is how long a source remembers the bodies it has handled.
//
// Twenty-four hours covers the realistic replay: a delivery captured off the
// wire or out of a log is used quickly, and a window measured in days would keep
// a fingerprint per delivery for a cost that buys very little more. It is a
// window and not a permanent ledger for exactly that reason.
const replayWindow = 24 * time.Hour

// bodyFingerprint identifies a delivery body.
//
// A hash, not the body: this is stored per arrival, and keeping a second copy of
// every payload here would double the retention footprint the retention window
// exists to bound.
func bodyFingerprint(body []byte) []byte {
	sum := sha256.Sum256(body)
	return sum[:]
}

// recordAccepted counts an inbound request that got through, best-effort on the
// same reasoning as recordSourceRejection: the delivery is already accepted, and
// a counter that failed to increment must not change that.
func (s *Service) recordAccepted(ctx context.Context, sourceID string) {
	if err := s.repo.RecordAccepted(ctx, sourceID); err != nil {
		s.logger.Error(ctx, "failed to count accepted delivery",
			logging.Field{Key: "source_id", Value: sourceID},
			logging.Field{Key: "error", Value: err.Error()},
		)
	}
}

// recordDuplicate writes the row for a repeat arrival and, for the triggers that
// asked not to dedupe, runs it as if it were fresh.
//
// The repeat becomes its own delivery pointing at the original rather than an
// edit of it: the original's outcome is history and must not be rewritten by a
// retry that arrived days later. No payload is captured — the body is by
// definition the one already stored on the original, and storing it again would
// multiply retention cost by however many times a provider retries.
func (s *Service) recordDuplicate(ctx context.Context, src WebhookSource, adapter SourceAdapter, attempted *Delivery, body []byte) {
	original, err := s.repo.GetDeliveryByProviderID(ctx, src.ID, attempted.ProviderDeliveryID)
	if err != nil {
		// The insert lost to the dedup index, so an original exists; failing to
		// read it is an infrastructure problem, not a duplicate that isn't one.
		// The sender is still acknowledged: it did its part.
		s.logger.Error(ctx, "duplicate delivery: could not resolve the original",
			logging.Field{Key: "source_id", Value: src.ID},
			logging.Field{Key: "provider_delivery_id", Value: attempted.ProviderDeliveryID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}

	reason := "the sender redelivered an event this source has already handled"
	dup := &Delivery{
		ID:                 uuid.NewString(),
		TenantID:           src.TenantID,
		SourceID:           src.ID,
		Kind:               KindWebhook,
		ProviderDeliveryID: attempted.ProviderDeliveryID,
		EventType:          attempted.EventType,
		Status:             StatusDuplicate,
		Reason:             &reason,
		RedeliveryOf:       &original.ID,
	}
	if _, err := s.repo.InsertDelivery(ctx, dup); err != nil {
		s.logger.Error(ctx, "failed to record duplicate delivery",
			logging.Field{Key: "source_id", Value: src.ID},
			logging.Field{Key: "provider_delivery_id", Value: attempted.ProviderDeliveryID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}

	s.logger.Info(ctx, "duplicate webhook delivery recorded",
		logging.Field{Key: "source_id", Value: src.ID},
		logging.Field{Key: "delivery_id", Value: dup.ID},
		logging.Field{Key: "redelivery_of", Value: original.ID},
	)

	// Whether a repeat is suppressed or re-run is a per-trigger decision, and
	// dedupe_enabled is where a tenant makes it. The unique index stays as the
	// storage-level guarantee (one original per provider delivery id); it cannot
	// express a per-trigger choice, and this is the level that can.
	go s.dispatchDuplicate(context.WithoutCancel(ctx), src, adapter, dup.ID, dup.EventType, body)
}

// dispatch matches the delivery against the source's triggers and starts a run
// for each match under the source's mapped identity, recording the outcome.
func (s *Service) dispatch(ctx context.Context, src WebhookSource, adapter SourceAdapter, deliveryID, eventType string, body []byte) {
	s.fanOut(ctx, src, adapter, deliveryID, eventType, body, nil)
}

// dispatchDuplicate fans a repeat arrival out to the triggers that turned dedupe
// off, and only those.
//
// A trigger with dedupe on records nothing here: the duplicate delivery row is
// the acknowledgement, and adding a per-trigger row saying "did nothing" to
// every trigger on a noisy source is noise, not audit.
func (s *Service) dispatchDuplicate(ctx context.Context, src WebhookSource, adapter SourceAdapter, deliveryID, eventType string, body []byte) {
	s.fanOut(ctx, src, adapter, deliveryID, eventType, body, func(t Trigger) bool { return !t.DedupeEnabled })
}

// fanOut matches a delivery against the source's triggers and admits each match.
//
// considers narrows which triggers this delivery is even offered to (nil means
// all of them). It is applied before matching so that a trigger excluded here
// records nothing at all, rather than a 'filtered' row it never had a say in.
func (s *Service) fanOut(
	ctx context.Context,
	src WebhookSource,
	adapter SourceAdapter,
	deliveryID, eventType string,
	body []byte,
	considers func(Trigger) bool,
) {
	ctx, cancel := context.WithTimeout(ctx, dispatchTimeout)
	defer cancel()

	fields := adapter.Fields(eventType, body)

	triggers, err := s.repo.ListMatchableTriggersForSource(ctx, src.ID)
	if err != nil {
		s.recordFailure(ctx, deliveryID, fmt.Sprintf("list triggers: %v", err))
		return
	}

	// Two reasons a listening trigger does not fire, and an admin needs to tell
	// them apart because the fixes differ:
	//
	//   filtered  — this trigger wanted this event type, but its own
	//               match_condition excluded this particular event. Recorded
	//               per trigger: the fix is a filter setting.
	//   unmatched — no trigger wanted this event type at all. Not recorded per
	//               trigger (there is nobody to attribute it to) and not a
	//               filter decision, so it does not multiply rows on a noisy
	//               source. The fix is a missing trigger.
	var matched []Trigger
	for _, t := range triggers {
		if considers != nil && !considers(t) {
			continue
		}
		// An empty event_type subscribes to every event the source emits; a
		// non-empty one is an exact match. The console renders '' as "any
		// event", and this is the line that makes that label true.
		if t.EventType != "" && t.EventType != eventType {
			continue
		}
		if !matchesCondition(t.MatchCondition, fields) {
			// Insert without deriving: the summary is computed once, below,
			// after the whole fan-out is written.
			s.outcomes.insert(ctx, &DeliveryRun{
				TenantID: src.TenantID, DeliveryID: deliveryID,
				TriggerID: t.ID, TriggerName: t.Name,
				Status: RunStatusFiltered,
				Error:  new("event excluded by the trigger's match condition"),
			})
			continue
		}
		matched = append(matched, t)
	}
	if len(matched) == 0 {
		// May still have filtered rows; the derived summary accounts for them.
		s.outcomes.derive(ctx, deliveryID)
		return
	}

	// Record an outcome for every matched trigger, then derive the delivery's
	// summary once. The summary is never written inside this loop: doing so
	// takes the first success as the whole answer, which cannot express
	// "started two runs, denied by a third" (ADR Decision 3).
	// Each match goes through admission rather than starting here. The run
	// begins when the worker claims it, so a fire the platform acknowledged
	// survives a restart instead of vanishing between the two.
	var queued int
	for _, t := range matched {
		if admitted, _ := s.admit(ctx, t, deliveryID, fields); admitted {
			queued++
		}
	}
	if queued == 0 {
		// Nothing will start, so the outcome is already final.
		s.outcomes.derive(ctx, deliveryID)
	}
}

// runStatusForDispatchError maps a dispatch failure to the per-trigger outcome
// that names its cause. A policy denial is not a failure — the trigger is
// working, its grant is not — and an admin fixing "denied" reaches for the grant
// rather than the agent.
func runStatusForDispatchError(err error) string {
	if errors.Is(err, agentsclient.ErrForbidden) {
		return RunStatusDenied
	}
	return RunStatusFailed
}

// countsTowardAutoPause reports whether a dispatch failure says something about
// this trigger, as opposed to something about the platform underneath it.
//
// Auto-pause is a statement that a trigger is broken, and undoing it costs a
// human a manual re-enable, so only a failure intrinsic to the trigger counts:
// a genuine authorization denial (a revoked grant, past the grant-propagation
// retry), or the agent type it binds having been deleted. Retrying either is
// futile and the failure is the trigger's own, so pausing stops the noise.
//
// Everything else is a platform condition that pausing cannot fix, and is
// matched by exclusion rather than by name: orbit-agents unreachable (a raw
// transport error) or erroring (5xx), a dispatch that timed out, shutdown
// canceling the call, or a token the identity service could not mint. Each of
// those fails every trigger in the tenant at once, so counting them would pause
// the whole estate during a blip and leave someone re-enabling each one by
// hand. An allowlist rather than a blocklist because the platform faults have
// no common sentinel (a connection refused is an arbitrary net error), and the
// harm worth avoiding is the wrongful mass-pause: an unrecognized error stays
// visible as a failed delivery without moving the trigger toward disabled.
func countsTowardAutoPause(err error) bool {
	return errors.Is(err, agentsclient.ErrForbidden) ||
		errors.Is(err, agentsclient.ErrAgentTypeNotFound)
}

// runPrincipal resolves whose identity a trigger's run executes under.
//
// Webhook triggers inherit their source's principal: a source owns the grant,
// and every trigger hanging off it acts as that source. Scheduled and manual
// triggers own a principal each, because there is no source to inherit from.
//
// Getting this wrong is not a small bug — it would start a run under the wrong
// identity, which is the one thing the trigger model exists to make verifiable.
func (s *Service) runPrincipal(ctx context.Context, t Trigger) (principalID, displayName string, err error) {
	if t.Kind != KindWebhook {
		if t.PrincipalID == "" {
			return "", "", fmt.Errorf("trigger %s has no principal", t.ID)
		}
		return t.PrincipalID, "trigger:" + t.ID, nil
	}
	if t.SourceID == "" {
		return "", "", fmt.Errorf("webhook trigger %s has no source to inherit identity from", t.ID)
	}
	src, err := s.repo.GetSourceByID(ctx, t.SourceID)
	if err != nil {
		return "", "", fmt.Errorf("load source for trigger identity: %w", err)
	}
	return src.PrincipalID, "trigger-source:" + src.Name, nil
}

// startRunForTriggerWithFields renders the prompt and starts a run under the
// trigger's resolved identity, telling orbit-agents which trigger and delivery
// the run came from.
//
// The backlink is set here, at the only place a run is started from a trigger.
// Without it the run's created_by is the trigger's SERVICE principal and nothing
// more, so a person opening the run cannot tell what caused it — the forward
// link (delivery → run) existed, and the reverse did not.
func (s *Service) startRunForTriggerWithFields(ctx context.Context, t Trigger, deliveryID string, fields map[string]string) (string, error) {
	principalID, displayName, err := s.runPrincipal(ctx, t)
	if err != nil {
		return "", err
	}
	prompt := renderTemplate(t.PromptTemplate, fields)
	tok, err := s.minter.MintServiceToken(ctx, t.TenantID, principalID, displayName, sourceTokenTTL)
	if err != nil {
		return "", fmt.Errorf("mint trigger token: %w", err)
	}
	req := agentsclient.StartRunRequest{
		AgentTypeID:    t.AgentTypeID,
		InitialMessage: prompt,
		// The name travels with the ids: orbit-agents does not read the triggers
		// database, and a run that outlives its trigger still has to render as
		// something a person recognizes.
		TriggerID:   t.ID,
		TriggerName: t.Name,
		DeliveryID:  deliveryID,
	}

	// A trigger's run grant is minted when the trigger is created, and is not
	// visible to orbit-agents until its next OPA bundle poll. A trigger fired
	// within that window is refused for a reason that is nothing to do with the
	// fire: the grant exists, it is merely young.
	//
	// Retry only while that explanation is available. Once the trigger is older
	// than the propagation window, a refusal is a real denial (a revoked grant,
	// a delegated agent type) and retrying would delay an honest failure and
	// hold a queue slot for the whole budget.
	young := time.Since(t.CreatedAt) < grantPropagationWindow

	var run agentsclient.StartRunResponse
	err = retryWhileGrantPropagates(ctx,
		func(err error) bool { return young && errors.Is(err, agentsclient.ErrForbidden) },
		func(backoff time.Duration) {
			s.logger.Debug(ctx, "run start refused, retrying while the grant propagates",
				logging.Field{Key: "trigger_id", Value: t.ID},
				logging.Field{Key: "backoff", Value: backoff.String()},
			)
		},
		func() error {
			var startErr error
			run, startErr = s.agents.StartRun(ctx, tok, req)
			return startErr
		})
	if err != nil {
		return "", fmt.Errorf("start run: %w", err)
	}
	return run.ID, nil
}

// retryWhileGrantPropagates runs call, retrying with exponential backoff while
// retryable classifies its error as grant-propagation lag and the budget
// allows. Both trigger creation (resolving the agent type under a just-minted
// grant) and dispatch (starting a run under one) hit the same window; the
// backoff arithmetic lives once so a fix to it cannot land in one path and
// miss the other.
//
// Returns the last error unwrapped — including ctx.Err() when canceled
// mid-backoff — so each caller adds its own context.
func retryWhileGrantPropagates(ctx context.Context, retryable func(error) bool, onRetry func(time.Duration), call func() error) error {
	backoff := agentTypeRetryInitialBackoff
	deadline := time.Now().Add(agentTypeRetryBudget)
	for {
		err := call()
		if err == nil {
			return nil
		}
		if !retryable(err) || time.Now().Add(backoff).After(deadline) {
			return err
		}
		if onRetry != nil {
			onRetry(backoff)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
	}
}

// Retry budget for reaching orbit-agents under a just-created grant, used both
// when resolving the agent type at create and when starting the first run.
//
// The budget is sized against orbit-agents' OPA bundle poll interval
// (policy.bundlePollIntervalSecs in backend/cmd/orbit-agents/config/base.yaml,
// currently 3s): it must comfortably exceed one poll, or a create request
// returns "not found in this tenant" for a grant that is merely young. Raising
// that interval without raising this budget breaks trigger creation.
const (
	agentTypeRetryInitialBackoff = 250 * time.Millisecond
	agentTypeRetryBudget         = 6 * time.Second

	// How long after a trigger is created a run-start refusal is still
	// attributable to grant propagation rather than to policy. Deliberately
	// wider than the retry budget: a fire queued behind others can reach
	// dispatch well after the trigger was written.
	grantPropagationWindow = 60 * time.Second
)

func (s *Service) recordFailure(ctx context.Context, deliveryID, msg string) {
	if err := s.repo.UpdateDeliveryOutcome(ctx, deliveryID, StatusFailed, nil, nil, &msg); err != nil {
		s.logger.Error(ctx, "failed to record failed delivery",
			logging.Field{Key: "delivery_id", Value: deliveryID},
			logging.Field{Key: "error", Value: err.Error()},
		)
	}
}
