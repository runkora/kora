package declarative

import (
	"slices"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/events"
)

// The declared failure payload validates, and its outcome and source enums
// hold every outcome a failure records and both sources.
func TestSpinePayloads_MatchSchema(t *testing.T) {
	p := events.DeclaredResourcePayload{Kind: "agents", Namespace: "kora-tenant-example", Name: "reviewer", Generation: 2, Outcome: OutcomeNameConflict}
	if err := events.ValidatePayload(events.DeclaredResourceFailed, p); err != nil {
		t.Error(err)
	}
	if got, want := events.Enum("declared_outcome"), []string{OutcomeNameConflict, OutcomeInvalid, OutcomePending}; !slices.Equal(got, want) {
		t.Errorf("declared_outcome = %v, want %v", got, want)
	}
	if got, want := events.Enum("resource_source"), []string{SourceAPI, SourceCRD}; !slices.Equal(got, want) {
		t.Errorf("resource_source = %v, want %v", got, want)
	}
}
