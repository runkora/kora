package example

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres integration tests for the example repository. They run only when
// ORBIT_TEST_DB_URL points at a database with the example tables migrated
// (the local docker-compose Postgres after `make migrate-up`); otherwise they
// skip. Each test isolates itself under random ids and cleans up after.
//
//	ORBIT_TEST_DB_URL='postgres://orbit:orbit@localhost:5432/orbit?sslmode=disable' \
//	  go test ./internal/example/ -run Postgres
func testRepo(t *testing.T) *PostgresRepository {
	t.Helper()
	dsn := os.Getenv("ORBIT_TEST_DB_URL")
	if dsn == "" {
		t.Skip("set ORBIT_TEST_DB_URL to run example Postgres integration tests")
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
	t.Cleanup(pool.Close)
	return NewPostgresRepository(pool)
}

func newTestUser() *User {
	id := "user-" + uuid.NewString()
	return &User{ID: id, Email: id + "@example.com"}
}

func TestPostgres_CreateAndGetUser(t *testing.T) {
	r := testRepo(t)
	ctx := context.Background()
	u := newTestUser()
	t.Cleanup(func() { _, _ = r.pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, u.ID) })

	if err := r.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	got, err := r.GetUserByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if got.Email != u.Email {
		t.Errorf("email round-trip = %q, want %q", got.Email, u.Email)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should be defaulted by the database")
	}

	// A second insert of the same id maps to ErrDuplicateKey.
	if err := r.CreateUser(ctx, u); !errors.Is(err, ErrDuplicateKey) {
		t.Errorf("duplicate insert err = %v, want ErrDuplicateKey", err)
	}
}

func TestPostgres_GetUserNotFound(t *testing.T) {
	r := testRepo(t)
	if _, err := r.GetUserByID(context.Background(), "does-not-exist"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("GetUserByID(missing) err = %v, want ErrUserNotFound", err)
	}
}

func TestPostgres_CreateUserWithProfile(t *testing.T) {
	r := testRepo(t)
	ctx := context.Background()
	u := newTestUser()
	p := &Profile{UserID: u.ID, Name: "Ada"}
	t.Cleanup(func() { _, _ = r.pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, u.ID) })

	if err := r.CreateUserWithProfile(ctx, u, p); err != nil {
		t.Fatalf("CreateUserWithProfile: %v", err)
	}
	if _, err := r.GetUserByID(ctx, u.ID); err != nil {
		t.Errorf("user should exist after transactional insert: %v", err)
	}
}
