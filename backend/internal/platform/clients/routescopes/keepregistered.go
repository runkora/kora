package routescopes

import (
	"context"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
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
// A var rather than a const so tests can tick without waiting five minutes.
// Nothing outside this package can reach it.
var reregisterInterval = 5 * time.Minute

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
// register is the one call that publishes this service's routes and scopes,
// whatever client it goes through: the anonymous routescopes client, or the
// authenticated policy client for services whose registration needs a bearer.
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
