package inferencegw

import (
	"context"

	mw "github.com/runkora/kora/backend/internal/platform/middleware"
)

// Run identity extraction lives in internal/platform/middleware (StripRunPrefix
// + RunIDFromContext) so the inference gateway and the MCP tool-gateway share
// one strip-and-context mechanism. This shim keeps the gateway's internal call
// sites unchanged. See docs/adr/2026-06-10-hierarchical-budget-enforcement.md
// and docs/adr/2026-07-23-delegated-agent-identity.md (Decision 4).
func runIDFromContext(ctx context.Context) string { return mw.RunIDFromContext(ctx) }
