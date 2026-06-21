package roles

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ── Test Helpers ──────────────────────────────────────────────────────────────

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, "postgres://postgres:postgres@localhost:5432/orbit_test?sslmode=disable")
	if err != nil {
		t.Fatalf("connect to test db: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

func cleanRoles(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(), "DELETE FROM policy_role_scopes")
	if err != nil {
		t.Fatalf("clean roles: %v", err)
	}
}

// ── Upsert ────────────────────────────────────────────────────────────────────

func TestUpsert_Insert(t *testing.T) {
	pool := newTestPool(t)
	cleanRoles(t, pool)
	repo := NewPostgresRepository(pool)

	entry := &RoleEntry{
		TenantID: "tenant-1",
		Role:     "engineer",
		Scopes:   []string{"inference:invoke", "models:read"},
	}

	err := repo.Upsert(context.Background(), entry)
	if err != nil {
		t.Fatalf("Upsert insert failed: %v", err)
	}

	// Verify ID was generated
	if entry.ID == "" {
		t.Error("expected non-empty ID after Upsert")
	}

	// Verify CreatedAt was set
	if entry.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt after Upsert")
	}

	// Verify it's persisted
	fetched, err := repo.GetByRole(context.Background(), "tenant-1", "engineer")
	if err != nil {
		t.Fatalf("GetByRole failed: %v", err)
	}

	if fetched.TenantID != entry.TenantID {
		t.Errorf("tenant_id: got %q, want %q", fetched.TenantID, entry.TenantID)
	}
	if fetched.Role != entry.Role {
		t.Errorf("role: got %q, want %q", fetched.Role, entry.Role)
	}
	if len(fetched.Scopes) != 2 {
		t.Errorf("scopes count: got %d, want 2", len(fetched.Scopes))
	}
}

func TestUpsert_Update(t *testing.T) {
	pool := newTestPool(t)
	cleanRoles(t, pool)
	repo := NewPostgresRepository(pool)

	// Insert initial entry
	initial := &RoleEntry{
		TenantID: "tenant-1",
		Role:     "engineer",
		Scopes:   []string{"scope-a", "scope-b"},
	}
	if err := repo.Upsert(context.Background(), initial); err != nil {
		t.Fatalf("initial Upsert failed: %v", err)
	}

	originalID := initial.ID
	originalCreatedAt := initial.CreatedAt
	time.Sleep(10 * time.Millisecond) // Ensure created_at would differ if replaced

	// Upsert again with same tenant+role, different scopes
	updated := &RoleEntry{
		TenantID: "tenant-1",
		Role:     "engineer",
		Scopes:   []string{"scope-c", "scope-d", "scope-e"},
	}
	if err := repo.Upsert(context.Background(), updated); err != nil {
		t.Fatalf("update Upsert failed: %v", err)
	}

	// Verify ID and CreatedAt were preserved (not replaced)
	if updated.ID != originalID {
		t.Errorf("ID changed on update: got %q, want %q", updated.ID, originalID)
	}
	if !updated.CreatedAt.Equal(originalCreatedAt) {
		t.Errorf("CreatedAt changed on update: got %v, want %v", updated.CreatedAt, originalCreatedAt)
	}

	// Verify scopes were replaced
	fetched, err := repo.GetByRole(context.Background(), "tenant-1", "engineer")
	if err != nil {
		t.Fatalf("GetByRole failed: %v", err)
	}
	if len(fetched.Scopes) != 3 {
		t.Fatalf("scopes count after update: got %d, want 3", len(fetched.Scopes))
	}
	if fetched.Scopes[0] != "scope-c" {
		t.Errorf("first scope: got %q, want scope-c", fetched.Scopes[0])
	}
}

