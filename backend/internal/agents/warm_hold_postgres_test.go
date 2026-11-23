package agents

import (
	"context"
	"testing"
	"time"
)

// A hold only lengthens, and goes with its agent type.
func TestPostgres_AgentWarmHold(t *testing.T) {
	r, tenant := testRunRepo(t)
	at := seedRunAgentType(t, r, tenant, IdentityModeAutonomous)
	ctx := context.Background()
	if until, err := r.AgentWarmUntil(ctx, at.ID); err != nil || !until.IsZero() {
		t.Fatalf("AgentWarmUntil with no hold = %v, %v; want zero", until, err)
	}
	later := time.Now().UTC().Add(10 * time.Minute).Truncate(time.Microsecond)
	for _, until := range []time.Time{later, later.Add(-5 * time.Minute)} {
		if err := r.HoldAgentWarm(ctx, at.ID, until); err != nil {
			t.Fatal(err)
		}
	}
	if until, err := r.AgentWarmUntil(ctx, at.ID); err != nil || !until.Equal(later) {
		t.Errorf("AgentWarmUntil = %v, %v; want the longer hold %v", until, err, later)
	}
	if err := r.SoftDeleteAgentType(ctx, at.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM agent_warm_holds WHERE agent_type_id = $1`, at.ID).Scan(&n); err != nil || n != 0 {
		t.Errorf("holds after the agent type was deleted = %d, %v; want 0", n, err)
	}
}
