package triggers

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// Overflow behaviors: what a trigger does when it is already at its concurrency
// ceiling and another fire arrives.
//
// Each kind gets the default that suits it. A webhook waits, because the sender
// has been acknowledged and dropping its event would lose work nobody can
// recover. A schedule replaces, because if the previous tick is still running
// the newest state is the one worth acting on. A manual fire refuses, because a
// person is standing there and an immediate "not now" beats silent queueing.
const (
	// OverflowWait holds the fire in order until a slot frees. The oldest is
	// discarded once the backlog exceeds max_queue_depth: an unbounded queue
	// would turn a slow agent into unbounded memory and an ever-growing lag.
	OverflowWait = "wait"

	// OverflowReplace keeps only the most recent waiting fire, marking the ones
	// it displaces as superseded.
	OverflowReplace = "replace"

	// OverflowRefuse rejects immediately rather than waiting.
	OverflowRefuse = "refuse"
)

// DefaultOverflowForKind returns the overflow behavior a trigger of this kind
// should start with.
func DefaultOverflowForKind(kind string) string {
	switch kind {
	case KindScheduled:
		return OverflowReplace
	case KindManual:
		return OverflowRefuse
	default:
		return OverflowWait
	}
}

// AdmissionResult is what admission decided about one fire.
type AdmissionResult int

const (
	// AdmitEnqueued means the fire is durably recorded and will dispatch when a
	// slot frees. This is the normal outcome: every fire goes through the queue
	// so that an acknowledged fire always starts.
	AdmitEnqueued AdmissionResult = iota
	// AdmitSuperseded means a newer fire replaced this one before it ran.
	AdmitSuperseded
	// AdmitDropped means the fire was refused at the ceiling, or evicted from a
	// full backlog.
	AdmitDropped
	// AdmitPaused means the trigger was auto-paused when this fire arrived.
	AdmitPaused
)

// RunStatus maps an admission outcome to the per-trigger outcome recorded
// against the delivery.
func (r AdmissionResult) RunStatus() string {
	switch r {
	case AdmitSuperseded:
		return RunStatusSuperseded
	case AdmitDropped:
		return RunStatusDropped
	case AdmitPaused:
		return RunStatusPaused
	default:
		return RunStatusDispatched
	}
}

// PendingDelivery is a fire waiting for a slot.
type PendingDelivery struct {
	ID         string
	TenantID   string
	TriggerID  string
	DeliveryID string
	Fields     map[string]string
	EnqueuedAt time.Time
}

// OrphanedPending is a waiting fire whose trigger can no longer run it.
//
// It carries the trigger's name where there still is one, so the dropped
// outcome reads as something a person recognizes rather than a bare id, and
// TriggerExists so the recorded reason can say which of the two happened.
type OrphanedPending struct {
	PendingDelivery
	TriggerName   string
	TriggerExists bool
}

// StaleClaimWindow is how long a claimed fire may sit undispatched before the
// worker takes it back.
//
// A worker claims a fire, then dispatches it. If the pod dies in between, the
// row stays claimed forever: nothing re-claims it, so the fire never starts —
// which falsifies the one promise the queue exists to keep, that a fire the
// platform acknowledged always starts. Worse, a claimed row counts against its
// trigger's ceiling, so each stranded row permanently costs that trigger a
// parallel slot until someone notices.
//
// Fifteen minutes is chosen against the ceiling on how long ONE dispatch may
// legitimately take: timeout_seconds is capped at 600 (ten minutes), so a claim
// older than this cannot belong to a dispatch still in progress. That holds only
// because the worker re-stamps each row's claim as its own dispatch begins
// (Repository.TouchPendingClaim) — a batch is claimed in a single statement and
// dispatched serially, so without the re-stamp the last row of a batch of
// sixteen could carry a claim over two hours old while still being live work.
// The margin is deliberately generous: reclaiming a live dispatch would start
// the same fire twice, which is worse than a stranded fire starting late.
const StaleClaimWindow = 15 * time.Minute

// autoPauseThreshold is how many consecutive failures disable a trigger.
//
// A policy denial counts: a trigger whose grant was revoked would otherwise be
// denied forever and never pause, which is precisely the case auto-pause exists
// for. Admission rejections do not count — dropping a fire at the ceiling is the
// configuration working, not the trigger being broken.
const autoPauseThreshold = 10

// Admission decides whether a matched trigger may start a run now, and durably
// holds the fires that cannot.
//
// It is deliberately narrow: concurrency and overflow, nothing more. A general
// deferred-work primitive belongs with the workflow engine, where it can be
// designed against that engine's requirements rather than invented here with
// triggers as its only consumer.
type Admission struct {
	repo     PendingQueue
	logger   logging.Logger
	outcomes outcomeRecorder
}

