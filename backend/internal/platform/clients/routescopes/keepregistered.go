package routescopes

import (
	"context"
	"time"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// Registration timing, shared by every binary that keeps itself registered.
// One policy here rather than one per main.go, so recovery behavior after a
// policy-service restart cannot drift between services.
const (
	// startupBackoffBase is the first retry delay while the policy service
	// comes up; subsequent retries double from here.
	startupBackoffBase = 2 * time.Second
	// startupBackoffCap bounds the doubling, so retry latency stays bounded
	// while the policy service comes up.
	startupBackoffCap = 30 * time.Second
)

// reregisterInterval is the steady-state cadence, and it is deliberately not
// configurable. It answers a platform question, "how long may a service's
// routes stay missing after the policy service restarts", and the answer is the
// same for every binary because the bundle they are all missing from is the
// same bundle. A per-service setting would only be a way for them to disagree,
// which is exactly what the backoff constants above exist to prevent.
//
// 30 seconds bounds how long routes stay missing after a restart. A tick that
// changes nothing costs one call and no bundle recompile: the policy service
// skips the recompile for an unchanged registration.
const reregisterInterval = 30 * time.Second

// KeepRegistered keeps a service's route table and scope vocabulary published
// to the policy service for the life of ctx. Blocks; run it in a goroutine.
//
// Registration lives in the policy service's memory. Registering once at
// startup therefore holds only until that pod restarts, after which nothing
// re-publishes the route table and every scope the service declares is unknown
// to it. So this registers immediately — retrying with exponential backoff
// while the policy service comes up — and then re-registers on every interval
// tick. register must be idempotent, which the policy service's registration
// endpoint is; a tick that changes nothing costs one call.
//
// register is the one call that publishes this service's routes and scopes:
// the routescopes client over HTTP, or an in-process call in the policy service.
//
// The cadence is not a parameter: see [reregisterInterval].
func KeepRegistered(ctx context.Context, logger logging.Logger, service string, register func(context.Context) error) {
	// Startup phase: retry until the first success. There is no attempt cap —
	// an unregistered service rejects every scoped request, so giving up is
	// never better than trying again.
	backoff := startupBackoffBase
	for {
		if err := register(ctx); err == nil {
			logger.Info(ctx, "route scope vocabulary registered with policy service",
				logging.Field{Key: "service", Value: service})
			break
		} else {
			logger.Warn(ctx, "route scope registration failed, retrying",
				logging.Field{Key: "service", Value: service},
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
			if backoff < startupBackoffCap {
				backoff *= 2
				if backoff > startupBackoffCap {
					backoff = startupBackoffCap
				}
			}
		}
	}

	// Steady state: re-register on a fixed cadence to self-heal after a
	// policy-service restart.
	ticker := time.NewTicker(reregisterInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := register(ctx); err != nil {
				logger.Warn(ctx, "periodic route scope re-registration failed",
					logging.Field{Key: "service", Value: service},
					logging.Field{Key: "error", Value: err.Error()})
				continue
			}
			logger.Debug(ctx, "route scope vocabulary re-registered",
				logging.Field{Key: "service", Value: service})
		}
	}
}

// RepublishOnEvent returns the callback to hand to a domain-event hook when
// that event changes what the service publishes: a model catalog reload, an
// agent-type write, a connection-registry reload. It republishes immediately so
// the vocabulary is not stale until the next [KeepRegistered] tick.
//
// The callback never blocks its caller. These hooks fire on paths that must not
// wait on a policy-service round trip: an agent-type write invokes its hook
// synchronously inside the write, and a connection-registry reload runs on the
// fsnotify watcher goroutine, where blocking would delay the next reload. So
// the work runs in its own goroutine, bounded by the client's own request
// timeout rather than running unbounded.
//
// Errors are logged, not returned. There is no caller left to return them to,
// and the periodic tick is the backstop that makes a lost republish self-heal
// rather than persist.
//
// It registers against context.Background() deliberately. A hook fires long
// after startup, so a captured startup context may already be canceled, and a
// republish caused by a real change should not be silently skipped because of
// which context happened to be in scope where the hook was installed.
//
// Racing a concurrent tick is harmless: the policy service's registration
// endpoint is idempotent.
func RepublishOnEvent(logger logging.Logger, service string, register func(context.Context) error) func() {
	return func() {
		go func() {
			ctx := context.Background()
			if err := register(ctx); err != nil {
				logger.Error(ctx, "route scope republish after change failed",
					logging.Field{Key: "service", Value: service},
					logging.Field{Key: "error", Value: err.Error()},
				)
				return
			}
			logger.Debug(ctx, "route scope vocabulary republished after change",
				logging.Field{Key: "service", Value: service})
		}()
	}
}
