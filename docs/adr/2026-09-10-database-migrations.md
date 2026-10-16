# Database Migrations

**Status:** Proposed
**Date:** 2026-09-10
**Deciders:** Justin Wilkin
**Technical Story:** The migration mechanics deferred by [Install and Distribution Model](2026-09-04-install-and-distribution-model.md). Amends [Multi-Service Backend Deployment Structure](2026-03-19-multi-service-backend-deployment-structure.md) (the planned `cmd/migrate` Job) and [Database Infrastructure Layer](2026-03-03-database-infrastructure-layer.md) (embedded migrations).

---

## Context

### Background

kora stores all control-plane state in one PostgreSQL database, one `public` schema and one `goose_db_version` table. Six binaries own tables and carry their schema under `backend/cmd/<service>/migrations/` as goose SQL files, 60 in total, plus one under `cmd/example`. Tenancy is row-scoped by `tenant_id`.

The only runner is the developer workstation. `make migrate-up` copies every service's files into a temp directory and runs the goose CLI against Postgres on localhost. goose is not a Go module dependency. No runtime image contains migration files or a migration runner. The chart has no migration Job or hook.

Six files carry row changes alongside schema changes: a scope vocabulary seed that the vocabulary repository also registers at boot, a JSONB key rename, a backfill, a `Down` that rewrites `runs.state`, a `Down` that deletes lineage rows, and a data repair that joins tables owned by another service.

session-manager pods run in tenant namespaces and reach no database; `run_events` rows are written by orbit-agents on the event push ([sandbox-exec-trust-boundary](2026-09-18-sandbox-exec-trust-boundary.md)).

### Problem Statement

A Helm or Argo CD install of the published charts does not apply the schema. There is no compatibility rule between schema and binaries and no rule on what a migration may contain.

### Goals

- A consumer installs or upgrades kora with Helm or Argo CD and the schema is applied before any workload rolls, with no imperative step.
- A compatibility contract between schema and binaries under which `helm rollback` needs no schema step.
- A mechanically checked rule on migration content.
- A 0.1.0 baseline that discards development history.
- Local development runs the mechanism a consumer runs.

### Non-Goals

- Provisioning, backing up, patching or replicating PostgreSQL.
- Per-tenant databases or schemas.
- Per-service schemas. The cross-service SQL paths that exist today are recorded in Decision 3.
- A schema version check at service boot.
- Online DDL tooling (lock timeouts, batched backfills) beyond a `CONCURRENTLY` convention.
- Enforcing immutability of released migration files.
- Removing the database credential from tenant namespaces.

---

## Decision Drivers

- **No imperative install steps.** Schema application is part of `helm install`.
- **Fail before rollout.** A migration failure stops the release before any pod serves a request.
- **Rollback is a workload operation.**
- **One tested path.** `make up` runs what migrates a consumer's database.
- **Ownership stays per binary.** The per-service directory layout is the unit of ownership.
- **Rules are checks.** `make check` validates migration content.

---

## Considered Options

### Option 1: Helm hook Job running an embedded migrate binary

**Description:** A `cmd/migrate` binary embeds every service's migrations, runs goose as a library, and ships as its own image. The `orbit` chart renders it as a `pre-install,pre-upgrade` hook Job.

**Pros:**
- Runs once per release before any Deployment rolls. Failure fails the release.
- Argo CD maps Helm hooks to sync hooks with no extra configuration.
- No ordering or locking between service pods.

**Cons:**
- A pre-install hook cannot use non-hook resources rendered by the same release.
- A separate image.

### Option 2: initContainer on every service pod

**Description:** The same migrate binary runs as an initContainer on each service Deployment, serialised with a Postgres advisory lock.

**Pros:**
- No hook semantics. Works with any deployment tool.

**Cons:**
- Every rollout of every service runs the migrator. N pods race for the lock on each upgrade.
- A failing migration crash-loops every pod.
- A pod cannot roll back a schema it did not apply.

### Option 3: Migrate at service startup

**Description:** Each binary applies its own migrations when it boots.

**Pros:**
- No new binary or image.

**Cons:**
- Contradicts the database infrastructure ADR's rule against startup migration.
- Services own tables other services read; boot order becomes a schema dependency graph.
- Scaling a replica can run DDL.

---

## Decision

**Chosen Option:** Option 1, Helm hook Job running an embedded migrate binary.

