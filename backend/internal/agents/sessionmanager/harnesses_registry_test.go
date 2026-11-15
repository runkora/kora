package sessionmanager

import (
	"context"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/harnesses"
)

// TestEveryHarnessHasAProfile fails when a harness is added to the shared list
// without a profileFor case.
func TestEveryHarnessHasAProfile(t *testing.T) {
	for _, h := range harnesses.All() {
		c := &Controller{}
		_, _, _, ok := c.profileFor(context.Background(), CreateSessionRequest{Framework: h.ID, RunID: "r"}, "tok")
		if !ok {
			t.Errorf("harness %q has no profileFor case", h.ID)
		}
	}
}
