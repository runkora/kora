package devenvironments

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/orbitapi"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/watch"
)

// AuditIngester watches every OrbitDevEnvironment and derives audit events
// (Decision C3) from phase transitions the API layer has no other way to
// observe — the operator changes phase on its own reconcile loop, never
// through this package's own service calls. Best-effort: the DB's
// UNIQUE(environment_id, action, occurred_at) constraint plus in-memory
// last-observed-phase tracking dedups the overwhelmingly common case
// (redelivered watch events within one process's uptime); a full ingester
// restart concurrent with an in-flight Sleeping transition can in the rare
// case emit one duplicate row (Sleeping has no backing Condition to derive a
// stable transition timestamp from — see recordPhaseTransition). Acceptable
// for a best-effort audit projection: when the platform event spine ships,
// emission moves onto it and this table remains as the queryable projection.
type AuditIngester struct {
	cr     CRClient
	repo   Repository
	logger logging.Logger

	mu        sync.Mutex
	lastPhase map[string]string // environment name -> last-observed phase
}

// NewAuditIngester constructs an AuditIngester. cr should be the same
// CRClient the Service uses (no need for a second Kubernetes client).
func NewAuditIngester(cr CRClient, repo Repository, logger logging.Logger) *AuditIngester {
	return &AuditIngester{cr: cr, repo: repo, logger: logger, lastPhase: map[string]string{}}
}

// Run watches every OrbitDevEnvironment until ctx is canceled, reconnecting
// with exponential backoff (capped) on any watch error or channel closure —
// mirrors registerDevEnvironmentScopes' retry shape in cmd/identity/main.go.
func (ing *AuditIngester) Run(ctx context.Context) {
	backoff := 2 * time.Second
	const maxBackoff = 30 * time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		if err := ing.watchOnce(ctx); err != nil {
			ing.logger.Warn(ctx, "devenvironments audit ingester watch failed, reconnecting",
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func (ing *AuditIngester) watchOnce(ctx context.Context) error {
	w, err := ing.cr.WatchAll(ctx)
	if err != nil {
		return fmt.Errorf("start watch: %w", err)
	}
	defer w.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-w.ResultChan():
			if !ok {
				return errors.New("watch channel closed")
			}
			ing.handleEvent(ctx, event)
		}
	}
}

// handleEvent's ADDED/MODIFIED split is the key idempotency insight: on
// every ingester restart, the initial List+Watch redelivers the CURRENT
// state of every existing environment as synthetic ADDED events. Treating
// those as changes would re-emit an audit row for whatever phase each
// environment already happened to be in, every single restart. Only MODIFIED
// events — genuine transitions observed while this process has been
// running — ever derive an audit event; ADDED only primes the in-memory
// last-observed-phase map.
func (ing *AuditIngester) handleEvent(ctx context.Context, event watch.Event) {
	env, ok := event.Object.(*orbitapi.OrbitDevEnvironment)
	if !ok {
		return
	}

	switch event.Type {
	case watch.Added:
		ing.mu.Lock()
		ing.lastPhase[env.Name] = env.Status.Phase
		ing.mu.Unlock()

	case watch.Modified:
		ing.mu.Lock()
		prev, seen := ing.lastPhase[env.Name]
		ing.lastPhase[env.Name] = env.Status.Phase
		ing.mu.Unlock()
		if !seen || prev == env.Status.Phase {
			return
		}
		ing.recordPhaseTransition(ctx, env, prev)

	case watch.Deleted:
		ing.mu.Lock()
		delete(ing.lastPhase, env.Name)
		ing.mu.Unlock()
		recordExpiredIfNotTerminal(ctx, ing.repo, ing.logger, env.Name)
	}
}

// recordPhaseTransition derives a system-actor audit event from an observed
// phase change. occurred_at uses the relevant Condition's LastTransitionTime
// where one exists (stable across ingester restarts/replays, unlike
// ingestion-time wall-clock, which would vary on every redelivery) —
// ConditionDevEnvPlatformReady for Ready; ConditionDevEnvProgressing for
// Failed, though it may lag the exact threshold-crossing moment (it only
// updates when its own Status/Reason/Message changes, and several
// consecutive identical-reason failures share one LastTransitionTime) —
// still the best available proxy. Sleeping has no backing Condition at all,
// so this uses ingestion time; see the type doc comment for the accepted
// restart-duplicate risk that follows from that.
func (ing *AuditIngester) recordPhaseTransition(ctx context.Context, env *orbitapi.OrbitDevEnvironment, prevPhase string) {
	var action string
	occurredAt := time.Now().UTC()
	switch env.Status.Phase {
	case orbitapi.DevEnvPhaseReady:
		action = "provision_ready"
		if c := meta.FindStatusCondition(env.Status.Conditions, orbitapi.ConditionDevEnvPlatformReady); c != nil {
			occurredAt = c.LastTransitionTime.Time
		}
	case orbitapi.DevEnvPhaseFailed:
		action = "provision_failed"
		if c := meta.FindStatusCondition(env.Status.Conditions, orbitapi.ConditionDevEnvProgressing); c != nil {
			occurredAt = c.LastTransitionTime.Time
		}
	case orbitapi.DevEnvPhaseSleeping:
		if env.Spec.Awake {
			// A wake-in-progress artifact (spec.awake flipped true but the
			// reconciler hasn't caught up to Ready yet), not the idle
			// auto-sleep this action means.
			return
		}
		action = "auto_slept"
	default:
		return
	}

	if err := ing.repo.InsertAuditEvent(ctx, AuditEvent{
		EnvironmentID: env.Name,
		Action:        action,
		Actor:         "system:operator",
		ActorType:     "system",
		OccurredAt:    occurredAt,
		Details:       map[string]any{"fromPhase": prevPhase},
	}); err != nil {
		ing.logger.Warn(ctx, "failed to record derived audit event",
			logging.Field{Key: "environmentId", Value: env.Name},
			logging.Field{Key: "action", Value: action},
			logging.Field{Key: "error", Value: err.Error()},
		)
	}
}
