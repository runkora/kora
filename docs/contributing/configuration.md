# Configuration

Each kora config module (e.g. `cmd/identity`, `internal/platform/storage`) loads its non-secret configuration from YAML files in this order:

1.  `{module}/config/base.yaml` when the binary loads it with `LoadService`.
2.  `{module}/config/{APP_ENV}.yaml`. `APP_ENV` defaults to `production`, and binaries run on the host set `APP_ENV=local`.
3.  `/etc/orbit/config/{module}.yaml`, the file the chart renders for the install.

Decoding is strict. An unknown key or a value of the wrong type fails startup.

> **Secrets do not go in YAML files.** Passwords, API keys, and certificates belong in environment variables or a secret manager. If rotating a value would trigger a security incident, it's a secret.

---

## Two loading patterns

There are two places config is loaded, each with a slightly different pattern.

### Binary entry point (`cmd/{service}`)

Each service binary under `backend/cmd/` (for example the main API binary `cmd/identity`) has a dedicated `config/` sub-package with a `config.go` that exposes a package-level variable initialised at startup. Binaries use the **per-service loader** `config.MustLoadService[T]` (or `config.LoadService[T]` for the error-returning variant), which reads `backend/cmd/{service}/config/base.yaml`, an optional `{env}.yaml` overlay and the rendered file:

```go
// cmd/identity/config/config.go
package identityConfig

import "github.com/runkora/kora/backend/internal/platform/config"

// Config holds the identity service's own configuration values.
type Config struct {
    App struct {
        Version string `yaml:"version"`
    } `yaml:"app"`

    Server struct {
        Port                int    `yaml:"port"`
        BasePath            string `yaml:"basePath"`
        ShutdownTimeoutSecs int    `yaml:"shutdownTimeoutSecs"`
    } `yaml:"server"`

    Observability struct {
        ServiceName string `yaml:"serviceName"`
    } `yaml:"observability"`
}

// Current is initialised once at program start, reading
// cmd/identity/config/base.yaml (+ optional {env}.yaml overlay).
var Current = config.MustLoadService[Config]("cmd/identity", config.Environment())
```

`MustLoadService` panics on startup if the file is missing or unparseable: this surfaces misconfiguration immediately rather than silently at first use. `LoadService` returns the error instead of panicking.

> **`LoadService` vs `Load`.** The per-service loader `LoadService[T](serviceModule, env)` resolves config under `backend/cmd/{service}/config/` and is the primary pattern used by the service binaries. The generic loader `Load[T](module, env)` resolves config under an arbitrary module path (e.g. `internal/{feature}`) and is used by feature packages, as shown below. Both have `Must*` variants that panic on error.

### Feature packages (`internal/{feature}`)

Feature packages do **not** create a `config/config.go` sub-package. Instead they:

1. Put their YAML config files directly in `{feature}/config/`
2. Define their config struct inside `service.go` (or a dedicated `config.go` at the feature root)
3. Call `config.Load[T]()` explicitly inside a `LoadConfig()` function

```go
// internal/identity/service.go  (abbreviated)

// Config is the identity service configuration.
type Config struct {
    OIDC struct {
        IssuerURL string   `yaml:"issuerUrl"`
        Scopes    []string `yaml:"scopes"`
    } `yaml:"oidc"`

    // Secrets are never in YAML — tagged "-" and read from env vars directly.
    TokenSigningKey     string `yaml:"-"`
    TokenSigningKeyFile string `yaml:"-"`
    OIDCClientID        string `yaml:"-"`
    OIDCClientSecret    string `yaml:"-"`
}

// LoadConfig loads the YAML config for the current environment and overlays
// secrets from environment variables.
func LoadConfig() (*Config, error) {
    cfg, err := config.Load[Config]("internal/identity", config.Environment())
    if err != nil {
        return nil, err
    }

    // Overlay secrets from environment — never from YAML.
    cfg.TokenSigningKeyFile = os.Getenv("ORBIT_TOKEN_SIGNING_KEY_FILE")
    cfg.TokenSigningKey     = os.Getenv("ORBIT_TOKEN_SIGNING_KEY")
    cfg.OIDCClientID        = os.Getenv("ORBIT_OIDC_CLIENT_ID")
    cfg.OIDCClientSecret    = os.Getenv("ORBIT_OIDC_CLIENT_SECRET")

    // Runtime override: allows pointing at a real provider without editing YAML.
    if url := os.Getenv("ORBIT_OIDC_ISSUER_URL"); url != "" {
        cfg.OIDC.IssuerURL = url
    }

    return cfg, nil
}
```