### Decision 1: Embedding

Each `backend/cmd/<service>/migrations/` directory gains an `embed.go` declaring `package migrations` with `//go:embed *.sql` and an exported `embed.FS`. `go:embed` cannot reference parent directories. `cmd/migrate` imports the six packages (`identity`, `inference-gateway`, `orbit-agents`, `orbit-triggers`, `policy-service`, `tool-gateway`) and merges them into one `fs.FS` that serves every file at its root directory. A duplicate filename or version prefix across directories fails the merge before any migration runs; goose rejects a duplicate version independently. `cmd/example` migrations are not embedded and its tables do not ship.

### Decision 2: The migrate binary

`backend/cmd/migrate/` is a binary with no HTTP server. It opens a pool through `storage.LoadPostgresConfig` and `storage.New`, wraps it with pgx `stdlib.OpenDBFromPool`, and runs a `goose.Provider` (`github.com/pressly/goose/v3`, Postgres dialect) over the merged filesystem. The Provider carries its own state; `cmd/migrate` does not import `database/sql`. goose's log lines go through an adapter to the injected `logging.Logger`.

Subcommands:

- `up` applies every pending migration, including files versioned below the highest applied one (`WithAllowOutofOrder`), and exits 0.
- `status` prints applied and pending versions.
- `create --service <name> <slug>` writes a new timestamped file into that service's directory. Development only, run with `go run` from `backend/`.
- `down` refuses to run unless `ORBIT_MIGRATE_ALLOW_DOWN=1` is set.
- `lint` runs the content rule (Decision 7) over the embedded set.

The goose CLI is removed from `make install-tools`.

### Decision 3: One database, one schema, one version table

The migrate binary runs one goose pass over the merged set. Version prefixes are ordered globally across services and must be unique across all directories.

A migration must not declare a foreign key to a table another service owns. Cross-service reads at query time are permitted; the `triggers` join into `runs` is the existing instance.

One run covers every tenant.

### Decision 4: Packaging

`migrate` is one more entry in the `backend-images` bake matrix, built from `backend/Dockerfile` with `SERVICE=migrate` and published as `orbit-migrate`. The Dockerfile builds `./cmd/${SERVICE}`. `migrate.image` in chart values puts it through the existing release stamping, signing and inventory scripts.

Service runtime images contain their binary and YAML configuration only.

### Decision 5: Compatibility contract

Schema at release N is compatible with binaries from release N-1. Every migration is expand-contract:

- Expand steps (add table, add nullable column or column with a server default, add index, widen a constraint) ship with the code that uses them. Existing inserts name their columns.
- Contract steps (drop column, drop table, add `NOT NULL`, narrow a constraint) ship no earlier than the minor after the last binary that touched the old shape.

goose applies every pending file in ascending version order; a skipped minor migrates the schema in one pass. Binary compatibility is guaranteed across one minor only, and rollback is supported to the immediately preceding revision only.

The compatibility rule is checked in review, by reading contract steps against the previous release's code. CI does not exercise upgrade chains.

The compatibility statement each release publishes gains a supported PostgreSQL major range, 14 through 18. The database role kora connects with needs `CREATE` on schema `public` and owns every object the migrations create.

### Decision 6: Rollback never runs Down

Production rollback is `helm rollback orbit <previous revision>` or the GitOps equivalent, with the schema left in place. Argo CD runs the previous revision's hook on rollback. The older image's `up` is a no-op against the newer schema: goose applies only versions present in its filesystem. `Down` sections stay in every file for local development.

### Decision 7: Migration content rule

The rule is implemented as the `lint` subcommand of `cmd/migrate`, run by `make migrate-lint` inside `make check` and covered by its own unit tests. It reads the goose directives, tokenizes `--` and `/* */` comments together with quoted runs in one pass (either can contain the other), derives table ownership from `CREATE TABLE` statements per directory, and rejects a migration that:

