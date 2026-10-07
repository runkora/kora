// migrate applies kora's database schema. It embeds every service's goose
// migrations and runs them as one set, as a Helm hook Job in a cluster and
// through `go run` in local development (docs/adr/2026-09-10-database-migrations.md).
package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"text/template"

	identityMigrations "github.com/clear-route/orbit/cmd/identity/migrations"
	inferenceMigrations "github.com/clear-route/orbit/cmd/inference-gateway/migrations"
	agentsMigrations "github.com/clear-route/orbit/cmd/orbit-agents/migrations"
	triggersMigrations "github.com/clear-route/orbit/cmd/orbit-triggers/migrations"
	policyMigrations "github.com/clear-route/orbit/cmd/policy-service/migrations"
	toolGatewayMigrations "github.com/clear-route/orbit/cmd/tool-gateway/migrations"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/observability"
	"github.com/clear-route/orbit/internal/platform/storage"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// allowDownEnv guards down. The chart never sets it.
const allowDownEnv = "ORBIT_MIGRATE_ALLOW_DOWN"

// sources maps each table-owning service to its embedded migrations.
// cmd/example is the reference implementation and is left out.
var sources = map[string]fs.FS{
	"identity":          identityMigrations.FS,
	"inference-gateway": inferenceMigrations.FS,
	"orbit-agents":      agentsMigrations.FS,
	"orbit-triggers":    triggersMigrations.FS,
	"policy-service":    policyMigrations.FS,
	"tool-gateway":      toolGatewayMigrations.FS,
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	ctx := context.Background()
	logger := newLogger()

	var err error
	switch os.Args[1] {
	case "up":
		err = withProvider(ctx, logger, func(p *goose.Provider) error { return up(ctx, logger, p) })
	case "status":
		err = withProvider(ctx, logger, func(p *goose.Provider) error { return status(ctx, logger, p) })
	case "down":
		if os.Getenv(allowDownEnv) != "1" {
			err = fmt.Errorf("down is refused unless %s=1 is set", allowDownEnv)
			break
		}
		err = withProvider(ctx, logger, func(p *goose.Provider) error { return down(ctx, logger, p) })
	case "create":
		err = create(os.Args[2:])
	case "lint":
		err = lintMigrations(sources)
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		logger.Error(ctx, "migrate failed", logging.Field{Key: "command", Value: os.Args[1]}, logging.Field{Key: "error", Value: err})
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: migrate up | status | down | lint | create --service <name> <slug>")
}

func newLogger() logging.Logger {
	if observability.DefaultConfig.LogFormat == "console" {
		return logging.NewDevelopmentLogger()
	}
	return logging.NewLogger()
}

// withProvider opens the platform pool, hands goose a database/sql view of it
// and runs fn.
func withProvider(ctx context.Context, logger logging.Logger, fn func(*goose.Provider) error) error {
	merged, err := mergeMigrationFS(sources)
	if err != nil {
		return err
	}

	cfg, err := storage.LoadPostgresConfig()
	if err != nil {
		return err
	}
	db, err := storage.New(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer db.Close()

	sqlDB := stdlib.OpenDBFromPool(db.Pool())
	defer sqlDB.Close()

	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, merged,
		goose.WithAllowOutofOrder(true),
		goose.WithLogger(gooseLogger{ctx: ctx, logger: logger}),
	)
	if err != nil {
		return fmt.Errorf("create goose provider: %w", err)
	}
	defer provider.Close()

	return fn(provider)
}

func up(ctx context.Context, logger logging.Logger, p *goose.Provider) error {
	results, err := p.Up(ctx)
	for _, r := range results {
		logger.Info(ctx, "Applied migration",
			logging.Field{Key: "version", Value: r.Source.Version},
			logging.Field{Key: "file", Value: filepath.Base(r.Source.Path)},
			logging.Field{Key: "duration", Value: r.Duration.String()},
		)
	}
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	version, err := p.GetDBVersion(ctx)
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	logger.Info(ctx, "Schema up to date",
		logging.Field{Key: "applied", Value: len(results)},
		logging.Field{Key: "version", Value: version},
	)
	return nil
}

func status(ctx context.Context, logger logging.Logger, p *goose.Provider) error {
	statuses, err := p.Status(ctx)
	if err != nil {
		return fmt.Errorf("read migration status: %w", err)
	}
	for _, s := range statuses {
		fields := []logging.Field{
			{Key: "version", Value: s.Source.Version},
			{Key: "file", Value: filepath.Base(s.Source.Path)},
			{Key: "state", Value: string(s.State)},
		}
		if s.State == goose.StateApplied {
			fields = append(fields, logging.Field{Key: "applied_at", Value: s.AppliedAt})
		}
		logger.Info(ctx, "Migration", fields...)
	}
	return nil
}

func down(ctx context.Context, logger logging.Logger, p *goose.Provider) error {
	result, err := p.Down(ctx)
	if err != nil {
		if errors.Is(err, goose.ErrNoNextVersion) {
			logger.Info(ctx, "Nothing to roll back")
			return nil
		}
		return fmt.Errorf("roll back migration: %w", err)
	}
	logger.Info(ctx, "Rolled back migration",
		logging.Field{Key: "version", Value: result.Source.Version},
		logging.Field{Key: "file", Value: filepath.Base(result.Source.Path)},
	)
	return nil
}

// create writes a blank timestamped migration into a service's directory.
func create(args []string) error {
	if len(args) != 3 || args[0] != "--service" {
		usage()
		return errors.New("create needs --service <name> <slug>")
	}
	service, slug := args[1], args[2]
	if _, ok := sources[service]; !ok {
		return fmt.Errorf("unknown service %q", service)
	}
	dir := filepath.Join("cmd", service, "migrations")
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("run create from backend/: %w", err)
	}
	return goose.CreateWithTemplate(nil, dir, migrationTemplate, slug, "sql")
}

// migrationTemplate is the blank file create writes: the goose markers and
// nothing else, so an unedited file fails the content check instead of
// carrying a placeholder query.
var migrationTemplate = template.Must(template.New("migration").Parse(`-- +goose Up
-- +goose StatementBegin

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- +goose StatementEnd
`))

// gooseLogger routes goose's own output through the injected logger.
type gooseLogger struct {
	ctx    context.Context
	logger logging.Logger
}

func (g gooseLogger) Printf(format string, v ...any) {
	g.logger.Debug(g.ctx, fmt.Sprintf(format, v...))
}

func (g gooseLogger) Fatalf(format string, v ...any) {
	g.logger.Error(g.ctx, fmt.Sprintf(format, v...))
	os.Exit(1)
}
