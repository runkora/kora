# YAML File-Based Configuration with Typed Generic Loader

**Status:** Accepted
**Date:** 2026-02-18
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
- Live in `internal/platform/config/` as a shared platform utility, consistent with [repository-pattern-and-code-organization](2026-02-17-repository-pattern-and-code-organization.md)

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
* **Feature autonomy** — consistent with [repository-pattern-and-code-organization](2026-02-17-repository-pattern-and-code-organization.md), each feature owns its config shape and YAML files
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
- Configuration is done through a global singleton by default, which conflicts with Orbit's dependency injection philosophy ([dependency-injected-logging](2026-02-17-dependency-injected-logging.md))
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
- **Feature-owned config structs** — consistent with [repository-pattern-and-code-organization](2026-02-17-repository-pattern-and-code-organization.md) feature autonomy
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
- Contradicts the feature-autonomy principle in [repository-pattern-and-code-organization](2026-02-17-repository-pattern-and-code-organization.md) — features must now import a shared config package that knows about all features (circular dependency risk)
- The config struct becomes a "god struct" that must be updated every time a feature adds a config value
- Harder for feature teams to work independently

---

## Decision

**Chosen Option:** Option 3 — Typed Generic YAML Loader

### Rationale

Option 3 delivers type safety, feature autonomy, minimal dependencies, and an implementation small enough to be fully understood and audited in minutes — all of which are primary decision drivers for Orbit.

**Why not Viper (Option 2)?** Viper's global singleton conflicts with Orbit's dependency injection approach ([dependency-injected-logging](2026-02-17-dependency-injected-logging.md)). Its `mapstructure` reflection-based decoding gives up compile-time type safety. Its large feature set introduces capabilities (remote config, fsnotify) that we explicitly do not want yet and that expand the attack surface of a security-critical control plane.

**Why not environment variables only (Option 1)?** As Orbit grows toward production, the number of non-secret config values (ports, paths, endpoint URLs, timeout values, feature flags) will easily exceed 40+. Managing these as individual env vars across multiple deployment environments creates an operational burden disproportionate to the actual complexity of the values being managed.

**Why not a single app-wide struct (Option 4)?** It directly conflicts with [repository-pattern-and-code-organization](2026-02-17-repository-pattern-and-code-organization.md)'s feature autonomy principle and creates the same "god package" anti-pattern that [repository-pattern-and-code-organization](2026-02-17-repository-pattern-and-code-organization.md) explicitly rejected for repositories.

**The generic loader (Option 3) is the right fit because:**
- It is a thin utility, not a framework — features remain in control
- It mirrors the feature-owned repository pattern from [repository-pattern-and-code-organization](2026-02-17-repository-pattern-and-code-organization.md)
- It keeps secrets firmly out of version control by design (YAML is for non-secret config only)
- It is idiomatic Go: explicit paths, concrete types, errors returned not panicked

---

## Consequences

### Positive

- **Type safety at config load time** — mismatches between YAML and struct fields surface at startup, not in production
- **Feature teams are independent** — adding a new config value to `identity` requires no changes outside `internal/identity/`
- **Config is reviewable** — YAML files for each environment live in version control; changes are visible in PRs
- **Minimal dependency footprint** — `gopkg.in/yaml.v3` is the only new direct dependency
- **No global state** — config values are injected just like loggers ([dependency-injected-logging](2026-02-17-dependency-injected-logging.md) consistency)
- **Clear security boundary** — YAML files for structure, env vars for secrets; the two never mix

### Negative

- **No env var override layer** — a value in `production.yaml` cannot be overridden at runtime without changing the file or redeploying (mitigation: secrets already use env vars; structural values rarely need runtime override)
- **Multiple file reads at startup** — if 8 features each call `Load`, that is 8 file reads (negligible cost at startup)
- **Project root heuristic** — the `go.mod` walk is a convention; a project without a `go.mod` at root (unusual for a Go module) would need the fallback path

### Neutral

- `gopkg.in/yaml.v3` is added as a direct dependency — widely used, well-maintained, Apache 2.0 licensed
- Each feature that requires config adds a `config/` subdirectory and one YAML file per environment — small, predictable overhead

---

## Implementation Notes

### Public API (`internal/platform/config`)

The package exposes three functions:

```go
// Environment reads APP_ENV, defaulting to "local" when unset.
func Environment() string

// Load reads {projectRoot}/{module}/config/{environment}.yaml and
// unmarshals it into a new *T. Returns an error if the file is missing
// or cannot be parsed.
func Load[T any](module, environment string) (*T, error)

// MustLoad is like Load but panics on error. Use for package-level
// variable initialisation where misconfiguration is a fatal startup error.
func MustLoad[T any](module, environment string) *T
```

### Config File Convention

Each binary or feature owns its config struct and places environment-specific YAML files under a `config/` subdirectory co-located with its source:

```
backend/
├── internal/
│   └── {feature}/
│       ├── config/
│       │   ├── local.yaml
│       │   ├── development.yaml
│       │   └── production.yaml
│       ├── service.go
│       └── models.go
└── cmd/
    └── {binary}/
        ├── config/
        │   ├── config.go       ← typed config struct + package-level var
        │   ├── local.yaml
        │   └── production.yaml
        └── main.go
```

