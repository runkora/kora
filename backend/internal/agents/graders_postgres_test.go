package agents

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/runkora/kora/backend/internal/platform/declarative"
)

// A Grader upserts by tenant and name, keeps its id across a recreated resource, stores each
// version once, and leaves its versions behind when deleted.
func TestPostgres_DeclaredGraders(t *testing.T) {
	r, tenant := testEnvironmentRepo(t)
	ctx := context.Background()
	at := testAgentType(tenant)
	if err := r.CreateAgentType(ctx, at); err != nil {
		t.Fatalf("CreateAgentType: %v", err)
	}
	v1, v2 := "test-"+uuid.NewString(), "test-"+uuid.NewString()
	t.Cleanup(func() {
		_, _ = r.pool.Exec(ctx, `DELETE FROM graders WHERE tenant_id = $1`, tenant)
		_, _ = r.pool.Exec(ctx, `DELETE FROM grader_versions WHERE version = ANY($1)`, []string{v1, v2})
	})

	g := Grader{TenantID: tenant, Name: "reviews", AgentTypeID: at.ID, Description: "Checks reviews.", MinPassRate: new(90),
		CurrentVersion: v1, ResourceUID: "uid-1", ResourceGeneration: 1, Repository: "github.com/acme/agents"}
	created, err := r.PutGrader(ctx, g, []byte(`{"questions":[]}`))
	if err != nil {
		t.Fatalf("PutGrader: %v", err)
	}
	if created.ID == "" || !sameGrader(*created, g) {
		t.Errorf("created = %+v, want %+v", created, g)
	}

	// The same version again, from a recreated resource with no minimum and no repository.
	g.ResourceUID, g.ResourceGeneration, g.MinPassRate, g.Repository = "uid-2", 1, nil, ""
	if _, err := r.PutGrader(ctx, g, []byte(`{"questions":["changed"]}`)); err != nil {
		t.Fatalf("PutGrader again: %v", err)
	}
	got, err := r.GetGraderByName(ctx, tenant, "reviews")
	if err != nil {
		t.Fatalf("GetGraderByName: %v", err)
	}
	if got.ID != created.ID || got.ResourceUID != "uid-2" || got.MinPassRate != nil || got.Repository != "" {
		t.Errorf("takeover = %+v, want id %s kept and the new fields", got, created.ID)
	}
	var spec string
	if err := r.pool.QueryRow(ctx, `SELECT spec::text FROM grader_versions WHERE version = $1`, v1).Scan(&spec); err != nil || spec != `{"questions": []}` {
		t.Errorf("version spec = %q, %v, want the first write kept", spec, err)
	}

	g.CurrentVersion = v2
	if _, err := r.PutGrader(ctx, g, []byte(`{}`)); err != nil {
		t.Fatalf("PutGrader v2: %v", err)
	}
	if got, _ := r.GetGraderByName(ctx, tenant, "reviews"); got.CurrentVersion != v2 {
		t.Errorf("current version = %q, want %q", got.CurrentVersion, v2)
	}
	list, err := r.ListDeclaredGraders(ctx, tenant)
	if err != nil || !slices.Equal(list, []declarative.Declared{{Name: "reviews", UID: "uid-2"}}) {
		t.Errorf("ListDeclaredGraders = %+v, %v", list, err)
	}

	if err := r.DeleteGrader(ctx, tenant, "reviews"); err != nil {
		t.Fatalf("DeleteGrader: %v", err)
	}
	if _, err := r.GetGraderByName(ctx, tenant, "reviews"); !errors.Is(err, ErrNotFound) {
		t.Errorf("after delete = %v, want ErrNotFound", err)
	}
	var versions int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM grader_versions WHERE version = ANY($1)`, []string{v1, v2}).Scan(&versions); err != nil || versions != 2 {
		t.Errorf("versions after delete = %d, %v, want both kept", versions, err)
	}
	if err := r.DeleteGrader(ctx, tenant, "reviews"); err != nil {
		t.Errorf("deleting a missing grader = %v", err)
	}

	// A hard-deleted agent takes its Grader with it.
	if _, err := r.PutGrader(ctx, g, []byte(`{}`)); err != nil {
		t.Fatalf("PutGrader: %v", err)
	}
	if err := r.DeleteAgentType(ctx, at.ID); err != nil {
		t.Fatalf("DeleteAgentType: %v", err)
	}
	if _, err := r.GetGraderByName(ctx, tenant, "reviews"); !errors.Is(err, ErrNotFound) {
		t.Errorf("after the agent's delete = %v, want ErrNotFound", err)
	}
}
