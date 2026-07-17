# Backend Golden Path

Add a backend feature end to end. Every snippet is lifted from live code, so read [`internal/example/`](../../backend/internal/example/) (the reference feature) and [`internal/triggers/`](../../backend/internal/triggers/) (a real one) alongside this.

A feature package owns its whole stack and never imports another feature, sharing only through `internal/platform/`. See [code-organization.md](../code-organization.md).

## Package layout

```
internal/{feature}/
├── models.go       # feature types + sentinel errors
├── repository.go   # storage interface
├── postgres.go     # pgx/v5 implementation
├── service.go      # business logic
└── handlers.go     # HTTP transport (the api.Controller)
```

`internal/example/` implements exactly this layout, with tests for each layer. Read it alongside this guide.

## 1. Models and sentinel errors

Define package-level sentinel errors; callers match with `errors.Is`. From [`internal/example/repository.go`](../../backend/internal/example/repository.go):

```go
var (
	ErrUserNotFound = errors.New("user not found")
	ErrDuplicateKey = errors.New("duplicate key violation")
)

type User struct {
	ID        string
	Email     string
	CreatedAt time.Time
}
```

## 2. Repository interface + Postgres implementation

Data access is explicit SQL through the shared `*pgxpool.Pool`, with no ORM and no `database/sql`. Map `pgx.ErrNoRows` to your sentinel; wrap everything else with `%w`.

```go
type Repository interface {
	GetUserByID(ctx context.Context, id string) (*User, error)
	CreateUser(ctx context.Context, user *User) error
}

type PostgresRepository struct{ pool *pgxpool.Pool }

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) GetUserByID(ctx context.Context, id string) (*User, error) {
	var user User
	err := r.pool.QueryRow(ctx, `SELECT id, email, created_at FROM users WHERE id = $1`, id).
		Scan(&user.ID, &user.Email, &user.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("query user: %w", err)
	}
	return &user, nil
}
```

Detect a unique-violation with `pgconn.PgError` code `23505` and return `ErrDuplicateKey` (full helper in `repository.go`). Multi-statement writes go through `pool.Begin` / `tx.Commit` with `defer tx.Rollback(ctx)`.

## 3. The controller (HTTP transport)

Routes implement `api.Controller`, not gin/chi/echo. The interface is `GroupedBy()`, `Middleware()`, `Boot(*api.Mux)`. From [`internal/example/handlers.go`](../../backend/internal/example/handlers.go):

```go
type Controller struct {
	logger logging.Logger
	repo   Repository
}

func NewController(logger logging.Logger, repo Repository) *Controller {
	return &Controller{logger: logger, repo: repo}
}

func (c *Controller) GroupedBy() string { return "/example" }        // route prefix

func (c *Controller) Middleware() mw.MiddlewareHandler { return nil } // controller-scoped middleware, or nil

func (c *Controller) Boot(mux *api.Mux) {
	mux.Handle("GET /users/{id}", c.handleGetUser)  // Go 1.22 ServeMux patterns
	mux.Handle("POST /users", c.handleCreateUser)
}
```

`Middleware()` returns extra middleware for this controller's routes (e.g. auth) or `nil`; the policy service chains `AuthMiddleware` + `ScopeEnforcementMiddleware` here.

### Handlers

Never touch `r.Body` or `w` directly. Decode with `api.DecodeJSON` (returns `false` and writes the error response on failure), respond with `api.JSON` / `api.ErrorResponse`. All log calls take `ctx` first.

```go
func (c *Controller) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req CreateUserRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	if req.ID == "" || req.Email == "" {
		apiErr := api.ValidationError(api.ErrCodeMissingField, "id and email fields are required", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}

	user := &User{ID: req.ID, Email: req.Email, CreatedAt: time.Now()}
	if err := c.repo.CreateUser(ctx, user); err != nil {
		if errors.Is(err, ErrDuplicateKey) {
			apiErr := api.ConflictError(api.ErrCodeResourceExists, "user already exists", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		apiErr := api.InternalError(api.ErrCodeDatabaseError, "failed to create user", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err) // pass err last: logged + span, never leaked to client
		return
	}

	c.logger.Info(ctx, "User created", logging.Field{Key: "user_id", Value: user.ID})
	api.JSON(ctx, w, c.logger, http.StatusCreated, user)
}
```

Constructors (`ValidationError`, `NotFoundError`, `ConflictError`, `InternalError`) and codes (`ErrCodeMissingField`, `ErrCodeResourceNotFound`, `ErrCodeResourceExists`, `ErrCodeDatabaseError`, …) live in `internal/platform/api`.

## 4. Migration

Never hand-write migrations. Generate one per owning binary:

```bash
make migrate-create service=example name=create_users
```

Fill the generated `-- +goose Up` / `-- +goose Down` in `backend/cmd/example/migrations/`, then `make migrate-up`.

## 5. Wire it into the binary

Construct repo → controller and pass the controller into `apiServer.Server(...)`. From [`backend/cmd/example/main.go`](../../backend/cmd/example/main.go):

```go
func initServer(ctx context.Context, logger logging.Logger, db *storage.DB) *http.Server {
	apiServer := api.NewAPI(ctx, logger)

	repo := example.NewPostgresRepository(db.Pool())
	controller := example.NewController(logger, repo)

	return apiServer.Server(
		ctx,
		ec.Current.Server.Port,
		ec.Current.Server.BasePath,
		mw.MiddlewareChain(
			mw.TracingMiddleware(ec.Current.Observability.ServiceName), // tracing first: provides context
			mw.LoggingMiddleware(logger),
		),
		controller, // Server(...) is variadic; pass every controller here
	)
}
```

Config is loaded per-binary with `config.MustLoadService[T]` from `cmd/{service}/config/base.yaml` (+ optional `{env}.yaml`, selected by `APP_ENV`). No raw viper or yaml.

## 6. Test it

Unit-test services and handlers with table-driven tests and a fake repository (see the `*_test.go` files in `internal/triggers/`).

Repository tests that need a real database are gated on `ORBIT_TEST_DB_URL` so they stay out of `make test` and CI. From [`internal/triggers/postgres_test.go`](../../backend/internal/triggers/postgres_test.go):

```go
func testRepo(t *testing.T) (*postgresRepository, string) {
	t.Helper()
	dsn := os.Getenv("ORBIT_TEST_DB_URL")
	if dsn == "" {
		t.Skip("set ORBIT_TEST_DB_URL to run Postgres integration tests")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	tenant := "test-" + uuid.NewString() // isolate under a random tenant
	t.Cleanup(func() { /* delete tenant rows */ pool.Close() })
	return &postgresRepository{pool: pool}, tenant
}
```

Run against the local Postgres that `make up` starts:

```bash
ORBIT_TEST_DB_URL='postgres://orbit:orbit@localhost:5432/orbit?sslmode=disable' \
  go test ./internal/triggers/ -run Postgres
```

## Before you commit

```bash
make check
```

`gofmt`, `go vet`, `golangci-lint` (the Orbit AST + import-ban rules in [AGENTS.md](../../AGENTS.md)), `gitleaks`, `docs-check`, and tests. Do not add `//nolint`; exceptions are managed centrally in `.golangci.yml`.
