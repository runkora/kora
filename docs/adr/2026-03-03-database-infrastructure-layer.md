# Database Infrastructure Layer

**Status:** Accepted (amended 2026-09-10)
**Date:** 2026-03-03
**Deciders:** Engineering Team
**Technical Story:** Establishing the foundational database abstraction layer that supports feature repositories with connection pooling, transactions, and observability.

---

## Context

Orbit uses PostgreSQL as its primary database and follows a repository pattern where each feature owns its data access layer (established in [repository-pattern-and-code-organization](2026-02-17-repository-pattern-and-code-organization.md)). We need a solid infrastructure layer to support these feature repositories.

### Background

From [repository-pattern-and-code-organization](2026-02-17-repository-pattern-and-code-organization.md), we have established:
- Each feature package owns its repository interface and PostgreSQL implementation
- The `internal/platform/storage/` package provides database infrastructure
- Dependency flow is one-way: Features → Platform (never Platform → Features)

Current state:
- No database infrastructure exists yet
- We will use PostgreSQL for all persistent storage
- Features need connection pooling, transaction support, and health checks

### Problem Statement

We need to design the database infrastructure layer that:
1. Manages PostgreSQL connection pooling efficiently
2. Provides transaction support for multi-operation workflows
3. Supports dependency injection into feature repositories
4. Enables testing with mock databases or test containers
5. Provides observability (metrics, tracing, logging)
6. Handles graceful shutdown and health checks

### Goals

- **Simple API**: Feature repositories should have a clean, simple interface to the database
- **Performance**: Efficient connection pooling and prepared statement caching
- **Safety**: Proper transaction handling with rollback support
- **Testability**: Easy to test feature repositories in isolation
- **Observability**: Full visibility into database operations
- **Production-ready**: Connection limits, timeouts, health checks, graceful shutdown

### Non-Goals

- Supporting multiple database backends initially (PostgreSQL only)
- Building an ORM or query builder (use raw SQL for clarity)
- Auto-generating repositories or models (explicit code)
- Supporting NoSQL databases (out of scope)

---

## Decision Drivers

* **Go ecosystem standard** - Use widely adopted Go database libraries
* **Performance** - Connection pooling must be efficient for production workloads
* **PostgreSQL-native** - Leverage PostgreSQL-specific features (JSONB, arrays, etc.)
* **Transaction safety** - Ensure ACID guarantees for multi-step operations
* **Testing support** - Easy to write unit tests with mocked repositories
* **Migration safety** - Schema changes must be versioned and reversible
* **Observability** - Database calls should be traced and logged

---

## Considered Options

### Option 1: database/sql with lib/pq

**Description:** Use Go's standard `database/sql` package with the `lib/pq` PostgreSQL driver.

```go
import (
    "database/sql"
    _ "github.com/lib/pq"
)

db, err := sql.Open("postgres", connString)
```

**Pros:**
- Standard library - no external dependencies for core API
- Well-documented and widely used
- Works with many SQL libraries and tools
- Simple mental model

**Cons:**
- `lib/pq` is in maintenance mode (no new features)
- Less efficient than pgx for PostgreSQL-specific workloads
- No native support for PostgreSQL types (JSONB, arrays require manual handling)
- Connection pooling is basic compared to pgx
- No batch operations support

### Option 2: pgx/v5 with pgxpool

**Description:** Use the modern `pgx/v5` PostgreSQL driver with `pgxpool` for connection pooling.

```go
import (
    "github.com/jackc/pgx/v5/pgxpool"
)

pool, err := pgxpool.New(ctx, connString)
```

**Pros:**
- **Best-in-class PostgreSQL driver** - actively maintained, excellent performance
- **Native PostgreSQL support** - JSONB, arrays, COPY, LISTEN/NOTIFY, etc.
- **Superior connection pooling** - `pgxpool` is highly optimized
- **Batch operations** - efficient bulk inserts/updates
- **Query pipelining** - reduce round trips
- **Better type safety** - strong typing for PostgreSQL types
- **Standard interface compatibility** - can still use `database/sql` interface if needed
- **Excellent logging/tracing** - built-in support for OpenTelemetry

**Cons:**
- Not part of standard library (but de facto standard for PostgreSQL in Go)
- Slightly larger API surface than `database/sql`
- Migration from `database/sql` requires code changes

### Option 3: GORM or similar ORM

**Description:** Use an ORM like GORM to abstract database operations.

```go
db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
db.AutoMigrate(&User{})
db.Create(&user)
```

**Pros:**
- Automatic migrations from struct definitions
- Reduces boilerplate for simple CRUD operations
- Association handling and eager loading

