package metrics

import (
	"context"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// RegisterDomain builds a service's domain metrics via construct, logging
// and returning nil on failure instead of aborting startup — the one rule
// every service's main.go should follow (a service refusing to start
// because a gauge failed to register has turned its own observability into
// an outage). Every domain Metrics type must itself be nil-safe to call
// methods on when RegisterDomain returns nil, so callers can wire the result
// straight into their service without an extra nil check.
func RegisterDomain[T any](ctx context.Context, logger logging.Logger, area string, construct func() (*T, error)) *T {
	m, err := construct()
	if err != nil {
		logger.Error(ctx, "failed to register "+area+" metrics; running uninstrumented",
			logging.Field{Key: "error", Value: err.Error()})
		return nil
	}
	return m
}
