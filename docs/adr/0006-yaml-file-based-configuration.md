# 0006. YAML File-Based Configuration with Typed Generic Loader

**Status:** Proposed
**Date:** 2026-06-10
**Deciders:** Engineering Team
**Technical Story:** Establishing a configuration management strategy for the Orbit control plane that supports multiple environments without requiring every non-secret value to be managed as an environment variable.

---

## Context

Orbit is a control plane for autonomous agents that must run across multiple environments: local development, CI, staging, and production. Each environment requires different values for server ports, base paths, service endpoints, feature flags, log levels, and similar non-sensitive configuration.

### Background

Currently, the Orbit binary is configured entirely through environment variables (e.g., `observability.SetupFromEnv`). This works well for secrets and for twelve-factor app deployments, but it becomes unwieldy as the number of configuration values grows. Managing dozens of environment variables across environments — locally, in `.env` files, in Kubernetes manifests, and in CI pipelines — creates maintenance overhead and room for error.

Go's standard library does not ship with a configuration loading abstraction. The ecosystem offers several approaches ranging from raw environment variable parsing, to generic third-party config libraries (Viper), to structured YAML loading. Each trade-off is meaningful for a control plane with multiple feature domains and a strong emphasis on type safety.

### Problem Statement

We need a configuration strategy that:

1. Supports environment-specific values (local, development, staging, production) without requiring individual env vars for every config key
2. Provides **type safety** — config structs should be verified at compile time, not parsed into `map[string]interface{}` at runtime
3. Works cleanly across all environments including local development (where the working directory varies depending on how tests are run) and containerised production deployments
4. Keeps **secrets out of config files** — the approach must not encourage storing sensitive values (API keys, DB passwords, TLS certs) in YAML
5. Fits Orbit's feature-based architecture — features should own their config shape, not depend on a centralised config struct
6. Remains simple and auditable — the implementation should be small enough that any engineer can understand it in minutes

### Goals

- Load environment-specific YAML config files with a single function call
- Return fully typed config structs via Go generics (no type assertions at call sites)
- Resolve config file paths correctly regardless of the current working directory (critical for `go test ./...` runs from any subdirectory)
- Provide a clear convention for where config files live per feature/module
- Enforce at the architectural level that secrets are not stored in YAML config
- Live in `internal/platform/config/` as a shared platform utility, consistent with ADR-0003

### Non-Goals

- Managing secrets — all secret values must continue to come from environment variables or a secret manager (Vault, AWS Secrets Manager, etc.)
- Hot-reloading config at runtime without a restart
- Merging or layering config from multiple sources (file + env overlay)
- Supporting config formats other than YAML initially (JSON, TOML, etc. can be added later)
- Building a plugin system or dynamic config schema registration
- Replacing Twelve-Factor App practices for secrets

---

## Decision Drivers

* **Type safety** — config structs defined by each feature, verified at compile time, no `interface{}` at call sites
* **Developer experience** — one function call to load config; YAML files are human-readable and easy to diff
* **Test reliability** — `go test ./...` is run from various directories; path resolution must be robust
* **Feature autonomy** — consistent with ADR-0003, each feature owns its config shape and YAML files
* **Security posture** — the pattern must make it structurally difficult to accidentally commit secrets
* **Minimal dependencies** — prefer a thin implementation over pulling in a heavy config framework
* **Idiomatic Go** — generics over reflection-heavy magic; explicit file paths over convention-over-configuration magic

---

## Considered Options

### Option 1: Environment Variables Only (Status Quo)

**Description:** Continue the current approach of reading all configuration from environment variables using `os.Getenv` or a thin `env.Env(key, default)` helper.

**Pros:**
- Zero new dependencies
- Twelve-Factor App compliant
- Works identically in all environments
- No file I/O at startup