**Cons:**
- **Loss of control** - abstracts away SQL, making debugging harder
- **Performance overhead** - ORMs add reflection and query building cost
- **Complex queries become awkward** - advanced SQL is harder with ORM
- **Not idiomatic for this project** - Orbit values explicit SQL for clarity
- **Migration management** - Auto-migrations are risky in production
- **Testing complexity** - Harder to mock than repository pattern

---

## Decision

**Chosen Option:** Option 2 - pgx/v5 with pgxpool

### Rationale

**Why pgx over database/sql?**

1. **PostgreSQL-native**: Orbit is a PostgreSQL-only application. Using a driver optimized for PostgreSQL makes sense.
2. **Performance**: `pgxpool` provides superior connection pooling and batch operations for production workloads.
3. **Modern tooling**: `pgx` is actively maintained and the Go community's preferred PostgreSQL driver.
4. **Native types**: We'll likely use JSONB for agent metadata, arrays for tags, etc. - pgx handles these natively.
5. **Observability**: Built-in support for tracing and logging aligns with Orbit's observability requirements.

**Why not GORM?**

Orbit follows a repository pattern ([repository-pattern-and-code-organization](2026-02-17-repository-pattern-and-code-organization.md)) where SQL is explicit. ORMs hide SQL complexity, making debugging and optimization harder. For a control plane where correctness and performance are critical, explicit SQL is preferred.

**Compatibility note**: pgx can also work with `database/sql` interface if needed for third-party tools, giving us flexibility.

---

## Consequences

### Positive

- **Excellent performance** - pgxpool is one of the fastest PostgreSQL connection pools in Go
- **Type safety** - Native support for PostgreSQL types reduces bugs
- **Observability** - Built-in tracing and logging support
- **Future-proof** - pgx is actively developed and widely adopted
- **Batch operations** - Efficient bulk inserts for event storage and metrics
- **Clear dependency** - Features depend on `storage.DB` abstraction, not directly on pgx

### Negative

- **Not standard library** - Adds external dependency (though pgx is de facto standard)
- **Learning curve** - Developers unfamiliar with pgx need to learn its API
- **Migration effort** - If we ever need to support another database, requires abstraction layer

### Neutral

- **Larger API** - pgx has more features than database/sql, but we don't have to use all of them

---

## Context Best Practices

### Context Usage Pattern

Always pass `context.Context` as the first parameter to database operations. Never store context in structs.

```go
// ❌ DON'T: Store context in struct
type repository struct {
    ctx context.Context  // Anti-pattern!
    db  *storage.DB
}

// ✅ DO: Pass context per-operation
type repository struct {
    db *storage.DB
}

func (r *repository) GetUser(ctx context.Context, id string) (*User, error) {
    return r.db.Pool().QueryRow(ctx, query, id)
}
```

### Context Lifecycle

```go
// ❌ DON'T: Use context.Background() in application code
func (r *repository) GetUser(id string) (*User, error) {
    return r.db.Pool().QueryRow(context.Background(), query, id)
}

// ✅ DO: Use the context passed from the caller (carries cancellation, deadlines, values)
func (r *repository) GetUser(ctx context.Context, id string) (*User, error) {
    return r.db.Pool().QueryRow(ctx, query, id)
}

// ✅ DO: Create timeouts when needed
func (r *repository) SlowQuery(ctx context.Context) error {
    queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
    defer cancel()
    return r.db.Pool().QueryRow(queryCtx, complexQuery)
}
```

### Why This Matters

- **Cancellation propagation**: If an HTTP request is cancelled, the database query should also be cancelled
- **Deadline enforcement**: Timeouts set at the handler level flow through to the database
- **Request tracing**: Trace IDs stored in context flow through the entire call chain
- **Testing**: Tests can pass custom contexts with deadlines or cancellation

---

## Implementation

### Package Structure

```
internal/platform/storage/
├── db.go              # DB struct and connection pooling
├── transaction.go     # Transaction management
├── config.go          # Database configuration
├── migrations.go      # Goose migration runner
├── health.go          # Health check implementation
└── migrations/        # SQL migration files
    ├── 00001_initial_schema.sql
    └── ...
```

### Core Types