### Canonical Usage Pattern — Binary

Config for a binary is defined and loaded in a dedicated package co-located with the binary. A package-level variable is initialised at startup using `MustLoad`, mirroring the `regexp.MustCompile` convention:

```go
// cmd/{binary}/config/config.go
var Config = config.MustLoad[BinaryConfig]("cmd/{binary}", config.Environment())

type BinaryConfig struct {
    App struct {
        Version   string `yaml:"version"`
        LogFormat string `yaml:"logFormat"`
    } `yaml:"app"`

    Server struct {
        Port                int    `yaml:"port"`
        BasePath            string `yaml:"basePath"`
        ShutdownTimeoutSecs int    `yaml:"shutdownTimeoutSecs"`
    } `yaml:"server"`
}

// Helper methods on the top-level struct keep call sites readable.
func (c BinaryConfig) ShutdownTimeout() time.Duration {
    return time.Duration(c.Server.ShutdownTimeoutSecs) * time.Second
}
```

`main.go` imports the config package and reads values directly:

```go
import oc "github.com/clear-route/orbit/cmd/{binary}/config"

server := apiServer.Server(ctx, oc.Config.Server.Port, oc.Config.Server.BasePath, ...)
```

### Canonical Usage Pattern — Internal Feature

For internal features, load config in the service constructor using `Load` (not `MustLoad`) so the error surfaces through the normal dependency wiring path:

```go
// internal/{feature}/service.go
type featureConfig struct {
    Server struct {
        Port int `yaml:"port"`
    } `yaml:"server"`

    OIDC struct {
        Issuer   string `yaml:"issuer"`
        ClientID string `yaml:"clientId"`
        // Secrets (client secret, API keys) are NOT here — use env vars
    } `yaml:"oidc"`
}

func NewService(repo Repository) (*Service, error) {
    cfg, err := config.Load[featureConfig]("internal/{feature}", config.Environment())
    if err != nil {
        return nil, fmt.Errorf("{feature} config: %w", err)
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

The loader walks up from the current working directory looking for `go.mod`. This ensures `go test ./...` invoked from any subdirectory resolves config files correctly. Symlinks are resolved before walking (important on macOS where `t.TempDir()` returns paths through `/private/var`). In containerised deployments where `go.mod` is absent, the working directory is used as the fallback root.

```
Load called from: backend/internal/identity/
Walk up: backend/internal/identity/ → backend/internal/ → backend/ (go.mod found ✓)
Resolved path: backend/internal/identity/config/production.yaml
```

### Security Notes

- YAML config files **must not** contain passwords, API keys, certificates, or any value harmful if leaked via version control
- `gitleaks` secret scanning in CI will flag accidental secret commits
- The `Load` function has no opinion on what constitutes a secret — enforcement is by code review and CI tooling
- The clear rule: if rotating the value requires a security incident, it is a secret → use env var or secret manager

---

## Related Decisions

- [repository-pattern-and-code-organization](2026-02-17-repository-pattern-and-code-organization.md) — Feature-owned architecture; config structs follow the same feature-owned model as repository interfaces
- [dependency-injected-logging](2026-02-17-dependency-injected-logging.md) — Dependency injection pattern; loaded config structs are injected, not accessed globally

---

## References

- [gopkg.in/yaml.v3](https://pkg.go.dev/gopkg.in/yaml.v3) — YAML parser used by the loader
- [The Twelve-Factor App — Config](https://12factor.net/config) — Guidance on separating config from code; secrets remain env-var-driven
- [Go Proverbs](https://go-proverbs.github.io/) — "Clear is better than clever"
- [repository-pattern-and-code-organization](2026-02-17-repository-pattern-and-code-organization.md) — Repository pattern and feature autonomy

---

## Notes

**On secrets vs. config:**
The single most important invariant of this system is that YAML config files are for *structure*, not *secrets*. A good heuristic: if rotating the value requires a security incident response, it's a secret and belongs in an env var or secret manager. If changing it is a routine deployment concern (port number, log level, endpoint URL), it can live in YAML.

**On environment parity:**
The `local.yaml` file exists to support developer machines where values differ from even `development` (e.g., `localhost` vs. an internal hostname). It should be committed to version control like any other config file — it contains no secrets.

**On future extension:**
If the need arises for env-var override of specific YAML keys (e.g., for Kubernetes config injection), this can be added to `Load` without changing call sites. The function signature is stable.

**On config layering (future enhancement):**
As the number of environments grows, the current model requires duplicating all shared values across every environment-specific YAML file (e.g., `local.yaml` and `development.yaml` are currently identical except for `logFormat`). A future improvement is to introduce a `base.yaml` per module that holds all shared defaults, with environment-specific files containing only their overrides — merged at load time via a second `MergeInConfig`-style call. This would eliminate duplication across environment files and make diffs between environments trivially small. This is explicitly deferred until the config surface area grows large enough to justify the added loader complexity. See `docs/product/scratch/config-layering.md` for a short feature sketch.

**Last Updated:** 2026-06-10