func TestUpsert_WithBudget_PerMember(t *testing.T) {
	pool := newTestPool(t)
	cleanRoles(t, pool)
	repo := NewPostgresRepository(pool)

	entry := &RoleEntry{
		TenantID: "tenant-1",
		Role:     "data-scientist",
		Scopes:   []string{"inference:invoke"},
		Budget: &BudgetAssignment{
			LimitUSD:     100.0,
			ResetCadence: "weekly",
			Pooled:       false,
		},
	}

	err := repo.Upsert(context.Background(), entry)
	if err != nil {
		t.Fatalf("Upsert with budget failed: %v", err)
	}

	fetched, err := repo.GetByRole(context.Background(), "tenant-1", "data-scientist")
	if err != nil {
		t.Fatalf("GetByRole failed: %v", err)
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
	if fetched.Budget.Pooled {
		t.Error("expected Pooled=false for per-member budget")
	}
}

func TestUpsert_WithBudget_Pooled(t *testing.T) {
	pool := newTestPool(t)
	cleanRoles(t, pool)
	repo := NewPostgresRepository(pool)

	entry := &RoleEntry{
		TenantID: "tenant-1",
		Role:     "shared-pool",
		Scopes:   []string{"scope"},
		Budget: &BudgetAssignment{
			LimitUSD:     500.0,
			ResetCadence: "monthly",
			Pooled:       true,
		},
	}

	err := repo.Upsert(context.Background(), entry)
	if err != nil {
		t.Fatalf("Upsert with pooled budget failed: %v", err)
	}

	fetched, err := repo.GetByRole(context.Background(), "tenant-1", "shared-pool")
	if err != nil {
		t.Fatalf("GetByRole failed: %v", err)
	}

	if fetched.Budget == nil {
		t.Fatal("expected non-nil budget")
	}
	if !fetched.Budget.Pooled {
		t.Error("expected Pooled=true for shared budget")
	}
	if fetched.Budget.LimitUSD != 500.0 {
		t.Errorf("budget.limit_usd: got %f, want 500.0", fetched.Budget.LimitUSD)
	}
}

func TestUpsert_BudgetPreservedOnUpdate(t *testing.T) {
	pool := newTestPool(t)
	cleanRoles(t, pool)
	repo := NewPostgresRepository(pool)

	// Insert with budget
	initial := &RoleEntry{
		TenantID: "tenant-1",
		Role:     "engineer",
		Scopes:   []string{"scope-a"},
		Budget:   &BudgetAssignment{LimitUSD: 50.0, ResetCadence: "weekly"},
	}
	if err := repo.Upsert(context.Background(), initial); err != nil {
		t.Fatalf("initial Upsert failed: %v", err)
	}

	// Update scopes only (no budget field)
	updated := &RoleEntry{
		TenantID: "tenant-1",
		Role:     "engineer",
		Scopes:   []string{"scope-b", "scope-c"},
	}
	if err := repo.Upsert(context.Background(), updated); err != nil {
		t.Fatalf("update Upsert failed: %v", err)
	}

	// Verify budget was preserved (COALESCE logic in SQL)
	fetched, err := repo.GetByRole(context.Background(), "tenant-1", "engineer")
	if err != nil {
		t.Fatalf("GetByRole failed: %v", err)
	}

	if fetched.Budget == nil {
		t.Fatal("expected budget to be preserved on update")
	}
	if fetched.Budget.LimitUSD != 50.0 {
		t.Errorf("budget.limit_usd: got %f, want 50.0 (preserved)", fetched.Budget.LimitUSD)
	}
}

// ── GetByRole ─────────────────────────────────────────────────────────────────

func TestGetByRole_NotFound(t *testing.T) {
	pool := newTestPool(t)
	cleanRoles(t, pool)
	repo := NewPostgresRepository(pool)

	_, err := repo.GetByRole(context.Background(), "tenant-1", "nonexistent-role")
	if err != ErrRoleNotFound {
		t.Errorf("expected ErrRoleNotFound, got %v", err)
	}
}

func TestGetByRole_DifferentTenantNotFound(t *testing.T) {
	pool := newTestPool(t)
	cleanRoles(t, pool)
	repo := NewPostgresRepository(pool)

	entry := &RoleEntry{TenantID: "tenant-1", Role: "engineer", Scopes: []string{"scope"}}
	if err := repo.Upsert(context.Background(), entry); err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	// Try to fetch from different tenant
	_, err := repo.GetByRole(context.Background(), "tenant-2", "engineer")
	if err != ErrRoleNotFound {
		t.Errorf("expected ErrRoleNotFound for different tenant, got %v", err)
	}
}

// ── ListByTenant ──────────────────────────────────────────────────────────────

func TestListByTenant_OrderedByRole(t *testing.T) {
	pool := newTestPool(t)
	cleanRoles(t, pool)
	repo := NewPostgresRepository(pool)

	// Insert in reverse alphabetical order
	entries := []*RoleEntry{
		{TenantID: "t1", Role: "zebra", Scopes: []string{"z"}},
		{TenantID: "t1", Role: "alpha", Scopes: []string{"a"}},
		{TenantID: "t1", Role: "beta", Scopes: []string{"b"}},
	}
	for _, e := range entries {
		if err := repo.Upsert(context.Background(), e); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
	}

	// List should be ordered by role name
	roles, err := repo.ListByTenant(context.Background(), "t1", 0, 0)
	if err != nil {
		t.Fatalf("ListByTenant failed: %v", err)
	}

	if len(roles) != 3 {
		t.Fatalf("roles count: got %d, want 3", len(roles))
	}
	if roles[0].Role != "alpha" || roles[1].Role != "beta" || roles[2].Role != "zebra" {
		t.Errorf("roles not alphabetically ordered: got [%s %s %s]", roles[0].Role, roles[1].Role, roles[2].Role)
	}
}

func TestListByTenant_Pagination(t *testing.T) {
	pool := newTestPool(t)
	cleanRoles(t, pool)
	repo := NewPostgresRepository(pool)

	// Create 5 roles
	for i := 0; i < 5; i++ {
		e := &RoleEntry{TenantID: "t1", Role: string(rune('a' + i)), Scopes: []string{"scope"}}
		if err := repo.Upsert(context.Background(), e); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
	}

	// Fetch first 2
	page1, err := repo.ListByTenant(context.Background(), "t1", 2, 0)
	if err != nil {
		t.Fatalf("ListByTenant page 1 failed: %v", err)
	}
	if len(page1) != 2 {
		t.Errorf("page 1 count: got %d, want 2", len(page1))
	}

	// Fetch next 2 (offset 2)
	page2, err := repo.ListByTenant(context.Background(), "t1", 2, 2)
	if err != nil {
		t.Fatalf("ListByTenant page 2 failed: %v", err)
	}
	if len(page2) != 2 {
		t.Errorf("page 2 count: got %d, want 2", len(page2))
	}

	// Verify no overlap
	if page1[0].ID == page2[0].ID {
		t.Error("pagination returned duplicate role IDs")
	}

	// Verify ordering maintained
	if page1[0].Role != "a" || page1[1].Role != "b" {
		t.Errorf("page 1 roles: got [%s %s], want [a b]", page1[0].Role, page1[1].Role)
	}
	if page2[0].Role != "c" || page2[1].Role != "d" {
		t.Errorf("page 2 roles: got [%s %s], want [c d]", page2[0].Role, page2[1].Role)
	}
}

func TestListByTenant_TenantIsolation(t *testing.T) {
	pool := newTestPool(t)
	cleanRoles(t, pool)
	repo := NewPostgresRepository(pool)

	e1 := &RoleEntry{TenantID: "t1", Role: "role-a", Scopes: []string{"scope"}}
	e2 := &RoleEntry{TenantID: "t2", Role: "role-b", Scopes: []string{"scope"}}
	e3 := &RoleEntry{TenantID: "t1", Role: "role-c", Scopes: []string{"scope"}}

	for _, e := range []*RoleEntry{e1, e2, e3} {
		if err := repo.Upsert(context.Background(), e); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
	}

	// List for t1 only
	roles, err := repo.ListByTenant(context.Background(), "t1", 0, 0)
	if err != nil {
		t.Fatalf("ListByTenant failed: %v", err)
	}

	if len(roles) != 2 {
		t.Errorf("t1 roles count: got %d, want 2", len(roles))
	}
	for _, r := range roles {
		if r.TenantID != "t1" {
			t.Errorf("found role from tenant %q in t1 results", r.TenantID)
		}
	}
}

func TestListByTenant_Empty(t *testing.T) {
	pool := newTestPool(t)
	cleanRoles(t, pool)
	repo := NewPostgresRepository(pool)

	roles, err := repo.ListByTenant(context.Background(), "nonexistent", 0, 0)
	if err != nil {
		t.Fatalf("ListByTenant failed: %v", err)
	}

	if len(roles) != 0 {
		t.Errorf("expected 0 roles, got %d", len(roles))
	}
}

// ── Delete ────────────────────────────────────────────────────────────────────

func TestDelete_Success(t *testing.T) {
	pool := newTestPool(t)
	cleanRoles(t, pool)
	repo := NewPostgresRepository(pool)

	entry := &RoleEntry{TenantID: "t1", Role: "engineer", Scopes: []string{"scope"}}
	if err := repo.Upsert(context.Background(), entry); err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	// Delete it
	err := repo.Delete(context.Background(), "t1", "engineer")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify it's gone
	_, err = repo.GetByRole(context.Background(), "t1", "engineer")
	if err != ErrRoleNotFound {
		t.Errorf("expected ErrRoleNotFound after delete, got %v", err)
	}
}

func TestDelete_NotFound(t *testing.T) {
	pool := newTestPool(t)
	cleanRoles(t, pool)
	repo := NewPostgresRepository(pool)

	err := repo.Delete(context.Background(), "t1", "nonexistent-role")
	if err != ErrRoleNotFound {
		t.Errorf("expected ErrRoleNotFound, got %v", err)
	}
}

func TestDelete_WrongTenant(t *testing.T) {
	pool := newTestPool(t)
	cleanRoles(t, pool)
	repo := NewPostgresRepository(pool)

	entry := &RoleEntry{TenantID: "t1", Role: "engineer", Scopes: []string{"scope"}}
	if err := repo.Upsert(context.Background(), entry); err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	// Try to delete from different tenant
	err := repo.Delete(context.Background(), "t2", "engineer")
	if err != ErrRoleNotFound {
		t.Errorf("expected ErrRoleNotFound when tenant mismatch, got %v", err)
	}

	// Verify it still exists in t1
	_, err = repo.GetByRole(context.Background(), "t1", "engineer")
	if err != nil {
		t.Error("role should still exist after failed delete from wrong tenant")
	}
}

// ── CountByTenant ─────────────────────────────────────────────────────────────

func TestCountByTenant(t *testing.T) {
	pool := newTestPool(t)
	cleanRoles(t, pool)
	repo := NewPostgresRepository(pool)

	// Create 3 roles for t1, 1 for t2
	e1 := &RoleEntry{TenantID: "t1", Role: "role-a", Scopes: []string{"scope"}}
	e2 := &RoleEntry{TenantID: "t1", Role: "role-b", Scopes: []string{"scope"}}
	e3 := &RoleEntry{TenantID: "t1", Role: "role-c", Scopes: []string{"scope"}}
	e4 := &RoleEntry{TenantID: "t2", Role: "role-d", Scopes: []string{"scope"}}

	for _, e := range []*RoleEntry{e1, e2, e3, e4} {
		if err := repo.Upsert(context.Background(), e); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
	}

	count, err := repo.CountByTenant(context.Background(), "t1")
	if err != nil {
		t.Fatalf("CountByTenant failed: %v", err)
	}

	if count != 3 {
		t.Errorf("count: got %d, want 3", count)
	}
}

func TestCountByTenant_Zero(t *testing.T) {
	pool := newTestPool(t)
	cleanRoles(t, pool)
	repo := NewPostgresRepository(pool)

	count, err := repo.CountByTenant(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("CountByTenant failed: %v", err)
	}

	if count != 0 {
		t.Errorf("count: got %d, want 0", count)
	}
}

// ── PurgeScopeFromAll ─────────────────────────────────────────────────────────

func TestPurgeScopeFromAll_RemovesScopeFromMultipleRoles(t *testing.T) {
	pool := newTestPool(t)
	cleanRoles(t, pool)
	repo := NewPostgresRepository(pool)

	// Create roles with "deprecated:scope" mixed in
	e1 := &RoleEntry{TenantID: "t1", Role: "role-a", Scopes: []string{"good:scope", "deprecated:scope"}}
	e2 := &RoleEntry{TenantID: "t2", Role: "role-b", Scopes: []string{"deprecated:scope", "another:scope"}}
	e3 := &RoleEntry{TenantID: "t1", Role: "role-c", Scopes: []string{"only:good"}}

	for _, e := range []*RoleEntry{e1, e2, e3} {
		if err := repo.Upsert(context.Background(), e); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
	}

	// Purge "deprecated:scope"
	err := repo.PurgeScopeFromAll(context.Background(), "deprecated:scope")
	if err != nil {
		t.Fatalf("PurgeScopeFromAll failed: %v", err)
	}

	// Verify e1 no longer has it
	fetched1, _ := repo.GetByRole(context.Background(), "t1", "role-a")
	if contains(fetched1.Scopes, "deprecated:scope") {
		t.Error("role-a still contains deprecated:scope after purge")
	}
	if !contains(fetched1.Scopes, "good:scope") {
		t.Error("role-a lost good:scope during purge")
	}

	// Verify e2 no longer has it
	fetched2, _ := repo.GetByRole(context.Background(), "t2", "role-b")
	if contains(fetched2.Scopes, "deprecated:scope") {
		t.Error("role-b still contains deprecated:scope after purge")
	}
	if !contains(fetched2.Scopes, "another:scope") {
		t.Error("role-b lost another:scope during purge")
	}

	// Verify e3 unchanged
	fetched3, _ := repo.GetByRole(context.Background(), "t1", "role-c")
	if len(fetched3.Scopes) != 1 || fetched3.Scopes[0] != "only:good" {
		t.Errorf("role-c scopes changed unexpectedly: got %v", fetched3.Scopes)
	}
}

func TestPurgeScopeFromAll_NoMatch(t *testing.T) {
	pool := newTestPool(t)
	cleanRoles(t, pool)
	repo := NewPostgresRepository(pool)

	e1 := &RoleEntry{TenantID: "t1", Role: "role-a", Scopes: []string{"scope-a"}}
	if err := repo.Upsert(context.Background(), e1); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	// Purge non-existent scope
	err := repo.PurgeScopeFromAll(context.Background(), "nonexistent:scope")
	if err != nil {
		t.Fatalf("PurgeScopeFromAll failed: %v", err)
	}

	// Verify e1 unchanged
	fetched, _ := repo.GetByRole(context.Background(), "t1", "role-a")
	if len(fetched.Scopes) != 1 || fetched.Scopes[0] != "scope-a" {
		t.Errorf("role scopes changed unexpectedly: got %v", fetched.Scopes)
	}
}

func TestPurgeScopeFromAll_EmptyResultingScopes(t *testing.T) {
	pool := newTestPool(t)
	cleanRoles(t, pool)
	repo := NewPostgresRepository(pool)

	// Create role with only the scope that will be purged
	e1 := &RoleEntry{TenantID: "t1", Role: "role-a", Scopes: []string{"to-purge"}}
	if err := repo.Upsert(context.Background(), e1); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	// Purge the only scope
	err := repo.PurgeScopeFromAll(context.Background(), "to-purge")
	if err != nil {
		t.Fatalf("PurgeScopeFromAll failed: %v", err)
	}

	// Verify scopes is now empty array (not null)
	fetched, _ := repo.GetByRole(context.Background(), "t1", "role-a")
	if fetched.Scopes == nil {
		t.Error("scopes should be empty array, not nil")
	}
	if len(fetched.Scopes) != 0 {
		t.Errorf("expected empty scopes after purge, got %v", fetched.Scopes)
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