```go
// internal/platform/storage/db.go
package storage

import (
    "context"
    "fmt"
    "time"

    "github.com/clear-route/orbit/internal/platform/logging"
    "github.com/jackc/pgx/v5/pgxpool"
)

// DB wraps pgxpool.Pool and provides database infrastructure
type DB struct {
    pool   *pgxpool.Pool
    logger logging.Logger
}

// New creates and initializes a database connection pool
// Performs single-phase initialization with connectivity verification
func New(ctx context.Context, config *Config, logger logging.Logger) (*DB, error) {
    logger.Info("Initializing database connection pool",
        logging.Field{Key: "host", Value: config.Host},
        logging.Field{Key: "database", Value: config.Database},
        logging.Field{Key: "port", Value: config.Port},
    )

    poolConfig, err := config.BuildPoolConfig()
    if err != nil {
        return nil, fmt.Errorf("build pool config: %w", err)
    }

    pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
    if err != nil {
        return nil, fmt.Errorf("create connection pool: %w", err)
    }

    // Verify connectivity on initialization
    pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
    defer cancel()
    
    if err := pool.Ping(pingCtx); err != nil {
        pool.Close()
        return nil, fmt.Errorf("ping database: %w", err)
    }

    logger.Info("Database connection pool ready",
        logging.Field{Key: "max_conns", Value: poolConfig.MaxConns},
        logging.Field{Key: "min_conns", Value: poolConfig.MinConns},
    )

    return &DB{
        pool:   pool,
        logger: logger,
    }, nil
}

// Pool returns the underlying pgxpool.Pool for feature repositories
// Repositories use this directly - no need to manually acquire/release connections
func (db *DB) Pool() *pgxpool.Pool {
    return db.pool
}

// Close gracefully shuts down the connection pool
func (db *DB) Close() {
    db.logger.Info("Closing database connection pool")
    db.pool.Close()
}

// Stats returns connection pool statistics for monitoring and observability
func (db *DB) Stats() *pgxpool.Stat {
    return db.pool.Stat()
}

// Health checks database connectivity with a timeout
// Used by health check endpoints and readiness probes
func (db *DB) Health(ctx context.Context) error {
    healthCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
    defer cancel()

    if err := db.pool.Ping(healthCtx); err != nil {
        return fmt.Errorf("database unhealthy: %w", err)
    }

    // Check if pool is under pressure
    stats := db.pool.Stat()
    if stats.AcquiringConns() > 0 && stats.IdleConns() == 0 {
        db.logger.Warn("Database connection pool under pressure",
            logging.Field{Key: "total_conns", Value: stats.TotalConns()},
            logging.Field{Key: "acquiring_count", Value: stats.AcquiringConns()},
        )
    }

    return nil
}
```

### Transaction Support

```go
// internal/platform/storage/transaction.go
package storage

import (
    "context"
    "fmt"

    "github.com/clear-route/orbit/internal/platform/logging"
    "github.com/jackc/pgx/v5"
)

// TxFunc is a function that runs inside a transaction
type TxFunc func(tx pgx.Tx) error

// WithTransaction executes fn inside a database transaction
// Commits if fn returns nil, rolls back if fn returns error
func (db *DB) WithTransaction(ctx context.Context, fn TxFunc) error {
    tx, err := db.pool.Begin(ctx)
    if err != nil {
        return fmt.Errorf("begin transaction: %w", err)
    }

    defer func() {
        if err := tx.Rollback(ctx); err != nil && err != pgx.ErrTxClosed {
            db.logger.Error("Transaction rollback failed",
                logging.Field{Key: "error", Value: err.Error()},
            )
        }
    }()

    if err := fn(tx); err != nil {
        return err // Rollback via defer
    }

    if err := tx.Commit(ctx); err != nil {
        return fmt.Errorf("commit transaction: %w", err)
    }

    return nil
}
```



### Configuration

