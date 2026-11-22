package identity

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/runkora/kora/backend/internal/platform/storage"
)

func TestPostgres_AgentLive(t *testing.T) {
	dsn := os.Getenv("KORA_TEST_DB_URL")
	if dsn == "" {
		t.Skip("set KORA_TEST_DB_URL to run identity Postgres integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database unreachable: %v", err)
	}
	tenant := "test-" + uuid.NewString()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM agent_types WHERE tenant_id = $1`, tenant)
		pool.Close()
	})
	for _, row := range []struct {
		slug, state string
		deleted     bool
	}{
		{"deploy-bot", "active", false},
		{"new-bot", "provisioning", false},
		{"leaving-bot", "deleting", false},
		{"retired-bot", "deleted", true},
	} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO agent_types (id, tenant_id, name, slug, state, deleted_at)
			VALUES ($1, $2, $3, $3, $4, CASE WHEN $5 THEN now() END)`,
			uuid.NewString(), tenant, row.slug, row.state, row.deleted); err != nil {
			t.Fatalf("insert %s: %v", row.slug, err)
		}
	}

	registry := NewPostgresAgentRegistry(storage.NewPool(pool))
	for _, tc := range []struct {
		tenant, slug string
		want         bool
	}{
		{tenant, "deploy-bot", true},
		{tenant, "new-bot", true},
		{tenant, "leaving-bot", true},
		{tenant, "retired-bot", false},
		{tenant, "s2z-pooltest", false},
		{"other-" + tenant, "deploy-bot", false},
	} {
		got, err := registry.AgentLive(ctx, tc.tenant, tc.slug)
		if err != nil || got != tc.want {
			t.Errorf("AgentLive(%s, %s) = %v, %v, want %v", tc.tenant, tc.slug, got, err, tc.want)
		}
	}
}