// NewAdmission constructs the admission controller. It takes the whole
// Repository and keeps only the two roles it uses: the queue it manages, and the
// outcome rows it writes when it refuses or displaces a fire.
func NewAdmission(repo Repository, logger logging.Logger) *Admission {
	return &Admission{
		repo:     repo,
		logger:   logger,
		outcomes: outcomeRecorder{repo: repo, logger: logger},
	}
}

// Admit records a fire for later dispatch, applying the trigger's overflow
// behavior when it is already at its ceiling.
//
// Everything goes through the queue, including the uncontended case. The
// alternative — dispatch directly when nothing is waiting — loses the fire if
// the process dies between recording the delivery and starting the run, which
// leaves a durable record of a start that never happened. The extra latency is
// on no caller's path: the sender was acknowledged before this is reached.
func (a *Admission) Admit(ctx context.Context, t Trigger, deliveryID string, fields map[string]string) (AdmissionResult, error) {
	if t.AutoPausedAt != nil {
		return AdmitPaused, nil
	}

	switch t.Overflow {
	case OverflowReplace:
		// Displace anything already waiting: if the previous fire has not run
		// yet, the newest state is the one worth acting on.
		superseded, err := a.repo.SupersedePending(ctx, t.ID)
		if err != nil {
			return AdmitDropped, fmt.Errorf("supersede pending: %w", err)
		}
		for _, s := range superseded {
			a.recordOutcome(ctx, t, s.DeliveryID, RunStatusSuperseded, "replaced by a newer fire")
		}

	case OverflowRefuse:
		inFlight, err := a.repo.InFlightCount(ctx, t.ID)
		if err != nil {
			return AdmitDropped, fmt.Errorf("count in-flight: %w", err)
		}
		// Fires already waiting count toward the ceiling too. In-flight alone
		// only sees work a worker has picked up, and the worker polls on an
		// interval — so two fires a fraction of a second apart both saw an empty
		// in-flight count and both were accepted, which is silent queueing under
		// the one overflow behavior whose entire promise is that it does not
		// queue. A person told "not now" can retry; a person told "accepted" and
		// then made to wait cannot tell the difference from a stall.
		waiting, err := a.repo.UnclaimedPendingCount(ctx, t.ID)
		if err != nil {
			return AdmitDropped, fmt.Errorf("count waiting fires: %w", err)
		}
		if inFlight+waiting >= int(t.MaxParallel) {
			return AdmitDropped, nil
		}

	case OverflowWait:
		// Bound the backlog. Dropping the oldest rather than the newest is
		// deliberate: when a trigger is behind, the freshest events are the
		// ones still worth acting on.
		evicted, err := a.repo.TrimPending(ctx, t.ID, int(t.MaxQueueDepth))
		if err != nil {
			return AdmitDropped, fmt.Errorf("trim pending: %w", err)
		}
		for _, e := range evicted {
			a.recordOutcome(ctx, t, e.DeliveryID, RunStatusDropped, "evicted: the trigger's backlog is full")
		}
	}

	if err := a.repo.EnqueuePending(ctx, &PendingDelivery{
		ID:         newID(),
		TenantID:   t.TenantID,
		TriggerID:  t.ID,
		DeliveryID: deliveryID,
		Fields:     fields,
	}); err != nil {
		return AdmitDropped, fmt.Errorf("enqueue pending: %w", err)
	}
	return AdmitEnqueued, nil
}

// Drain removes everything waiting for a trigger, recording each as dropped.
//
// Called when a trigger is disabled, auto-paused or deleted. Waiting work must
// not silently vanish: an admin turning a trigger off should be able to see what
// that cost.
func (a *Admission) Drain(ctx context.Context, t Trigger, reason string) (int, error) {
	pending, err := a.repo.DrainPending(ctx, t.ID)
	if err != nil {
		return 0, fmt.Errorf("drain pending: %w", err)
	}
	for _, p := range pending {
		a.recordOutcome(ctx, t, p.DeliveryID, RunStatusDropped, reason)
	}
	if len(pending) > 0 {
		a.logger.Info(ctx, "dropped waiting fires",
			logging.Field{Key: "trigger_id", Value: t.ID},
			logging.Field{Key: "count", Value: len(pending)},
			logging.Field{Key: "reason", Value: reason},
		)
	}
	return len(pending), nil
}

// recordOutcome writes the audit row for a fire admission refused or displaced.
//
// Admission both decides and records here. That is a deliberate narrowing of the
// alternative: returning displaced rows for the service to record would put the
// invariant back in the caller's hands, which is exactly the split this
// consolidated.
func (a *Admission) recordOutcome(ctx context.Context, t Trigger, deliveryID, status, detail string) {
	a.outcomes.record(ctx, &DeliveryRun{
		TenantID:   t.TenantID,
		DeliveryID: deliveryID,
		TriggerID:  t.ID,
		// Captured with the outcome: a trigger deleted or renamed later must not
		// leave this row identified by an id alone.
		TriggerName: t.Name,
		Status:      status,
		Error:       &detail,
	})
}

// newID returns a fresh identifier for a pending row or delivery run.
func newID() string { return uuid.NewString() }