```go
// internal/platform/storage/config.go
package storage

import (
    "fmt"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
    Host            string
    Port            int
    Database        string
    User            string
    Password        string
    SSLMode         string
    MaxConnections  int32
    MinConnections  int32
    MaxConnLifetime time.Duration
    MaxConnIdleTime time.Duration
    
    // Optional: Advanced tuning
    HealthCheckPeriod time.Duration
    ConnectTimeout    time.Duration
}

// DefaultConfig returns sensible defaults for production use
func DefaultConfig() *Config {
    return &Config{
        Host:              "localhost",
        Port:              5432,
        Database:          "orbit",
        User:              "orbit",
        SSLMode:           "prefer",
        MaxConnections:    25,  // (CPU cores * 2) + effective_spindle_count
        MinConnections:    5,   // Keep warm connections
        MaxConnLifetime:   1 * time.Hour,
        MaxConnIdleTime:   30 * time.Minute,
        HealthCheckPeriod: 1 * time.Minute,
        ConnectTimeout:    5 * time.Second,
    }
}

// ConnectionString builds a PostgreSQL connection string
func (c *Config) ConnectionString() string {
    return fmt.Sprintf(
        "host=%s port=%d dbname=%s user=%s password=%s sslmode=%s",
        c.Host, c.Port, c.Database, c.User, c.Password, c.SSLMode,
    )
}

// BuildPoolConfig creates a pgxpool.Config with all settings applied
func (c *Config) BuildPoolConfig() (*pgxpool.Config, error) {
    poolConfig, err := pgxpool.ParseConfig(c.ConnectionString())
    if err != nil {
        return nil, fmt.Errorf("parse connection string: %w", err)
    }

    // Apply pool settings
    poolConfig.MaxConns = c.MaxConnections
    poolConfig.MinConns = c.MinConnections
    poolConfig.MaxConnLifetime = c.MaxConnLifetime
    poolConfig.MaxConnIdleTime = c.MaxConnIdleTime
    poolConfig.HealthCheckPeriod = c.HealthCheckPeriod

    // Set connection timeout
    if c.ConnectTimeout > 0 {
        poolConfig.ConnConfig.ConnectTimeout = c.ConnectTimeout
    }

    return poolConfig, nil
}
```

### Feature Repository Example

```go
// internal/identity/postgres.go
package identity

import (
    "context"
    "fmt"

    "github.com/jackc/pgx/v5"
    "github.com/clear-route/orbit/internal/platform/logging"
    "github.com/clear-route/orbit/internal/platform/storage"
)

type postgresRepository struct {
    db     *storage.DB
    logger logging.Logger
}

func NewPostgresRepository(db *storage.DB, logger logging.Logger) Repository {
    return &postgresRepository{
        db:     db,
        logger: logger,
    }
}

func (r *postgresRepository) GetUserByID(ctx context.Context, id string) (*User, error) {
    var user User
    
    query := `SELECT id, email, created_at FROM users WHERE id = $1`
    
    // Use pool directly - it handles connection acquisition automatically
    err := r.db.Pool().QueryRow(ctx, query, id).Scan(
        &user.ID,
        &user.Email,
        &user.CreatedAt,
    )
    
    if err == pgx.ErrNoRows {
        return nil, ErrUserNotFound
    }
    if err != nil {
        r.logger.Error("Failed to query user",
            logging.Field{Key: "user_id", Value: id},
            logging.Field{Key: "error", Value: err.Error()},
        )
        return nil, fmt.Errorf("query user: %w", err)
    }
    
    return &user, nil
}

func (r *postgresRepository) CreateUser(ctx context.Context, user *User) error {
    query := `
        INSERT INTO users (id, email, created_at)
        VALUES ($1, $2, $3)
    `
    
    _, err := r.db.Pool().Exec(ctx, query, user.ID, user.Email, user.CreatedAt)
    if err != nil {
        r.logger.Error("Failed to create user",
            logging.Field{Key: "user_id", Value: user.ID},
            logging.Field{Key: "error", Value: err.Error()},
        )
        return fmt.Errorf("insert user: %w", err)
    }
    
    r.logger.Debug("Created user",
        logging.Field{Key: "user_id", Value: user.ID},
    )
    
    return nil
}

// Transaction example using WithTransaction helper
func (r *postgresRepository) CreateUserWithProfile(ctx context.Context, user *User, profile *Profile) error {
    return r.db.WithTransaction(ctx, func(tx pgx.Tx) error {
        // Insert user
        _, err := tx.Exec(ctx, 
            `INSERT INTO users (id, email) VALUES ($1, $2)`,
            user.ID, user.Email,
        )
        if err != nil {
            return fmt.Errorf("insert user: %w", err)
        }
        
        // Insert profile
        _, err = tx.Exec(ctx,
            `INSERT INTO profiles (user_id, name) VALUES ($1, $2)`,
            user.ID, profile.Name,
        )
        if err != nil {
            return fmt.Errorf("insert profile: %w", err)
        }
        
        return nil // Commit
    })
}

// Batch operation example - efficient for bulk inserts
func (r *postgresRepository) CreateUsers(ctx context.Context, users []*User) error {
    batch := &pgx.Batch{}
    
    query := `INSERT INTO users (id, email, created_at) VALUES ($1, $2, $3)`
    for _, user := range users {
        batch.Queue(query, user.ID, user.Email, user.CreatedAt)
    }
    
    results := r.db.Pool().SendBatch(ctx, batch)
    defer results.Close()
    
    // Check all results
    for i := 0; i < batch.Len(); i++ {
        _, err := results.Exec()
        if err != nil {
            return fmt.Errorf("batch insert failed at index %d: %w", i, err)
        }
    }
    
    r.logger.Info("Batch created users",
        logging.Field{Key: "count", Value: len(users)},
    )
    
    return nil
}
```