**Cons:**
- Managing 30+ env vars across local, CI, staging, production becomes a significant operational burden
- No type structure — values are always strings, requiring manual parsing
- Verbose to document and hard to get a complete picture of what a component needs
- Developers lose the ability to diff environment configs in version control

### Option 2: Third-Party Config Library (Viper)

**Description:** Use [Viper](https://github.com/spf13/viper), the most popular Go configuration library, which supports YAML, JSON, TOML, env vars, remote config (etcd/Consul), and automatic env binding.

**Pros:**
- Feature-rich: supports file watching, remote config, multiple formats
- Widely used in the Go ecosystem
- Automatic environment variable override binding

**Cons:**
- Large dependency with its own dependency tree (mapstructure, fsnotify, etc.)
- Configuration is done through a global singleton by default, which conflicts with Orbit's dependency injection philosophy (ADR-0004)
- Type safety is via `mapstructure` struct tags and reflection, not compile-time generics
- "All features in one library" introduces capabilities (remote config, file watching) we explicitly do not want yet
- More surface area to audit for security concerns in a security-critical control plane

### Option 3: Typed Generic YAML Loader (Chosen)

**Description:** A small `internal/platform/config` package exposing a single generic function `Load[T any](module, environment string) (*T, error)`. Each feature defines its own config struct with YAML tags, places environment-specific YAML files under `{module}/config/{environment}.yaml`, and calls `config.Load[MyConfig]("internal/myfeature", env)` at startup.

The loader:
1. Determines the project root by walking up from the current working directory looking for a `go.mod` marker (with fallback to relative path for containerised environments)
2. Constructs the full path: `{projectRoot}/{module}/config/{environment}.yaml`
3. Reads and unmarshals the file into `T` using `gopkg.in/yaml.v3`
4. Returns `(*T, error)` — callers decide whether to `log.Fatal` or handle gracefully

**Pros:**
- **Fully type-safe** — `Load[ServerConfig]` returns `*ServerConfig`, no type assertions
- **Minimal implementation** — ~80 lines, easy to audit
- **Minimal dependencies** — only `gopkg.in/yaml.v3`, already a very common transitive dependency
- **Feature-owned config structs** — consistent with ADR-0003 feature autonomy
- **Explicit** — the file path convention is clear and auditable
- **No global state** — returns a value, callers manage lifecycle
- **Test-friendly** — project root detection works from any working directory

**Cons:**
- No automatic environment variable override (intentional — secrets use a different path)
- Path resolution heuristic (go.mod walk) is a convention; unusual project structures could break it
- Each feature calling `Load` independently means multiple file reads at startup (acceptable)

### Option 4: Single App-Wide Config Struct

**Description:** Define one large `AppConfig` struct in `internal/platform/config/` that holds all features' configuration, loaded once from a single `config/{environment}.yaml`.

**Pros:**
- Single file read at startup
- One place to see all configuration

**Cons:**
- Contradicts the feature-autonomy principle in ADR-0003 — features must now import a shared config package that knows about all features (circular dependency risk)
- The config struct becomes a "god struct" that must be updated every time a feature adds a config value
- Harder for feature teams to work independently

---

## Decision

**Chosen Option:** Option 3 — Typed Generic YAML Loader

### Rationale

Option 3 delivers type safety, feature autonomy, minimal dependencies, and an implementation small enough to be fully understood and audited in minutes — all of which are primary decision drivers for Orbit.

**Why not Viper (Option 2)?** Viper's global singleton conflicts with Orbit's dependency injection approach (ADR-0004). Its `mapstructure` reflection-based decoding gives up compile-time type safety. Its large feature set introduces capabilities (remote config, fsnotify) that we explicitly do not want yet and that expand the attack surface of a security-critical control plane.

**Why not environment variables only (Option 1)?** As Orbit grows toward production, the number of non-secret config values (ports, paths, endpoint URLs, timeout values, feature flags) will easily exceed 40+. Managing these as individual env vars across multiple deployment environments creates an operational burden disproportionate to the actual complexity of the values being managed.

**Why not a single app-wide struct (Option 4)?** It directly conflicts with ADR-0003's feature autonomy principle and creates the same "god package" anti-pattern that ADR-0003 explicitly rejected for repositories.

**The generic loader (Option 3) is the right fit because:**
- It is a thin utility, not a framework — features remain in control
- It mirrors the feature-owned repository pattern from ADR-0003
- It keeps secrets firmly out of version control by design (YAML is for non-secret config only)
- It is idiomatic Go: explicit paths, concrete types, errors returned not panicked

---

## Consequences

### Positive

- **Type safety at config load time** — mismatches between YAML and struct fields surface at startup, not in production
- **Feature teams are independent** — adding a new config value to `identity` requires no changes outside `internal/identity/`
- **Config is reviewable** — YAML files for each environment live in version control; changes are visible in PRs
- **Minimal dependency footprint** — `gopkg.in/yaml.v3` is the only new direct dependency
- **No global state** — config values are injected just like loggers (ADR-0004 consistency)
- **Clear security boundary** — YAML files for structure, env vars for secrets; the two never mix

### Negative

- **No env var override layer** — a value in `production.yaml` cannot be overridden at runtime without changing the file or redeploying (mitigation: secrets already use env vars; structural values rarely need runtime override)
- **Multiple file reads at startup** — if 8 features each call `Load`, that is 8 file reads (negligible cost at startup)
- **Project root heuristic** — the `go.mod` walk is a convention; a project without a `go.mod` at root (unusual for a Go module) would need the fallback path

### Neutral

- `gopkg.in/yaml.v3` is added as a direct dependency — widely used, well-maintained, Apache 2.0 licensed
- Each feature that requires config adds a `config/` subdirectory and one YAML file per environment — small, predictable overhead

---

## Implementation

### Package Location

The loader lives in `internal/platform/config/` — a platform utility, not a feature. Consistent with ADR-0003, platform packages provide infrastructure; features consume it.

```
backend/
├── internal/
│   └── platform/
│       └── config/
│           ├── config.go        # Load[T] and supporting functions
│           └── config_test.go   # Unit tests with temp files
```

### Config File Convention

Each feature (or the application itself) places environment-specific YAML files under a `config/` subdirectory within its own package directory:

```
backend/
├── internal/
│   ├── platform/
│   │   └── config/
│   │       └── config.go
│   └── identity/
│       ├── config/
│       │   ├── local.yaml
│       │   ├── development.yaml
│       │   └── production.yaml
│       ├── service.go
│       └── models.go
└── cmd/
    └── orbit/
        ├── config/
        │   ├── local.yaml
        │   ├── development.yaml
        │   └── production.yaml
        └── main.go
```

### Public API

```go
// internal/platform/config/config.go
package config

// Load reads the YAML config file for the given module and environment,
// unmarshals it into a new instance of T, and returns a pointer to it.
//
// module is a path relative to the project root (e.g. "internal/identity").
// environment is the name of the YAML file without extension (e.g. "production").
//
// Config files must not contain secrets. Use environment variables for all
// sensitive values.
func Load[T any](module, environment string) (*T, error)
```

### Usage Pattern

```go
// internal/identity/service.go

type identityConfig struct {
    Server struct {
        Port int    `yaml:"port"`
    } `yaml:"server"`
    OIDC struct {
        Issuer   string `yaml:"issuer"`
        ClientID string `yaml:"clientId"`
        // Note: client secret comes from an env var, NOT this file
    } `yaml:"oidc"`
}

var environment = os.Getenv("APP_ENV") // "local" | "development" | "production"

func NewService(repo Repository) (*Service, error) {
    cfg, err := config.Load[identityConfig]("internal/identity", environment)
    if err != nil {
        return nil, fmt.Errorf("identity config: %w", err)
    }
    return &Service{repo: repo, cfg: cfg}, nil
}
```

### Environment Variable

A single environment variable, `APP_ENV`, controls which YAML file is loaded. Defaults to `local` if unset.

| Value         | File loaded                          |
|---------------|--------------------------------------|
| `local`       | `{module}/config/local.yaml`         |
| `development` | `{module}/config/development.yaml`   |
| `staging`     | `{module}/config/staging.yaml`       |
| `production`  | `{module}/config/production.yaml`    |

### Project Root Resolution

The loader walks up from the current working directory looking for `go.mod`. This ensures `go test ./...` invoked from any subdirectory resolves config files correctly. In containerised deployments where `go.mod` is absent, the loader falls back to treating the working directory as the project root.

```
Load called from: backend/internal/identity/
Walk up: backend/internal/identity/ → backend/internal/ → backend/ (go.mod found ✓)
Path: backend/internal/identity/config/production.yaml
```

### Security Notes

- YAML config files **must not** contain passwords, API keys, certificates, or any value that would be harmful if leaked via version control
- Linting / secret scanning (e.g. `gitleaks`) should be added to CI to enforce this
- The `Load` function itself has no opinion on what values are "secrets" — this is a convention enforced by code review and tooling

### Migration Path

1. Add `gopkg.in/yaml.v3` to `backend/go.mod`
2. Implement `internal/platform/config/config.go` and tests
3. Introduce the first config file for `cmd/orbit` (server port, base path, log level) as a proof of concept
4. Migrate each feature to use `config.Load` as features are built out — no big-bang migration needed
5. Update `main.go` to read `APP_ENV` and pass it (or the loaded config) through dependency injection

### Timeline

Implement alongside the first feature that would otherwise require 5+ environment variables for non-secret config. The platform package itself is a day's work; the convention pays off immediately.

---

## Validation

### Success Metrics

- All non-secret config values for a feature are visible in a single YAML file diff — no hunting across env var documentation
- `go test ./...` passes with correct config resolution from all working directories
- Zero secrets committed to config YAML files (enforced by `gitleaks` in CI)
- Adding a new config value to a feature requires changes only within that feature's package

### Monitoring

- CI linting step validates no secrets in YAML files
- Startup logs emit the resolved config file path (not the values) so operators can confirm which file was loaded
- Test suite for `internal/platform/config` covers: happy path, missing file, malformed YAML, working-directory independence

---

## Related Decisions

- [ADR-0003](0003-repository-pattern-and-code-organization.md) — Feature-owned architecture; config structs follow the same feature-owned model as repository interfaces
- [ADR-0004](0004-dependency-injected-logging.md) — Dependency injection pattern; loaded config structs are injected, not accessed globally

---

## References

- [gopkg.in/yaml.v3](https://pkg.go.dev/gopkg.in/yaml.v3) — YAML parser used by the loader
- [The Twelve-Factor App — Config](https://12factor.net/config) — Guidance on separating config from code; secrets remain env-var-driven
- [Go Proverbs](https://go-proverbs.github.io/) — "Clear is better than clever"
- [ADR-0003](0003-repository-pattern-and-code-organization.md) — Repository pattern and feature autonomy

---

## Notes

**On secrets vs. config:**
The single most important invariant of this system is that YAML config files are for *structure*, not *secrets*. A good heuristic: if rotating the value requires a security incident response, it's a secret and belongs in an env var or secret manager. If changing it is a routine deployment concern (port number, log level, endpoint URL), it can live in YAML.

**On environment parity:**
The `local.yaml` file exists to support developer machines where values differ from even `development` (e.g., `localhost` vs. an internal hostname). It should be committed to version control like any other config file — it contains no secrets.

**On future extension:**
If the need arises for env-var override of specific YAML keys (e.g., for Kubernetes config injection), this can be added to `Load` without changing call sites. The function signature is stable.

**Last Updated:** 2026-06-10
