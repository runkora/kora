package grants

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ── Test Helpers ──────────────────────────────────────────────────────────────

// newTestPool creates a connection pool to the test database.
// Tests assume DATABASE_URL is set (typically via make test or CI).
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	// Use standard test database URL pattern
	pool, err := pgxpool.New(ctx, "postgres://postgres:postgres@localhost:5432/orbit_test?sslmode=disable")
	if err != nil {
		t.Fatalf("connect to test db: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

// cleanGrants removes all grants from the test database.
func cleanGrants(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(), "DELETE FROM policy_grants")
	if err != nil {
		t.Fatalf("clean grants: %v", err)
	}
}

// ── Create ────────────────────────────────────────────────────────────────────

func TestCreate_Success(t *testing.T) {
	pool := newTestPool(t)
	cleanGrants(t, pool)
	repo := NewPostgresRepository(pool)

	grant := &Grant{
		TenantID:      "tenant-1",
		PrincipalType: PrincipalTypeHuman,
		PrincipalID:   "user-123",
		Scopes:        []string{"inference:invoke", "models:read"},
	}

	err := repo.Create(context.Background(), grant)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Verify ID was generated
	if grant.ID == "" {
		t.Error("expected non-empty ID after Create")
	}

	// Verify CreatedAt was set
	if grant.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt after Create")
	}

	// Verify it's persisted
	fetched, err := repo.GetByID(context.Background(), grant.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}

	if fetched.TenantID != grant.TenantID {
		t.Errorf("tenant_id: got %q, want %q", fetched.TenantID, grant.TenantID)
	}
	if fetched.PrincipalType != grant.PrincipalType {
		t.Errorf("principal_type: got %q, want %q", fetched.PrincipalType, grant.PrincipalType)
	}
	if fetched.PrincipalID != grant.PrincipalID {
		t.Errorf("principal_id: got %q, want %q", fetched.PrincipalID, grant.PrincipalID)
	}
	if len(fetched.Scopes) != 2 || fetched.Scopes[0] != "inference:invoke" {
		t.Errorf("scopes: got %v, want [inference:invoke models:read]", fetched.Scopes)
	}
}

func TestCreate_WithBudget(t *testing.T) {
	pool := newTestPool(t)
	cleanGrants(t, pool)
	repo := NewPostgresRepository(pool)

	grant := &Grant{
		TenantID:      "tenant-1",
		PrincipalType: PrincipalTypeAgent,
		PrincipalID:   "agent-456",
		Scopes:        []string{"inference:invoke"},
		Budget: &BudgetAssignment{
			LimitUSD:       100.0,
			ResetCadence:   "weekly",
			PerRunLimitUSD: 10.0,
		},
	}

	err := repo.Create(context.Background(), grant)
	if err != nil {
		t.Fatalf("Create with budget failed: %v", err)
	}

	// Verify budget persisted
	fetched, err := repo.GetByID(context.Background(), grant.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}

	if fetched.Budget == nil {
		t.Fatal("expected non-nil budget")
	}
	if fetched.Budget.LimitUSD != 100.0 {
		t.Errorf("budget.limit_usd: got %f, want 100.0", fetched.Budget.LimitUSD)
	}
	if fetched.Budget.ResetCadence != "weekly" {
		t.Errorf("budget.reset_cadence: got %q, want weekly", fetched.Budget.ResetCadence)
	}
	if fetched.Budget.PerRunLimitUSD != 10.0 {
		t.Errorf("budget.per_run_limit_usd: got %f, want 10.0", fetched.Budget.PerRunLimitUSD)
	}
}