1. Contains `INSERT`, `DELETE` or `SELECT` in either direction, or has an empty `Up`. Seeds and reference data are registered idempotently at boot by the owning service.
2. Contains `UPDATE` in `Up` unless the same file also alters the same table.
3. Contains any DML in `Down`.
4. References a table created in another service's directory, or a table no directory creates, in `REFERENCES`, `INSERT INTO`, `DELETE FROM`, `UPDATE`, `ALTER TABLE`, `DROP TABLE` or `CREATE INDEX ... ON`, or renames a table.
5. Uses `CONCURRENTLY` without `-- +goose NO TRANSACTION`.
6. Puts a `CONCURRENTLY` statement in a `-- +goose StatementBegin` block that holds
   another statement. goose sends one block to the driver as a single `Exec`, and two
   statements in one `Exec` are an implicit transaction, which both `CONCURRENTLY` forms
   refuse with `cannot run inside a transaction block`. `NO TRANSACTION` alone does not
   save a file that packs them, so the failure would otherwise land in the chart's
   pre-upgrade hook and block the release. Give each `CONCURRENTLY` statement its own block.
7. Has a filename that does not match `<version>_<slug>.sql`, or a version prefix that another directory also uses.

Exceptions are not annotated inline.

### Decision 8: 0.1.0 baseline

Development history is discarded. Each table-owning service's directory is rewritten as a baseline of one file per table, named `<version>_create_<table>.sql`, carrying the table, its indexes and constraints, with a `Down` that drops the table. Baseline versions are sequential within a service in dependency order and unique across services; files created afterwards carry the timestamp `create` assigns. Tables dropped during development do not appear. The baseline carries no functions, triggers, extensions, types or views; none exist.

### Decision 9: Chart

`charts/orbit/templates/migrate/job.yaml` renders the Job with:

- `helm.sh/hook: pre-install,pre-upgrade`, `helm.sh/hook-weight: "-10"`, `helm.sh/hook-delete-policy: before-hook-creation,hook-succeeded`.
- `sidecar.istio.io/inject: "false"`. The Job needs no SVID and no Envoy for a Postgres connection.
- `command: ["./orbit"]`, `args: ["up"]`, `backoffLimit: 0`, `activeDeadlineSeconds` from `migrate.activeDeadlineSeconds`, default 240, below Helm's default timeout of 300 and the 600 the reference Helmfile sets.
- The restricted security context the services use.
- `DB_HOST`, `DB_PORT`, `DB_NAME`, `DB_USER` rendered inline from `.Values.config.db`, `DB_PASSWORD` from the pre-existing platform Secret, and `ORBIT_POSTGRES_SSL_MODE` and `ORBIT_LOG_FORMAT` through `orbit.serviceConfig`, with the `global` values as defaults and `migrate.config` as the override. A pre-install hook runs before the release's ConfigMaps exist; the Job cannot mount `orbit-config`.
- The namespace default ServiceAccount with no token mounted. The Job needs no API access.

The Job has no NetworkPolicy of its own. When `networkPolicy.externalServices` lists the database, `allow-external-services` selects every pod in the namespace and covers the Job.

Argo CD runs `pre-install` and `pre-upgrade` hooks as `PreSync` on every sync. A failed Job is retained under Helm. Automated sync does not reattempt a failed revision; the reference Argo CD application sets a `retry` policy, and `before-hook-creation` deletes the failed Job on each attempt, leaving the failure visible in sync history.

Values: `migrate.image`, `migrate.activeDeadlineSeconds`, `migrate.config`, and the scheduling, resources, annotation and extraEnv block every workload has.

### Decision 10: Local development

`make up` no longer runs `migrate-up`. The reference Helmfile applies the `orbit` chart and the hook runs in-cluster against the compose Postgres through the resolved Docker IP. Helmfile upgrades only a release whose rendered output changed, so every target that destroys the Postgres volume stamps a reset time onto the Job as a pod annotation in `local.generated.yaml`; the next `make up` renders a different Job and the hook re-applies the schema.

`make redeploy`, for any service, hashes `backend/cmd/*/migrations/*.sql` and compares it with the hash recorded in `local.generated.yaml`. On a change it rebuilds and pushes the migrate image, records the digest under `migrate.image.digest` and the hash beside it, and runs `helmfile -e local apply` for the `orbit` release. Helmfile upgrades the release on the digest change and the hook fires.

`make test-postgres` exports the `DB_*` variables and `ORBIT_POSTGRES_SSL_MODE=disable` and runs `go run ./cmd/migrate up` before the tests. The `example` repository test is gated on `ORBIT_EXAMPLE_TEST_DB_URL`. Nothing sets it; its tables are no longer migrated. `make migrate-create` and `make migrate-status` wrap `go run ./cmd/migrate`. `make migrate-up` and `make migrate-down` are removed.

