package inferencegw

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/runkora/kora/backend/internal/platform/declarative"
)

// Postgres tests for the virtual model source column and the declared write
// path. They skip unless KORA_TEST_DB_URL is set. Each test works under its
// own random family and resource names.
func testCatalogRepo(t *testing.T) (*PostgresCatalogRepository, *pgxpool.Pool, string) {
	t.Helper()
	dsn := os.Getenv("KORA_TEST_DB_URL")
	if dsn == "" {
		t.Skip("set KORA_TEST_DB_URL to run catalog Postgres integration tests")
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
	family := "t" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM virtual_models WHERE family = $1`, family)
		pool.Close()
	})
	return NewPostgresCatalogRepository(pool), pool, family
}

// testVM builds a virtual model. A non-empty resource makes it declared, with
// a resource name unique to the family.
func testVM(family, name, resource, uid string, routes ...string) VirtualModel {
	vm := VirtualModel{
		ID: uuid.NewString(), Family: family, Name: name, DisplayName: name,
		Strategy: VirtualStrategyPriority, Enabled: true, ResourceUID: uid,
	}
	if resource != "" {
		vm.ResourceName = family + "-" + resource
	}
	for i, model := range routes {
		vm.Routes = append(vm.Routes, VirtualModelRoute{
			ID: uuid.NewString(), Provider: ProviderBedrock, ModelID: family + "-" + model, Priority: i + 1, Enabled: true,
		})
	}
	return vm
}

// rowsOf returns the family's rows keyed by name.
func rowsOf(t *testing.T, repo *PostgresCatalogRepository, family string) map[string]VirtualModel {
	t.Helper()
	all, err := repo.ListVirtualModels(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	out := map[string]VirtualModel{}
	for _, vm := range all {
		if vm.Family == family {
			out[vm.Name] = vm
		}
	}
	return out
}

func TestPostgres_VirtualModelSourceDefaultsToAPI(t *testing.T) {
	repo, pool, family := testCatalogRepo(t)
	ctx := context.Background()
	vm := testVM(family, "a", "", "", "m1")
	if err := repo.CreateVirtualModel(ctx, vm); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := repo.GetVirtualModel(ctx, vm.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Source != declarative.SourceAPI || got.ResourceUID != "" || got.ResourceName != "" {
		t.Errorf("row = %+v, want an API row with no resource", got)
	}
	_, err = pool.Exec(ctx, `INSERT INTO virtual_models (id, family, name, display_name, strategy, source, resource_uid)
		VALUES ($1, $2, 'b', 'b', 'priority', 'crd', 'uid')`, uuid.NewString(), family)
	if err == nil {
		t.Error("a declared row without a resource name was accepted")
	}
}

func TestPostgres_DeclaredUpsert(t *testing.T) {
	repo, _, family := testCatalogRepo(t)
	ctx := context.Background()
	uid := uuid.NewString()

	id, changed, err := repo.UpsertDeclaredVirtualModel(ctx, testVM(family, "a", "res", uid, "m1"))
	if err != nil || !changed {
		t.Fatalf("first upsert: changed=%v err=%v", changed, err)
	}
	first := rowsOf(t, repo, family)["a"]
	if first.ID != id || first.Source != declarative.SourceCRD || first.ResourceUID != uid || first.ResourceName != family+"-res" || len(first.Routes) != 1 {
		t.Fatalf("row = %+v, id %s", first, id)
	}

	again, changed, err := repo.UpsertDeclaredVirtualModel(ctx, testVM(family, "a", "res", uid, "m1"))
	if err != nil || changed || again != id {
		t.Fatalf("same spec: id=%s changed=%v err=%v, want %s and no write", again, changed, err, id)
	}

	if _, changed, err = repo.UpsertDeclaredVirtualModel(ctx, testVM(family, "b", "res", uid, "m2", "m3")); err != nil || !changed {
		t.Fatalf("new spec: changed=%v err=%v", changed, err)
	}
	rows := rowsOf(t, repo, family)
	renamed, ok := rows["b"]
	if _, stale := rows["a"]; stale || !ok {
		t.Fatalf("rows after rename = %v", rows)
	}
	if renamed.ID != id || len(renamed.Routes) != 2 || renamed.Routes[0].ModelID != family+"-m2" {
		t.Errorf("renamed row = %+v, want id %s with the two new routes", renamed, id)
	}

	// A resource recreated under the same name keeps the row and takes the new UID.
	recreated := uuid.NewString()
	if got, _, err := repo.UpsertDeclaredVirtualModel(ctx, testVM(family, "b", "res", recreated, "m2", "m3")); err != nil || got != id {
		t.Fatalf("recreated: id=%s err=%v, want %s", got, err, id)
	}
	if got := rowsOf(t, repo, family)["b"]; got.ResourceUID != recreated {
		t.Errorf("uid = %s, want %s", got.ResourceUID, recreated)
	}
}

func TestPostgres_DeclaredUpsertConflicts(t *testing.T) {
	repo, _, family := testCatalogRepo(t)
	ctx := context.Background()
	if err := repo.CreateVirtualModel(ctx, testVM(family, "api", "", "", "m1")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, _, err := repo.UpsertDeclaredVirtualModel(ctx, testVM(family, "held", "one", uuid.NewString())); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	cases := map[string]struct {
		vm   VirtualModel
		want error
	}{
		"API key":            {testVM(family, "api", "two", uuid.NewString()), ErrVirtualModelNameConflict},
		"other resource key": {testVM(family, "held", "two", uuid.NewString()), ErrVirtualModelNameConflict},
		"taken route":        {testVM(family, "free", "two", uuid.NewString(), "m1"), ErrVirtualModelRouteTaken},
	}
	for name, tc := range cases {
		if _, _, err := repo.UpsertDeclaredVirtualModel(ctx, tc.vm); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", name, err, tc.want)
		}
	}
	if _, ok := rowsOf(t, repo, family)["free"]; ok {
		t.Error("a refused upsert wrote a row")
	}
}

func TestPostgres_APIWritesRefuseDeclaredRows(t *testing.T) {
	repo, _, family := testCatalogRepo(t)
	ctx := context.Background()
	if _, _, err := repo.UpsertDeclaredVirtualModel(ctx, testVM(family, "a", "res", uuid.NewString())); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	declared := rowsOf(t, repo, family)["a"]

	update := testVM(family, "a", "", "")
	update.ID = declared.ID
	if _, err := repo.UpdateVirtualModel(ctx, update); !errors.Is(err, declarative.ErrManagedByResource) {
		t.Errorf("update: err = %v, want ErrManagedByResource", err)
	}
	if err := repo.DeleteVirtualModel(ctx, declared.ID); !errors.Is(err, declarative.ErrManagedByResource) {
		t.Errorf("delete: err = %v, want ErrManagedByResource", err)
	}
	if err := repo.CreateVirtualModel(ctx, testVM(family, "a", "", "")); !errors.Is(err, ErrVirtualModelExists) {
		t.Errorf("create on a declared key: err = %v, want ErrVirtualModelExists", err)
	}
	if err := repo.CreateVirtualModel(ctx, testVM(family, "b", "", "")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := repo.CreateVirtualModel(ctx, testVM(family, "b", "", "")); !errors.Is(err, ErrVirtualModelExists) {
		t.Errorf("create on an API key: err = %v, want ErrVirtualModelExists", err)
	}
	if err := repo.DeleteVirtualModel(ctx, uuid.NewString()); !errors.Is(err, ErrVirtualModelNotFound) {
		t.Errorf("delete unknown: err = %v, want ErrVirtualModelNotFound", err)
	}
}

func TestPostgres_APIWritesRefuseTakenRouteAndKey(t *testing.T) {
	repo, _, family := testCatalogRepo(t)
	ctx := context.Background()
	if _, _, err := repo.UpsertDeclaredVirtualModel(ctx, testVM(family, "declared", "res", uuid.NewString(), "m1")); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := repo.CreateVirtualModel(ctx, testVM(family, "a", "", "", "m1")); !errors.Is(err, ErrVirtualModelRouteTaken) {
		t.Errorf("create on a taken route: err = %v, want ErrVirtualModelRouteTaken", err)
	}
	api := testVM(family, "a", "", "", "m2")
	if err := repo.CreateVirtualModel(ctx, api); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := repo.CreateVirtualModel(ctx, testVM(family, "b", "", "", "m3")); err != nil {
		t.Fatalf("create b: %v", err)
	}

	moved := testVM(family, "a", "", "", "m1")
	moved.ID = api.ID
	if _, err := repo.UpdateVirtualModel(ctx, moved); !errors.Is(err, ErrVirtualModelRouteTaken) {
		t.Errorf("update onto a taken route: err = %v, want ErrVirtualModelRouteTaken", err)
	}
	for _, name := range []string{"declared", "b"} {
		renamed := testVM(family, name, "", "", "m2")
		renamed.ID = api.ID
		if _, err := repo.UpdateVirtualModel(ctx, renamed); !errors.Is(err, ErrVirtualModelExists) {
			t.Errorf("rename onto %s: err = %v, want ErrVirtualModelExists", name, err)
		}
	}
	if _, err := repo.UpdateVirtualModel(ctx, api); err != nil {
		t.Errorf("update keeping its own route: %v", err)
	}
}

func TestPostgres_DeclaredDeleteAndList(t *testing.T) {
	repo, _, family := testCatalogRepo(t)
	ctx := context.Background()
	keptUID, goneUID := uuid.NewString(), uuid.NewString()
	for name, uid := range map[string]string{"kept": keptUID, "gone": goneUID} {
		if _, _, err := repo.UpsertDeclaredVirtualModel(ctx, testVM(family, name, name, uid)); err != nil {
			t.Fatalf("upsert %s: %v", name, err)
		}
	}
	if err := repo.CreateVirtualModel(ctx, testVM(family, "api", "", "")); err != nil {
		t.Fatalf("create: %v", err)
	}

	listed, err := repo.ListDeclaredVirtualModels(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	found := map[string]string{}
	for _, d := range listed {
		found[d.Name] = d.UID
	}
	if found[family+"-kept"] != keptUID || found[family+"-gone"] != goneUID {
		t.Errorf("listed = %v, want both declared resources with their UIDs", listed)
	}

	if deleted, err := repo.DeleteDeclaredVirtualModel(ctx, family+"-gone"); err != nil || !deleted {
		t.Fatalf("delete: deleted=%v err=%v", deleted, err)
	}
	if deleted, err := repo.DeleteDeclaredVirtualModel(ctx, family+"-gone"); err != nil || deleted {
		t.Errorf("delete again: deleted=%v err=%v, want nothing deleted", deleted, err)
	}
	if deleted, _ := repo.DeleteDeclaredVirtualModel(ctx, "api"); deleted {
		t.Error("a declared delete removed a row it does not own")
	}
	rows := rowsOf(t, repo, family)
	if _, ok := rows["gone"]; ok {
		t.Error("the deleted resource's row survived")
	}
	if _, ok := rows["kept"]; !ok {
		t.Error("another resource's row was deleted")
	}
	if _, ok := rows["api"]; !ok {
		t.Error("an API row was deleted")
	}
}