---

## Rendered Config Files

The `orbit` chart renders each workload's per-install config into a ConfigMap and mounts it at `/etc/orbit/config`. Each config module the workload loads is one file, `{module}.yaml`, and a change to it rolls the workload through its `checksum/config` annotation. Env vars carry only secrets and third-party conventions such as `SPIFFE_ENDPOINT_SOCKET`.

*   **Curated values:** Chart keys such as `global.logFormat`, `config.db` and `identity.config.accessTokenTtlMinutes` render into the files.
*   **Overrides:** `<component>.configOverrides` merges last, keyed by config module. Its keys follow the Go config structs.

```yaml
identity:
  configOverrides:
    internal/platform/observability:
      logFormat: console
    internal/identity:
      token:
        refreshTokenTtlDays: 1
```

Read a workload's effective config from its ConfigMap, e.g. `kubectl -n orbit-system get configmap orbit-identity-config -o yaml`.

The session-manager and the identity-proxy sidecar have no rendered file. orbit-agents and the operator set their env vars in Go.

*Reference: See [rendered-service-config-files](../adr/2026-10-06-rendered-service-config-files.md).*

---

## Adding a value to an existing config

1. Add the field to the config struct:

```go
type appConfig struct {
    Server struct {
        Port                int    `yaml:"port"`
        BasePath            string `yaml:"basePath"`
        ShutdownTimeoutSecs int    `yaml:"shutdownTimeoutSecs"`
        ReadTimeoutSecs     int    `yaml:"readTimeoutSecs"`  // ← new
    } `yaml:"server"`
}
```

2. Add the value to each environment YAML file:

```yaml
# cmd/identity/config/base.yaml
server:
  port: 8080
  basePath: /api/v1
  shutdownTimeoutSecs: 5
  readTimeoutSecs: 30  # ← new
```

The loader rejects unknown keys at startup.

---

## Adding config for a new feature

1. Create the YAML files (no `config.go` sub-package needed):

```
backend/internal/{feature}/config/
├── local.yaml
└── production.yaml
```

2. Define the config struct and `LoadConfig()` in `service.go` (or a top-level `config.go` in the feature package):

```go
// internal/{feature}/service.go

type Config struct {
    Cache struct {
        TTLSeconds int `yaml:"ttlSeconds"`
        MaxSize    int `yaml:"maxSize"`
    } `yaml:"cache"`

    // Secrets always use yaml:"-" and are loaded from env vars.
    APIKey string `yaml:"-"`
}

func LoadConfig() (*Config, error) {
    cfg, err := config.Load[Config]("internal/{feature}", config.Environment())
    if err != nil {
        return nil, err
    }
    cfg.APIKey = os.Getenv("ORBIT_FEATURE_API_KEY")
    return cfg, nil
}
```

3. Call `LoadConfig()` from `main.go` when wiring the service:

```go
// cmd/identity/main.go
featureCfg, err := feature.LoadConfig()
if err != nil {
    logger.Error(ctx, "failed to load feature config", logging.Field{Key: "error", Value: err})
    os.Exit(1)
}
svc := feature.NewService(db, redis, logger, featureCfg)
```

---

## Reference

- `internal/platform/config/config.go`: the loader package (`Load`, `MustLoad`, `LoadService`, `MustLoadService`, `Environment`)
- `charts/orbit/templates/*/config.yaml`: the config files the chart renders for each workload
- `internal/identity/service.go`: canonical example of feature-level `LoadConfig()` with secret overlays
- `cmd/identity/config/config.go`: canonical example of binary-level `MustLoadService` package var
- [yaml-file-based-configuration](../adr/2026-02-18-yaml-file-based-configuration.md): decision record and rationale
- [Secrets reference](../reference/secrets.md): how secrets are separated from config