---

## Architecture

```
release N                         consumer cluster
┌──────────────────────┐          ┌─────────────────────────────────────────────┐
│ orbit-migrate image  │  helm    │ orbit-system                                │
│  cmd/migrate         │ upgrade  │  Job orbit-migrate (pre-upgrade hook) ──────┼──▶ PostgreSQL
│  embeds identity/    │ ───────▶ │      goose up over merged set               │    goose_db_version
│  agents/ triggers/   │          │      exit 0  ──▶ Deployments roll           │
│  inference/ policy/  │          │      exit 1  ──▶ release fails, pods unchanged
│  tool-gateway/       │          │                                             │
└──────────────────────┘          └─────────────────────────────────────────────┘
```

---

## Consequences

### Positive

- Schema application is part of `helm install`.
- `helm rollback` needs no schema step.
- `make check` rejects the migration patterns in Decision 7.

### Negative

- Every contract change is a two-release change with a release-notes entry.
- One more image to build, sign, pin and list.
- The baseline resets every development database once.
- The `orbit-example` tenant workload still deploys locally but its tables are not created; only its health endpoint works.
- Under Argo CD a failed migration Job is deleted on the next attempt; the failure is visible only in sync history.
- On a skipped upgrade, pods from the old release may return errors between the hook completing and the rollout finishing.

### Neutral

- goose is added to `backend/go.mod`; only `cmd/migrate` imports it.
- Each `migrations/` directory becomes a Go package.

---

## Implementation

1. `embed.go` in each of the six `migrations/` directories.
2. `cmd/migrate`: merged filesystem, `up` with `WithAllowOutofOrder`, `status`, `create`, guarded `down`; goose in `go.mod`; bake matrix entry.
3. Baseline: rewrite the six directories as one file per table; `run_state_test.go` reads the index it asserts on from the embedded filesystem.
4. `lint` subcommand and `make migrate-lint` wired into `make check`.
5. Chart: Job template, values, helm-docs; a `retry` policy on the reference Argo CD application.
6. Makefile and `redeploy.sh`: remove the goose CLI install, `migrate-up` and `migrate-down`; `make up`, `redeploy`, `test-postgres`, `migrate-create` and `migrate-status` on the binary.
7. `compatibility.yaml` gains the PostgreSQL range.
8. Docs: `code-organization.md` (including a `migrate` row in the binary table), `local-development.md`, `backend-golden-path.md`, `deployment.md` (database prerequisites including the role privilege, migration, upgrade, skipping and rollback), the two amended ADRs, `AGENTS.md`.

### Migration Path

Local development: `make db-reset && make up` after step 6 lands.

No upgrade path is supported for schemas created before the 0.1.0 baseline.

---

## Validation

- `helm install` of the `orbit` chart on a blank cluster with an empty database reaches every service Ready with no manual step.
- `helm upgrade` from N-1 to N with N-1 pods serving throughout, then `helm rollback` to N-1 against the N schema, verified on the local cluster before each release.
- `make migrate-lint` fails on a migration that violates any clause of Decision 7.

---

## Related Decisions

- [Multi-Service Backend Deployment Structure](2026-03-19-multi-service-backend-deployment-structure.md): per-service migration ownership and the planned Job.
- [Database Infrastructure Layer](2026-03-03-database-infrastructure-layer.md): pgx and goose; no migration at startup.
- [Install and Distribution Model](2026-09-04-install-and-distribution-model.md): chart publication, release pipeline, compatibility statement and workload rollback.
- [Dynamic Route-Scope Registration](2026-04-14-dynamic-route-scope-registration.md): vocabulary is registered at boot.

## References

- [goose](https://github.com/pressly/goose): library usage, `WithAllowOutofOrder`, `NO TRANSACTION`, embedded filesystems.
- [Helm chart hooks](https://helm.sh/docs/topics/charts_hooks/): hook weights, delete policies, hook resources and release ordering.
- [Argo CD resource hooks](https://argo-cd.readthedocs.io/en/stable/user-guide/resource_hooks/): Helm hook mapping.
- [PostgreSQL: building indexes concurrently](https://www.postgresql.org/docs/current/sql-createindex.html#SQL-CREATEINDEX-CONCURRENTLY).
