package triggers

import (
	"context"
	"testing"
	"time"
)

func TestPostgres_GatewayState(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	s := seedSource(t, r, tenant)
	if _, err := r.pool.Exec(ctx, `UPDATE webhook_sources SET provider = $2 WHERE id = $1`, s.ID, providerDiscord); err != nil {
		t.Fatalf("set provider: %v", err)
	}
	listed := func() bool {
		srcs, err := r.ListGatewaySources(ctx, providerDiscord)
		if err != nil {
			t.Fatalf("ListGatewaySources: %v", err)
		}
		for _, src := range srcs {
			if src.ID == s.ID {
				return true
			}
		}
		return false
	}
	if !listed() {
		t.Fatal("an enabled source with a secret must be listed")
	}

	stored, _ := r.GetSource(ctx, tenant, s.ID)
	if err := r.SetGatewayStatus(ctx, s.ID, stored.SecretRotatedAt, connRejected, "bad token"); err != nil {
		t.Fatalf("SetGatewayStatus: %v", err)
	}
	got, _ := r.GetSource(ctx, tenant, s.ID)
	if got.GatewayStatus != connRejected || got.GatewayReason != "bad token" || got.GatewayStatusAt == nil {
		t.Fatalf("state = %q %q %v, want rejected with its reason and time", got.GatewayStatus, got.GatewayReason, got.GatewayStatusAt)
	}

	// A session of a replaced secret writes nothing.
	stale := stored.SecretRotatedAt.Add(-time.Second)
	if err := r.SetGatewayStatus(ctx, s.ID, &stale, connConnected, ""); err != nil {
		t.Fatalf("SetGatewayStatus(stale): %v", err)
	}
	if got, _ := r.GetSource(ctx, tenant, s.ID); got.GatewayStatus != connRejected {
		t.Errorf("a stale session changed the state to %q", got.GatewayStatus)
	}

	// Rotating the secret clears the state; changing nothing else keeps it.
	got.Name += "-renamed"
	if err := r.UpdateSource(ctx, got); err != nil {
		t.Fatalf("UpdateSource: %v", err)
	}
	if kept, _ := r.GetSource(ctx, tenant, s.ID); kept.GatewayStatus != connRejected {
		t.Errorf("a rename cleared the state: %q", kept.GatewayStatus)
	}
	got.SecretCiphertext = "rotated"
	if err := r.UpdateSource(ctx, got); err != nil {
		t.Fatalf("UpdateSource(rotate): %v", err)
	}
	if rot, _ := r.GetSource(ctx, tenant, s.ID); rot.GatewayStatus != "" || rot.GatewayReason != "" || rot.GatewayStatusAt != nil {
		t.Errorf("rotation left state %q %q %v, want it cleared", rot.GatewayStatus, rot.GatewayReason, rot.GatewayStatusAt)
	}

	got.Enabled = false
	if err := r.UpdateSource(ctx, got); err != nil {
		t.Fatalf("UpdateSource(disable): %v", err)
	}
	if listed() {
		t.Error("a disabled source must not be listed")
	}
}