func TestCreate_WithEmptyScopes(t *testing.T) {
	pool := newTestPool(t)
	cleanGrants(t, pool)
	repo := NewPostgresRepository(pool)

	grant := &Grant{
		TenantID:      "tenant-1",
		PrincipalType: PrincipalTypeHuman,
		PrincipalID:   "user-123",
		Scopes:        []string{},
	}

	err := repo.Create(context.Background(), grant)
	if err != nil {
		t.Fatalf("Create with empty scopes failed: %v", err)
	}

	// Verify empty array persisted (not null)
	fetched, err := repo.GetByID(context.Background(), grant.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if fetched.Scopes == nil {
		t.Error("expected non-nil scopes array")
	}
	if len(fetched.Scopes) != 0 {
		t.Errorf("expected empty scopes, got %v", fetched.Scopes)
	}
}

// ── GetByID ───────────────────────────────────────────────────────────────────

func TestGetByID_NotFound(t *testing.T) {
	pool := newTestPool(t)
	cleanGrants(t, pool)
	repo := NewPostgresRepository(pool)

	_, err := repo.GetByID(context.Background(), "nonexistent-id")
	if err != ErrGrantNotFound {
		t.Errorf("expected ErrGrantNotFound, got %v", err)
	}
}

// ── ListByPrincipal ───────────────────────────────────────────────────────────

func TestListByPrincipal_Success(t *testing.T) {
	pool := newTestPool(t)
	cleanGrants(t, pool)
	repo := NewPostgresRepository(pool)

	// Create 3 grants: 2 for user-1, 1 for user-2
	g1 := &Grant{TenantID: "t1", PrincipalType: PrincipalTypeHuman, PrincipalID: "user-1", Scopes: []string{"scope-a"}}
	g2 := &Grant{TenantID: "t1", PrincipalType: PrincipalTypeHuman, PrincipalID: "user-1", Scopes: []string{"scope-b"}}
	g3 := &Grant{TenantID: "t1", PrincipalType: PrincipalTypeHuman, PrincipalID: "user-2", Scopes: []string{"scope-c"}}

	for _, g := range []*Grant{g1, g2, g3} {
		if err := repo.Create(context.Background(), g); err != nil {
			t.Fatalf("Create: %v", err)
		}
		// Ensure created_at differs slightly for ordering test
		time.Sleep(10 * time.Millisecond)
	}

	// List grants for user-1
	grants, total, err := repo.ListByPrincipal(context.Background(), "t1", PrincipalTypeHuman, "user-1", 0, 0)
	if err != nil {
		t.Fatalf("ListByPrincipal failed: %v", err)
	}

	if total != 2 {
		t.Errorf("total: got %d, want 2", total)
	}
	if len(grants) != 2 {
		t.Fatalf("grants count: got %d, want 2", len(grants))
	}

	// Verify order (created_at ascending)
	if grants[0].Scopes[0] != "scope-a" || grants[1].Scopes[0] != "scope-b" {
		t.Errorf("grants not ordered by created_at: got %v then %v", grants[0].Scopes, grants[1].Scopes)
	}
}

func TestListByPrincipal_Pagination(t *testing.T) {
	pool := newTestPool(t)
	cleanGrants(t, pool)
	repo := NewPostgresRepository(pool)

	// Create 5 grants for same principal
	for i := 0; i < 5; i++ {
		g := &Grant{TenantID: "t1", PrincipalType: PrincipalTypeAgent, PrincipalID: "agent-x", Scopes: []string{"scope"}}
		if err := repo.Create(context.Background(), g); err != nil {
			t.Fatalf("Create: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Fetch first 2
	grants, total, err := repo.ListByPrincipal(context.Background(), "t1", PrincipalTypeAgent, "agent-x", 2, 0)
	if err != nil {
		t.Fatalf("ListByPrincipal failed: %v", err)
	}

	if total != 5 {
		t.Errorf("total: got %d, want 5", total)
	}
	if len(grants) != 2 {
		t.Errorf("grants count: got %d, want 2", len(grants))
	}

	// Fetch next 2 (offset 2)
	grants2, total2, err := repo.ListByPrincipal(context.Background(), "t1", PrincipalTypeAgent, "agent-x", 2, 2)
	if err != nil {
		t.Fatalf("ListByPrincipal with offset failed: %v", err)
	}

	if total2 != 5 {
		t.Errorf("total on page 2: got %d, want 5", total2)
	}
	if len(grants2) != 2 {
		t.Errorf("grants count on page 2: got %d, want 2", len(grants2))
	}

	// Verify no overlap
	if grants[0].ID == grants2[0].ID {
		t.Error("pagination returned duplicate grant IDs")
	}
}

func TestListByPrincipal_EmptyResult(t *testing.T) {
	pool := newTestPool(t)
	cleanGrants(t, pool)
	repo := NewPostgresRepository(pool)

	grants, total, err := repo.ListByPrincipal(context.Background(), "t1", PrincipalTypeHuman, "nonexistent", 0, 0)
	if err != nil {
		t.Fatalf("ListByPrincipal failed: %v", err)
	}

	if total != 0 {
		t.Errorf("total: got %d, want 0", total)
	}
	if len(grants) != 0 {
		t.Errorf("grants: got %d, want 0", len(grants))
	}
}

// ── ListByTenant ──────────────────────────────────────────────────────────────

func TestListByTenant_MultipleTypes(t *testing.T) {
	pool := newTestPool(t)
	cleanGrants(t, pool)
	repo := NewPostgresRepository(pool)

	// Create grants across different principal types
	g1 := &Grant{TenantID: "t1", PrincipalType: PrincipalTypeHuman, PrincipalID: "u1", Scopes: []string{"a"}}
	g2 := &Grant{TenantID: "t1", PrincipalType: PrincipalTypeAgent, PrincipalID: "a1", Scopes: []string{"b"}}
	g3 := &Grant{TenantID: "t2", PrincipalType: PrincipalTypeHuman, PrincipalID: "u2", Scopes: []string{"c"}}

	for _, g := range []*Grant{g1, g2, g3} {
		if err := repo.Create(context.Background(), g); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	// List all grants for t1
	grants, total, err := repo.ListByTenant(context.Background(), "t1", 0, 0)
	if err != nil {
		t.Fatalf("ListByTenant failed: %v", err)
	}

	if total != 2 {
		t.Errorf("total: got %d, want 2", total)
	}
	if len(grants) != 2 {
		t.Fatalf("grants count: got %d, want 2", len(grants))
	}

	// Verify ordering: principal_type, principal_id, created_at
	// AGENT sorts before HUMAN alphabetically
	if grants[0].PrincipalType != PrincipalTypeAgent {
		t.Errorf("first grant type: got %q, want AGENT", grants[0].PrincipalType)
	}
	if grants[1].PrincipalType != PrincipalTypeHuman {
		t.Errorf("second grant type: got %q, want HUMAN", grants[1].PrincipalType)
	}
}

// ── UpdateBudget ──────────────────────────────────────────────────────────────

func TestUpdateBudget_AddBudget(t *testing.T) {
	pool := newTestPool(t)
	cleanGrants(t, pool)
	repo := NewPostgresRepository(pool)

	// Create grant without budget
	grant := &Grant{TenantID: "t1", PrincipalType: PrincipalTypeHuman, PrincipalID: "u1", Scopes: []string{"scope"}}
	if err := repo.Create(context.Background(), grant); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Add budget
	newBudget := &BudgetAssignment{LimitUSD: 50.0, ResetCadence: "monthly"}
	err := repo.UpdateBudget(context.Background(), grant.ID, "t1", newBudget)
	if err != nil {
		t.Fatalf("UpdateBudget failed: %v", err)
	}

	// Verify budget was added
	fetched, err := repo.GetByID(context.Background(), grant.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if fetched.Budget == nil {
		t.Fatal("expected non-nil budget after update")
	}
	if fetched.Budget.LimitUSD != 50.0 {
		t.Errorf("budget.limit_usd: got %f, want 50.0", fetched.Budget.LimitUSD)
	}
}

func TestUpdateBudget_RemoveBudget(t *testing.T) {
	pool := newTestPool(t)
	cleanGrants(t, pool)
	repo := NewPostgresRepository(pool)

	// Create grant with budget
	grant := &Grant{
		TenantID:      "t1",
		PrincipalType: PrincipalTypeAgent,
		PrincipalID:   "a1",
		Scopes:        []string{"scope"},
		Budget:        &BudgetAssignment{LimitUSD: 100.0, ResetCadence: "weekly"},
	}
	if err := repo.Create(context.Background(), grant); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Remove budget (nil pointer)
	err := repo.UpdateBudget(context.Background(), grant.ID, "t1", nil)
	if err != nil {
		t.Fatalf("UpdateBudget to nil failed: %v", err)
	}

	// Verify budget was removed
	fetched, err := repo.GetByID(context.Background(), grant.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if fetched.Budget != nil {
		t.Errorf("expected nil budget after removal, got %+v", fetched.Budget)
	}
}

func TestUpdateBudget_NotFound(t *testing.T) {
	pool := newTestPool(t)
	cleanGrants(t, pool)
	repo := NewPostgresRepository(pool)

	err := repo.UpdateBudget(context.Background(), "nonexistent-id", "t1", &BudgetAssignment{LimitUSD: 10})
	if err != ErrGrantNotFound {
		t.Errorf("expected ErrGrantNotFound, got %v", err)
	}
}

func TestUpdateBudget_WrongTenant(t *testing.T) {
	pool := newTestPool(t)
	cleanGrants(t, pool)
	repo := NewPostgresRepository(pool)

	grant := &Grant{TenantID: "t1", PrincipalType: PrincipalTypeHuman, PrincipalID: "u1", Scopes: []string{"scope"}}
	if err := repo.Create(context.Background(), grant); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Try to update from different tenant
	err := repo.UpdateBudget(context.Background(), grant.ID, "t2", &BudgetAssignment{LimitUSD: 10})
	if err != ErrGrantNotFound {
		t.Errorf("expected ErrGrantNotFound when tenant mismatch, got %v", err)
	}
}

// ── Delete ────────────────────────────────────────────────────────────────────

func TestDelete_Success(t *testing.T) {
	pool := newTestPool(t)
	cleanGrants(t, pool)
	repo := NewPostgresRepository(pool)

	grant := &Grant{TenantID: "t1", PrincipalType: PrincipalTypeHuman, PrincipalID: "u1", Scopes: []string{"scope"}}
	if err := repo.Create(context.Background(), grant); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Delete it
	err := repo.Delete(context.Background(), grant.ID, "t1")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify it's gone
	_, err = repo.GetByID(context.Background(), grant.ID)
	if err != ErrGrantNotFound {
		t.Errorf("expected ErrGrantNotFound after delete, got %v", err)
	}
}

func TestDelete_NotFound(t *testing.T) {
	pool := newTestPool(t)
	cleanGrants(t, pool)
	repo := NewPostgresRepository(pool)

	err := repo.Delete(context.Background(), "nonexistent-id", "t1")
	if err != ErrGrantNotFound {
		t.Errorf("expected ErrGrantNotFound, got %v", err)
	}
}

func TestDelete_WrongTenant(t *testing.T) {
	pool := newTestPool(t)
	cleanGrants(t, pool)
	repo := NewPostgresRepository(pool)

	grant := &Grant{TenantID: "t1", PrincipalType: PrincipalTypeHuman, PrincipalID: "u1", Scopes: []string{"scope"}}
	if err := repo.Create(context.Background(), grant); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Try to delete from different tenant
	err := repo.Delete(context.Background(), grant.ID, "t2")
	if err != ErrGrantNotFound {
		t.Errorf("expected ErrGrantNotFound when tenant mismatch, got %v", err)
	}

	// Verify it still exists
	_, err = repo.GetByID(context.Background(), grant.ID)
	if err != nil {
		t.Error("grant should still exist after failed delete from wrong tenant")
	}
}

// ── PurgeScopeFromAll ─────────────────────────────────────────────────────────

func TestPurgeScopeFromAll_RemovesScopeFromMultipleGrants(t *testing.T) {
	pool := newTestPool(t)
	cleanGrants(t, pool)
	repo := NewPostgresRepository(pool)

	// Create grants with "deprecated:scope" mixed in
	g1 := &Grant{TenantID: "t1", PrincipalType: PrincipalTypeHuman, PrincipalID: "u1", Scopes: []string{"good:scope", "deprecated:scope"}}
	g2 := &Grant{TenantID: "t2", PrincipalType: PrincipalTypeAgent, PrincipalID: "a1", Scopes: []string{"deprecated:scope", "another:scope"}}
	g3 := &Grant{TenantID: "t1", PrincipalType: PrincipalTypeHuman, PrincipalID: "u2", Scopes: []string{"only:good"}}

	for _, g := range []*Grant{g1, g2, g3} {
		if err := repo.Create(context.Background(), g); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	// Purge "deprecated:scope"
	err := repo.PurgeScopeFromAll(context.Background(), "deprecated:scope")
	if err != nil {
		t.Fatalf("PurgeScopeFromAll failed: %v", err)
	}

	// Verify g1 no longer has it
	fetched1, _ := repo.GetByID(context.Background(), g1.ID)
	if contains(fetched1.Scopes, "deprecated:scope") {
		t.Error("g1 still contains deprecated:scope after purge")
	}
	if !contains(fetched1.Scopes, "good:scope") {
		t.Error("g1 lost good:scope during purge")
	}

	// Verify g2 no longer has it
	fetched2, _ := repo.GetByID(context.Background(), g2.ID)
	if contains(fetched2.Scopes, "deprecated:scope") {
		t.Error("g2 still contains deprecated:scope after purge")
	}
	if !contains(fetched2.Scopes, "another:scope") {
		t.Error("g2 lost another:scope during purge")
	}

	// Verify g3 unchanged
	fetched3, _ := repo.GetByID(context.Background(), g3.ID)
	if len(fetched3.Scopes) != 1 || fetched3.Scopes[0] != "only:good" {
		t.Errorf("g3 scopes changed unexpectedly: got %v", fetched3.Scopes)
	}
}

func TestPurgeScopeFromAll_NoMatch(t *testing.T) {
	pool := newTestPool(t)
	cleanGrants(t, pool)
	repo := NewPostgresRepository(pool)

	g1 := &Grant{TenantID: "t1", PrincipalType: PrincipalTypeHuman, PrincipalID: "u1", Scopes: []string{"scope-a"}}
	if err := repo.Create(context.Background(), g1); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Purge non-existent scope
	err := repo.PurgeScopeFromAll(context.Background(), "nonexistent:scope")
	if err != nil {
		t.Fatalf("PurgeScopeFromAll failed: %v", err)
	}

	// Verify g1 unchanged
	fetched, _ := repo.GetByID(context.Background(), g1.ID)
	if len(fetched.Scopes) != 1 || fetched.Scopes[0] != "scope-a" {
		t.Errorf("grant scopes changed unexpectedly: got %v", fetched.Scopes)
	}
}

// ── ListDistinctTenants ───────────────────────────────────────────────────────

func TestListDistinctTenants(t *testing.T) {
	pool := newTestPool(t)
	cleanGrants(t, pool)
	repo := NewPostgresRepository(pool)

	// Create grants for multiple tenants
	g1 := &Grant{TenantID: "tenant-a", PrincipalType: PrincipalTypeHuman, PrincipalID: "u1", Scopes: []string{"scope"}}
	g2 := &Grant{TenantID: "tenant-b", PrincipalType: PrincipalTypeHuman, PrincipalID: "u2", Scopes: []string{"scope"}}
	g3 := &Grant{TenantID: "tenant-a", PrincipalType: PrincipalTypeAgent, PrincipalID: "a1", Scopes: []string{"scope"}}

	for _, g := range []*Grant{g1, g2, g3} {
		if err := repo.Create(context.Background(), g); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	tenants, err := repo.ListDistinctTenants(context.Background())
	if err != nil {
		t.Fatalf("ListDistinctTenants failed: %v", err)
	}

	if len(tenants) != 2 {
		t.Errorf("distinct tenants: got %d, want 2", len(tenants))
	}

	// Verify both tenant IDs present (order is deterministic: ORDER BY tenant_id)
	if tenants[0] != "tenant-a" || tenants[1] != "tenant-b" {
		t.Errorf("tenants: got %v, want [tenant-a tenant-b]", tenants)
	}
}

func TestListDistinctTenants_Empty(t *testing.T) {
	pool := newTestPool(t)
	cleanGrants(t, pool)
	repo := NewPostgresRepository(pool)

	tenants, err := repo.ListDistinctTenants(context.Background())
	if err != nil {
		t.Fatalf("ListDistinctTenants failed: %v", err)
	}

	if len(tenants) != 0 {
		t.Errorf("expected 0 tenants, got %d", len(tenants))
	}
}

// ── CountByTenant ─────────────────────────────────────────────────────────────

func TestCountByTenant(t *testing.T) {
	pool := newTestPool(t)
	cleanGrants(t, pool)
	repo := NewPostgresRepository(pool)

	// Create grants of different types for t1
	g1 := &Grant{TenantID: "t1", PrincipalType: PrincipalTypeHuman, PrincipalID: "u1", Scopes: []string{"scope"}}
	g2 := &Grant{TenantID: "t1", PrincipalType: PrincipalTypeHuman, PrincipalID: "u2", Scopes: []string{"scope"}}
	g3 := &Grant{TenantID: "t1", PrincipalType: PrincipalTypeAgent, PrincipalID: "a1", Scopes: []string{"scope"}}
	g4 := &Grant{TenantID: "t2", PrincipalType: PrincipalTypeHuman, PrincipalID: "u3", Scopes: []string{"scope"}}

	for _, g := range []*Grant{g1, g2, g3, g4} {
		if err := repo.Create(context.Background(), g); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	total, byType, err := repo.CountByTenant(context.Background(), "t1")
	if err != nil {
		t.Fatalf("CountByTenant failed: %v", err)
	}

	if total != 3 {
		t.Errorf("total: got %d, want 3", total)
	}
	if byType[PrincipalTypeHuman] != 2 {
		t.Errorf("HUMAN count: got %d, want 2", byType[PrincipalTypeHuman])
	}
	if byType[PrincipalTypeAgent] != 1 {
		t.Errorf("AGENT count: got %d, want 1", byType[PrincipalTypeAgent])
	}
	if byType[PrincipalTypeService] != 0 {
		t.Errorf("SERVICE count: got %d, want 0", byType[PrincipalTypeService])
	}
}

func TestCountByTenant_Empty(t *testing.T) {
	pool := newTestPool(t)
	cleanGrants(t, pool)
	repo := NewPostgresRepository(pool)

	total, byType, err := repo.CountByTenant(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("CountByTenant failed: %v", err)
	}

	if total != 0 {
		t.Errorf("total: got %d, want 0", total)
	}
	if len(byType) != 0 {
		t.Errorf("byType: got %v, want empty map", byType)
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func contains(scopes []string, target string) bool {
	for _, s := range scopes {
		if s == target {
			return true
		}
	}
	return false
}