### Dependency Injection

```go
// cmd/orbit/main.go
package main

import (
    "context"
    "log"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/clear-route/orbit/internal/platform/logging"
    "github.com/clear-route/orbit/internal/platform/storage"
    "github.com/clear-route/orbit/internal/identity"
)

func main() {
    ctx := context.Background()
    
    // Initialize logger (from [dependency-injected-logging](2026-02-17-dependency-injected-logging.md))
    logger := logging.New(logging.Config{
        Level:  "info",
        Format: "json",
    })
    
    // Load database configuration (combines YAML config + env secrets per [yaml-file-based-configuration](2026-02-18-yaml-file-based-configuration.md))
    dbConfig, err := storage.LoadConfig("internal/platform/storage", config.Environment())
    if err != nil {
        log.Fatalf("Failed to load database config: %v", err)
    }
    
    // Initialize database with single-phase construction
    db, err := storage.New(ctx, dbConfig, logger)
    if err != nil {
        log.Fatalf("Failed to initialize database: %v", err)
    }
    defer db.Close()
    
    // NOTE: Migrations are run via `make migrate-up` before deployment
    // Never run migrations at application startup in production!
    
    // Create feature repositories with dependency injection
    identityRepo := identity.NewPostgresRepository(db, logger)
    
    // Create services
    identityService := identity.NewService(identityRepo, logger)
    
    // Start HTTP server...
    
    // Graceful shutdown
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
    <-sigChan
    
    logger.Info("Shutting down gracefully")
    
    // Close database connections
    db.Close()
}
```

### Testing Support

```go
// internal/identity/repository_test.go
package identity

import (
    "context"
    "testing"
)

// Mock repository for unit tests
type mockRepository struct {
    users map[string]*User
}

func NewMockRepository() Repository {
    return &mockRepository{
        users: make(map[string]*User),
    }
}

func (m *mockRepository) GetUserByID(ctx context.Context, id string) (*User, error) {
    user, ok := m.users[id]
    if !ok {
        return nil, ErrUserNotFound
    }
    return user, nil
}

// Integration test with real database
func TestPostgresRepository_GetUserByID(t *testing.T) {
    ctx := context.Background()
    
    // Use testcontainers or similar for real database
    db := setupTestDB(t)
    defer db.Close()
    
    repo := NewPostgresRepository(db)
    
    // Test implementation...
}
```

---

## Validation

### Success Metrics

- **Connection efficiency**: Pool utilization < 80% under normal load
- **Query performance**: P95 latency < 10ms for simple queries
- **Test coverage**: >80% coverage for repository implementations
- **Zero connection leaks**: No unclosed connections in production
- **Migration reliability**: Zero failed migrations in production

### Monitoring

- Track connection pool metrics (active, idle, wait time)
- Monitor query latency (P50, P95, P99)
- Alert on connection pool exhaustion
- Track migration execution success/failure
- Log slow queries (> 100ms)

---

## Related Decisions

- [repository-pattern-and-code-organization](2026-02-17-repository-pattern-and-code-organization.md) - Repository pattern and code organization
- [dependency-injected-logging](2026-02-17-dependency-injected-logging.md) - Logging infrastructure (will integrate with DB layer)

---

## References

- [repository-pattern-and-code-organization](2026-02-17-repository-pattern-and-code-organization.md)
- [yaml-file-based-configuration](2026-02-18-yaml-file-based-configuration.md)
- [pgx documentation](https://github.com/jackc/pgx)
- [pgxpool documentation](https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool)
- [PostgreSQL connection pooling best practices](https://www.postgresql.org/docs/current/runtime-config-connection.html)

---

## Notes

**On connection pooling:**

The default pgxpool configuration is quite conservative. For production, we'll tune:
- `MaxConns`: Based on `(CPU cores * 2) + effective_spindle_count`
- `MinConns`: Keep warm connections for low-latency responses
- `MaxConnLifetime`: Prevent stale connections (1 hour default)
- `MaxConnIdleTime`: Release idle connections (30 min default)

**On migrations:**

Goose requires `database/sql`, but pgx provides `stdlib` package for compatibility. Migration files are embedded in the dedicated `cmd/migrate` binary, not in service binaries; see [Database Migrations](2026-09-10-database-migrations.md).

**On testing:**

Unit tests use mock repositories. Integration tests use testcontainers or a dedicated test database. Never run migrations against production from tests.

**Last Updated:** 2025-01-23
