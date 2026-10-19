package triggers

import (
	"context"
	"errors"

	policyclient "github.com/runkora/kora/backend/internal/platform/clients/policy"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// deleteGrantsBestEffort deletes policy grants, tolerating grants that are
// already gone and logging any other failure rather than returning it.
//
// It is the one implementation of grant cleanup for the whole feature —
// rollback after a failed create, and delete of a trigger or source — so the
// tolerance and the logging cannot drift between paths. Failures are logged
// rather than returned because the caller's own outcome is the one that
// matters: a leaked grant is an operational cleanup, not a reason to tell the
// caller something different about their request. ErrGrantNotFound is success,
// not failure — the desired end state is "no grant", and a grant already
// removed (a retried rollback, an operator's manual cleanup) is exactly that.
func deleteGrantsBestEffort(ctx context.Context, policy *policyclient.Client, logger logging.Logger, msg string, grantIDs []string, fields ...logging.Field) {
	for _, id := range grantIDs {
		err := policy.DeleteGrant(ctx, id)
		if err == nil || errors.Is(err, policyclient.ErrGrantNotFound) {
			continue
		}
		logger.Error(ctx, msg, append(fields,
			logging.Field{Key: "grant_id", Value: id},
			logging.Field{Key: "error", Value: err.Error()},
		)...)
	}
}
